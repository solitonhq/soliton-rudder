package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"0.1.0", "0.1.1", true}, {"0.1.0", "v0.2.0", true}, {"1.9.9", "2.0.0", true},
		{"0.1.0", "0.1.0", false}, {"0.2.0", "0.1.9", false},
		{"0.1.0-dev", "0.1.0", true},  // a dev build is older than its release
		{"0.1.0", "0.1.0-rc1", false}, // never "update" to a pre-release of the same version
		{"0.1.0", "garbage", false}, {"dev", "1.0.0", false},
		{"1.0.0+build5", "1.0.1", true},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func fake(t *testing.T, status int, body string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/repos/acme/rudder/releases/latest") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub requires a User-Agent")
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCheck(t *testing.T) {
	base := fake(t, 200, `{"tag_name":"v0.3.0","html_url":"https://github.com/acme/rudder/releases/tag/v0.3.0","body":"notes","published_at":"2026-01-01T00:00:00Z"}`)
	r, err := Check(context.Background(), base, "acme/rudder", "0.2.0")
	if err != nil || !r.Available || r.Latest != "0.3.0" || r.Notes != "notes" {
		t.Fatalf("%v %+v", err, r)
	}
	r, _ = Check(context.Background(), base, "acme/rudder", "0.3.0")
	if r.Available {
		t.Fatal("same version is not an update")
	}
}

func TestCheckEdgeCases(t *testing.T) {
	if r, err := Check(context.Background(), fake(t, 404, `{}`), "acme/rudder", "0.1.0"); err != nil || r.Available {
		t.Fatalf("no release yet must be quiet: %v %+v", err, r)
	}
	if r, _ := Check(context.Background(), fake(t, 200, `{"tag_name":"v9.0.0","prerelease":true}`), "acme/rudder", "0.1.0"); r.Available {
		t.Fatal("pre-releases are ignored")
	}
	if _, err := Check(context.Background(), fake(t, 500, `{}`), "acme/rudder", "0.1.0"); err == nil {
		t.Fatal("server errors surface")
	}
	if _, err := Check(context.Background(), fake(t, 200, `not json`), "acme/rudder", "0.1.0"); err == nil {
		t.Fatal("bad JSON surfaces")
	}
	if _, err := Check(context.Background(), "", "OWNER/"+"soliton-rudder", "0.1.0"); err == nil {
		t.Fatal("placeholder repo must disable checks")
	}
}
