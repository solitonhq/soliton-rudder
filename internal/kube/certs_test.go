package kube

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// pemChain builds a leaf signed by a CA with the given issuer organisation (or self-signed if issuerOrg == "").
func pemChain(t *testing.T, cn, issuerOrg string, sans []string, ips []net.IP, notAfter time.Time, rsaKey bool) []byte {
	t.Helper()
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	var leafPub any = &leafKey.PublicKey
	if rsaKey {
		k, _ := rsa.GenerateKey(rand.Reader, 2048)
		leafPub = &k.PublicKey
		leafKey = nil
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(0xabc123), Subject: pkix.Name{CommonName: cn}, DNSNames: sans, IPAddresses: ips,
		NotBefore: notAfter.Add(-60 * 24 * time.Hour), NotAfter: notAfter,
	}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	var der []byte
	var err error
	if issuerOrg == "" {
		tmpl.IsCA = true
		signer := caKey
		if leafKey != nil {
			signer = leafKey
		}
		if rsaKey {
			der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, leafPub, caKey) // not self-signed by key, but subject==issuer is what we test
		} else {
			der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &signer.PublicKey, signer)
		}
	} else {
		ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "R10", Organization: []string{issuerOrg}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true}
		der, err = x509.CreateCertificate(rand.Reader, tmpl, ca, leafPub, caKey)
	}
	if err != nil {
		t.Fatal(err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// append a second (intermediate-ish) block so ChainLen counts it
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
}

func tlsSecret(ns, name string, crt []byte, annotations map[string]any) unstructured.Unstructured {
	meta := map[string]any{"name": name, "namespace": ns}
	if annotations != nil {
		meta["annotations"] = annotations
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "kubernetes.io/tls", "metadata": meta,
		"data": map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(crt), "tls.key": "c2VjcmV0LWtleQ=="},
	}}
}

func TestSummarizeCert(t *testing.T) {
	now := time.Now()
	le := pemChain(t, "shop.example.com", "Let's Encrypt", []string{"shop.example.com", "www.shop.example.com"}, []net.IP{net.ParseIP("10.1.2.3")}, now.Add(45*24*time.Hour+time.Hour), false)
	cs, err := SummarizeCert(le, now)
	if err != nil {
		t.Fatal(err)
	}
	if cs.IssuerKind != "letsencrypt" || cs.DaysLeft != 45 || cs.ChainLen != 2 || cs.SelfSigned || !strings.Contains(cs.Issuer, "Let's Encrypt") || !strings.HasPrefix(cs.KeyAlg, "ECDSA P-256") {
		t.Fatalf("%+v", cs)
	}
	if len(cs.SANs) != 3 || cs.SANs[0] != "shop.example.com" || cs.SANs[2] != "10.1.2.3" || cs.Serial != "abc123" {
		t.Fatalf("sans/serial: %+v", cs)
	}
	staging := pemChain(t, "x.io", "(STAGING) Let's Encrypt", []string{"x.io"}, nil, now.Add(24*time.Hour), false)
	if s, _ := SummarizeCert(staging, now); s.IssuerKind != "letsencrypt-staging" {
		t.Fatalf("%+v", s)
	}
	self := pemChain(t, "internal.svc", "", []string{"internal.svc"}, nil, now.Add(-72*time.Hour), false)
	s, _ := SummarizeCert(self, now)
	if s.IssuerKind != "self-signed" || !s.SelfSigned || s.DaysLeft != -3 {
		t.Fatalf("expired self-signed: %+v", s)
	}
	rsaCert := pemChain(t, "r.io", "Corp CA", []string{"r.io"}, nil, now.Add(24*time.Hour), true)
	if s, _ := SummarizeCert(rsaCert, now); s.KeyAlg != "RSA 2048" || s.IssuerKind != "other" {
		t.Fatalf("%+v", s)
	}
	for _, bad := range []string{"", "not pem", "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"} {
		if _, err := SummarizeCert([]byte(bad), now); err == nil {
			t.Fatalf("%q must fail", bad)
		}
	}
}

