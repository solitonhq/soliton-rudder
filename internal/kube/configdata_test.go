package kube

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetectFormat(t *testing.T) {
	now := time.Now()
	cases := []struct{ in, want string }{
		{`{"a":1}`, "json"},
		{"[1,2]", "json"},
		{"A=1\nB=two\n# c\nexport C=3", "env"},
		{"server:\n  port: 80\nname: x\n", "yaml"},
		{"just some text", "text"},
		{"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----", "text"},
		{"a\x00b", "binary"},
		{"\xff\xfe", "binary"},
	}
	for _, c := range cases {
		if got, _ := detectFormat([]byte(c.in), now); got != c.want {
			t.Errorf("detectFormat(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestSecretIsMaskedUntilRevealed(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"type": "Opaque",
		"data": map[string]any{
			"password": base64.StdEncoding.EncodeToString([]byte("hunter2")),
			"blob":     base64.StdEncoding.EncodeToString([]byte{0, 1, 2}),
		},
	}}
	cd := buildConfigData(true, obj, time.Now())
	if cd.Type != "Opaque" || len(cd.Entries) != 2 {
		t.Fatalf("%+v", cd)
	}
	for _, e := range cd.Entries {
		if strings.Contains(e.Value, "hunter2") {
			t.Fatal("secret value leaked")
		}
	}
	pw := cd.Entries[1]
	if pw.Key != "password" || !pw.Masked || pw.Size != 7 {
		t.Fatalf("%+v", pw)
	}
	if blob := cd.Entries[0]; blob.Format != "binary" || blob.Masked {
		t.Fatalf("%+v", blob)
	}
}

func TestConfigMapShowsValuesAndPrettyJSON(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"data":       map[string]any{"app.json": `{"a":1,"b":[1,2]}`, "note": "hi"},
		"binaryData": map[string]any{"img": base64.StdEncoding.EncodeToString([]byte{0xff, 0, 1})},
	}}
	cd := buildConfigData(false, obj, time.Now())
	if len(cd.Entries) != 3 {
		t.Fatalf("%+v", cd)
	}
	j := cd.Entries[0]
	if j.Key != "app.json" || j.Format != "json" || !strings.Contains(j.Value, "\n  \"a\": 1") {
		t.Fatalf("%+v", j)
	}
	if cd.Entries[2].Format != "binary" || cd.Entries[2].Value != "" {
		t.Fatalf("%+v", cd.Entries[2])
	}
}

func TestCertEntryInfo(t *testing.T) {
	sec := certSecret(t, "d", "c", []string{"x.example.com"}, time.Now().Add(48*time.Hour))
	crt, _ := base64.StdEncoding.DecodeString(sec.Object["data"].(map[string]any)["tls.crt"].(string))
	f, info := detectFormat(crt, time.Now())
	if f != "cert" || !strings.Contains(info, "x.example.com") || !strings.Contains(info, "expires in") {
		t.Fatalf("%s %q", f, info)
	}
}
