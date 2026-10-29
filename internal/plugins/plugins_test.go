package plugins

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"soliton-rudder/internal/kube"
)

// TestMain doubles as the plugin backend: tests launch this binary with GO_PLUGIN_HELPER=1 to get a real
// out-of-process plugin speaking the wire protocol.
func TestMain(m *testing.M) {
	if os.Getenv("GO_PLUGIN_HELPER") == "1" {
		helperPlugin()
		return
	}
	os.Exit(m.Run())
}

func helperPlugin() {
	var wmu sync.Mutex
	enc := json.NewEncoder(os.Stdout)
	send := func(v any) { wmu.Lock(); defer wmu.Unlock(); enc.Encode(v) }
	var seq int
	pending := map[int]chan json.RawMessage{}
	var pmu sync.Mutex
	callHost := func(method string, params any) (json.RawMessage, error) {
		pmu.Lock()
		seq++
		id := seq
		ch := make(chan json.RawMessage, 1)
		pending[id] = ch
		pmu.Unlock()
		send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		res := <-ch
		var r struct {
			Error  *struct{ Message string } `json:"error"`
			Result json.RawMessage           `json:"result"`
		}
		json.Unmarshal(res, &r)
		if r.Error != nil {
			return nil, fmt.Errorf("%s", r.Error.Message)
		}
		return r.Result, nil
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		line := sc.Bytes()
		json.Unmarshal(line, &m)
		if m.Method == "" { // response to one of our calls
			var idOnly struct{ ID int }
			json.Unmarshal(line, &idOnly)
			pmu.Lock()
			ch := pending[idOnly.ID]
			pmu.Unlock()
			if ch != nil {
				ch <- append(json.RawMessage{}, line...)
			}
			continue
		}
		go func(id json.RawMessage, method string, params json.RawMessage) {
			reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}) }
			fail := func(msg string) {
				send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -1, "message": msg}})
			}
			switch method {
			case "initialize":
				reply(map[string]bool{"ok": true})
			case "shutdown":
				os.Exit(0)
			case "command/run":
				var p struct{ Command string }
				json.Unmarshal(params, &p)
				switch p.Command {
				case "store":
					if _, err := callHost("storage.set", map[string]any{"key": "k", "value": map[string]int{"n": 7}}); err != nil {
						fail(err.Error())
						return
					}
					callHost("ui.notify", map[string]any{"text": "stored"})
					reply(map[string]string{"message": "ok:stored"})
				case "readsecrets":
					_, err := callHost("kube.list", map[string]any{"cluster": "c1", "version": "v1", "resource": "secrets", "namespaced": true})
					if err != nil {
						reply(map[string]string{"message": "denied:" + err.Error()})
						return
					}
					reply(map[string]string{"message": "allowed"})
				case "crash":
					os.Exit(3)
				default:
					fail("unknown command")
				}
			case "assess/run":
				reply(map[string]any{"checked": 5, "findings": []map[string]string{{"severity": "medium", "kind": "Namespace", "name": "demo", "message": "needs label"}}})
			case "ui/call":
				var p struct {
					Method string
					Params json.RawMessage
				}
				json.Unmarshal(params, &p)
				reply(map[string]any{"echo": p.Method})
			default:
				fail("unknown method")
			}
		}(m.ID, m.Method, m.Params)
	}
}

type fakeHost struct {
	mu     sync.Mutex
	toasts []string
	events []string
}

func (h *fakeHost) Clusters() ([]ClusterInfo, error) {
	return []ClusterInfo{{ID: "c1", Name: "Demo"}}, nil
}
func (h *fakeHost) Session(id string) (*kube.Session, error) {
	return nil, fmt.Errorf("no session in tests")
}
func (h *fakeHost) Toast(plugin, kind, text string) {
	h.mu.Lock()
	h.toasts = append(h.toasts, plugin+":"+kind+":"+text)
	h.mu.Unlock()
}
func (h *fakeHost) Emit(plugin, name string, data any) {
	h.mu.Lock()
	h.events = append(h.events, plugin+":"+name)
	h.mu.Unlock()
}