func TestCertificateReportJoinsCertManagerAndIngresses(t *testing.T) {
	now := time.Now()
	good := tlsSecret("shop", "shop-tls", pemChain(t, "shop.io", "Let's Encrypt", []string{"shop.io"}, nil, now.Add(80*24*time.Hour), false), map[string]any{"cert-manager.io/certificate-name": "shop-cert"})
	soon := tlsSecret("shop", "api-tls", pemChain(t, "api.io", "Corp CA", []string{"api.io"}, nil, now.Add(10*24*time.Hour), false), nil)
	month := tlsSecret("ops", "ops-tls", pemChain(t, "ops.io", "Corp CA", []string{"ops.io"}, nil, now.Add(20*24*time.Hour), false), nil)
	dead := tlsSecret("legacy", "old-tls", pemChain(t, "old.io", "Corp CA", []string{"old.io"}, nil, now.Add(-5*24*time.Hour), false), nil)
	broken := unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "empty-tls", "namespace": "legacy"}, "data": map[string]any{}}}
	cmc := u(t, `{metadata: {name: shop-cert, namespace: shop}, spec: {secretName: shop-tls}, status: {renewalTime: "2026-12-01T00:00:00Z", conditions: [{type: Ready, status: "False", message: "issuer unreachable"}]}}`)
	ing := u(t, `{metadata: {name: web, namespace: shop}, spec: {tls: [{secretName: shop-tls}, {secretName: api-tls}]}}`)

	rep := buildCertReport([]unstructured.Unstructured{good, soon, month, dead, broken}, []unstructured.Unstructured{cmc}, []unstructured.Unstructured{ing}, now)
	if rep.Counts != (CertCounts{Expired: 1, Soon14: 1, Soon30: 1, OK: 1, Errors: 1}) {
		t.Fatalf("%+v", rep.Counts)
	}
	order := []string{}
	for _, it := range rep.Items {
		order = append(order, it.Name)
	}
	// unreadable first, then by days left ascending
	if strings.Join(order, ",") != "empty-tls,old-tls,api-tls,ops-tls,shop-tls" {
		t.Fatalf("order: %v", order)
	}
	var shop CertItem
	for _, it := range rep.Items {
		if it.Name == "shop-tls" {
			shop = it
		}
	}
	if shop.Source != "cert-manager" || shop.Managed != "shop-cert" || shop.CMReady != "False" || shop.CMMessage != "issuer unreachable" || shop.Renewal == "" {
		t.Fatalf("cert-manager join: %+v", shop)
	}
	if len(shop.UsedBy) != 1 || shop.UsedBy[0] != "Ingress/web" {
		t.Fatalf("usedBy: %v", shop.UsedBy)
	}
}

func TestPrivateKeysAreDroppedOnReceipt(t *testing.T) {
	s := tlsSecret("a", "b", []byte("x"), nil)
	items := []unstructured.Unstructured{s}
	items[0].Object["stringData"] = map[string]any{"tls.key": "k"}
	StripPrivateKeys(items)
	d, _, _ := unstructured.NestedMap(items[0].Object, "data")
	if _, ok := d["tls.key"]; ok {
		t.Fatal("tls.key must be removed")
	}
	if _, ok, _ := unstructured.NestedString(items[0].Object, "stringData", "tls.key"); ok {
		t.Fatal("stringData tls.key must be removed")
	}
	if _, ok := d["tls.crt"]; !ok {
		t.Fatal("the certificate itself must stay")
	}
}

func TestCertEntryInConfigDataCarriesSummary(t *testing.T) {
	now := time.Now()
	crt := pemChain(t, "x.io", "Let's Encrypt", []string{"x.io", "y.io"}, nil, now.Add(10*24*time.Hour), false)
	obj := &unstructured.Unstructured{Object: map[string]any{"type": "kubernetes.io/tls", "data": map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(crt)}}}
	cd := buildConfigData(true, obj, now)
	e := cd.Entries[0]
	if !e.Masked || e.Format != "cert" || e.Cert == nil || len(e.Cert.SANs) != 2 || e.Cert.IssuerKind != "letsencrypt" || e.Value != "" {
		t.Fatalf("certificate details are public even when the PEM stays masked: %+v", e)
	}
}
