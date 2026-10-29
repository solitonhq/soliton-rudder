package kube

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type PodRef struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	IP    string `json:"ip"` // the endpoint's address (the pod IP)
}

type PathInfo struct {
	Path     string `json:"path"`
	PathType string `json:"pathType"`
	Service  string `json:"service"`  // "" for resource backends
	Port     string `json:"port"`     // number or name as written
	Resource string `json:"resource"` // Kind/name for non-service backends
	// ClusterIP and ServiceType describe the backing Service ("None" for headless Services).
	ClusterIP   string   `json:"clusterIP"`
	ServiceType string   `json:"serviceType"`
	Found       bool     `json:"found"` // service exists and exposes the port
	Ready       int      `json:"ready"`
	Total       int      `json:"total"`
	Pods        []PodRef `json:"pods"`
	Issue       string   `json:"issue"`
}

type RuleInfo struct {
	Host   string     `json:"host"`
	Secure bool       `json:"secure"`
	Paths  []PathInfo `json:"paths"`
}

type TLSInfo struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secretName"`
	Issuer     string   `json:"issuer"`
	NotAfter   string   `json:"notAfter"`
	DaysLeft   int      `json:"daysLeft"`
	Names      []string `json:"names"`
	Error      string   `json:"error"`
}

type Issue struct {
	Severity string `json:"severity"` // error | warn | info
	Message  string `json:"message"`
}

type IngressInfo struct {
	Name      string     `json:"name"`
	Namespace string     `json:"namespace"`
	Class     string     `json:"class"`
	Addresses []string   `json:"addresses"`
	Created   string     `json:"created"`
	TLS       []TLSInfo  `json:"tls"`
	Rules     []RuleInfo `json:"rules"`
	Issues    []Issue    `json:"issues"`
}

type IngressView struct {
	Ingresses []IngressInfo `json:"ingresses"`
}

var (
	ingressRes = Resource{Group: "networking.k8s.io", Version: "v1", Name: "ingresses", Namespaced: true}
	sliceRes   = Resource{Group: "discovery.k8s.io", Version: "v1", Name: "endpointslices", Namespaced: true}
	secretRes  = Resource{Group: "core", Version: "v1", Name: "secrets", Namespaced: true}
)

// IngressMap analyses every ingress in ns ("" = all): routing chain, TLS and problems.
func (s *Session) IngressMap(ctx context.Context, ns string) (IngressView, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	ingItems, err := s.ListScoped(ctx, ingressRes, ns, metav1.ListOptions{})
	if err != nil {
		return IngressView{}, err
	}
	svcItems, err := s.ListScoped(ctx, svcRes, ns, metav1.ListOptions{})
	if err != nil {
		return IngressView{}, err
	}
	// EndpointSlices are optional: without them backends just show as "unknown readiness".
	sliceItems, _ := s.ListScoped(ctx, sliceRes, ns, metav1.ListOptions{})

	secrets := map[string]*unstructured.Unstructured{}
	for i := range ingItems {
		ing := &ingItems[i]
		tls, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
		for _, t := range tls {
			tm, _ := t.(map[string]any)
			name, _ := tm["secretName"].(string)
			k := ing.GetNamespace() + "/" + name
			if name == "" || secrets[k] != nil {
				continue
			}
			if sec, err := s.client(secretRes, ing.GetNamespace()).Get(ctx, name, metav1.GetOptions{}); err == nil {
				secrets[k] = sec
			} else {
				secrets[k] = nil
			}
		}
	}
	return buildIngressView(ingItems, svcItems, sliceItems, secrets, time.Now()), nil
}

type svcInfo struct {
	obj   *unstructured.Unstructured
	ports []map[string]any
}

type endpointSet struct {
	pods []PodRef
}

