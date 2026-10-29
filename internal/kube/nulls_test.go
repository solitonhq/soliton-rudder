package kube

import (
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The UI maps over every array it receives; Go encodes a nil slice as null and the view then throws.
func assertNoNilSlices(t *testing.T, label string, v any) {
	t.Helper()
	var walk func(path string, rv reflect.Value)
	walk = func(path string, rv reflect.Value) {
		switch rv.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !rv.IsNil() {
				walk(path, rv.Elem())
			}
		case reflect.Slice:
			if rv.IsNil() {
				t.Errorf("%s: %s is a nil slice (JSON null)", label, path)
				return
			}
			for i := 0; i < rv.Len(); i++ {
				walk(path+"[]", rv.Index(i))
			}
		case reflect.Map:
			for _, k := range rv.MapKeys() {
				walk(path+"{}", rv.MapIndex(k))
			}
		case reflect.Struct:
			for i := 0; i < rv.NumField(); i++ {
				if rv.Type().Field(i).IsExported() {
					walk(path+"."+rv.Type().Field(i).Name, rv.Field(i))
				}
			}
		}
	}
	walk("$", reflect.ValueOf(v))
}

func TestNoNilSlicesInUIPayloads(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	minimal := func(kind string) unstructured.Unstructured {
		return u(t, "{apiVersion: v1, kind: "+kind+", metadata: {name: x, namespace: a}}")
	}

	app := minimal("Application")
	assertNoNilSlices(t, "ArgoApp", parseArgoApp(&app))

	assertNoNilSlices(t, "HelmRelease", releaseFrom(helmMeta{name: "r", ns: "a", revision: 1}, nil))

	fl := minimal("Kustomization")
	assertNoNilSlices(t, "FluxObject", parseFlux(Resource{Group: "kustomize.toolkit.fluxcd.io", Name: "kustomizations", Kind: "Kustomization"}, &fl))

	for _, kind := range []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "Node", "Service", "PersistentVolumeClaim", "Job", "CronJob", "Ingress"} {
		o := minimal(kind)
		assertNoNilSlices(t, "Health "+kind, buildHealth(map[string]string{"Pod": "pods", "Deployment": "deployments", "StatefulSet": "statefulsets", "DaemonSet": "daemonsets", "Node": "nodes", "Service": "services", "PersistentVolumeClaim": "persistentvolumeclaims", "Job": "jobs", "CronJob": "cronjobs", "Ingress": "ingresses"}[kind], &o))
	}

	assertNoNilSlices(t, "CertReport (empty)", buildCertReport(nil, nil, nil, now))
	cm := minimal("ConfigMap")
	assertNoNilSlices(t, "ConfigData configmap", buildConfigData(false, &cm, now))
	sec := minimal("Secret")
	assertNoNilSlices(t, "ConfigData secret", buildConfigData(true, &sec, now))

	assertNoNilSlices(t, "NetResult", simulateNetwork(netInput{}, NetQuery{From: NetEndpoint{Kind: "Pod", Namespace: "a", Name: "x"}, To: NetEndpoint{Kind: "Pod", Namespace: "a", Name: "y"}}))
	assertNoNilSlices(t, "Topology", buildTopology(topoInput{}))
	assertNoNilSlices(t, "DriftGroup", compareSets("x", "k", nil, nil))
	assertNoNilSlices(t, "FormatsTargets", SimTargets{Namespaces: []string{}, Pods: []string{}, Services: []string{}, Nodes: []string{}})
	r, _ := simulateDrain(drainInput{Nodes: []unstructured.Unstructured{u(t, "{apiVersion: v1, kind: Node, metadata: {name: n1}}")}}, "n1")
	assertNoNilSlices(t, "DrainResult", r)
}
