// Package kube provides live, lazily-watched Kubernetes resource streams
// and resource operations for connected clusters.
package kube

// Resource describes a listable API resource discovered on a cluster.
type Resource struct {
	Key        string   `json:"key"` // group/version/resource ("core" for the legacy group)
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Name       string   `json:"name"` // plural resource name, e.g. "pods"
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	ShortNames []string `json:"shortNames"`
	Verbs      []string `json:"verbs"`
}

// Column is a server-provided printer column.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Priority    int32  `json:"priority"` // >0 means "wide" only
	Description string `json:"description"`
}

// Row is one object in a table. Cells align with the subscription's Columns.
type Row struct {
	UID       string `json:"uid"`
	Namespace string `json:"ns"`
	Name      string `json:"name"`
	Created   string `json:"created"` // RFC3339; the UI renders a ticking age from this
	Cells     []any  `json:"cells"`
}

// RowsEvent is a batched change set for one subscription.
type RowsEvent struct {
	Sub     string   `json:"sub"`
	Reset   bool     `json:"reset"`           // replace all rows (initial list / relist)
	Scope   string   `json:"scope,omitempty"` // with Reset: replace only the rows of this namespace
	Columns []Column `json:"columns,omitempty"`
	Upsert  []Row    `json:"upsert"`
	Delete  []string `json:"delete"` // uids
}

// StatusEvent reports the stream's connection state.
type StatusEvent struct {
	Sub     string `json:"sub"`
	State   string `json:"state"` // connecting | live | error
	Message string `json:"message"`
}

// Emitter delivers events to the UI (Wails runtime in production).
type Emitter func(event string, data any)

const (
	EventRows   = "rows"
	EventStatus = "rowstatus"
)

// Event is a Kubernetes Event attached to an object (detail panel).
type Event struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
	Last    string `json:"last"`
	Source  string `json:"source"`
	Object  string `json:"object"` // Kind/name the event is about
}
