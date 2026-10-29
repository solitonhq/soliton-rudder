// Package cluster manages the user's registered Kubernetes clusters.
package cluster

type SourceKind string

const (
	// SourceFile references a kubeconfig on disk (kept in sync with the file).
	SourceFile SourceKind = "file"
	// SourceInline stores a self-contained kubeconfig in the OS keychain.
	SourceInline SourceKind = "inline"
)

// Cluster is a registered cluster. Credentials never live in this struct.
type Cluster struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Color          string     `json:"color"`
	Icon           string     `json:"icon"` // optional short label/emoji; UI falls back to initials
	Pinned         bool       `json:"pinned"`
	Position       int        `json:"position"`
	Source         SourceKind `json:"source"`
	KubeconfigPath string     `json:"kubeconfigPath"`
	Context        string     `json:"context"`
	CreatedAt      int64      `json:"createdAt"`
}

// Patch carries editable fields; nil means "leave unchanged".
type Patch struct {
	Name   *string `json:"name"`
	Color  *string `json:"color"`
	Icon   *string `json:"icon"`
	Pinned *bool   `json:"pinned"`
}

// ContextInfo describes a context found in a kubeconfig (import preview).
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
	Exists    bool   `json:"exists"` // already registered
}

// Health is the result of a connectivity probe.
type Health struct {
	Status    string `json:"status"` // connected | unreachable | unauthorized | error
	Version   string `json:"version"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error"`
}

var palette = []string{"#5b8def", "#3fb97f", "#e0a63a", "#e5584f", "#a77bf3", "#2fc4c4", "#ef7bb0", "#8b95a5"}
