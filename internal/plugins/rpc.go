package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// The wire format is JSON-RPC 2.0, one JSON object per line (NDJSON) in each direction.

const maxLine = 8 << 20

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handler serves requests from the other side. A nil result is sent as JSON null.
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// Peer is one end of a line-delimited JSON-RPC connection.
type Peer struct {
	w       io.Writer
	wmu     sync.Mutex
	handler Handler
	seq     atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan message
	closed  chan struct{}
	err     error
}

func NewPeer(w io.Writer, h Handler) *Peer {
	return &Peer{w: w, handler: h, pending: map[int64]chan message{}, closed: make(chan struct{})}
}

func (p *Peer) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.w.Write(append(b, '\n'))
	return err
}

// Serve reads messages until r ends. It blocks; run it in a goroutine.
func (p *Peer) Serve(ctx context.Context, r io.Reader) {
	br := bufio.NewReaderSize(r, 64*1024)
	var serveErr error
	defer func() {
		p.mu.Lock()
		p.err = serveErr
		for id, ch := range p.pending {
			close(ch)
			delete(p.pending, id)
		}
		p.mu.Unlock()
		close(p.closed)
	}()
	for {
		line, err := readLimited(br)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				serveErr = err
			}
			return
		}
		if len(line) == 0 {
			continue
		}
		var m message
		if json.Unmarshal(line, &m) != nil {
			continue // ignore garbage (e.g. a stray print) rather than killing the connection
		}
		switch {
		case m.Method != "":
			go p.dispatch(ctx, m)
		case len(m.ID) > 0:
			var id int64
			if json.Unmarshal(m.ID, &id) != nil {
				continue
			}
			p.mu.Lock()
			ch := p.pending[id]
			delete(p.pending, id)
			p.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

func readLimited(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(buf)+len(chunk) > maxLine {
			return nil, errors.New("message too large")
		}
		buf = append(buf, chunk...)
		if !isPrefix {
			return buf, nil
		}
	}
}

func (p *Peer) dispatch(ctx context.Context, m message) {
	notify := len(m.ID) == 0
	if p.handler == nil {
		if !notify {
			_ = p.send(message{ID: m.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
		}
		return
	}
	res, err := p.handler(ctx, m.Method, m.Params)
	if notify {
		return
	}
	if err != nil {
		code := -32000
		var re *rpcError
		if errors.As(err, &re) {
			code = re.Code
		}
		_ = p.send(message{ID: m.ID, Error: &rpcError{Code: code, Message: err.Error()}})
		return
	}
	b, merr := json.Marshal(res)
	if merr != nil {
		_ = p.send(message{ID: m.ID, Error: &rpcError{Code: -32603, Message: merr.Error()}})
		return
	}
	_ = p.send(message{ID: m.ID, Result: b})
}

// Call sends a request and waits for the response, decoding it into out (if non-nil).
func (p *Peer) Call(ctx context.Context, method string, params, out any) error {
	pb, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := p.seq.Add(1)
	ch := make(chan message, 1)
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return errors.New("plugin connection closed")
	default:
	}
	p.pending[id] = ch
	p.mu.Unlock()
	idb, _ := json.Marshal(id)
	if err := p.send(message{ID: idb, Method: method, Params: pb}); err != nil {
		p.drop(id)
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return errors.New("plugin connection closed")
		}
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		p.drop(id)
		return ctx.Err()
	case <-p.closed:
		return errors.New("plugin connection closed")
	}
}

func (p *Peer) drop(id int64) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

// Notify sends a request without id (no response expected).
func (p *Peer) Notify(method string, params any) error {
	pb, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.send(message{Method: method, Params: pb})
}

func (p *Peer) Closed() <-chan struct{} { return p.closed }

func errorf(code int, format string, a ...any) error {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, a...)}
}
