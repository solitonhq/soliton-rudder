package kube

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CertSummary is the non-sensitive description of an X.509 certificate (leaf of a PEM bundle).
type CertSummary struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	IssuerKind string   `json:"issuerKind"` // letsencrypt | letsencrypt-staging | self-signed | other
	SANs       []string `json:"sans"`
	NotBefore  string   `json:"notBefore"`
	NotAfter   string   `json:"notAfter"`
	DaysLeft   int      `json:"daysLeft"` // whole days, truncated toward zero (negative once expired)
	Expired    bool     `json:"expired"`
	KeyAlg     string   `json:"keyAlg"`
	IsCA       bool     `json:"isCA"`
	SelfSigned bool     `json:"selfSigned"`
	Serial     string   `json:"serial"`
	ChainLen   int      `json:"chainLen"`
}

// SummarizeCert parses the first certificate in a PEM bundle.
func SummarizeCert(pemBytes []byte, now time.Time) (CertSummary, error) {
	var leaf *x509.Certificate
	chain := 0
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		chain++
		if leaf == nil {
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return CertSummary{}, fmt.Errorf("invalid certificate: %w", err)
			}
			leaf = c
		}
	}
	if leaf == nil {
		return CertSummary{}, fmt.Errorf("no PEM certificate found")
	}
	cs := CertSummary{
		Subject: dnString(leaf.Subject.CommonName, leaf.Subject.Organization), Issuer: dnString(leaf.Issuer.CommonName, leaf.Issuer.Organization),
		NotBefore: leaf.NotBefore.UTC().Format(time.RFC3339), NotAfter: leaf.NotAfter.UTC().Format(time.RFC3339),
		DaysLeft: int(math.Trunc(leaf.NotAfter.Sub(now).Hours() / 24)), Expired: leaf.NotAfter.Before(now), IsCA: leaf.IsCA, ChainLen: chain,
		Serial: leaf.SerialNumber.Text(16), SelfSigned: bytes.Equal(leaf.RawIssuer, leaf.RawSubject), SANs: []string{},
	}
	cs.SANs = append(cs.SANs, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		cs.SANs = append(cs.SANs, ip.String())
	}
	cs.SANs = append(cs.SANs, leaf.EmailAddresses...)
	if len(cs.SANs) == 0 && leaf.Subject.CommonName != "" {
		cs.SANs = []string{leaf.Subject.CommonName}
	}
	switch k := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		cs.KeyAlg = fmt.Sprintf("RSA %d", k.N.BitLen())
	case *ecdsa.PublicKey:
		cs.KeyAlg = "ECDSA " + k.Curve.Params().Name
	case ed25519.PublicKey:
		cs.KeyAlg = "Ed25519"
	default:
		cs.KeyAlg = leaf.PublicKeyAlgorithm.String()
	}
	issuerOrg := strings.Join(leaf.Issuer.Organization, " ")
	switch {
	case cs.SelfSigned:
		cs.IssuerKind = "self-signed"
	case strings.Contains(issuerOrg, "(STAGING)") && strings.Contains(issuerOrg, "Let's Encrypt"):
		cs.IssuerKind = "letsencrypt-staging"
	case strings.Contains(issuerOrg, "Let's Encrypt"):
		cs.IssuerKind = "letsencrypt"
	default:
		cs.IssuerKind = "other"
	}
	return cs, nil
}

func dnString(cn string, org []string) string {
	switch {
	case cn != "" && len(org) > 0:
		return cn + " (" + strings.Join(org, ", ") + ")"
	case cn != "":
		return cn
	case len(org) > 0:
		return strings.Join(org, ", ")
	}
	return "—"
}

type CertItem struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CertSummary
	Source    string   `json:"source"`    // cert-manager | manual
	Managed   string   `json:"managed"`   // cert-manager Certificate name
	CMReady   string   `json:"cmReady"`   // True | False | "" (no Certificate object)
	CMMessage string   `json:"cmMessage"` // failing reason from cert-manager
	Renewal   string   `json:"renewal"`   // planned renewal time (cert-manager)
	UsedBy    []string `json:"usedBy"`    // Ingress/<name> referencing the secret
	Error     string   `json:"error"`
}

type CertCounts struct {
	Expired int `json:"expired"`
	Soon14  int `json:"soon14"` // expires in <14 days (not yet expired)
	Soon30  int `json:"soon30"` // 14–30 days
	OK      int `json:"ok"`
	Errors  int `json:"errors"`
}

