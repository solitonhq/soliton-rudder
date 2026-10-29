package kube

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// NetEndpoint is one end of a simulated connection.
type NetEndpoint struct {
	Kind      string `json:"kind"` // Pod | Service | External
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	IP        string `json:"ip"` // External only
}

type NetQuery struct {
	From     NetEndpoint `json:"from"`
	To       NetEndpoint `json:"to"`
	Port     string      `json:"port"` // number or container port name
	Protocol string      `json:"protocol"`
}

// NetStep is the verdict of one side (egress of the source, ingress of the destination).
type NetStep struct {
	Side     string   `json:"side"` // egress | ingress
	Subject  string   `json:"subject"`
	Isolated bool     `json:"isolated"`
	Allowed  bool     `json:"allowed"`
	Policies []string `json:"policies"` // policies that select the subject for this side
	Matched  string   `json:"matched"`  // policy that allowed the traffic
	Reason   string   `json:"reason"`
}

// NetPath is the evaluation towards one destination pod (a Service fans out to several).
type NetPath struct {
	Target  string    `json:"target"`
	Port    int       `json:"port"`
	Allowed bool      `json:"allowed"`
	Steps   []NetStep `json:"steps"`
}

type NetResult struct {
	Verdict string    `json:"verdict"` // allowed | denied | partial | unknown
	Summary string    `json:"summary"`
	Paths   []NetPath `json:"paths"`
	Notes   []string  `json:"notes"`
}

type SimTargets struct {
	Namespaces []string `json:"namespaces"`
	Pods       []string `json:"pods"`     // ns/name
	Services   []string `json:"services"` // ns/name
	Nodes      []string `json:"nodes"`
}

type netInput struct {
	Pods, Namespaces, Policies, Services []unstructured.Unstructured
}

var (
	nsRes  = Resource{Group: "core", Version: "v1", Name: "namespaces"}
	npRes  = Resource{Group: "networking.k8s.io", Version: "v1", Name: "networkpolicies", Namespaced: true}
	pdbRes = Resource{Group: "policy", Version: "v1", Name: "poddisruptionbudgets", Namespaced: true}
)

// Namespaces lists the namespace names, sorted. It is one small call, unlike SimulatorTargets.
func (s *Session) Namespaces(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	l, err := s.client(nsRes, "").List(ctx, metav1.ListOptions{})
	if err != nil {
		// without the right to list namespaces, the ones the user works with are the best answer there is
		if apierrors.IsForbidden(err) {
			if a := s.Access(ctx); !a.ClusterWide && len(a.Namespaces) > 0 {
				return append([]string{}, a.Namespaces...), nil
			}
		}
		return nil, err
	}
	out := make([]string, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, l.Items[i].GetName())
	}
	sort.Strings(out)
	return out, nil
}

// SimulatorTargets lists the things the simulators let you pick.
func (s *Session) SimulatorTargets(ctx context.Context) (SimTargets, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	t := SimTargets{Namespaces: []string{}, Pods: []string{}, Services: []string{}, Nodes: []string{}}
	pods, err := s.ListScoped(ctx, podRes, "", metav1.ListOptions{})
	if err != nil {
		return t, err
	}
	for _, p := range pods {
		t.Pods = append(t.Pods, p.GetNamespace()+"/"+p.GetName())
	}
	if items, err := s.ListScoped(ctx, svcRes, "", metav1.ListOptions{}); err == nil {
		for _, p := range items {
			t.Services = append(t.Services, p.GetNamespace()+"/"+p.GetName())
		}
	}
	if names, err := s.Namespaces(ctx); err == nil {
		t.Namespaces = append(t.Namespaces, names...)
	}
	if items, err := s.ListScoped(ctx, nodeRes, "", metav1.ListOptions{}); err == nil {
		for _, p := range items {
			t.Nodes = append(t.Nodes, p.GetName())
		}
	}
	sort.Strings(t.Namespaces)
	sort.Strings(t.Pods)
	sort.Strings(t.Services)
	sort.Strings(t.Nodes)
	return t, nil
}

// SimulateNetwork answers "can From reach To on Port?" from the NetworkPolicies in the cluster.
func (s *Session) SimulateNetwork(ctx context.Context, q NetQuery) (NetResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	var in netInput
	var notes []string
	for _, c := range []struct {
		r   Resource
		dst *[]unstructured.Unstructured
		req bool
	}{{podRes, &in.Pods, true}, {nsRes, &in.Namespaces, false}, {npRes, &in.Policies, true}, {svcRes, &in.Services, false}} {
		items, err := s.ListScoped(ctx, c.r, "", metav1.ListOptions{})
		if err != nil {
			if c.req {
				return NetResult{}, fmt.Errorf("reading %s: %w", c.r.Name, err)
			}
			if c.r.Name == "namespaces" {
				notes = append(notes, "Namespace labels could not be read, so namespaceSelector rules may be evaluated inaccurately.")
			}
			continue
		}
		*c.dst = items
	}
	res := simulateNetwork(in, q)
	res.Notes = append(res.Notes, notes...)
	return res, nil
}

