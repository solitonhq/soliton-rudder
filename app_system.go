package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"soliton-rudder/internal/appinfo"
	"soliton-rudder/internal/updater"
)

const updateCheckEvery = 24 * time.Hour

type AppInfo struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Repo           string `json:"repo"`
	RepoConfigured bool   `json:"repoConfigured"`
	DocsURL        string `json:"docsURL"`
	IssuesURL      string `json:"issuesURL"`
	ReleasesURL    string `json:"releasesURL"`
	DataDir        string `json:"dataDir"`
}

func (a *App) dataDir() string { return filepath.Dir(a.dbPath) }

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name: appinfo.Name, Version: appinfo.Version, Repo: appinfo.Repo, RepoConfigured: appinfo.RepoConfigured(),
		DocsURL: appinfo.URL("/tree/main/docs"), IssuesURL: appinfo.URL("/issues"), ReleasesURL: appinfo.URL("/releases"),
		DataDir: a.dataDir(),
	}
}

// OpenExternal opens a link in the browser. Only this project's own GitHub pages are allowed, so the UI
// (or a plugin) can never use it to launch arbitrary URLs.
func (a *App) OpenExternal(url string) error {
	base := appinfo.URL("")
	if base == "" || !(url == base || strings.HasPrefix(url, base+"/")) {
		return fmt.Errorf("refusing to open %q", url)
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// CheckForUpdates looks for a newer release now (explicit user action; ignores the daily throttle).
func (a *App) CheckForUpdates() (updater.Result, error) {
	res, err := updater.Check(a.ctx, "", appinfo.Repo, appinfo.Version)
	if err == nil {
		_ = a.settings.SetLastUpdateCheck(time.Now().Unix())
	}
	return res, err
}

func (a *App) SetCheckUpdates(on bool) error { return a.settings.SetCheckUpdates(on) }

// autoCheckUpdates runs shortly after startup, at most once a day, only if enabled.
func (a *App) autoCheckUpdates() {
	if !appinfo.RepoConfigured() {
		return
	}
	go func() {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}
		s := a.settings.Get()
		if !s.CheckUpdates || time.Since(time.Unix(s.LastUpdateCheck, 0)) < updateCheckEvery {
			return
		}
		if res, err := a.CheckForUpdates(); err == nil && res.Available {
			runtime.EventsEmit(a.ctx, "updateavailable", res)
		}
	}()
}

// RevealDataDir shows the app's data folder in Finder.
func (a *App) RevealDataDir() error { return exec.Command("open", a.dataDir()).Run() }

// ClearAssessmentHistory deletes every saved assessment report.
func (a *App) ClearAssessmentHistory() error { return a.reports.ClearAll() }

// ResetSettings restores default preferences (clusters, plugins and history are untouched).
func (a *App) ResetSettings() error { return a.settings.Reset() }

// OpenNotices opens the bundled third-party license notices (release builds only).
func (a *App) OpenNotices() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := filepath.Join(filepath.Dir(exe), "..", "Resources", "THIRD_PARTY_NOTICES.txt")
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("the notices file is only bundled in release builds; see THIRD_PARTY_NOTICES.txt in the source repository")
	}
	return exec.Command("open", p).Run()
}
