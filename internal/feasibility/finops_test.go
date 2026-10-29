package feasibility

import (
	"math"
	"testing"

	"soliton-rudder/internal/kube"
)

func finopsPod(name, ns, owner string, cpu, mem string) string {
	res := ""
	if cpu != "" {
		res = "resources: {requests: {cpu: " + cpu + ", memory: " + mem + "}}"
	}
	own := ""
	if owner != "" {
		own = ", ownerReferences: [{kind: ReplicaSet, name: " + owner + "}]"
	}
	return "metadata: {name: " + name + ", namespace: " + ns + own + "}\nspec: {containers: [{name: c, " + res + "}]}\nstatus: {phase: Running}"
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.011 }

func TestFinOpsCostAndSavingsMath(t *testing.T) {
	s := snap(t, "v1.36.0", map[string][]string{
		"pods": {
			finopsPod("api-1", "shop", "api-rs", "1000m", "1Gi"),
			finopsPod("api-2", "shop", "api-rs", "1000m", "1Gi"),
			finopsPod("batch-1", "data", "", "500m", "512Mi"),
		},
		"replicasets": {"metadata: {name: api-rs, namespace: shop, ownerReferences: [{kind: Deployment, name: api}]}"},
	})
	s.PromUsage = map[string]kube.Usage{
		"shop/api-1": {CPU: 100, Mem: 256 << 20}, "shop/api-2": {CPU: 100, Mem: 256 << 20},
		"data/batch-1": {CPU: 500, Mem: 500 << 20}, // right-sized already
	}
	rep := Evaluate(s)
	f := rep.FinOps
	if f == nil || f.Confidence != "high" || len(f.Namespaces) != 2 {
		t.Fatalf("%+v", f)
	}
	cfg := DefaultConfig()
	perPodCost := (1*cfg.CPUHourUSD + 1*cfg.MemGBHourUSD) * hoursPerMonth
	rc, rm := RightSize(100, 256<<20, 20)
	wantMem := 256 << 20
	if rc != 120 || rm != int64(float64(wantMem)*1.2) {
		t.Fatalf("right-size: %d %d", rc, rm)
	}
	perPodSave := (float64(1000-rc)/1000*cfg.CPUHourUSD + float64((1<<30)-rm)/gb*cfg.MemGBHourUSD) * hoursPerMonth
	shop := f.Namespaces[0]
	if shop.Namespace != "shop" || shop.Pods != 2 || !near(shop.MonthlyCostUSD, 2*perPodCost) || !near(shop.PotentialSaveUSD, 2*perPodSave) {
		t.Fatalf("shop: %+v (want cost %.2f save %.2f)", shop, 2*perPodCost, 2*perPodSave)
	}
	data := f.Namespaces[1]
	if data.PotentialSaveUSD > 5 { // 500m used of 500m requested, memory ~ 500/512: almost nothing to save
		t.Fatalf("a right-sized namespace shows no meaningful saving: %+v", data)
	}
	if len(f.Top) == 0 || f.Top[0].Name != "api" || f.Top[0].Kind != "Deployment" || f.Top[0].Pods != 2 || f.Top[0].ResourceKey != "apps/v1/deployments" {
		t.Fatalf("pods roll up to their Deployment: %+v", f.Top)
	}
	if !near(f.MonthlyCostUSD, shop.MonthlyCostUSD+data.MonthlyCostUSD) || !near(f.PotentialSaveUSD, shop.PotentialSaveUSD+data.PotentialSaveUSD) {
		t.Fatalf("totals: %+v", f)
	}
}

func TestFinOpsConfidenceLevelsAndEdgeCases(t *testing.T) {
	pods := []string{finopsPod("a", "app", "", "1000m", "1Gi"), finopsPod("sys", "kube-system", "", "1000m", "1Gi"), finopsPod("free", "app", "", "", "")}
	base := func() *Snapshot { return snap(t, "v1.36.0", map[string][]string{"pods": pods}) }

	none := Evaluate(base()).FinOps
	if none.Confidence != "none" || none.PotentialSaveUSD != 0 || len(none.Top) != 0 || none.MonthlyCostUSD <= 0 {
		t.Fatalf("without usage data only request-based cost is known: %+v", none)
	}
	low := base()
	low.Usage = map[string]kube.Usage{"app/a": {CPU: 50, Mem: 100 << 20}, "app/free": {CPU: 200, Mem: 300 << 20}, "kube-system/sys": {CPU: 1, Mem: 1 << 20}}
	lf := Evaluate(low).FinOps
	if lf.Confidence != "low" || len(lf.Notes) < 2 {
		t.Fatalf("%+v", lf)
	}
	var app, sys NSCost
	for _, n := range lf.Namespaces {
		if n.Namespace == "app" {
			app = n
		} else {
			sys = n
		}
	}
	if app.UnrequestedPods != 1 || app.PotentialSaveUSD <= 0 {
		t.Fatalf("a pod without requests is costed from its usage and counted: %+v", app)
	}
	if sys.PotentialSaveUSD != 0 || sys.MonthlyCostUSD <= 0 {
		t.Fatalf("system namespaces cost money but are never right-sized: %+v", sys)
	}
	high := base()
	high.Usage = low.Usage
	high.PromUsage = map[string]kube.Usage{"app/a": {CPU: 900, Mem: 900 << 20}}
	if hf := Evaluate(high).FinOps; hf.Confidence != "high" || hf.PotentialSaveUSD >= lf.PotentialSaveUSD {
		t.Fatalf("Prometheus p95 takes precedence over the single sample: %+v vs %+v", hf, lf)
	}
}

func TestFinOpsUsesConfiguredRates(t *testing.T) {
	s := snap(t, "v1.36.0", map[string][]string{"pods": {finopsPod("a", "app", "", "2000m", "4Gi")}})
	cheap := EvaluateWith(s, Config{CPUHourUSD: 0.01, MemGBHourUSD: 0.001, HeadroomPct: 20}).FinOps
	pricey := EvaluateWith(s, Config{CPUHourUSD: 0.10, MemGBHourUSD: 0.01, HeadroomPct: 20}).FinOps
	if !near(cheap.MonthlyCostUSD, (2*0.01+4*0.001)*hoursPerMonth) || !near(pricey.MonthlyCostUSD, 10*cheap.MonthlyCostUSD) {
		t.Fatalf("%v %v", cheap.MonthlyCostUSD, pricey.MonthlyCostUSD)
	}
	if bad := (Config{CPUHourUSD: -1, HeadroomPct: 9999}).Normalize(); bad.CPUHourUSD <= 0 || bad.HeadroomPct != 20 {
		t.Fatalf("invalid values fall back to defaults: %+v", bad)
	}
}
