package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StatPan/datapan-cli/internal/datago"
)

func TestApprovalCandidateIDsSelectsUniqueForbiddenDatasets(t *testing.T) {
	results := []datago.VerificationResult{
		{DatasetID: "100", HTTPStatus: 403},
		{DatasetID: "100", HTTPStatus: 403},
		{DatasetID: "200", HTTPStatus: 200},
		{DatasetID: "300", HTTPStatus: 403},
	}
	got := approvalCandidateIDs(results, 0)
	if strings.Join(got, ",") != "100,300" {
		t.Fatalf("unexpected candidates: %#v", got)
	}
	if limited := approvalCandidateIDs(results, 1); len(limited) != 1 || limited[0] != "100" {
		t.Fatalf("unexpected limited candidates: %#v", limited)
	}
}

func TestAccessPlanIsReadOnlyAndDeduplicatesDatasets(t *testing.T) {
	t.Chdir(t.TempDir())
	registry := `[
  {"id":"100","title":"첫 API","provider":"data.go.kr","priority":"P1","operations":[]},
  {"id":"200","title":"둘 API","provider":"data.go.kr","priority":"P1","operations":[]}
]`
	writeRegistryInstallStateForTest(t, defaultRegistryPath, registry, "v1")
	verification := datago.VerificationReport{Provider: "data.go.kr", Results: []datago.VerificationResult{
		{DatasetID: "100", HTTPStatus: 403}, {DatasetID: "100", HTTPStatus: 403}, {DatasetID: "200", HTTPStatus: 403},
	}}
	input := filepath.Join(t.TempDir(), "verification.json")
	if err := writeJSONFile(input, verification); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "plan.json")
	original := runBrowserWorkflowFunc
	calls := 0
	runBrowserWorkflowFunc = func(opts browserWorkflowOptions, stdout, _ io.Writer) int {
		calls++
		if opts.Apply {
			t.Fatal("planning must never apply")
		}
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK: true, Command: "submit", Provider: "data.go.kr", Status: "inspected", DryRun: true,
			Action: "dry_run_inspection", DetectedState: map[string]any{"status": "access_user_action_required"},
		}, opts)
	}
	defer func() { runBrowserWorkflowFunc = original }()

	code, stdout, stderr := runTest([]string{"access", "plan", "--input", input, "--output", output, "--browser-debug-url", "ws://127.0.0.1/test", "--json"}, nil, nil)
	if code != exitOK || stderr != "" || calls != 2 {
		t.Fatalf("code=%d calls=%d stdout=%s stderr=%s", code, calls, stdout, stderr)
	}
	plan, err := readApprovalPlan(output)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Total != 2 || plan.Summary.ApplicationRequired != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if strings.Contains(stdout, "ws://127.0.0.1/test") {
		t.Fatalf("debugger URL leaked: %s", stdout)
	}
}

func TestAccessApplyPlanRequiresPositiveLimitAndBoundsSubmissions(t *testing.T) {
	t.Chdir(t.TempDir())
	registry := `[
  {"id":"100","title":"첫 API","provider":"data.go.kr","priority":"P1","operations":[]},
  {"id":"200","title":"둘 API","provider":"data.go.kr","priority":"P1","operations":[]}
]`
	writeRegistryInstallStateForTest(t, defaultRegistryPath, registry, "v1")
	planPath := filepath.Join(t.TempDir(), "plan.json")
	plan := approvalPlan{SchemaVersion: approvalPlanSchemaVersion, Provider: "data.go.kr", DryRun: true, Items: []approvalPlanItem{
		{ListID: "100", Status: "access_user_action_required"},
		{ListID: "200", Status: "access_user_action_required"},
	}}
	if err := writeAtomicJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}

	code, _, _ := runTest([]string{"access", "apply", "--plan", planPath, "--json"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("apply without limit code=%d", code)
	}
	original := runBrowserWorkflowFunc
	calls := 0
	runBrowserWorkflowFunc = func(opts browserWorkflowOptions, stdout, _ io.Writer) int {
		calls++
		if !opts.Apply {
			t.Fatal("apply workflow did not set Apply")
		}
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK: true, Command: "submit", Provider: "data.go.kr", Status: "inspected",
			Action: "access_requested_not_confirmed",
		}, opts)
	}
	defer func() { runBrowserWorkflowFunc = original }()
	output := filepath.Join(t.TempDir(), "apply.json")
	code, stdout, stderr := runTest([]string{"access", "apply", "--plan", planPath, "--limit", "1", "--output", output, "--json"}, nil, nil)
	if code != exitOK || stderr != "" || calls != 1 {
		t.Fatalf("code=%d calls=%d stdout=%s stderr=%s", code, calls, stdout, stderr)
	}
	if !strings.Contains(stdout, `"attempted": 1`) || !strings.Contains(stdout, `"submitted": 1`) {
		t.Fatalf("unexpected apply output: %s", stdout)
	}
}
