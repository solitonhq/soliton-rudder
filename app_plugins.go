package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"soliton-rudder/internal/appinfo"
	"soliton-rudder/internal/feasibility"
	"soliton-rudder/internal/kube"
	"soliton-rudder/internal/plugins"
)

// pluginHost adapts the App to the plugin broker's Host interface.
type pluginHost struct{ a *App }

func (h pluginHost) Clusters() ([]plugins.ClusterInfo, error) {
	list, err := h.a.clusters.List()
	if err != nil {
		return nil, err
	}
	out := make([]plugins.ClusterInfo, 0, len(list))
	for _, c := range list {
		out = append(out, plugins.ClusterInfo{ID: c.ID, Name: c.Name})
	}
	return out, nil
}

func (h pluginHost) Session(id string) (*kube.Session, error) { return h.a.kube.Ensure(id) }

func (h pluginHost) emit(event string, data any) {
	if h.a.ctx != nil {
		runtime.EventsEmit(h.a.ctx, event, data)
	}
}

func (h pluginHost) Toast(plugin, kind, text string) {
	h.emit("plugintoast", map[string]string{"plugin": plugin, "kind": kind, "text": text})
}

func (h pluginHost) Emit(plugin, name string, data any) {
	h.emit("pluginevent", map[string]any{"plugin": plugin, "name": name, "data": data})
}

func (a *App) initPlugins(configDir string) error {
	m, err := plugins.NewManager(filepath.Join(configDir, "plugins"), appinfo.Version, pluginHost{a}, func() {
		pluginHost{a}.emit("pluginschanged", nil)
	})
	if err != nil {
		return err
	}
	a.plugins = m
	a.reports.SetExtraRules(a.pluginRules)
	a.reports.SetConfigProvider(a.doctorConfig)
	a.reports.SetPromProvider(func(id string) (kube.PromConfig, bool) {
		c, ok, _ := a.integr.PromConfig(id)
		return c, ok
	})
	return nil
}

// pluginRules converts plugin-contributed assessment rules into report rule results.
func (a *App) pluginRules(ctx context.Context, clusterID string) []feasibility.RuleResult {
	outcomes := a.plugins.AssessRules(ctx, clusterID)
	out := make([]feasibility.RuleResult, 0, len(outcomes))
	for _, o := range outcomes {
		rr := feasibility.RuleResult{
			ID: "plugin:" + o.PluginID + ":" + o.Rule.ID, Title: o.Rule.Title, Category: feasibility.Category(o.Rule.Category),
			Description: o.Rule.Description, Remediation: o.Rule.Remediation, Checked: o.Checked, Findings: []feasibility.Finding{},
		}
		if rr.Description == "" {
			rr.Description = "Contributed by the plugin “" + o.PluginName + "”."
		} else {
			rr.Description += " (Plugin: " + o.PluginName + ")"
		}
		if o.Error != "" {
			rr.Skipped = o.Error
			out = append(out, rr)
			continue
		}
		rank := map[feasibility.Severity]int{feasibility.Info: 0, feasibility.Low: 1, feasibility.Medium: 2, feasibility.High: 3, feasibility.Critical: 4}
		for _, f := range o.Findings {
			sev := feasibility.Severity(f.Severity)
			if _, ok := rank[sev]; !ok {
				sev = feasibility.Medium
			}
			rr.Affected++
			if rank[sev] >= rank[rr.Severity] {
				rr.Severity = sev
			}
			if len(rr.Findings) < 100 {
				rr.Findings = append(rr.Findings, feasibility.Finding{Severity: sev, Kind: f.Kind, Namespace: f.Namespace, Name: f.Name, ResourceKey: f.ResourceKey, Message: f.Message})
			} else {
				rr.Truncated = true
			}
		}
		out = append(out, rr)
	}
	return out
}

// ---- Plugins API ----

func (a *App) ListPlugins() []plugins.PluginInfo { return a.plugins.List() }

func (a *App) InspectPlugin(path string) (plugins.Inspection, error) { return a.plugins.Inspect(path) }

func (a *App) InstallPlugin(path string, granted []string) (plugins.PluginInfo, error) {
	return a.plugins.Install(path, granted)
}

func (a *App) PickPluginFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select plugin folder"})
}

func (a *App) PickPluginZip() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select plugin archive",
		Filters: []runtime.FileFilter{{DisplayName: "Plugin archive (*.zip)", Pattern: "*.zip"}},
	})
}

func (a *App) UninstallPlugin(id string) error { return a.plugins.Uninstall(id) }

func (a *App) SetPluginEnabled(id string, enabled bool) error {
	return a.plugins.SetEnabled(id, enabled)
}

func (a *App) SetPluginPermissions(id string, granted []string) error {
	return a.plugins.SetGranted(id, granted)
}

func (a *App) RestartPlugin(id string) error { return a.plugins.Restart(id) }

func (a *App) GetPluginLogs(id string) []string { return a.plugins.Logs(id) }

// PluginUICall is the single entry point for plugin UI panels (via the iframe bridge).
func (a *App) PluginUICall(id, method, paramsJSON string) (string, error) {
	var params json.RawMessage
	if paramsJSON != "" {
		params = json.RawMessage(paramsJSON)
	}
	res, err := a.plugins.UICall(a.ctx, id, method, params)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("plugin returned unserialisable data: %w", err)
	}
	return string(b), nil
}

func (a *App) RunPluginCommand(id, command, contextJSON string) (string, error) {
	var c json.RawMessage
	if contextJSON != "" {
		c = json.RawMessage(contextJSON)
	}
	return a.plugins.RunCommand(a.ctx, id, command, c)
}