func buildIngressView(ings, svcs, slices []unstructured.Unstructured, secrets map[string]*unstructured.Unstructured, now time.Time) IngressView {
	svcIdx := map[string]*svcInfo{}
	for i := range svcs {
		sv := &svcs[i]
		ps, _, _ := unstructured.NestedSlice(sv.Object, "spec", "ports")
		info := &svcInfo{obj: sv}
		for _, p := range ps {
			if pm, ok := p.(map[string]any); ok {
				info.ports = append(info.ports, pm)
			}
		}
		svcIdx[sv.GetNamespace()+"/"+sv.GetName()] = info
	}
	eps := map[string]*endpointSet{}
	for i := range slices {
		sl := &slices[i]
		name := sl.GetLabels()["kubernetes.io/service-name"]
		if name == "" {
			continue
		}
		k := sl.GetNamespace() + "/" + name
		set := eps[k]
		if set == nil {
			set = &endpointSet{}
			eps[k] = set
		}
		items, _, _ := unstructured.NestedSlice(sl.Object, "endpoints")
		for _, it := range items {
			em, _ := it.(map[string]any)
			ready := true
			if c, ok := em["conditions"].(map[string]any); ok {
				if r, ok := c["ready"].(bool); ok {
					ready = r
				}
			}
			pod, ip := "", ""
			if a, ok := em["addresses"].([]any); ok && len(a) > 0 {
				ip = fmt.Sprint(a[0])
			}
			if tr, ok := em["targetRef"].(map[string]any); ok {
				pod, _ = tr["name"].(string)
			}
			if pod == "" {
				pod = ip
			}
			set.pods = append(set.pods, PodRef{Name: pod, Ready: ready, IP: ip})
		}
	}

	// duplicate host+path detection across ingresses (same class)
	owners := map[string][]string{}
	routeOf := func(ing *unstructured.Unstructured, host, path string) string {
		return fmt.Sprintf("%s|%s|%s", ingressClass(ing), host, path)
	}
	for i := range ings {
		ing := &ings[i]
		id := ing.GetNamespace() + "/" + ing.GetName()
		rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			host, _ := rm["host"].(string)
			paths, _, _ := unstructured.NestedSlice(rm, "http", "paths")
			for _, p := range paths {
				pm, _ := p.(map[string]any)
				path, _ := pm["path"].(string)
				if path == "" {
					path = "/"
				}
				k := routeOf(ing, host, path)
				if !contains(owners[k], id) {
					owners[k] = append(owners[k], id)
				}
			}
		}
	}

	out := IngressView{Ingresses: []IngressInfo{}}
	for i := range ings {
		ing := &ings[i]
		id := ing.GetNamespace() + "/" + ing.GetName()
		info := IngressInfo{
			Name: ing.GetName(), Namespace: ing.GetNamespace(), Class: ingressClass(ing),
			Created: ing.GetCreationTimestamp().UTC().Format(time.RFC3339),
			Rules:   []RuleInfo{}, TLS: []TLSInfo{}, Issues: []Issue{}, Addresses: []string{},
		}
		if lb, _, _ := unstructured.NestedSlice(ing.Object, "status", "loadBalancer", "ingress"); len(lb) > 0 {
			for _, e := range lb {
				em, _ := e.(map[string]any)
				if v, _ := em["hostname"].(string); v != "" {
					info.Addresses = append(info.Addresses, v)
				} else if v, _ := em["ip"].(string); v != "" {
					info.Addresses = append(info.Addresses, v)
				}
			}
		} else {
			info.Issues = append(info.Issues, Issue{"warn", "No address assigned yet (is an ingress controller running for this class?)"})
		}
		if info.Class == "" {
			info.Issues = append(info.Issues, Issue{"info", "No ingress class set; the default controller (if any) will pick this up"})
		}

		// TLS
		tlsHosts := map[string]*TLSInfo{}
		tlsList, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
		for _, t := range tlsList {
			tm, _ := t.(map[string]any)
			ti := TLSInfo{SecretName: str(tm["secretName"]), Hosts: strSlice(tm["hosts"])}
			if ti.SecretName == "" {
				ti.Error = "no secretName (controller default certificate)"
			} else if sec := secrets[ing.GetNamespace()+"/"+ti.SecretName]; sec == nil {
				ti.Error = fmt.Sprintf("secret %q not found or not readable", ti.SecretName)
				info.Issues = append(info.Issues, Issue{"error", ti.Error})
			} else {
				fillCert(&ti, sec, now)
				if ti.Error != "" {
					info.Issues = append(info.Issues, Issue{"error", fmt.Sprintf("TLS secret %q: %s", ti.SecretName, ti.Error)})
				} else if ti.DaysLeft < 0 {
					info.Issues = append(info.Issues, Issue{"error", fmt.Sprintf("Certificate in %q expired %d days ago", ti.SecretName, -ti.DaysLeft)})
				} else if ti.DaysLeft < 14 {
					info.Issues = append(info.Issues, Issue{"error", fmt.Sprintf("Certificate in %q expires in %d days", ti.SecretName, ti.DaysLeft)})
				} else if ti.DaysLeft < 30 {
					info.Issues = append(info.Issues, Issue{"warn", fmt.Sprintf("Certificate in %q expires in %d days", ti.SecretName, ti.DaysLeft)})
				}
			}
			info.TLS = append(info.TLS, ti)
			cp := &info.TLS[len(info.TLS)-1]
			for _, h := range ti.Hosts {
				tlsHosts[h] = cp
			}
		}

		// rules
		rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			host, _ := rm["host"].(string)
			ri := RuleInfo{Host: host, Paths: []PathInfo{}}
			if host != "" {
				if ti := matchTLS(tlsHosts, host); ti != nil {
					ri.Secure = true
					if ti.Error == "" && len(ti.Names) > 0 && !certCovers(ti.Names, host) {
						info.Issues = append(info.Issues, Issue{"warn", fmt.Sprintf("Certificate in %q does not cover host %q", ti.SecretName, host)})
					}
				}
			}
			paths, _, _ := unstructured.NestedSlice(rm, "http", "paths")
			for _, p := range paths {
				pm, _ := p.(map[string]any)
				pi := backendInfo(ing.GetNamespace(), pm["backend"], svcIdx, eps)
				pi.Path, _ = pm["path"].(string)
				if pi.Path == "" {
					pi.Path = "/"
				}
				pi.PathType, _ = pm["pathType"].(string)
				if len(owners[routeOf(ing, host, pi.Path)]) > 1 {
					msg := fmt.Sprintf("%s%s is also defined by: %s", orStar(host), pi.Path, strings.Join(without(owners[routeOf(ing, host, pi.Path)], id), ", "))
					info.Issues = append(info.Issues, Issue{"error", "Conflicting route: " + msg})
					if pi.Issue == "" {
						pi.Issue = "conflicting route"
					}
				}
				if pi.Issue != "" && pi.Issue != "conflicting route" {
					info.Issues = append(info.Issues, Issue{severityFor(pi), fmt.Sprintf("%s%s → %s", orStar(host), pi.Path, pi.Issue)})
				}
				ri.Paths = append(ri.Paths, pi)
			}
			info.Rules = append(info.Rules, ri)
		}
		if db, _, _ := unstructured.NestedMap(ing.Object, "spec", "defaultBackend"); len(db) > 0 {
			pi := backendInfo(ing.GetNamespace(), db, svcIdx, eps)
			pi.Path, pi.PathType = "(default)", ""
			if pi.Issue != "" {
				info.Issues = append(info.Issues, Issue{severityFor(pi), "default backend → " + pi.Issue})
			}
			info.Rules = append(info.Rules, RuleInfo{Host: "", Paths: []PathInfo{pi}})
		}
		out.Ingresses = append(out.Ingresses, info)
	}
	sort.Slice(out.Ingresses, func(i, j int) bool {
		a, b := out.Ingresses[i], out.Ingresses[j]
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	return out
}

func severityFor(p PathInfo) string {
	if p.Found && p.Total > 0 && p.Ready == 0 {
		return "error"
	}
	if !p.Found {
		return "error"
	}
	return "warn"
}

func backendInfo(ns string, backend any, svcIdx map[string]*svcInfo, eps map[string]*endpointSet) PathInfo {
	pi := PathInfo{Pods: []PodRef{}}
	bm, _ := backend.(map[string]any)
	if res, ok := bm["resource"].(map[string]any); ok {
		pi.Resource = fmt.Sprintf("%s/%s", str(res["kind"]), str(res["name"]))
		pi.Found = true
		return pi
	}
	sm, _ := bm["service"].(map[string]any)
	pi.Service = str(sm["name"])
	if pm, ok := sm["port"].(map[string]any); ok {
		if n, ok := pm["number"]; ok {
			pi.Port = fmt.Sprint(n)
		} else {
			pi.Port = str(pm["name"])
		}
	}
	sv := svcIdx[ns+"/"+pi.Service]
	if sv == nil {
		pi.Issue = fmt.Sprintf("service %q not found", pi.Service)
		return pi
	}
	portOK := false
	for _, p := range sv.ports {
		if fmt.Sprint(p["port"]) == pi.Port || str(p["name"]) == pi.Port {
			portOK = true
		}
	}
	if !portOK {
		pi.Issue = fmt.Sprintf("service %q does not expose port %s", pi.Service, pi.Port)
		return pi
	}
	pi.Found = true
	pi.ClusterIP, _, _ = unstructured.NestedString(sv.obj.Object, "spec", "clusterIP")
	pi.ServiceType, _, _ = unstructured.NestedString(sv.obj.Object, "spec", "type")
	if pi.ServiceType == "" {
		pi.ServiceType = "ClusterIP"
	}
	if pi.ServiceType == "ExternalName" {
		return pi
	}
	set := eps[ns+"/"+pi.Service]
	if set == nil {
		pi.Issue = "no endpoints (no pods match the service selector)"
		return pi
	}
	pi.Pods = set.pods
	if pi.Pods == nil {
		pi.Pods = []PodRef{} // JSON null would crash the UI's pods.map
	}
	pi.Total = len(set.pods)
	for _, p := range set.pods {
		if p.Ready {
			pi.Ready++
		}
	}
	switch {
	case pi.Total == 0:
		pi.Issue = "no endpoints (no pods match the service selector)"
	case pi.Ready == 0:
		pi.Issue = "no ready pods behind the service"
	}
	return pi
}

func fillCert(ti *TLSInfo, sec *unstructured.Unstructured, now time.Time) {
	data, _, _ := unstructured.NestedMap(sec.Object, "data")
	raw, _ := data["tls.crt"].(string)
	if raw == "" {
		ti.Error = "secret has no tls.crt"
		return
	}
	der, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		ti.Error = "tls.crt is not valid base64"
		return
	}
	cert, err := parseFirstCert(der)
	if err != nil {
		ti.Error = err.Error()
		return
	}
	ti.Issuer = cert.Issuer.CommonName
	if ti.Issuer == "" && len(cert.Issuer.Organization) > 0 {
		ti.Issuer = cert.Issuer.Organization[0]
	}
	ti.NotAfter = cert.NotAfter.UTC().Format(time.RFC3339)
	ti.DaysLeft = int(cert.NotAfter.Sub(now).Hours() / 24)
	if cert.NotAfter.Before(now) {
		ti.DaysLeft = -int(now.Sub(cert.NotAfter).Hours() / 24)
	}
	ti.Names = cert.DNSNames
	if len(ti.Names) == 0 && cert.Subject.CommonName != "" {
		ti.Names = []string{cert.Subject.CommonName}
	}
}

// parseFirstCert accepts PEM (as stored in tls.crt) and returns the leaf certificate.
func parseFirstCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("tls.crt is not PEM encoded")
	}
	return x509.ParseCertificate(block.Bytes)
}

func certCovers(names []string, host string) bool {
	for _, n := range names {
		if hostMatches(n, host) {
			return true
		}
	}
	return false
}

// hostMatches implements TLS wildcard matching (*.example.com matches one label).
func hostMatches(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		i := strings.IndexByte(host, '.')
		return i > 0 && host[i+1:] == pattern[2:]
	}
	return false
}

func matchTLS(m map[string]*TLSInfo, host string) *TLSInfo {
	for h, ti := range m {
		if hostMatches(h, host) {
			return ti
		}
	}
	return nil
}

func ingressClass(ing *unstructured.Unstructured) string {
	if c, _, _ := unstructured.NestedString(ing.Object, "spec", "ingressClassName"); c != "" {
		return c
	}
	return ing.GetAnnotations()["kubernetes.io/ingress.class"]
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strSlice(v any) []string {
	a, _ := v.([]any)
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func without(l []string, s string) []string {
	var out []string
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func orStar(h string) string {
	if h == "" {
		return "*"
	}
	return h
}
