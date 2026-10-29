package integrations

import (
	"path/filepath"
	"testing"

	"soliton-rudder/internal/kube"
)

func TestPromConfigRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok, err := s.PromConfig("c1"); ok || err != nil {
		t.Fatalf("expected unconfigured: %v %v", ok, err)
	}
	cfg := kube.PromConfig{Namespace: "monitoring", Service: "prom", Port: "9090", Scheme: "http"}
	if err := s.SetPromConfig("c1", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Port = "9091"
	if err := s.SetPromConfig("c1", cfg); err != nil { // upsert
		t.Fatal(err)
	}
	got, ok, _ := s.PromConfig("c1")
	if !ok || got.Port != "9091" || got.Service != "prom" {
		t.Fatalf("%+v", got)
	}
	if _, ok, _ := s.PromConfig("c2"); ok {
		t.Fatal("config leaked across clusters")
	}
	if err := s.DeleteCluster("c1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.PromConfig("c1"); ok {
		t.Fatal("not deleted")
	}
	_ = s.SetPromConfig("c3", cfg)
	_ = s.ClearPromConfig("c3")
	if _, ok, _ := s.PromConfig("c3"); ok {
		t.Fatal("not cleared")
	}
}
