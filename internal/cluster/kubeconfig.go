package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ExpandPath resolves a leading ~ and returns an absolute path.
func ExpandPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}

// DefaultKubeconfig returns ~/.kube/config.
func DefaultKubeconfig() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kube", "config")
}

func contextsOf(cfg *clientcmdapi.Config) []ContextInfo {
	out := make([]ContextInfo, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		info := ContextInfo{Name: name, Cluster: c.Cluster, User: c.AuthInfo, Namespace: c.Namespace, Current: name == cfg.CurrentContext}
		if cl := cfg.Clusters[c.Cluster]; cl != nil {
			info.Server = cl.Server
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func ParseContextsFile(path string) ([]ContextInfo, error) {
	p, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	cfg, err := clientcmd.LoadFromFile(p)
	if err != nil {
		return nil, fmt.Errorf("cannot read kubeconfig: %w", err)
	}
	if len(cfg.Contexts) == 0 {
		return nil, fmt.Errorf("no contexts found in %s", p)
	}
	return contextsOf(cfg), nil
}

func ParseContextsText(raw string) ([]ContextInfo, error) {
	cfg, err := clientcmd.Load([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid kubeconfig: %w", err)
	}
	if len(cfg.Contexts) == 0 {
		return nil, fmt.Errorf("no contexts found")
	}
	return contextsOf(cfg), nil
}

// ExtractInline returns a self-contained kubeconfig (certs embedded, only the
// requested context) suitable for keychain storage.
func ExtractInline(raw, ctxName string) (string, error) {
	cfg, err := clientcmd.Load([]byte(raw))
	if err != nil {
		return "", err
	}
	if _, ok := cfg.Contexts[ctxName]; !ok {
		return "", fmt.Errorf("context %q not found", ctxName)
	}
	cfg.CurrentContext = ctxName
	if err := clientcmdapi.MinifyConfig(cfg); err != nil {
		return "", err
	}
	if err := clientcmdapi.FlattenConfig(cfg); err != nil {
		return "", err
	}
	out, err := clientcmd.Write(*cfg)
	return string(out), err
}
