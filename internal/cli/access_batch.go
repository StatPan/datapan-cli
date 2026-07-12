package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const (
	approvalPlanSchemaVersion  = "datapan.approval-plan.v1"
	approvalApplySchemaVersion = "datapan.approval-apply.v1"
	defaultApprovalPlanPath    = ".datapan/data-go-kr-approval-plan.json"
	defaultApprovalApplyPath   = ".datapan/data-go-kr-approval-apply.json"
)

type approvalPlan struct {
	SchemaVersion string               `json:"schema_version"`
	GeneratedAt   string               `json:"generated_at"`
	Provider      string               `json:"provider"`
	Input         string               `json:"input"`
	DryRun        bool                 `json:"dry_run"`
	RegistryTrust registryTrustContext `json:"registry_trust"`
	Summary       approvalPlanSummary  `json:"summary"`
	Items         []approvalPlanItem   `json:"items"`
}

type approvalPlanSummary struct {
	Total               int `json:"total"`
	ApplicationRequired int `json:"application_required"`
	RequestedOrGranted  int `json:"requested_or_granted"`
	HumanGate           int `json:"human_gate"`
	Unknown             int `json:"unknown"`
	InspectionFailed    int `json:"inspection_failed"`
}

type approvalPlanItem struct {
	ListID            string         `json:"list_id"`
	Title             string         `json:"title"`
	ApplicationURL    string         `json:"application_url"`
	Status            string         `json:"status"`
	HumanGateDetected bool           `json:"human_gate_detected"`
	Action            string         `json:"action"`
	DetectedState     map[string]any `json:"detected_state,omitempty"`
	Error             string         `json:"error,omitempty"`
}

type approvalApplyReport struct {
	SchemaVersion string                `json:"schema_version"`
	GeneratedAt   string                `json:"generated_at"`
	Provider      string                `json:"provider"`
	Plan          string                `json:"plan"`
	Limit         int                   `json:"limit"`
	RegistryTrust registryTrustContext  `json:"registry_trust"`
	Summary       approvalApplySummary  `json:"summary"`
	Results       []approvalApplyResult `json:"results"`
}