// makePlugin writes a plugin folder whose backend re-executes the test binary as the helper.
func makePlugin(t *testing.T, id string, perms []string, extra func(m map[string]any)) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(filepath.Join(dir, "ui"), 0o755)
	exe, _ := os.Executable()
	script := fmt.Sprintf("#!/bin/sh\nGO_PLUGIN_HELPER=1 exec %q\n", exe)
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o755)
	os.WriteFile(filepath.Join(dir, "ui", "index.html"), []byte("<html>hi</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("outside ui root"), 0o644)
	man := map[string]any{
		"id": id, "name": "Test " + id, "version": "1.0.0", "apiVersion": 1, "permissions": perms,
		"backend":         map[string]any{"command": "./run.sh"},
		"ui":              map[string]any{"panels": []map[string]any{{"id": "main", "title": "Main", "entry": "index.html"}}},
		"commands":        []map[string]any{{"id": "store", "title": "Store"}, {"id": "readsecrets", "title": "Read"}, {"id": "crash", "title": "Crash"}},
		"assessmentRules": []map[string]any{{"id": "r1", "title": "Rule one", "category": "maintenance", "severity": "low"}},
	}
	if extra != nil {
		extra(man)
	}
	b, _ := json.Marshal(man)
	os.WriteFile(filepath.Join(dir, "plugin.json"), b, 0o644)
	return dir
}

func newMgr(t *testing.T) (*Manager, *fakeHost) {
	t.Helper()
	h := &fakeHost{}
	m, err := NewManager(filepath.Join(t.TempDir(), "plugins"), "test", h, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Shutdown)
	return m, h
}

func waitState(t *testing.T, m *Manager, id, want string) PluginInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last PluginInfo
	for time.Now().Before(deadline) {
		for _, p := range m.List() {
			if p.Manifest.ID == id {
				last = p
				if p.State == want {
					return p
				}
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("plugin %s never reached %q (last %q: %s)", id, want, last.State, last.Error)
	return last
}

func TestManifestValidation(t *testing.T) {
	good := func() *Manifest {
		return &Manifest{ID: "io.example.demo", Name: "Demo", Version: "1", APIVersion: 1}
	}
	if err := good().Validate(""); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(m *Manifest){
		"bad id":           func(m *Manifest) { m.ID = "Bad ID!" },
		"wrong api":        func(m *Manifest) { m.APIVersion = 2 },
		"unknown perm":     func(m *Manifest) { m.Permissions = []string{"root.everything"} },
		"duplicate perm":   func(m *Manifest) { m.Permissions = []string{"network", "network"} },
		"abs backend":      func(m *Manifest) { m.Backend = &Backend{Command: "/bin/sh"} },
		"escaping backend": func(m *Manifest) { m.Backend = &Backend{Command: "./../evil"} },
		"slash bare":       func(m *Manifest) { m.Backend = &Backend{Command: "bin/x"} },
		"ui root escape":   func(m *Manifest) { m.UI = &UI{Root: "../x"} },
		"entry escape":     func(m *Manifest) { m.UI = &UI{Panels: []Panel{{ID: "p", Title: "P", Entry: "../../x.html"}}} },
		"cmd needs host":   func(m *Manifest) { m.Commands = []Command{{ID: "c", Title: "C"}} },
		"rule no backend":  func(m *Manifest) { m.AssessmentRules = []AssessmentRule{{ID: "r", Title: "R", Category: "security"}} },
		"bad category": func(m *Manifest) {
			m.Backend = &Backend{Command: "node"}
			m.AssessmentRules = []AssessmentRule{{ID: "r", Title: "R", Category: "fun"}}
		},
	}
	for name, mut := range cases {
		m := good()
		mut(m)
		if err := m.Validate(""); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestPeerRoundTripAndMalformedInput(t *testing.T) {
	ar, aw := pipe()
	br, bw := pipe()
	a := NewPeer(aw, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return nil, errorf(-1, "a?")
	})
	b := NewPeer(bw, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "sum" {
			var p []int
			json.Unmarshal(params, &p)
			return p[0] + p[1], nil
		}
		return nil, errorf(-32601, "nope")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Serve(ctx, br)
	go b.Serve(ctx, ar)
	aw.Write([]byte("this is not json\n\n")) // garbage must not break the stream
	var out int
	if err := a.Call(ctx, "sum", []int{2, 3}, &out); err != nil || out != 5 {
		t.Fatalf("%v %d", err, out)
	}
	if err := a.Call(ctx, "missing", nil, nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected remote error: %v", err)
	}
}

func TestInstallInspectGrantsAndLifecycle(t *testing.T) {
	m, h := newMgr(t)
	src := makePlugin(t, "io.test.alpha", []string{"clusters.read", "ui.notify", "kube.read", "secrets.read"}, nil)

	ins, err := m.Inspect(src)
	if err != nil || ins.Update || !ins.HasBackend || len(ins.Permissions) != 4 || len(ins.Warnings) < 2 {
		t.Fatalf("%v %+v", err, ins)
	}
	if _, err := m.Install(src, []string{"network"}); err == nil {
		t.Fatal("granting a permission the plugin did not request must fail")
	}
	if _, err := m.Install(src, []string{"ui.notify"}); err != nil {
		t.Fatal(err)
	}
	p := waitState(t, m, "io.test.alpha", StateRunning)
	if !p.Enabled || len(p.Granted) != 1 {
		t.Fatalf("%+v", p)
	}

	// command that uses a granted API (storage is always allowed; ui.notify granted)
	msg, err := m.RunCommand(context.Background(), "io.test.alpha", "store", nil)
	if err != nil || msg != "ok:stored" {
		t.Fatalf("%v %q", err, msg)
	}
	if v, _ := m.UICall(context.Background(), "io.test.alpha", "storage.get", json.RawMessage(`{"key":"k"}`)); string(v.(json.RawMessage)) != `{"n":7}` {
		t.Fatalf("storage: %v", v)
	}
	h.mu.Lock()
	if len(h.toasts) != 1 || h.toasts[0] != "io.test.alpha:success:stored" {
		t.Fatalf("toasts: %v", h.toasts)
	}
	h.mu.Unlock()

	// a permission that was requested but not granted is refused, even from the backend
	msg, _ = m.RunCommand(context.Background(), "io.test.alpha", "readsecrets", nil)
	if !strings.HasPrefix(msg, "denied:") || !strings.Contains(msg, "kube.read") {
		t.Fatalf("expected permission denial, got %q", msg)
	}
	// grant kube.read only: secrets still protected by the separate secrets.read permission
	if err := m.SetGranted("io.test.alpha", []string{"kube.read"}); err != nil {
		t.Fatal(err)
	}
	msg, _ = m.RunCommand(context.Background(), "io.test.alpha", "readsecrets", nil)
	if !strings.Contains(msg, "secrets.read") {
		t.Fatalf("secrets must need their own grant: %q", msg)
	}
	if err := m.SetGranted("io.test.alpha", []string{"network"}); err == nil {
		t.Fatal("SetGranted must reject unrequested permissions")
	}

	// UI → backend passthrough and unknown host methods
	out, err := m.UICall(context.Background(), "io.test.alpha", "plugin.call", json.RawMessage(`{"method":"hello","params":{}}`))
	if err != nil || !strings.Contains(string(out.(json.RawMessage)), `"echo":"hello"`) {
		t.Fatalf("%v %v", err, out)
	}
	if _, err := m.UICall(context.Background(), "io.test.alpha", "nope.method", nil); err == nil {
		t.Fatal("unknown method must fail")
	}

	// disable → backend stops, calls refused; enable → comes back
	if err := m.SetEnabled("io.test.alpha", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RunCommand(context.Background(), "io.test.alpha", "store", nil); err == nil {
		t.Fatal("disabled plugin must refuse commands")
	}
	if err := m.SetEnabled("io.test.alpha", true); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "io.test.alpha", StateRunning)

	// update keeps state; uninstall removes everything
	ins, _ = m.Inspect(src)
	if !ins.Update || ins.ExistingVersion != "1.0.0" {
		t.Fatalf("%+v", ins)
	}
	if err := m.Uninstall("io.test.alpha"); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("not uninstalled")
	}
	if _, err := os.Stat(filepath.Join(m.installedDir(), "io.test.alpha")); !os.IsNotExist(err) {
		t.Fatal("files remain")
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	h := &fakeHost{}
	m1, _ := NewManager(root, "t", h, nil)
	src := makePlugin(t, "io.test.persist", []string{"ui.notify"}, nil)
	if _, err := m1.Install(src, []string{"ui.notify"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m1, "io.test.persist", StateRunning)
	m1.SetEnabled("io.test.persist", false)
	m1.Shutdown()

	m2, err := NewManager(root, "t", h, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Shutdown()
	l := m2.List()
	if len(l) != 1 || l[0].Enabled || len(l[0].Granted) != 1 || l[0].Granted[0] != "ui.notify" {
		t.Fatalf("%+v", l)
	}
}

func TestCrashLoopIsContained(t *testing.T) {
	m, _ := newMgr(t)
	// a backend that exits immediately: supervisor must give up and report "crashed", not spin forever
	src := makePlugin(t, "io.test.crashy", nil, func(man map[string]any) {
		man["backend"] = map[string]any{"command": "./die.sh"}
	})
	os.WriteFile(filepath.Join(src, "die.sh"), []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755)
	if _, err := m.Install(src, nil); err != nil {
		t.Fatal(err)
	}
	p := waitState(t, m, "io.test.crashy", StateCrashed)
	if !strings.Contains(p.Error, "handshake") && !strings.Contains(p.Error, "closed") && !strings.Contains(p.Error, "crashed") {
		t.Fatalf("error: %q", p.Error)
	}
	if logs := m.Logs("io.test.crashy"); len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "boom") {
		t.Fatalf("stderr should be captured: %v", logs)
	}
}

func TestAssessmentRulesFromPlugins(t *testing.T) {
	m, _ := newMgr(t)
	src := makePlugin(t, "io.test.rules", nil, nil)
	if _, err := m.Install(src, nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "io.test.rules", StateRunning)
	out := m.AssessRules(context.Background(), "c1")
	if len(out) != 1 || out[0].Error != "" || out[0].Checked != 5 || len(out[0].Findings) != 1 || out[0].Findings[0].Name != "demo" || out[0].Rule.ID != "r1" {
		t.Fatalf("%+v", out)
	}
	m.SetEnabled("io.test.rules", false)
	if out := m.AssessRules(context.Background(), "c1"); len(out) != 0 {
		t.Fatal("disabled plugins contribute no rules")
	}
}

func TestAssetHandlerIsConfinedAndSandboxed(t *testing.T) {
	m, _ := newMgr(t)
	src := makePlugin(t, "io.test.ui", nil, nil)
	if _, err := m.Install(src, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(m.AssetHandler())
	defer srv.Close()
	get := func(p string) (*http.Response, string) {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		return r, string(b[:n])
	}
	r, body := get("/plugins/io.test.ui/index.html")
	if r.StatusCode != 200 || !strings.Contains(body, "hi") {
		t.Fatalf("%d %q", r.StatusCode, body)
	}
	if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'none'") {
		t.Fatalf("csp: %q", csp)
	}
	for _, bad := range []string{"/plugins/io.test.ui/../secret.txt", "/plugins/io.test.ui/%2e%2e/secret.txt", "/plugins/io.test.ui/secret.txt", "/plugins/io.test.ui/plugin.json", "/plugins/nope/index.html", "/plugins/io.test.ui/"} {
		if r, _ := get(bad); r.StatusCode == 200 {
			t.Errorf("%s must not be served", bad)
		}
	}
	if r, b := get("/plugins/sdk.js"); r.StatusCode != 200 || !strings.Contains(b, "window.soliton") {
		t.Fatalf("sdk: %d", r.StatusCode)
	}
	m.SetEnabled("io.test.ui", false)
	if r, _ := get("/plugins/io.test.ui/index.html"); r.StatusCode == 200 {
		t.Fatal("disabled plugin UI must not be served")
	}
}

func TestZipInstallRejectsTraversal(t *testing.T) {
	m, _ := newMgr(t)
	zp := filepath.Join(t.TempDir(), "evil.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../../escape.txt")
	w.Write([]byte("x"))
	zw.Close()
	f.Close()
	if _, err := m.Inspect(zp); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected traversal rejection: %v", err)
	}

	// a good zip with a single top-level folder works
	src := makePlugin(t, "io.test.zipped", nil, func(man map[string]any) {
		delete(man, "backend")
		delete(man, "commands")
		delete(man, "assessmentRules")
	})
	zp2 := filepath.Join(t.TempDir(), "ok.zip")
	f2, _ := os.Create(zp2)
	zw2 := zip.NewWriter(f2)
	filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		w, _ := zw2.Create("myplugin/" + filepath.ToSlash(rel))
		b, _ := os.ReadFile(p)
		w.Write(b)
		return nil
	})
	zw2.Close()
	f2.Close()
	if _, err := m.Install(zp2, nil); err != nil {
		t.Fatal(err)
	}
	p := waitState(t, m, "io.test.zipped", StateNoBack)
	if p.Manifest.UI == nil {
		t.Fatal("ui lost")
	}
}

func TestSymlinksAreNotCopied(t *testing.T) {
	m, _ := newMgr(t)
	src := makePlugin(t, "io.test.link", nil, func(man map[string]any) {
		delete(man, "backend")
		delete(man, "commands")
		delete(man, "assessmentRules")
	})
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	os.Symlink(outside, filepath.Join(src, "ui", "leak.txt"))
	if _, err := m.Install(src, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.installedDir(), "io.test.link", "ui", "leak.txt")); err == nil {
		t.Fatal("symlink must not be installed")
	}
}

func TestStorageLimits(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.Set("p", "a", json.RawMessage(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("p", "", json.RawMessage(`1`)); err == nil {
		t.Fatal("empty key")
	}
	if err := s.Set("p", "b", json.RawMessage(`{bad`)); err == nil {
		t.Fatal("invalid json")
	}
	if err := s.Set("p", "big", json.RawMessage(`"`+strings.Repeat("x", maxValueSize)+`"`)); err == nil {
		t.Fatal("oversized value")
	}
	for i := 0; i < maxKeys; i++ {
		_ = s.Set("q", fmt.Sprintf("k%d", i), json.RawMessage(`1`))
	}
	if err := s.Set("q", "overflow", json.RawMessage(`1`)); err == nil {
		t.Fatal("key limit")
	}
	if err := s.Set("p2", "a", json.RawMessage(`1`)); err != nil {
		t.Fatal("limits are per plugin")
	}
	keys, _ := s.Keys("p")
	if len(keys) != 1 || keys[0] != "a" {
		t.Fatalf("%v", keys)
	}
}

// pipe returns a connected reader/writer pair.
func pipe() (*os.File, *os.File) {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	return r, w
}
