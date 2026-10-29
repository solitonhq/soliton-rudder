package kube

import (
	"fmt"
	"regexp"
	"strings"
)

// AppLinkTemplate is a user-defined link shown on every application, for example a CI job or a dashboard:
// "https://ci.example.com/job/{release}". It is the tool-neutral way to point at whatever deploys or watches an app.
type AppLinkTemplate struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

const (
	maxAppLinkTemplates = 20
	maxAppLinkTitle     = 60
	maxAppLinkURL       = 500
)

var (
	linkPlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)
	linkVars        = map[string]bool{"name": true, "namespace": true, "release": true, "version": true}
)

// ValidateAppLinkTemplates checks and cleans user-supplied templates: web addresses only, known placeholders only.
func ValidateAppLinkTemplates(in []AppLinkTemplate) ([]AppLinkTemplate, error) {
	if len(in) > maxAppLinkTemplates {
		return nil, fmt.Errorf("at most %d links are allowed", maxAppLinkTemplates)
	}
	out := make([]AppLinkTemplate, 0, len(in))
	for i, l := range in {
		l.Title, l.URL = strings.TrimSpace(l.Title), strings.TrimSpace(l.URL)
		if l.Title == "" && l.URL == "" {
			continue
		}
		where := fmt.Sprintf("link %d", i+1)
		switch {
		case l.Title == "":
			return nil, fmt.Errorf("%s needs a title", where)
		case len(l.Title) > maxAppLinkTitle:
			return nil, fmt.Errorf("%s: the title is too long", where)
		case len(l.URL) > maxAppLinkURL:
			return nil, fmt.Errorf("%s: the address is too long", where)
		}
		lower := strings.ToLower(l.URL)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			return nil, fmt.Errorf("%s (%s): the address must start with http:// or https://", where, l.Title)
		}
		for _, m := range linkPlaceholder.FindAllStringSubmatch(l.URL, -1) {
			if !linkVars[m[1]] {
				return nil, fmt.Errorf("%s (%s): {%s} is not a known placeholder (use {name}, {namespace}, {release} or {version})", where, l.Title, m[1])
			}
		}
		if strings.ContainsAny(linkPlaceholder.ReplaceAllString(l.URL, ""), "{}") {
			return nil, fmt.Errorf("%s (%s): unbalanced braces in the address", where, l.Title)
		}
		out = append(out, l)
	}
	return out, nil
}