type approvalApplySummary struct {
	Eligible  int `json:"eligible"`
	Attempted int `json:"attempted"`
	Submitted int `json:"submitted"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type approvalApplyResult struct {
	ListID            string `json:"list_id"`
	Status            string `json:"status"`
	Action            string `json:"action"`
	HumanGateDetected bool   `json:"human_gate_detected"`
	Error             string `json:"error,omitempty"`
}

func (a app) accessPlan(args []string, jsonOut bool) int {
	input, args, err := consumeString(args, "--input", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", defaultApprovalPlanPath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 0)
	if err != nil || limit < 0 {
		return a.fail(exitUsage, "--limit requires a non-negative integer")
	}
	profileDir, browserPath, debugURL, args, err := consumeAccessBrowserOptions(a, args)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if input == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan access plan --input VERIFICATION [--output PATH] [--limit N] [--profile-dir PATH] [--browser-path PATH] [--browser-debug-url URL] [--json]")
	}
	report, err := readVerificationReport(input)
	if err != nil {
		return a.fail(exitUsage, "read verification report: %v", err)
	}
	trust := a.localRegistryTrust()
	if !trust.ExecutionAllowed {
		return a.rejectBlockedRegistryExecution(jsonOut, trust)
	}
	ids := approvalCandidateIDs(report.Results, limit)
	plan := approvalPlan{
		SchemaVersion: approvalPlanSchemaVersion,
		GeneratedAt:   time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Provider:      "data.go.kr",
		Input:         input,
		DryRun:        true,
		RegistryTrust: trust,
		Items:         make([]approvalPlanItem, 0, len(ids)),
	}
	for _, id := range ids {
		spec, code, ok := a.resolveOne(id, true)
		if !ok {
			plan.Items = append(plan.Items, approvalPlanItem{ListID: id, Status: "inspection_failed", Action: "not_inspected", Error: fmt.Sprintf("resolve exit %d", code)})
			continue
		}
		result, err := invokeBrowserWorkflow(browserWorkflowOptions{
			Command: "submit", ListID: spec.ID, ApplicationURL: spec.ApplicationURL(), ProfileDir: profileDir,
			BrowserPath: browserPath, BrowserDebugURL: debugURL, PurposeText: "", Apply: false, RegistryTrust: &trust,
		})
		item := approvalPlanItem{ListID: spec.ID, Title: spec.Title, ApplicationURL: spec.ApplicationURL(), Status: resultStatus(result), Action: result.Action, DetectedState: result.DetectedState, HumanGateDetected: result.HumanGateDetected}
		if err != nil {
			item.Status, item.Action, item.Error = "inspection_failed", "not_inspected", err.Error()
		}
		plan.Items = append(plan.Items, item)
	}
	plan.Summary = summarizeApprovalPlan(plan.Items)
	if err := writeAtomicJSON(output, plan); err != nil {
		return a.fail(exitRequest, "write approval plan: %v", err)
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{"ok": plan.Summary.InspectionFailed == 0, "output": output, "plan": plan}); code != exitOK {
			return code
		}
		if plan.Summary.InspectionFailed > 0 {
			return exitRequest
		}
		return exitOK
	}
	fmt.Fprintf(a.stdout, "Approval plan: total=%d required=%d requested=%d human_gate=%d failed=%d output=%s\n", plan.Summary.Total, plan.Summary.ApplicationRequired, plan.Summary.RequestedOrGranted, plan.Summary.HumanGate, plan.Summary.InspectionFailed, output)
	return exitOK
}

func (a app) accessApplyPlan(args []string, jsonOut bool) int {
	planPath, args, err := consumeString(args, "--plan", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", defaultApprovalApplyPath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 0)
	if err != nil || limit <= 0 {
		return a.fail(exitUsage, "--limit requires a positive integer for approval submission")
	}
	profileDir, browserPath, debugURL, args, err := consumeAccessBrowserOptions(a, args)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if planPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan access apply --plan PLAN --limit N [--output PATH] [--profile-dir PATH] [--browser-path PATH] [--browser-debug-url URL] [--json]")
	}
	plan, err := readApprovalPlan(planPath)
	if err != nil {
		return a.fail(exitUsage, "read approval plan: %v", err)
	}
	trust := a.localRegistryTrust()
	if !trust.ExecutionAllowed {
		return a.rejectBlockedRegistryExecution(jsonOut, trust)
	}
	report := approvalApplyReport{SchemaVersion: approvalApplySchemaVersion, GeneratedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Provider: "data.go.kr", Plan: planPath, Limit: limit, RegistryTrust: trust}
	for _, item := range plan.Items {
		if item.Status != "access_user_action_required" || report.Summary.Attempted >= limit {
			report.Summary.Skipped++
			continue
		}
		report.Summary.Eligible++
		spec, _, ok := a.resolveOne(item.ListID, true)
		if !ok {
			report.Summary.Failed++
			report.Results = append(report.Results, approvalApplyResult{ListID: item.ListID, Status: "resolve_failed", Action: "not_submitted"})
			continue
		}
		result, invokeErr := invokeBrowserWorkflow(browserWorkflowOptions{
			Command: "submit", ListID: spec.ID, ApplicationURL: spec.ApplicationURL(), ProfileDir: profileDir,
			BrowserPath: browserPath, BrowserDebugURL: debugURL, PurposeText: "", Apply: true, RegistryTrust: &trust,
		})
		report.Summary.Attempted++
		entry := approvalApplyResult{ListID: item.ListID, Status: result.Status, Action: result.Action, HumanGateDetected: result.HumanGateDetected}
		if invokeErr != nil {
			entry.Status, entry.Action, entry.Error = "apply_failed", "not_submitted", invokeErr.Error()
			report.Summary.Failed++
		} else if result.Action == "apply_submitted_review_required" || result.Action == "access_requested_not_confirmed" {
			report.Summary.Submitted++
		} else {
			report.Summary.Failed++
		}
		report.Results = append(report.Results, entry)
	}
	if err := writeAtomicJSON(output, report); err != nil {
		return a.fail(exitRequest, "write approval apply report: %v", err)
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{"ok": report.Summary.Failed == 0, "output": output, "report": report}); code != exitOK {
			return code
		}
		if report.Summary.Failed > 0 {
			return exitRequest
		}
		return exitOK
	}
	fmt.Fprintf(a.stdout, "Approval apply: attempted=%d submitted=%d failed=%d skipped=%d output=%s\n", report.Summary.Attempted, report.Summary.Submitted, report.Summary.Failed, report.Summary.Skipped, output)
	if report.Summary.Failed > 0 {
		return exitRequest
	}
	return exitOK
}

func consumeAccessBrowserOptions(a app, args []string) (string, string, string, []string, error) {
	profileDir, args, err := consumeString(args, "--profile-dir", defaultBrowserProfilePath)
	if err != nil {
		return "", "", "", nil, err
	}
	browserPath, args, err := consumeString(args, "--browser-path", "")
	if err != nil {
		return "", "", "", nil, err
	}
	debugURL, args, err := consumeString(args, "--browser-debug-url", "")
	if err != nil {
		return "", "", "", nil, err
	}
	if browserPath == "" {
		if value, ok := a.env.LookupEnv("DATAPAN_BROWSER_PATH"); ok {
			browserPath = strings.TrimSpace(value)
		}
	}
	if debugURL == "" {
		if value, ok := a.env.LookupEnv("DATAPAN_BROWSER_DEBUG_URL"); ok {
			debugURL = strings.TrimSpace(value)
		}
	}
	return profileDir, browserPath, debugURL, args, nil
}

func approvalCandidateIDs(results []datago.VerificationResult, limit int) []string {
	seen := map[string]bool{}
	var ids []string
	for _, result := range results {
		if result.HTTPStatus != 403 || strings.TrimSpace(result.DatasetID) == "" || seen[result.DatasetID] {
			continue
		}
		seen[result.DatasetID] = true
		ids = append(ids, result.DatasetID)
		if limit > 0 && len(ids) >= limit {
			break
		}
	}
	return ids
}

func invokeBrowserWorkflow(opts browserWorkflowOptions) (browserResult, error) {
	var stdout, stderr bytes.Buffer
	code := runBrowserWorkflowFunc(opts, &stdout, &stderr)
	var result browserResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		return result, fmt.Errorf("decode browser result: %w", err)
	}
	if code != exitOK || !result.OK {
		return result, fmt.Errorf("browser workflow status %s", result.Status)
	}
	return result, nil
}

func resultStatus(result browserResult) string {
	if result.DetectedState != nil {
		if status, ok := result.DetectedState["status"].(string); ok && status != "" {
			return status
		}
	}
	if result.Status != "" {
		return result.Status
	}
	return "unknown"
}

func summarizeApprovalPlan(items []approvalPlanItem) approvalPlanSummary {
	summary := approvalPlanSummary{Total: len(items)}
	for _, item := range items {
		switch item.Status {
		case "access_user_action_required":
			summary.ApplicationRequired++
		case "access_requested_not_confirmed":
			summary.RequestedOrGranted++
		case "human_gate":
			summary.HumanGate++
		case "inspection_failed":
			summary.InspectionFailed++
		default:
			summary.Unknown++
		}
	}
	return summary
}

func readApprovalPlan(path string) (approvalPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return approvalPlan{}, err
	}
	var plan approvalPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return approvalPlan{}, err
	}
	if plan.SchemaVersion != approvalPlanSchemaVersion || plan.Provider != "data.go.kr" || !plan.DryRun {
		return approvalPlan{}, fmt.Errorf("unsupported or unsafe approval plan")
	}
	return plan, nil
}

func writeAtomicJSON(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
