package feasibility

import (
	"bytes"
	"html/template"
	"time"
)

var reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"date": func(ts int64) string { return time.Unix(ts, 0).Format("2006-01-02 15:04 MST") },
	"tone": func(score int) string {
		switch {
		case score >= 85:
			return "good"
		case score >= 60:
			return "warn"
		}
		return "bad"
	},
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Assessment — {{.ClusterName}}</title>
<style>
body{font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#16181d;margin:32px auto;max-width:960px;padding:0 20px}
h1{font-size:22px;margin:0}h2{font-size:16px;margin:28px 0 8px;border-bottom:1px solid #dcdee4;padding-bottom:4px}
.muted{color:#626875}.score{font-size:44px;font-weight:700}.good{color:#1f9d63}.warn{color:#b97d0e}.bad{color:#d1403a}
.row{display:flex;gap:24px;flex-wrap:wrap;align-items:center;margin-top:12px}
.cat{border:1px solid #dcdee4;border-radius:8px;padding:8px 14px;min-width:130px}.cat b{font-size:20px}
.chip{display:inline-block;border-radius:10px;padding:0 8px;font-size:11px;font-weight:600;text-transform:uppercase;color:#fff}
.critical{background:#8f1d17}.high{background:#d1403a}.medium{background:#b97d0e}.low{background:#2f6fe0}.info{background:#8b95a5}
table{border-collapse:collapse;width:100%;margin-top:6px}td,th{text-align:left;padding:4px 8px;border-bottom:1px solid #eceef2;vertical-align:top;font-size:13px}
.rule{margin:14px 0;page-break-inside:avoid}.fix{background:#f0f4ff;border-radius:6px;padding:6px 10px;margin:6px 0;font-size:13px}
@media print{body{margin:0}}
</style></head><body>
<h1>Cluster assessment — {{.ClusterName}}</h1>
<div class="muted">{{date .StartedAt}} · Kubernetes {{.ServerVersion}} · {{.Stats.Nodes}} nodes, {{.Stats.Namespaces}} namespaces, {{.Stats.Pods}} pods · {{.Stats.RulesRun}} rules</div>
<div class="row"><div class="score {{tone .Score}}">{{.Score}}<span class="muted" style="font-size:18px"> / 100</span></div>
{{range .Categories}}<div class="cat"><div class="muted" style="text-transform:capitalize">{{.Category}}</div><b class="{{tone .Score}}">{{.Score}}</b> <span class="muted">· {{.Findings}} findings</span></div>{{end}}</div>
<div class="row muted">Critical {{.Counts.Critical}} · High {{.Counts.High}} · Medium {{.Counts.Medium}} · Low {{.Counts.Low}} · Info {{.Counts.Info}}</div>
<h2>Findings</h2>
{{range .Rules}}{{if .Affected}}<div class="rule"><span class="chip {{.Severity}}">{{.Severity}}</span> <b>{{.Title}}</b> <span class="muted">· {{.Affected}} of {{.Checked}} affected · {{.Category}}</span>
<div class="muted">{{.Description}}</div><div class="fix"><b>Fix:</b> {{.Remediation}}</div>
<table>{{range .Findings}}<tr><td style="width:90px"><span class="chip {{.Severity}}">{{.Severity}}</span></td><td>{{.Kind}}{{if .Name}}/{{if .Namespace}}{{.Namespace}}/{{end}}{{.Name}}{{end}}</td><td>{{.Message}}</td></tr>{{end}}</table>
{{if .Truncated}}<div class="muted">Showing the first {{len .Findings}} of {{.Affected}}.</div>{{end}}</div>{{end}}{{end}}
<h2>Passed checks</h2><div class="muted">{{range .Rules}}{{if and (not .Affected) (not .Skipped)}}✓ {{.Title}}<br>{{end}}{{end}}</div>
{{if .Notes}}<h2>Notes</h2><ul class="muted">{{range .Notes}}<li>{{.}}</li>{{end}}</ul>{{end}}
</body></html>`))

// ExportHTML renders a self-contained, print-friendly report (use the browser's "Save as PDF").
func ExportHTML(r Report) (string, error) {
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, r); err != nil {
		return "", err
	}
	return buf.String(), nil
}
