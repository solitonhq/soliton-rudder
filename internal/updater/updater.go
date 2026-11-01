// Package updater checks GitHub Releases for a newer version. It only reads public release metadata
// (one HTTPS GET to api.github.com); it never downloads or installs anything by itself.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	URL         string `json:"url"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"publishedAt"`
}

type version struct {
	major, minor, patch int
	pre                 string
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+.*)?$`)

func parse(s string) (version, bool) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return version{}, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return version{a, b, c, m[4]}, true
}

// less reports whether a < b (a pre-release sorts before its release).
func less(a, b version) bool {
	switch {
	case a.major != b.major:
		return a.major < b.major
	case a.minor != b.minor:
		return a.minor < b.minor
	case a.patch != b.patch:
		return a.patch < b.patch
	case a.pre == b.pre:
		return false
	case a.pre == "":
		return false
	case b.pre == "":
		return true
	}
	return a.pre < b.pre
}

// Newer reports whether latest is a newer version than current.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	return ok1 && ok2 && less(c, l)
}

type release struct {
	Tag        string `json:"tag_name"`
	URL        string `json:"html_url"`
	Body       string `json:"body"`
	Published  string `json:"published_at"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// pick returns the newest published release. Pre-releases only count while no stable
// release exists, so a stable install is never offered a pre-release.
func pick(rels []release) (release, bool) {
	stable := false
	for _, r := range rels {
		if !r.Draft && !r.Prerelease {
			stable = true
		}
	}
	var best release
	found := false
	for _, r := range rels {
		if r.Draft || (stable && r.Prerelease) {
			continue
		}
		if _, ok := parse(r.Tag); !ok {
			continue
		}
		if !found || Newer(best.Tag, r.Tag) {
			best, found = r, true
		}
	}
	return best, found
}

// Check queries <apiBase>/repos/<repo>/releases (apiBase "" = GitHub). It lists releases rather
// than asking for /releases/latest, because GitHub answers 404 to that while only pre-releases exist.
func Check(ctx context.Context, apiBase, repo, current string) (Result, error) {
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	if repo == "" || strings.HasPrefix(repo, "OWNER/") {
		return Result{}, errors.New("update checks are disabled: this build has no repository configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+repo+"/releases?per_page=30", nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "soliton-rudder/"+current)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Result{Current: current, Latest: current}, nil // no release published yet
	case resp.StatusCode != http.StatusOK:
		return Result{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rels []release
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 8<<20)).Decode(&rels); err != nil {
		return Result{}, fmt.Errorf("unexpected response from GitHub: %w", err)
	}
	rel, ok := pick(rels)
	if !ok {
		return Result{Current: current, Latest: current}, nil
	}
	notes := rel.Body
	if len(notes) > 4000 {
		notes = notes[:4000] + "…"
	}
	return Result{
		Current: current, Latest: strings.TrimPrefix(rel.Tag, "v"), Available: Newer(current, rel.Tag),
		URL: rel.URL, Notes: notes, PublishedAt: rel.Published,
	}, nil
}