type peerInfo struct {
	ns       string
	labels   map[string]string
	nsLabels map[string]string
	ip       string
	external bool
}

func simulateNetwork(in netInput, q NetQuery) NetResult {
	res := NetResult{Paths: []NetPath{}, Notes: []string{"Enforcement depends on your CNI: Flannel without a policy add-on ignores NetworkPolicies. The cluster's rules are evaluated, not live traffic."}}
	proto := strings.ToUpper(strings.TrimSpace(q.Protocol))
	if proto == "" {
		proto = "TCP"
	}
	nsLabels := map[string]map[string]string{}
	for _, n := range in.Namespaces {
		nsLabels[n.GetName()] = n.GetLabels()
	}
	podIdx := map[string]*unstructured.Unstructured{}
	for i := range in.Pods {
		podIdx[in.Pods[i].GetNamespace()+"/"+in.Pods[i].GetName()] = &in.Pods[i]
	}
	pol := map[string][]*unstructured.Unstructured{}
	for i := range in.Policies {
		pol[in.Policies[i].GetNamespace()] = append(pol[in.Policies[i].GetNamespace()], &in.Policies[i])
	}
	podPeer := func(p *unstructured.Unstructured) peerInfo {
		ip, _, _ := unstructured.NestedString(p.Object, "status", "podIP")
		return peerInfo{ns: p.GetNamespace(), labels: p.GetLabels(), nsLabels: nsLabels[p.GetNamespace()], ip: ip}
	}

	// source
	var src *unstructured.Unstructured
	var srcPeer peerInfo
	switch q.From.Kind {
	case "External":
		if net.ParseIP(q.From.IP) == nil {
			res.Verdict, res.Summary = "unknown", "The source IP address is not valid."
			return res
		}
		srcPeer = peerInfo{external: true, ip: q.From.IP}
	default:
		src = podIdx[q.From.Namespace+"/"+q.From.Name]
		if src == nil {
			res.Verdict, res.Summary = "unknown", fmt.Sprintf("Source pod %s/%s not found.", q.From.Namespace, q.From.Name)
			return res
		}
		srcPeer = podPeer(src)
	}

	// destination pods (+ port translation for Services)
	type dest struct {
		pod  *unstructured.Unstructured
		port string
	}
	var dests []dest
	var extDest *peerInfo
	switch q.To.Kind {
	case "External":
		if net.ParseIP(q.To.IP) == nil {
			res.Verdict, res.Summary = "unknown", "The destination IP address is not valid."
			return res
		}
		extDest = &peerInfo{external: true, ip: q.To.IP}
	case "Service":
		var svc *unstructured.Unstructured
		for i := range in.Services {
			if in.Services[i].GetNamespace() == q.To.Namespace && in.Services[i].GetName() == q.To.Name {
				svc = &in.Services[i]
			}
		}
		if svc == nil {
			res.Verdict, res.Summary = "unknown", fmt.Sprintf("Service %s/%s not found.", q.To.Namespace, q.To.Name)
			return res
		}
		sel, _, _ := unstructured.NestedStringMap(svc.Object, "spec", "selector")
		target := q.Port
		ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
		var chosen map[string]any
		for _, p := range ports {
			pm, _ := p.(map[string]any)
			if q.Port == "" || fmt.Sprint(pm["port"]) == q.Port || str(pm["name"]) == q.Port {
				chosen = pm
				break
			}
		}
		if chosen == nil && q.Port != "" {
			res.Verdict, res.Summary = "unknown", fmt.Sprintf("Service %s/%s does not expose port %s.", q.To.Namespace, q.To.Name, q.Port)
			return res
		}
		if chosen != nil {
			if tp, ok := chosen["targetPort"]; ok && fmt.Sprint(tp) != "" && fmt.Sprint(tp) != "0" {
				target = fmt.Sprint(tp)
			} else {
				target = fmt.Sprint(chosen["port"])
			}
			if proto == "TCP" && str(chosen["protocol"]) != "" {
				proto = strings.ToUpper(str(chosen["protocol"]))
			}
			res.Notes = append(res.Notes, fmt.Sprintf("Service port %v is translated to target port %s on the backing pods.", chosen["port"], target))
		}
		if len(sel) == 0 {
			res.Verdict, res.Summary = "unknown", "The Service has no selector, so there are no pods to evaluate."
			return res
		}
		for i := range in.Pods {
			p := &in.Pods[i]
			if p.GetNamespace() == q.To.Namespace && labels.SelectorFromSet(sel).Matches(labels.Set(p.GetLabels())) {
				dests = append(dests, dest{p, target})
			}
		}
		if len(dests) == 0 {
			res.Verdict, res.Summary = "unknown", "No pods match the Service selector."
			return res
		}
	default:
		p := podIdx[q.To.Namespace+"/"+q.To.Name]
		if p == nil {
			res.Verdict, res.Summary = "unknown", fmt.Sprintf("Destination pod %s/%s not found.", q.To.Namespace, q.To.Name)
			return res
		}
		dests = append(dests, dest{p, q.Port})
	}

	subj := func(p *unstructured.Unstructured) string { return p.GetNamespace() + "/" + p.GetName() }

	if extDest != nil {
		path := NetPath{Target: q.To.IP}
		port, _ := strconv.Atoi(q.Port)
		path.Port = port
		path.Allowed = true
		if src != nil {
			st := evalSide("egress", src, pol[src.GetNamespace()], *extDest, proto, q.Port, nil)
			path.Steps = append(path.Steps, st)
			path.Allowed = st.Allowed
		}
		res.Paths = append(res.Paths, path)
	}
	for _, d := range dests {
		portNum, _ := resolvePort(d.pod, d.port)
		path := NetPath{Target: subj(d.pod), Port: portNum, Allowed: true}
		if d.port != "" && portNum == 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("Port %q is not declared by pod %s; named-port rules cannot be matched.", d.port, subj(d.pod)))
		}
		resolver := func(n string) (int, bool) { return resolvePort(d.pod, n) }
		dp := podPeer(d.pod)
		if src != nil {
			st := evalSide("egress", src, pol[src.GetNamespace()], dp, proto, strconv.Itoa(portNum), resolver)
			path.Steps = append(path.Steps, st)
			path.Allowed = path.Allowed && st.Allowed
		}
		st := evalSide("ingress", d.pod, pol[d.pod.GetNamespace()], srcPeer, proto, strconv.Itoa(portNum), resolver)
		path.Steps = append(path.Steps, st)
		path.Allowed = path.Allowed && st.Allowed
		res.Paths = append(res.Paths, path)
	}
	allowed, denied := 0, 0
	for _, p := range res.Paths {
		if p.Allowed {
			allowed++
		} else {
			denied++
		}
	}
	switch {
	case denied == 0:
		res.Verdict, res.Summary = "allowed", "Traffic is allowed."
	case allowed == 0:
		res.Verdict, res.Summary = "denied", "Traffic is blocked."
		// every path is denied: explain the first one
		for _, s := range res.Paths[0].Steps {
			if !s.Allowed {
				res.Summary = fmt.Sprintf("Blocked at the %s of %s: %s", s.Side, s.Subject, s.Reason)
				break
			}
		}
	default:
		res.Verdict, res.Summary = "partial", fmt.Sprintf("Allowed to %d of %d backing pods.", allowed, allowed+denied)
	}
	return res
}

