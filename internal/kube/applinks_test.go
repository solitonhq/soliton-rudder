package kube

import (
	"strings"
	"testing"
)

func TestValidateAppLinkTemplates(t *testing.T) {
	ok, err := ValidateAppLinkTemplates([]AppLinkTemplate{
		{" CI ", " https://ci.example.com/job/{release}/ "}, {"", ""}, {"Dash", "http://g.example.com/d/{namespace}?v={version}&n={name}"},
	})
	if err != nil || len(ok) != 2 || ok[0] != (AppLinkTemplate{"CI", "https://ci.example.com/job/{release}/"}) {
		t.Fatalf("%v %+v", err, ok)
	}
	bad := map[string][]AppLinkTemplate{
		"scheme":      {{"x", "javascript:alert(1)"}},
		"ftp":         {{"x", "ftp://a/b"}},
		"no scheme":   {{"x", "ci.example.com"}},
		"placeholder": {{"x", "https://a/{cluster}"}},
		"braces":      {{"x", "https://a/{name"}},
		"no title":    {{"", "https://a"}},
		"long title":  {{strings.Repeat("a", 61), "https://a"}},
		"long url":    {{"x", "https://" + strings.Repeat("a", 500)}},
	}
	for name, in := range bad {
		if _, err := ValidateAppLinkTemplates(in); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
	many := make([]AppLinkTemplate, 21)
	for i := range many {
		many[i] = AppLinkTemplate{"t", "https://a"}
	}
	if _, err := ValidateAppLinkTemplates(many); err == nil {
		t.Error("too many")
	}
}

func TestApplicationsWorkForAUserWithLimitedAccess(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	got, err := s.Applications(ctx5(t), false)
	if err != nil {
		t.Fatalf("the cluster-wide secrets list is forbidden, but the namespaces are readable: %v", err)
	}
	for _, n := range got.Notes {
		if strings.Contains(n, "Helm releases could not be read") {
			t.Errorf("Helm releases are read per namespace for this user: %v", got.Notes)
		}
	}
}
