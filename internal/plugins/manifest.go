// Package plugins implements Soliton Rudder's extension system: out-of-process plugin backends
// speaking JSON-RPC over stdio, sandboxed UI panels, and a permission-checked host API.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// APIVersion is the plugin API generation this host implements.
const APIVersion = 1

type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Homepage    string   `json:"homepage"`
	APIVersion  int      `json:"apiVersion"`
	Permissions []string `json:"permissions"`

	Backend *Backend `json:"backend,omitempty"`
	UI      *UI      `json:"ui,omitempty"`

	Commands        []Command        `json:"commands"`
	AssessmentRules []AssessmentRule `json:"assessmentRules"`
}

type Backend struct {
	Command string   `json:"command"` // "./relative/path" inside the plugin, or a bare interpreter name (node, python3)
	Args    []string `json:"args"`
}

type UI struct {
	Root   string  `json:"root"` // directory served to the sandboxed iframe (default "ui")
	Panels []Panel `json:"panels"`
}

type Panel struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Entry     string `json:"entry"`     // html file relative to ui.root
	Placement string `json:"placement"` // cluster-nav | dock
	Icon      string `json:"icon"`
}

type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Panel string `json:"panel"` // optional: open this panel instead of calling the backend
}

type AssessmentRule struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Category    string `json:"category"` // resources | reliability | security | maintenance
	Severity    string `json:"severity"` // default severity (informational)
	Description string `json:"description"`
	Remediation string `json:"remediation"`
}

// Permission describes a capability a plugin can request.
type Permission struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Risk        string `json:"risk"` // low | medium | high
}

var permissionCatalog = []Permission{
	{"clusters.read", "List clusters", "See the names of your registered clusters.", "low"},
	{"kube.read", "Read cluster resources", "Read any resource the cluster credentials allow, except Secrets.", "medium"},
	{"secrets.read", "Read Secrets", "Read Secret values in your clusters.", "high"},
	{"kube.write", "Modify cluster resources", "Delete, scale, restart and apply manifests in your clusters.", "high"},
	{"network", "Network access", "Make HTTP requests to the internet or your network on the plugin's behalf.", "medium"},
	{"ui.notify", "Show notifications", "Display toast notifications.", "low"},
}

var permSet = func() map[string]Permission {
	m := map[string]Permission{}
	for _, p := range permissionCatalog {
		m[p.ID] = p
	}
	return m
}()

// PermissionInfo returns catalog entries for the given ids (unknown ids are skipped).
func PermissionInfo(ids []string) []Permission {
	out := []Permission{}
	for _, id := range ids {
		if p, ok := permSet[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,62}$`)
	slugRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)
	catSet  = map[string]bool{"resources": true, "reliability": true, "security": true, "maintenance": true}
	placeOK = map[string]bool{"cluster-nav": true, "dock": true}
)

// ReadManifest loads and validates <dir>/plugin.json.
func ReadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("plugin.json not found: %w", err)
	}
	if len(b) > 256<<10 {
		return nil, errors.New("plugin.json is unreasonably large")
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("plugin.json is not valid JSON: %w", err)
	}
	if err := m.Validate(dir); err != nil {
		return nil, err
	}
	return &m, nil
}

// safeRel reports whether p stays inside its base directory (no absolute paths, no ..).
func safeRel(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\x00") {
		return false
	}
	c := filepath.Clean(p)
	return c != ".." && !strings.HasPrefix(c, ".."+string(filepath.Separator))
}

func (m *Manifest) Validate(dir string) error {
	switch {
	case !idRe.MatchString(m.ID):
		return fmt.Errorf("invalid id %q (use lowercase letters, digits, '.', '-', '_', 3–63 chars)", m.ID)
	case strings.TrimSpace(m.Name) == "" || len(m.Name) > 80:
		return errors.New("name is required (max 80 characters)")
	case strings.TrimSpace(m.Version) == "" || len(m.Version) > 40:
		return errors.New("version is required")
	case m.APIVersion != APIVersion:
		return fmt.Errorf("plugin targets API version %d but this app implements %d", m.APIVersion, APIVersion)
	}
	seen := map[string]bool{}
	for _, p := range m.Permissions {
		if _, ok := permSet[p]; !ok {
			return fmt.Errorf("unknown permission %q", p)
		}
		if seen[p] {
			return fmt.Errorf("duplicate permission %q", p)
		}
		seen[p] = true
	}
	if m.Backend != nil {
		c := m.Backend.Command
		switch {
		case c == "":
			return errors.New("backend.command is required when backend is set")
		case strings.HasPrefix(c, "./"):
			if !safeRel(c) {
				return errors.New("backend.command escapes the plugin directory")
			}
			if dir != "" {
				st, err := os.Stat(filepath.Join(dir, c))
				if err != nil || st.IsDir() {
					return fmt.Errorf("backend.command %q not found", c)
				}
			}
		case strings.ContainsAny(c, `/\`):
			return errors.New(`backend.command must be "./relative/path" or a bare program name such as "node"`)
		}
		if len(m.Backend.Args) > 32 {
			return errors.New("too many backend.args")
		}
	}
	if m.UI != nil {
		if m.UI.Root == "" {
			m.UI.Root = "ui"
		}
		if !safeRel(m.UI.Root) {
			return errors.New("ui.root escapes the plugin directory")
		}
		ids := map[string]bool{}
		for i := range m.UI.Panels {
			p := &m.UI.Panels[i]
			if !slugRe.MatchString(p.ID) || ids[p.ID] {
				return fmt.Errorf("panel id %q is invalid or duplicated", p.ID)
			}
			ids[p.ID] = true
			if p.Title == "" {
				return fmt.Errorf("panel %q needs a title", p.ID)
			}
			if p.Placement == "" {
				p.Placement = "cluster-nav"
			}
			if !placeOK[p.Placement] {
				return fmt.Errorf("panel %q: unknown placement %q", p.ID, p.Placement)
			}
			if !safeRel(p.Entry) {
				return fmt.Errorf("panel %q: invalid entry", p.ID)
			}
		}
	}
	cmdIDs := map[string]bool{}
	for _, c := range m.Commands {
		if !slugRe.MatchString(c.ID) || cmdIDs[c.ID] || c.Title == "" {
			return fmt.Errorf("command %q is invalid or duplicated", c.ID)
		}
		cmdIDs[c.ID] = true
		if c.Panel == "" && m.Backend == nil {
			return fmt.Errorf("command %q needs a backend or a panel", c.ID)
		}
	}
	ruleIDs := map[string]bool{}
	for _, r := range m.AssessmentRules {
		if !slugRe.MatchString(r.ID) || ruleIDs[r.ID] || r.Title == "" {
			return fmt.Errorf("assessment rule %q is invalid or duplicated", r.ID)
		}
		ruleIDs[r.ID] = true
		if !catSet[r.Category] {
			return fmt.Errorf("assessment rule %q: category must be one of resources, reliability, security, maintenance", r.ID)
		}
		if m.Backend == nil {
			return fmt.Errorf("assessment rule %q requires a backend", r.ID)
		}
	}
	return nil
}

// HasPermission reports whether the manifest requests a permission.
func (m *Manifest) Requests(perm string) bool {
	for _, p := range m.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}
