package feasibility

import (
	"encoding/json"
	"fmt"
	"testing"
)

// The UI calls .map/.filter/.length on every array in a report; a JSON null there blanks the whole view.
func assertNoNulls(t *testing.T, label string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	_ = json.Unmarshal(raw, &generic)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if e == nil {
					t.Errorf("%s: %s.%s is null", label, path, k)
				}
				walk(path+"."+k, e)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		}
	}
	walk("$", generic)
}

func TestReportJSONHasNoNulls(t *testing.T) {
	empty := snap(t, "v1.36.0", nil)
	assertNoNulls(t, "empty cluster", Evaluate(empty))

	withMissing := snap(t, "", map[string][]string{"deployments": {goodDeploy, badDeploy}})
	withMissing.Missing["poddisruptionbudgets"] = "forbidden"
	withMissing.UsageErr = "metrics unavailable"
	assertNoNulls(t, "partial cluster", Evaluate(withMissing))

	full := snap(t, "v1.36.0", map[string][]string{"deployments": {goodDeploy, badDeploy}})
	assertNoNulls(t, "with workloads", Evaluate(full))
}

func TestPluginRulesWithoutFindingsDoNotProduceNulls(t *testing.T) {
	rep := Evaluate(snap(t, "v1.36.0", nil))
	AppendRules(&rep, []RuleResult{{ID: "plugin-x", Title: "X", Category: CatSecurity}, {ID: "plugin-y", Title: "Y", Skipped: "no access"}})
	assertNoNulls(t, "with plugin rules", rep)
}
