package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

func podFrom(t *testing.T, doc string) *corev1.Pod {
	t.Helper()
	o := u(t, doc)
	var p corev1.Pod
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

const envPod = `
apiVersion: v1
kind: Pod
metadata: {name: web-1, namespace: shop}
spec:
  initContainers:
    - {name: migrate, image: x, envFrom: [{configMapRef: {name: app-config}}]}
  containers:
    - name: web
      image: x
      env:
        - {name: MODE, value: production}
        - {name: DB_PASSWORD, valueFrom: {secretKeyRef: {name: db, key: password}}}
        - {name: THEME, valueFrom: {configMapKeyRef: {name: app-config, key: theme, optional: true}}}
        - {name: POD_IP, valueFrom: {fieldRef: {fieldPath: status.podIP}}}
        - {name: CPU_LIMIT, valueFrom: {resourceFieldRef: {resource: limits.cpu}}}
      envFrom:
        - {secretRef: {name: api-keys}, prefix: API_}
      volumeMounts:
        - {name: certs, mountPath: /etc/tls, readOnly: true}
        - {name: scratch, mountPath: /tmp}
  volumes:
    - {name: certs, secret: {secretName: tls-cert}}
    - {name: scratch, emptyDir: {}}
    - {name: data, persistentVolumeClaim: {claimName: web-data}}
    - name: token
      projected:
        sources: [{serviceAccountToken: {path: t}}, {configMap: {name: ca}}, {secret: {name: extra}}]
`

func TestPodEnvironmentListsReferencesWithoutValues(t *testing.T) {
	env := podEnvironment(podFrom(t, envPod))
	if len(env.Containers) != 2 || !env.Containers[0].Init || env.Containers[0].Name != "migrate" || env.Containers[1].Name != "web" {
		t.Fatalf("init containers come first and are marked: %+v", env.Containers)
	}
	web := env.Containers[1]
	by := map[string]EnvVar{}
	for _, e := range web.Env {
		by[e.Name] = e
	}
	if e := by["MODE"]; e.Source != "literal" || e.Value != "production" {
		t.Errorf("literal: %+v", e)
	}
	if e := by["DB_PASSWORD"]; e.Source != "secret" || e.Ref != "db" || e.Key != "password" || e.Value != "" {
		t.Errorf("secret reference must carry no value: %+v", e)
	}
	if e := by["THEME"]; e.Source != "configmap" || e.Ref != "app-config" || e.Key != "theme" || !e.Optional || e.Value != "" {
		t.Errorf("configmap reference: %+v", e)
	}
	if by["POD_IP"].Source != "field" || by["POD_IP"].Detail != "status.podIP" || by["CPU_LIMIT"].Source != "resource" {
		t.Errorf("downward API: %+v %+v", by["POD_IP"], by["CPU_LIMIT"])
	}
	if len(web.EnvFrom) != 1 || web.EnvFrom[0].Kind != "secret" || web.EnvFrom[0].Name != "api-keys" || web.EnvFrom[0].Prefix != "API_" {
		t.Errorf("envFrom: %+v", web.EnvFrom)
	}
	if env.Containers[0].EnvFrom[0].Kind != "configmap" {
		t.Errorf("init envFrom: %+v", env.Containers[0].EnvFrom)
	}
	if len(web.Mounts) != 2 || web.Mounts[0].Volume != "certs" || !web.Mounts[0].ReadOnly || web.Mounts[0].Path != "/etc/tls" {
		t.Errorf("mounts: %+v", web.Mounts)
	}
	kinds := map[string]PodVolume{}
	for _, v := range env.Volumes {
		kinds[v.Name] = v
	}
	if v := kinds["certs"]; v.Kind != "secret" || v.Ref != "tls-cert" {
		t.Errorf("secret volume: %+v", v)
	}
	if v := kinds["data"]; v.Kind != "pvc" || v.Ref != "web-data" || kinds["scratch"].Kind != "emptyDir" {
		t.Errorf("other volumes: %+v", kinds)
	}
	if v := kinds["token"]; v.Kind != "projected" || len(v.Sources) != 3 || v.Sources[1].Kind != "configmap" || v.Sources[1].Name != "ca" {
		t.Errorf("projected volume: %+v", v)
	}
}

func TestPodEnvironmentNeverSerialisesNullArrays(t *testing.T) {
	env := podEnvironment(podFrom(t, `{apiVersion: v1, kind: Pod, metadata: {name: p}, spec: {containers: [{name: c, image: x}]}}`))
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("the UI maps these arrays directly, null would crash it: %s", b)
	}
}

func refServer(t *testing.T) *Session {
	t.Helper()
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api/v1/namespaces/shop/secrets/db", func(w http.ResponseWriter, r *http.Request) {
		js(w, 200, map[string]any{"kind": "Secret", "apiVersion": "v1", "metadata": map[string]any{"name": "db"},
			"data": map[string]any{"password": base64.StdEncoding.EncodeToString([]byte("s3cr3t!")), "big": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxRefValue+10)))}})
	})
	mux.HandleFunc("/api/v1/namespaces/shop/secrets/locked", func(w http.ResponseWriter, r *http.Request) {
		js(w, 403, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "Forbidden", "code": 403, "message": "secrets \"locked\" is forbidden"})
	})
	mux.HandleFunc("/api/v1/namespaces/shop/configmaps/app-config", func(w http.ResponseWriter, r *http.Request) {
		js(w, 200, map[string]any{"kind": "ConfigMap", "apiVersion": "v1", "metadata": map[string]any{"name": "app-config"},
			"data": map[string]any{"theme": "dark"}, "binaryData": map[string]any{"blob": "AAEC"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestRefValueDecodesSecretsAndReadsConfigMaps(t *testing.T) {
	s := refServer(t)
	ctx := context.Background()
	if v, err := s.RefValue(ctx, "shop", "secret", "db", "password"); err != nil || v != "s3cr3t!" {
		t.Fatalf("secrets are base64 on the wire: %q %v", v, err)
	}
	if v, err := s.RefValue(ctx, "shop", "configmap", "app-config", "theme"); err != nil || v != "dark" {
		t.Fatalf("%q %v", v, err)
	}
	if v, err := s.RefValue(ctx, "shop", "secret", "db", "big"); err != nil || !strings.HasSuffix(v, "cut at 64 KiB)") || len(v) > maxRefValue+40 {
		t.Fatalf("a huge value is cut, not returned whole: len=%d err=%v", len(v), err)
	}
}

func TestRefValueErrors(t *testing.T) {
	s := refServer(t)
	ctx := context.Background()
	if _, err := s.RefValue(ctx, "shop", "secret", "db", "nope"); err == nil || !strings.Contains(err.Error(), `no key "nope"`) {
		t.Fatalf("missing key: %v", err)
	}
	if _, err := s.RefValue(ctx, "shop", "configmap", "app-config", "blob"); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary key: %v", err)
	}
	if _, err := s.RefValue(ctx, "shop", "secret", "locked", "x"); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("the RBAC error must reach the user: %v", err)
	}
	if _, err := s.RefValue(ctx, "shop", "pods", "db", "x"); err == nil {
		t.Fatal("only secrets and configmaps can be read")
	}
}
