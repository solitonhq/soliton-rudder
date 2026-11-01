package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

func relObj(t *testing.T, kind, name, uid string, owners ...string) unstructured.Unstructured {
	o := ""
	for _, ow := range owners {
		o += fmt.Sprintf("{uid: %s, kind: Owner, name: x},", ow)
	}
	return u(t, fmt.Sprintf(`{apiVersion: v1, kind: %s, metadata: {name: %s, namespace: shop, uid: %s, ownerReferences: [%s]}}`, kind, name, uid, o))
}

func relEvent(t *testing.T, kind, name, uid string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`{apiVersion: v1, kind: Event, metadata: {name: e-%s}, involvedObject: {kind: %s, name: %s, uid: "%s"}}`, name, kind, name, uid))
}

func TestRelatedFollowsOwnershipNotNamePrefixes(t *testing.T) {
	web := relObj(t, "Deployment", "web", "d-web")
	cands := []unstructured.Unstructured{
		relObj(t, "Pod", "web-aaa-1", "p1", "rs-web"), // listed before its ReplicaSet: order must not matter
		relObj(t, "ReplicaSet", "web-aaa", "rs-web", "d-web"),
		relObj(t, "ReplicaSet", "web-api-bbb", "rs-api", "d-api"), // another deployment whose name starts with "web-"
		relObj(t, "Pod", "web-api-bbb-1", "p2", "rs-api"),
	}
	r := relatedTo(web, cands)
	for _, want := range []unstructured.Unstructured{
		relEvent(t, "Deployment", "web", "d-web"),
		relEvent(t, "ReplicaSet", "web-aaa", "rs-web"),
		relEvent(t, "Pod", "web-aaa-1", "p1"),
	} {
		if !r.matches(want) {
			t.Errorf("%v should belong to web", want.Object["involvedObject"])
		}
	}
	for _, not := range []unstructured.Unstructured{
		relEvent(t, "ReplicaSet", "web-api-bbb", "rs-api"),
		relEvent(t, "Pod", "web-api-bbb-1", "p2"),
		relEvent(t, "Service", "web", "s1"), // same name, other kind
	} {
		if r.matches(not) {
			t.Errorf("%v must not belong to web (name prefixes are not ownership)", not.Object["involvedObject"])
		}
	}
}

func TestRelatedKeepsEventsOfPodsThatAreAlreadyGone(t *testing.T) {
	web := relObj(t, "Deployment", "web", "d-web")
	r := relatedTo(web, []unstructured.Unstructured{relObj(t, "ReplicaSet", "web-aaa", "rs-web", "d-web")})
	// the pod was replaced and no longer exists, but its Events live for a while: its name starts with the ReplicaSet's
	if !r.matches(relEvent(t, "Pod", "web-aaa-zzzzz", "gone")) {
		t.Error("an event of a deleted pod of this ReplicaSet should be kept")
	}
	if r.matches(relEvent(t, "Pod", "web-aaaa-zzzzz", "other")) {
		t.Error("web-aaaa is a different ReplicaSet than web-aaa")
	}
}

func TestRelatedStatefulSetOrdinalsAndCronJobChain(t *testing.T) {
	db := relObj(t, "StatefulSet", "db", "s-db")
	r := relatedTo(db, nil)
	if !r.matches(relEvent(t, "Pod", "db-0", "")) || !r.matches(relEvent(t, "Pod", "db-12", "")) {
		t.Error("ordinal pods of the StatefulSet should match even when not listed")
	}
	if r.matches(relEvent(t, "Pod", "db-backup-0", "")) || r.matches(relEvent(t, "Pod", "db-0-x", "")) {
		t.Error("db-backup-0 is not an ordinal pod of db")
	}

	cron := relObj(t, "CronJob", "nightly", "c1")
	cr := relatedTo(cron, []unstructured.Unstructured{
		relObj(t, "Pod", "nightly-28-x", "p", "j1"),
		relObj(t, "Job", "nightly-28", "j1", "c1"),
	})
	if !cr.matches(relEvent(t, "Job", "nightly-28", "j1")) || !cr.matches(relEvent(t, "Pod", "nightly-28-x", "p")) {
		t.Error("CronJob -> Job -> Pod chain should be followed")
	}
}

func TestTimelineOfADeploymentUsesOwnershipAndSurvivesUnreadableLists(t *testing.T) {
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/apps/v1/namespaces/shop/deployments/web", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web", "uid": "d-web"}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/shop/replicasets", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "ReplicaSetList", "apiVersion": "apps/v1", "items": []any{
			map[string]any{"kind": "ReplicaSet", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web-aaa", "uid": "rs-web", "ownerReferences": []any{map[string]any{"uid": "d-web", "kind": "Deployment", "name": "web"}}}},
		}})
	})
	mux.HandleFunc("/api/v1/namespaces/shop/pods", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{}})
	})
	// jobs are forbidden for this user: the timeline must still work
	mux.HandleFunc("/apis/batch/v1/namespaces/shop/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "Forbidden", "code": 403})
	})
	ev := func(name, kind, obj, reason, last string) map[string]any {
		return map[string]any{"kind": "Event", "apiVersion": "v1", "metadata": map[string]any{"name": name},
			"involvedObject": map[string]any{"kind": kind, "name": obj}, "type": "Normal", "reason": reason, "lastTimestamp": last}
	}
	mux.HandleFunc("/api/v1/namespaces/shop/events", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "EventList", "apiVersion": "v1", "items": []any{
			ev("e1", "Deployment", "web", "ScalingReplicaSet", "2026-01-01T00:00:03Z"),
			ev("e2", "ReplicaSet", "web-aaa", "SuccessfulCreate", "2026-01-01T00:00:02Z"),
			ev("e3", "Pod", "web-aaa-gone", "Killing", "2026-01-01T00:00:01Z"),
			ev("e4", "ReplicaSet", "web-api-bbb", "SuccessfulCreate", "2026-01-01T00:00:09Z"), // another deployment
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.catalog = map[string]Resource{"apps/v1/deployments": {Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}}

	got, err := s.Timeline(context.Background(), "apps/v1/deployments", "shop", "web")
	if err != nil {
		t.Fatal(err)
	}
	var reasons []string
	for _, e := range got {
		reasons = append(reasons, e.Reason)
	}
	want := []string{"ScalingReplicaSet", "SuccessfulCreate", "Killing"} // newest first; web-api-bbb is excluded
	if fmt.Sprint(reasons) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", reasons, want)
	}
}
