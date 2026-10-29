package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// DriftKindInfo describes a resource kind the comparison offers. Secrets are deliberately absent:
// their values must never be read for a cross-cluster comparison or copied between clusters.
type DriftKindInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func DriftKinds() []DriftKindInfo {
	return []DriftKindInfo{
		{"apps/v1/deployments", "Deployments"},
		{"apps/v1/statefulsets", "StatefulSets"},
		{"apps/v1/daemonsets", "DaemonSets"},
		{"batch/v1/cronjobs", "CronJobs"},
		{"core/v1/configmaps", "ConfigMaps"},
		{"core/v1/services", "Services"},
		{"networking.k8s.io/v1/ingresses", "Ingresses"},
		{"autoscaling/v2/horizontalpodautoscalers", "HorizontalPodAutoscalers"},
		{"policy/v1/poddisruptionbudgets", "PodDisruptionBudgets"},
		{"networking.k8s.io/v1/networkpolicies", "NetworkPolicies"},
		{"core/v1/serviceaccounts", "ServiceAccounts"},
	}
}

func driftKindAllowed(key string) bool {
	for _, k := range DriftKinds() {
		if k.Key == key {
			return true
		}
	}
	return false
}

// FieldDiff is one differing field between the source and the target object.
type FieldDiff struct {
	Path        string `json:"path"`
	Op          string `json:"op"` // changed | added (only in source) | removed (only in target)
	Src         any    `json:"src"`
	Dst         any    `json:"dst"`
	Promotable  bool   `json:"promotable"`
	EnvSpecific bool   `json:"envSpecific"`
	Note        string `json:"note"`
}

type DriftItem struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"` // source namespace
	Status    string `json:"status"`    // same | differs | onlySource | onlyTarget
	Changes   int    `json:"changes"`
}

type DriftGroup struct {
	Key   string      `json:"key"`
	Label string      `json:"label"`
	Items []DriftItem `json:"items"`
	Error string      `json:"error"`
}

type DriftReport struct {
	Groups []DriftGroup `json:"groups"`
	Notes  []string     `json:"notes"`
}

type DriftObject struct {
	Name      string      `json:"name"`
	Status    string      `json:"status"`
	Fields    []FieldDiff `json:"fields"`
	SrcYAML   string      `json:"srcYaml"`
	DstYAML   string      `json:"dstYaml"`
	SrcExists bool        `json:"srcExists"`
	DstExists bool        `json:"dstExists"`
}

type Promotion struct {
	Manifest string `json:"manifest"`
	Create   bool   `json:"create"` // the object does not exist in the target: the whole object is created
	Fields   int    `json:"fields"`
}

// ---- normalisation ----

var driftIgnoredMeta = []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink", "deletionTimestamp", "deletionGracePeriodSeconds", "ownerReferences", "finalizers", "namespace"}

// normalizeForDrift strips what is server-assigned or cluster-local so only intentional configuration remains.
func normalizeForDrift(o *unstructured.Unstructured) map[string]any {
	c := o.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "status")
	for _, f := range driftIgnoredMeta {
		unstructured.RemoveNestedField(c.Object, "metadata", f)
	}
	if an, ok, _ := unstructured.NestedMap(c.Object, "metadata", "annotations"); ok {
		for k := range an {
			if k == "deployment.kubernetes.io/revision" || strings.HasPrefix(k, "kubectl.kubernetes.io/") {
				delete(an, k)
			}
		}
		if len(an) == 0 {
			unstructured.RemoveNestedField(c.Object, "metadata", "annotations")
		} else {
			_ = unstructured.SetNestedMap(c.Object, an, "metadata", "annotations")
		}
	}
	switch c.GetKind() {
	case "Service":
		for _, f := range []string{"clusterIP", "clusterIPs"} {
			unstructured.RemoveNestedField(c.Object, "spec", f)
		}
		if ports, ok, _ := unstructured.NestedSlice(c.Object, "spec", "ports"); ok {
			for _, p := range ports {
				if pm, ok := p.(map[string]any); ok {
					delete(pm, "nodePort") // allocated per cluster
				}
			}
			_ = unstructured.SetNestedSlice(c.Object, ports, "spec", "ports")
		}
	case "Deployment", "StatefulSet", "DaemonSet":
		unstructured.RemoveNestedField(c.Object, "spec", "template", "metadata", "creationTimestamp")
	case "CronJob":
		unstructured.RemoveNestedField(c.Object, "spec", "jobTemplate", "metadata", "creationTimestamp")
		unstructured.RemoveNestedField(c.Object, "spec", "jobTemplate", "spec", "template", "metadata", "creationTimestamp")
	case "ServiceAccount":
		unstructured.RemoveNestedField(c.Object, "secrets") // token secrets are generated
	}
	return c.Object
}

