// Package appinfo holds build-time identity: version and the GitHub repository.
// Release builds override both with -ldflags (see scripts/build-dmg.sh).
package appinfo

import "strings"

var (
	// Version is the semantic version of this build.
	Version = "0.1.0-dev"
	// Repo is the GitHub "owner/name". The placeholder disables update checks and hides repository links.
	Repo = "solitonhq/soliton-rudder"
)

const Name = "Soliton Rudder"

func RepoConfigured() bool { return Repo != "" && !strings.HasPrefix(Repo, "OWNER/") }

// URL returns a URL inside the repository ("" if the repo is not configured).
func URL(path string) string {
	if !RepoConfigured() {
		return ""
	}
	return "https://github.com/" + Repo + path
}