// resolvePort turns a number or a container port name into a number using the pod's declared ports.
func resolvePort(p *unstructured.Unstructured, port string) (int, bool) {
	if n, err := strconv.Atoi(port); err == nil {
		return n, false
	}
	if port == "" {
		return 0, false
	}
	for _, key := range []string{"containers", "initContainers"} {
		cs, _, _ := unstructured.NestedSlice(p.Object, "spec", key)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			ps, _ := cm["ports"].([]any)
			for _, x := range ps {
				pm, _ := x.(map[string]any)
				if str(pm["name"]) == port {
					n, _ := strconv.Atoi(fmt.Sprint(pm["containerPort"]))
					return n, true
				}
			}
		}
	}
	return 0, true
}

func policyTypes(p *unstructured.Unstructured) (ingress, egress bool) {
	types, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "policyTypes")
	if len(types) == 0 {
		_, hasEgress, _ := unstructured.NestedSlice(p.Object, "spec", "egress")
		return true, hasEgress
	}
	for _, t := range types {
		ingress = ingress || t == "Ingress"
		egress = egress || t == "Egress"
	}
	return
}

func selectorMatches(sel map[string]any, lbls map[string]string) bool {
	ls := &metav1.LabelSelector{}
	if sel != nil {
		if ml, ok := sel["matchLabels"].(map[string]any); ok {
			ls.MatchLabels = map[string]string{}
			for k, v := range ml {
				ls.MatchLabels[k] = fmt.Sprint(v)
			}
		}
		if me, ok := sel["matchExpressions"].([]any); ok {
			for _, e := range me {
				em, _ := e.(map[string]any)
				ls.MatchExpressions = append(ls.MatchExpressions, metav1.LabelSelectorRequirement{
					Key: str(em["key"]), Operator: metav1.LabelSelectorOperator(str(em["operator"])), Values: strSlice(em["values"]),
				})
			}
		}
	}
	s, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return false
	}
	return s.Matches(labels.Set(lbls)) // an empty selector matches everything
}