type CertReport struct {
	Items  []CertItem `json:"items"`
	Counts CertCounts `json:"counts"`
}

// StripPrivateKeys removes tls.key from secrets we only need certificates from, as early as possible.
func StripPrivateKeys(items []unstructured.Unstructured) {
	for i := range items {
		unstructured.RemoveNestedField(items[i].Object, "data", "tls.key")
		unstructured.RemoveNestedField(items[i].Object, "stringData", "tls.key")
	}
}

// Certificates inventories every kubernetes.io/tls Secret (ns "" = all namespaces) with expiry details,
// joined with cert-manager Certificates and the Ingresses that use them. Private keys are dropped on receipt.
func (s *Session) Certificates(ctx context.Context, ns string) (CertReport, error) {
	secrets, err := s.ListRawSelector(ctx, "core", "v1", "secrets", true, ns, "type=kubernetes.io/tls")
	if err != nil {
		return CertReport{}, err
	}
	StripPrivateKeys(secrets)
	cm, _ := s.ListRawSelector(ctx, "cert-manager.io", "v1", "certificates", true, ns, "")
	ings, _ := s.ListRawSelector(ctx, "networking.k8s.io", "v1", "ingresses", true, ns, "")
	return buildCertReport(secrets, cm, ings, time.Now()), nil
}

func buildCertReport(secrets, cmCerts, ingresses []unstructured.Unstructured, now time.Time) CertReport {
	cmBySecret := map[string]*unstructured.Unstructured{}
	for i := range cmCerts {
		c := &cmCerts[i]
		if sn, _, _ := unstructured.NestedString(c.Object, "spec", "secretName"); sn != "" {
			cmBySecret[c.GetNamespace()+"/"+sn] = c
		}
	}
	usedBy := map[string][]string{}
	for i := range ingresses {
		ing := &ingresses[i]
		tls, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
		for _, t := range tls {
			tm, _ := t.(map[string]any)
			if sn := str(tm["secretName"]); sn != "" {
				k := ing.GetNamespace() + "/" + sn
				usedBy[k] = append(usedBy[k], "Ingress/"+ing.GetName())
			}
		}
	}
	rep := CertReport{Items: []CertItem{}}
	for i := range secrets {
		sec := &secrets[i]
		key := sec.GetNamespace() + "/" + sec.GetName()
		item := CertItem{Namespace: sec.GetNamespace(), Name: sec.GetName(), Source: "manual", UsedBy: usedBy[key]}
		if item.UsedBy == nil {
			item.UsedBy = []string{}
		}
		item.SANs = []string{}
		ann := sec.GetAnnotations()
		if n := ann["cert-manager.io/certificate-name"]; n != "" {
			item.Source, item.Managed = "cert-manager", n
		}
		if cmc := cmBySecret[key]; cmc != nil {
			item.Source, item.Managed = "cert-manager", cmc.GetName()
			item.Renewal = nstr(cmc.Object, "status", "renewalTime")
			conds, _, _ := unstructured.NestedSlice(cmc.Object, "status", "conditions")
			for _, c := range conds {
				cm, _ := c.(map[string]any)
				if str(cm["type"]) == "Ready" {
					item.CMReady = str(cm["status"])
					if item.CMReady != "True" {
						item.CMMessage = str(cm["message"])
					}
				}
			}
		}
		raw, _, _ := unstructured.NestedString(sec.Object, "data", "tls.crt")
		der, derr := base64.StdEncoding.DecodeString(raw)
		switch {
		case raw == "":
			item.Error = "secret has no tls.crt"
		case derr != nil:
			item.Error = "tls.crt is not valid base64"
		default:
			if cs, err := SummarizeCert(der, now); err != nil {
				item.Error = err.Error()
			} else {
				item.CertSummary = cs
			}
		}
		switch {
		case item.Error != "":
			rep.Counts.Errors++
		case item.Expired:
			rep.Counts.Expired++
		case item.DaysLeft < 14:
			rep.Counts.Soon14++
		case item.DaysLeft < 30:
			rep.Counts.Soon30++
		default:
			rep.Counts.OK++
		}
		rep.Items = append(rep.Items, item)
	}
	sort.SliceStable(rep.Items, func(i, j int) bool {
		a, b := rep.Items[i], rep.Items[j]
		if (a.Error != "") != (b.Error != "") {
			return a.Error != "" // unreadable certificates first: they need attention
		}
		if a.DaysLeft != b.DaysLeft {
			return a.DaysLeft < b.DaysLeft
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	return rep
}