// ---- flattening ----

type seg struct {
	Key  string // map key; or list field name when ListKey != ""
	LKey string // merge key field of the list element ("name", "containerPort", …)
	LVal string // merge key value
}

type flatField struct {
	Path string
	Segs []seg
	Val  any
}

// mergeKeyFor returns the field that identifies elements of the named list, so they are compared by identity
// rather than by position. Unknown lists are compared as a whole.
func mergeKeyFor(kind string, path []seg, field string, elems []any) string {
	var key string
	switch field {
	case "containers", "initContainers", "ephemeralContainers", "env", "volumes", "imagePullSecrets":
		key = "name"
	case "volumeMounts":
		key = "mountPath"
	case "volumeDevices":
		key = "devicePath"
	case "ports":
		if len(elems) > 0 {
			if m, ok := elems[0].(map[string]any); ok {
				if _, has := m["containerPort"]; has {
					key = "containerPort"
				}
			}
		}
		if key == "" && kind == "Service" && len(path) == 1 && path[0].Key == "spec" {
			key = "port"
		}
	}
	if key == "" {
		return ""
	}
	seen := map[string]bool{}
	for _, e := range elems {
		m, ok := e.(map[string]any)
		if !ok {
			return ""
		}
		v, ok := m[key]
		if !ok || seen[fmt.Sprint(v)] {
			return ""
		}
		seen[fmt.Sprint(v)] = true
	}
	return key
}

func segPath(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		if s.LKey != "" {
			fmt.Fprintf(&b, "[%s=%s]", s.LKey, s.LVal)
			continue
		}
		if strings.ContainsAny(s.Key, `.[]"=`) {
			fmt.Fprintf(&b, "[%q]", s.Key)
		} else {
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.Key)
		}
	}
	return b.String()
}

func flatten(kind string, segs []seg, v any, out *[]flatField) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			*out = append(*out, flatField{segPath(segs), segs, x})
			return
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			flatten(kind, append(append([]seg(nil), segs...), seg{Key: k}), x[k], out)
		}
	case []any:
		field := ""
		if n := len(segs); n > 0 {
			field = segs[n-1].Key
		}
		if key := mergeKeyFor(kind, segs[:max(len(segs)-1, 0)], field, x); key != "" && len(x) > 0 {
			for _, e := range x {
				m := e.(map[string]any)
				s := append(append([]seg(nil), segs...), seg{LKey: key, LVal: fmt.Sprint(m[key])})
				rest := map[string]any{}
				for k, val := range m {
					if k != key {
						rest[k] = val
					}
				}
				if len(rest) == 0 {
					*out = append(*out, flatField{segPath(s), s, map[string]any{key: m[key]}})
					continue
				}
				flatten(kind, s, rest, out)
			}
			return
		}
		*out = append(*out, flatField{segPath(segs), segs, x})
	default:
		*out = append(*out, flatField{segPath(segs), segs, v})
	}
}

func flatMap(kind string, obj map[string]any) ([]flatField, map[string]flatField) {
	var list []flatField
	flatten(kind, nil, obj, &list)
	idx := make(map[string]flatField, len(list))
	for _, f := range list {
		idx[f.Path] = f
	}
	return list, idx
}