func ipInBlock(ip string, block map[string]any) bool {
	addr := net.ParseIP(ip)
	_, cidr, err := net.ParseCIDR(str(block["cidr"]))
	if addr == nil || err != nil || !cidr.Contains(addr) {
		return false
	}
	for _, ex := range strSlice(block["except"]) {
		if _, x, err := net.ParseCIDR(ex); err == nil && x.Contains(addr) {
			return false
		}
	}
	return true
}

func peerMatches(peers []any, policyNS string, peer peerInfo) bool {
	if len(peers) == 0 {
		return true
	}
	for _, p := range peers {
		pm, _ := p.(map[string]any)
		if ib, ok := pm["ipBlock"].(map[string]any); ok {
			if ipInBlock(peer.ip, ib) {
				return true
			}
			continue
		}
		if peer.external {
			continue
		}
		podSel, hasPod := pm["podSelector"].(map[string]any)
		nsSel, hasNS := pm["namespaceSelector"].(map[string]any)
		switch {
		case hasPod && hasNS:
			if selectorMatches(nsSel, peer.nsLabels) && selectorMatches(podSel, peer.labels) {
				return true
			}
		case hasNS:
			if selectorMatches(nsSel, peer.nsLabels) {
				return true
			}
		case hasPod:
			if peer.ns == policyNS && selectorMatches(podSel, peer.labels) {
				return true
			}
		}
	}
	return false
}

func portsMatch(ports []any, proto, port string, resolve func(string) (int, bool)) bool {
	if len(ports) == 0 {
		return true
	}
	n, _ := strconv.Atoi(port)
	for _, p := range ports {
		pm, _ := p.(map[string]any)
		pp := str(pm["protocol"])
		if pp == "" {
			pp = "TCP"
		}
		if pp != proto {
			continue
		}
		pv, has := pm["port"]
		if !has || pv == nil {
			return true
		}
		lo := 0
		switch v := pv.(type) {
		case string:
			if resolve == nil {
				continue
			}
			lo, _ = resolve(v)
		default:
			lo, _ = strconv.Atoi(fmt.Sprint(v))
		}
		if lo == 0 {
			continue
		}
		hi := lo
		if e, ok := pm["endPort"]; ok {
			hi, _ = strconv.Atoi(fmt.Sprint(e))
		}
		if n >= lo && n <= hi {
			return true
		}
	}
	return false
}

func evalSide(side string, subject *unstructured.Unstructured, policies []*unstructured.Unstructured, peer peerInfo, proto, port string, resolve func(string) (int, bool)) NetStep {
	st := NetStep{Side: side, Subject: subject.GetNamespace() + "/" + subject.GetName(), Policies: []string{}}
	var selecting []*unstructured.Unstructured
	for _, p := range policies {
		ing, eg := policyTypes(p)
		if (side == "ingress" && !ing) || (side == "egress" && !eg) {
			continue
		}
		sel, _, _ := unstructured.NestedMap(p.Object, "spec", "podSelector")
		if selectorMatches(sel, subject.GetLabels()) {
			selecting = append(selecting, p)
			st.Policies = append(st.Policies, p.GetName())
		}
	}
	sort.Strings(st.Policies)
	if len(selecting) == 0 {
		st.Allowed = true
		st.Reason = "no NetworkPolicy selects this pod for " + side + ", so it is not isolated"
		return st
	}
	st.Isolated = true
	key, other := "ingress", "from"
	if side == "egress" {
		key, other = "egress", "to"
	}
	for _, p := range selecting {
		rules, _, _ := unstructured.NestedSlice(p.Object, "spec", key)
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			peers, _ := rm[other].([]any)
			ports, _ := rm["ports"].([]any)
			if peerMatches(peers, p.GetNamespace(), peer) && portsMatch(ports, proto, port, resolve) {
				st.Allowed, st.Matched = true, p.GetName()
				st.Reason = fmt.Sprintf("allowed by policy %q", p.GetName())
				return st
			}
		}
	}
	if len(selecting) > 0 {
		st.Reason = fmt.Sprintf("isolated by %s but no rule allows %s/%s here", strings.Join(st.Policies, ", "), proto, port)
	}
	return st
}
