package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"soliton-rudder/internal/secrets"
)

// Service combines the record store with credential storage.
type Service struct {
	store   *Store
	secrets secrets.Store
}

func NewService(s *Store, sec secrets.Store) *Service { return &Service{store: s, secrets: sec} }

func (s *Service) List() ([]Cluster, error) { return s.store.List() }

func (s *Service) markExisting(infos []ContextInfo, path string) []ContextInfo {
	existing, _ := s.store.List()
	for i := range infos {
		for _, c := range existing {
			if path != "" && c.Source == SourceFile && c.KubeconfigPath == path && c.Context == infos[i].Name {
				infos[i].Exists = true
			}
		}
	}
	return infos
}

func (s *Service) PreviewFile(path string) ([]ContextInfo, error) {
	p, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	infos, err := ParseContextsFile(p)
	if err != nil {
		return nil, err
	}
	return s.markExisting(infos, p), nil
}

func (s *Service) PreviewText(raw string) ([]ContextInfo, error) { return ParseContextsText(raw) }

// ImportFile registers the selected contexts of a kubeconfig file by reference.
func (s *Service) ImportFile(path string, contexts []string) ([]Cluster, error) {
	p, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	infos, err := s.PreviewFile(p)
	if err != nil {
		return nil, err
	}
	return s.importSelected(infos, contexts, func(ctxName string) (Cluster, error) {
		return Cluster{Source: SourceFile, KubeconfigPath: p, Context: ctxName}, nil
	})
}

// ImportText registers pasted kubeconfig contexts, keeping credentials in the keychain.
func (s *Service) ImportText(raw string, contexts []string) ([]Cluster, error) {
	infos, err := ParseContextsText(raw)
	if err != nil {
		return nil, err
	}
	return s.importSelected(infos, contexts, func(ctxName string) (Cluster, error) {
		flat, err := ExtractInline(raw, ctxName)
		if err != nil {
			return Cluster{}, err
		}
		c := Cluster{ID: uuid.NewString(), Source: SourceInline, Context: ctxName}
		if err := s.secrets.Set(secretKey(c.ID), flat); err != nil {
			return Cluster{}, fmt.Errorf("keychain: %w", err)
		}
		return c, nil
	})
}

func (s *Service) importSelected(infos []ContextInfo, selected []string, build func(string) (Cluster, error)) ([]Cluster, error) {
	if len(selected) == 0 {
		return nil, errors.New("no contexts selected")
	}
	byName := map[string]ContextInfo{}
	for _, i := range infos {
		byName[i.Name] = i
	}
	existing, _ := s.store.List()
	added := []Cluster{}
	for _, name := range selected {
		info, ok := byName[name]
		if !ok || info.Exists {
			continue
		}
		c, err := build(name)
		if err != nil {
			return added, err
		}
		if c.ID == "" {
			c.ID = uuid.NewString()
		}
		c.Name = name
		c.Color = palette[(len(existing)+len(added))%len(palette)]
		c, err = s.store.Insert(c)
		if err != nil {
			return added, err
		}
		added = append(added, c)
	}
	return added, nil
}

func (s *Service) Update(id string, p Patch) (Cluster, error) {
	if p.Name != nil {
		n := strings.TrimSpace(*p.Name)
		if n == "" {
			return Cluster{}, errors.New("name cannot be empty")
		}
		p.Name = &n
	}
	return s.store.Update(id, p)
}

func (s *Service) Delete(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	return s.secrets.Delete(secretKey(id))
}

func (s *Service) Reorder(ids []string) error { return s.store.Reorder(ids) }

// RestConfig builds a client config for a registered cluster.
func (s *Service) RestConfig(id string) (*rest.Config, error) {
	c, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	switch c.Source {
	case SourceInline:
		raw, err := s.secrets.Get(secretKey(id))
		if err != nil {
			return nil, fmt.Errorf("credentials unavailable: %w", err)
		}
		return clientcmd.RESTConfigFromKubeConfig([]byte(raw))
	default:
		rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: c.KubeconfigPath}
		return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			rules, &clientcmd.ConfigOverrides{CurrentContext: c.Context}).ClientConfig()
	}
}

// DefaultNamespace returns the namespace set on the cluster's kubeconfig context ("" if none): for users who can only
// read their own namespace, that is where to look.
func (s *Service) DefaultNamespace(id string) string {
	c, err := s.store.Get(id)
	if err != nil {
		return ""
	}
	var cfg clientcmd.ClientConfig
	ctxName := c.Context
	switch c.Source {
	case SourceInline:
		raw, err := s.secrets.Get(secretKey(id))
		if err != nil {
			return ""
		}
		cfg, err = clientcmd.NewClientConfigFromBytes([]byte(raw))
		if err != nil {
			return ""
		}
	default:
		cfg = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.KubeconfigPath}, &clientcmd.ConfigOverrides{CurrentContext: ctxName})
	}
	raw, err := cfg.RawConfig()
	if err != nil {
		return ""
	}
	if ctxName == "" {
		ctxName = raw.CurrentContext
	}
	if kc := raw.Contexts[ctxName]; kc != nil {
		return kc.Namespace
	}
	return ""
}

func (s *Service) Check(ctx context.Context, id string) Health {
	cfg, err := s.RestConfig(id)
	if err != nil {
		return Health{Status: "error", Error: err.Error()}
	}
	return Probe(ctx, cfg)
}

func secretKey(id string) string { return "kubeconfig:" + id }

// TempKubeconfig writes a self-contained, single-context kubeconfig to a private
// temp file (for shells that should talk to this cluster). Call cleanup when done.
func (s *Service) TempKubeconfig(id string) (string, func(), error) {
	c, err := s.store.Get(id)
	if err != nil {
		return "", nil, err
	}
	var raw string
	switch c.Source {
	case SourceInline:
		raw, err = s.secrets.Get(secretKey(id))
		if err != nil {
			return "", nil, fmt.Errorf("credentials unavailable: %w", err)
		}
	default:
		b, rerr := os.ReadFile(c.KubeconfigPath)
		if rerr != nil {
			return "", nil, rerr
		}
		raw, err = ExtractInline(string(b), c.Context)
		if err != nil {
			return "", nil, err
		}
	}
	f, err := os.CreateTemp("", "soliton-kubeconfig-*")
	if err != nil {
		return "", nil, err
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	if _, err := f.WriteString(raw); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	name := f.Name()
	return name, func() { os.Remove(name) }, nil
}

// Get returns one registered cluster.
func (s *Service) Get(id string) (Cluster, error) { return s.store.Get(id) }
