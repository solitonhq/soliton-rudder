package kube

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const maxValueBytes = 1 << 20 // values larger than 1 MiB are truncated for display

// DataEntry is one key of a ConfigMap or Secret, prepared for display.
type DataEntry struct {
	Key       string       `json:"key"`
	Size      int          `json:"size"`
	Format    string       `json:"format"` // json | yaml | env | cert | text | binary
	Value     string       `json:"value"`  // empty for masked secrets and binary data
	Masked    bool         `json:"masked"`
	Truncated bool         `json:"truncated"`
	Info      string       `json:"info"` // e.g. certificate summary
	Cert      *CertSummary `json:"cert"` // set for certificates (public data, shown even when the value is masked)
}

type ConfigData struct {
	Kind    string      `json:"kind"`
	Type    string      `json:"type"` // Secret type
	Entries []DataEntry `json:"entries"`
}

// ConfigData returns decoded entries. Secret values stay masked until RevealSecretKey.
func (s *Session) ConfigData(ctx context.Context, key, ns, name string) (ConfigData, error) {
	res, err := s.resource(key)
	if err != nil {
		return ConfigData{}, err
	}
	if res.Name != "configmaps" && res.Name != "secrets" {
		return ConfigData{}, fmt.Errorf("%s has no key/value data", res.Kind)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ConfigData{}, err
	}
	return buildConfigData(res.Name == "secrets", obj, time.Now()), nil
}

func buildConfigData(secret bool, obj *unstructured.Unstructured, now time.Time) ConfigData {
	out := ConfigData{Kind: "ConfigMap", Entries: []DataEntry{}}
	if secret {
		out.Kind = "Secret"
		out.Type, _, _ = unstructured.NestedString(obj.Object, "type")
	}
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw := fmt.Sprint(data[k])
		var b []byte
		if secret {
			dec, err := base64.StdEncoding.DecodeString(raw)
			if err != nil {
				out.Entries = append(out.Entries, DataEntry{Key: k, Size: len(raw), Format: "binary", Info: "invalid base64"})
				continue
			}
			b = dec
		} else {
			b = []byte(raw)
		}
		out.Entries = append(out.Entries, entryFor(k, b, secret, now))
	}
	if !secret {
		bin, _, _ := unstructured.NestedMap(obj.Object, "binaryData")
		bkeys := make([]string, 0, len(bin))
		for k := range bin {
			bkeys = append(bkeys, k)
		}
		sort.Strings(bkeys)
		for _, k := range bkeys {
			dec, _ := base64.StdEncoding.DecodeString(fmt.Sprint(bin[k]))
			out.Entries = append(out.Entries, DataEntry{Key: k, Size: len(dec), Format: "binary", Info: "binary data"})
		}
	}
	return out
}

func entryFor(key string, b []byte, mask bool, now time.Time) DataEntry {
	e := DataEntry{Key: key, Size: len(b)}
	format, info := detectFormat(b, now)
	e.Format, e.Info = format, info
	if format == "cert" {
		if cs, err := SummarizeCert(b, now); err == nil {
			e.Cert = &cs
		}
	}
	if format == "binary" {
		return e
	}
	if mask {
		e.Masked = true
		return e
	}
	e.Value, e.Truncated = displayValue(b, format)
	return e
}

func displayValue(b []byte, format string) (string, bool) {
	trunc := false
	if len(b) > maxValueBytes {
		b, trunc = b[:maxValueBytes], true
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	if format == "json" && !trunc {
		var buf bytes.Buffer
		if json.Indent(&buf, b, "", "  ") == nil {
			return buf.String(), false
		}
	}
	return string(b), trunc
}

// RevealSecretKey returns one decoded secret value on explicit user request.
func (s *Session) RevealSecretKey(ctx context.Context, ns, name, k string) (DataEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(secretRes, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DataEntry{}, err
	}
	raw, ok, _ := unstructured.NestedString(obj.Object, "data", k)
	if !ok {
		return DataEntry{}, fmt.Errorf("key %q not found", k)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return DataEntry{}, fmt.Errorf("invalid base64")
	}
	e := entryFor(k, b, false, time.Now())
	if e.Format == "binary" {
		return e, fmt.Errorf("binary value cannot be displayed")
	}
	return e, nil
}

// detectFormat classifies a value for display and extracts a short non-sensitive summary.
func detectFormat(b []byte, now time.Time) (format, info string) {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return "binary", ""
	}
	t := bytes.TrimSpace(b)
	if bytes.HasPrefix(t, []byte("-----BEGIN CERTIFICATE-----")) {
		if c, err := parseFirstCert(t); err == nil {
			days := int(c.NotAfter.Sub(now).Hours() / 24)
			state := fmt.Sprintf("expires in %dd", days)
			if days < 0 {
				state = fmt.Sprintf("expired %dd ago", -days)
			}
			return "cert", fmt.Sprintf("%s · issuer %s · %s", c.Subject.CommonName, c.Issuer.CommonName, state)
		}
		return "cert", ""
	}
	if bytes.HasPrefix(t, []byte("-----BEGIN")) {
		return "text", "PEM"
	}
	if len(t) > 0 && (t[0] == '{' || t[0] == '[') && json.Valid(t) {
		return "json", ""
	}
	if looksEnv(t) {
		return "env", ""
	}
	if looksYAML(t) {
		return "yaml", ""
	}
	return "text", ""
}

func looksEnv(t []byte) bool {
	lines := strings.Split(string(t), "\n")
	n := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		l = strings.TrimPrefix(l, "export ")
		i := strings.IndexByte(l, '=')
		if i <= 0 {
			return false
		}
		for _, r := range l[:i] {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
				return false
			}
		}
		n++
	}
	return n > 0
}

func looksYAML(t []byte) bool {
	if !bytes.Contains(t, []byte(":")) || !bytes.Contains(t, []byte("\n")) {
		return false
	}
	var v any
	if yaml.Unmarshal(t, &v) != nil {
		return false
	}
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}