func sameValue(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

// envSpecific flags fields that normally differ on purpose between environments.
func envSpecific(path string) (bool, string) {
	switch {
	case path == "spec.replicas":
		return true, "replica counts are usually sized per environment"
	case strings.Contains(path, ".resources"):
		return true, "resource requests/limits are usually sized per environment"
	case strings.HasPrefix(path, "spec.rules") && strings.Contains(path, "host"), strings.HasPrefix(path, "spec.tls"):
		return true, "hostnames and certificates are usually environment-specific"
	case strings.Contains(path, "storageClassName"), strings.Contains(path, "loadBalancerIP"), strings.Contains(path, "externalIPs"),
		strings.Contains(path, "nodeSelector"), strings.Contains(path, "affinity"), strings.Contains(path, "tolerations"):
		return true, "placement and infrastructure settings are usually environment-specific"
	}
	return false, ""
}

// diffObjects lists the differences between two normalised objects.
func diffObjects(kind string, src, dst map[string]any) []FieldDiff {
	sl, si := flatMap(kind, src)
	dl, di := flatMap(kind, dst)
	var out []FieldDiff
	for _, f := range sl {
		d, ok := di[f.Path]
		switch {
		case !ok:
			out = append(out, FieldDiff{Path: f.Path, Op: "added", Src: f.Val, Promotable: true})
		case !sameValue(f.Val, d.Val):
			out = append(out, FieldDiff{Path: f.Path, Op: "changed", Src: f.Val, Dst: d.Val, Promotable: true})
		}
	}
	for _, f := range dl {
		if _, ok := si[f.Path]; !ok {
			out = append(out, FieldDiff{Path: f.Path, Op: "removed", Dst: f.Val, Note: "present only in the target: removing a field is not promoted"})
		}
	}
	for i := range out {
		if out[i].Promotable {
			out[i].EnvSpecific, out[i].Note = envSpecific(out[i].Path)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ---- promotion ----

func setPath(root map[string]any, segs []seg, val any) {
	cur := root
	for i, s := range segs {
		last := i == len(segs)-1
		if s.LKey != "" {
			continue // handled together with the list's field segment below
		}
		if i+1 < len(segs) && segs[i+1].LKey != "" {
			// s is a list field: find or create the element identified by the next segment
			ls, _ := cur[s.Key].([]any)
			ks := segs[i+1]
			var elem map[string]any
			for _, e := range ls {
				if m, ok := e.(map[string]any); ok && fmt.Sprint(m[ks.LKey]) == ks.LVal {
					elem = m
				}
			}
			if elem == nil {
				elem = map[string]any{ks.LKey: keyValue(ks.LKey, ks.LVal)}
				ls = append(ls, elem)
				cur[s.Key] = ls
			}
			if len(segs) == i+2 { // the whole element is the value
				if m, ok := val.(map[string]any); ok {
					for k, v := range m {
						elem[k] = v
					}
				}
				return
			}
			cur = elem
			continue
		}
		if last {
			cur[s.Key] = val
			return
		}
		next, _ := cur[s.Key].(map[string]any)
		if next == nil {
			next = map[string]any{}
			cur[s.Key] = next
		}
		cur = next
	}
}

// keyValue restores the original type of a numeric merge key (containerPort, port).
func keyValue(key, s string) any {
	if key == "containerPort" || key == "port" {
		var n int64
		if _, err := fmt.Sscan(s, &n); err == nil && fmt.Sprint(n) == s {
			return n
		}
	}
	return s
}

// promotionManifest builds the smallest manifest that carries the chosen fields to the target:
// server-side apply then only claims those fields and leaves everything else in the target alone.
func promotionManifest(src *unstructured.Unstructured, dstNS string, paths []string) (string, int, error) {
	kind := src.GetKind()
	_, idx := flatMap(kind, normalizeForDrift(src))
	root := map[string]any{"apiVersion": src.GetAPIVersion(), "kind": kind, "metadata": map[string]any{"name": src.GetName()}}
	if dstNS != "" {
		root["metadata"].(map[string]any)["namespace"] = dstNS
	}
	n := 0
	for _, p := range paths {
		f, ok := idx[p]
		if !ok {
			return "", 0, fmt.Errorf("field %q does not exist in the source object", p)
		}
		if strings.HasPrefix(p, "metadata.name") || p == "apiVersion" || p == "kind" {
			continue
		}
		setPath(root, f.Segs, f.Val)
		n++
	}
	if n == 0 {
		return "", 0, fmt.Errorf("no fields selected")
	}
	out, err := yaml.Marshal(root)
	return string(out), n, err
}

func fullManifest(src *unstructured.Unstructured, dstNS string) (string, error) {
	m := normalizeForDrift(src)
	meta, _ := m["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		m["metadata"] = meta
	}
	meta["name"] = src.GetName()
	if dstNS != "" {
		meta["namespace"] = dstNS
	}
	out, err := yaml.Marshal(m)
	return string(out), err
}

// ---- comparing two sets ----

func driftSkipped(o *unstructured.Unstructured) bool {
	if len(o.GetOwnerReferences()) > 0 { // generated by a controller
		return true
	}
	return o.GetName() == "kube-root-ca.crt" || (o.GetKind() == "Service" && o.GetName() == "kubernetes" && o.GetNamespace() == "default")
}

func compareSets(label, key string, src, dst []unstructured.Unstructured) DriftGroup {
	g := DriftGroup{Key: key, Label: label, Items: []DriftItem{}}
	dm := map[string]*unstructured.Unstructured{}
	for i := range dst {
		if !driftSkipped(&dst[i]) {
			dm[dst[i].GetName()] = &dst[i]
		}
	}
	seen := map[string]bool{}
	for i := range src {
		s := &src[i]
		if driftSkipped(s) {
			continue
		}
		seen[s.GetName()] = true
		it := DriftItem{Name: s.GetName(), Namespace: s.GetNamespace()}
		d := dm[s.GetName()]
		switch {
		case d == nil:
			it.Status = "onlySource"
		default:
			n := len(diffObjects(s.GetKind(), normalizeForDrift(s), normalizeForDrift(d)))
			it.Changes = n
			it.Status = "same"
			if n > 0 {
				it.Status = "differs"
			}
		}
		g.Items = append(g.Items, it)
	}
	for name, d := range dm {
		if !seen[name] {
			g.Items = append(g.Items, DriftItem{Name: name, Namespace: d.GetNamespace(), Status: "onlyTarget"})
		}
	}
	order := map[string]int{"differs": 0, "onlySource": 1, "onlyTarget": 2, "same": 3}
	sort.SliceStable(g.Items, func(i, j int) bool {
		a, b := g.Items[i], g.Items[j]
		if order[a.Status] != order[b.Status] {
			return order[a.Status] < order[b.Status]
		}
		return a.Name < b.Name
	})
	return g
}

// ---- session glue ----

func (s *Session) driftList(ctx context.Context, key, ns string) ([]unstructured.Unstructured, error) {
	if !driftKindAllowed(key) {
		return nil, fmt.Errorf("%s cannot be compared", key)
	}
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	l, err := s.client(res, ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range l.Items {
		if l.Items[i].GetKind() == "" {
			l.Items[i].SetKind(res.Kind)
		}
	}
	return l.Items, nil
}

func (s *Session) driftGet(ctx context.Context, key, ns, name string) (*unstructured.Unstructured, error) {
	if !driftKindAllowed(key) {
		return nil, fmt.Errorf("%s cannot be compared", key)
	}
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	o, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if o.GetKind() == "" {
		o.SetKind(res.Kind)
	}
	return o, nil
}

// CompareClusters compares the chosen kinds between a namespace of the source and one of the target cluster.
func CompareClusters(ctx context.Context, src, dst *Session, srcNS, dstNS string, keys []string) DriftReport {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	rep := DriftReport{Groups: []DriftGroup{}, Notes: []string{"Objects owned by a controller, and Secrets, are not compared. Status and server-assigned fields are ignored."}}
	labels := map[string]string{}
	for _, k := range DriftKinds() {
		labels[k.Key] = k.Label
	}
	for _, key := range keys {
		g := DriftGroup{Key: key, Label: labels[key], Items: []DriftItem{}}
		a, err := src.driftList(ctx, key, srcNS)
		if err != nil {
			g.Error = "source: " + shortErr(err)
			rep.Groups = append(rep.Groups, g)
			continue
		}
		b, err := dst.driftList(ctx, key, dstNS)
		if err != nil {
			g.Error = "target: " + shortErr(err)
			rep.Groups = append(rep.Groups, g)
			continue
		}
		rep.Groups = append(rep.Groups, compareSets(labels[key], key, a, b))
	}
	return rep
}

// CompareObject returns the field-level differences of one object.
func CompareObject(ctx context.Context, src, dst *Session, srcNS, dstNS, key, name string) (DriftObject, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	a, err := src.driftGet(ctx, key, srcNS, name)
	if err != nil {
		return DriftObject{}, fmt.Errorf("source: %w", err)
	}
	b, err := dst.driftGet(ctx, key, dstNS, name)
	if err != nil {
		return DriftObject{}, fmt.Errorf("target: %w", err)
	}
	out := DriftObject{Name: name, Fields: []FieldDiff{}, SrcExists: a != nil, DstExists: b != nil}
	render := func(o *unstructured.Unstructured) string {
		if o == nil {
			return ""
		}
		y, _ := yaml.Marshal(normalizeForDrift(o))
		return strings.TrimSpace(string(y))
	}
	out.SrcYAML, out.DstYAML = render(a), render(b)
	switch {
	case a == nil && b == nil:
		return out, fmt.Errorf("%s not found in either cluster", name)
	case b == nil:
		out.Status = "onlySource"
	case a == nil:
		out.Status = "onlyTarget"
	default:
		out.Fields = diffObjects(a.GetKind(), normalizeForDrift(a), normalizeForDrift(b))
		out.Status = "same"
		if len(out.Fields) > 0 {
			out.Status = "differs"
		}
	}
	return out, nil
}

// BuildPromotion prepares the manifest that carries the chosen fields (or the whole object, if missing) to the target.
// It only reads; applying goes through the normal dry-run + apply flow on the target cluster.
func BuildPromotion(ctx context.Context, src, dst *Session, srcNS, dstNS, key, name string, paths []string) (Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	a, err := src.driftGet(ctx, key, srcNS, name)
	if err != nil {
		return Promotion{}, fmt.Errorf("source: %w", err)
	}
	if a == nil {
		return Promotion{}, fmt.Errorf("%s does not exist in the source", name)
	}
	b, err := dst.driftGet(ctx, key, dstNS, name)
	if err != nil {
		return Promotion{}, fmt.Errorf("target: %w", err)
	}
	if b == nil {
		m, err := fullManifest(a, dstNS)
		return Promotion{Manifest: m, Create: true}, err
	}
	m, n, err := promotionManifest(a, dstNS, paths)
	return Promotion{Manifest: m, Fields: n}, err
}
