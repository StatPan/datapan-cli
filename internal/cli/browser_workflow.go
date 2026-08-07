package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	"github.com/chromedp/chromedp"
)

const (
	dataGoKrBaseURL  = "https://www.data.go.kr"
	dataGoKrLoginURL = "https://www.data.go.kr/uim/login/loginView.do"
)

type browserWorkflowOptions struct {
	Command         string
	ListID          string
	ApplicationURL  string
	ProfileDir      string
	BrowserPath     string
	BrowserDebugURL string
	PurposeText     string
	ManualWait      time.Duration
	Headed          bool
	Apply           bool
	Output          string
	RegistryTrust   *registryTrustContext
	HTTPSession     *dataGoKrHTTPSession
}

func runBrowserWorkflow(opts browserWorkflowOptions, stdout, stderr io.Writer) int {
	if opts.ProfileDir == "" {
		opts.ProfileDir = defaultBrowserProfilePath
	}
	opts.ProfileDir = normalizeProfileDir(opts.ProfileDir)
	if opts.PurposeText == "" {
		opts.PurposeText = datago.PurposeTextKO
	}
	if opts.Command == "submit" && opts.Apply {
		compatibility := blockedDataGoKrPortalSubmissionCompatibility(opts.ApplicationURL)
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:                  false,
			Command:             opts.Command,
			Provider:            "data.go.kr",
			Status:              "portal_submission_disabled",
			ListID:              opts.ListID,
			ApplicationURL:      opts.ApplicationURL,
			Action:              "portal_submission_blocked_pending_form_contract",
			DetectedState:       compatibility.detectedState(),
			PortalCompatibility: &compatibility,
		}, opts)
	}
	if opts.HTTPSession != nil && opts.Command == "submit" && opts.Apply {
		return runHTTPSessionSubmit(opts, stdout)
	}
	if err := os.MkdirAll(opts.ProfileDir, 0o700); err != nil {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "profile_dir_error",
			Error:    err.Error(),
		}, opts)
	}

	ctx, cancel, err := newBrowserContext(opts)
	if err != nil {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "browser_start_error",
			Error:    err.Error(),
		}, opts)
	}
	defer cancel()

	switch opts.Command {
	case "login":
		return runBrowserLogin(ctx, opts, stdout)
	case "submit":
		return runBrowserSubmit(ctx, opts, stdout)
	default:
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "unknown_browser_workflow",
		}, opts)
	}
}

func runHTTPSessionSubmit(opts browserWorkflowOptions, stdout io.Writer) int {
	compatibility := blockedDataGoKrPortalSubmissionCompatibility(opts.ApplicationURL)
	return writeWorkflowResultForOptions(stdout, browserResult{
		OK:                  false,
		Command:             "submit",
		Provider:            "data.go.kr",
		Status:              "portal_submission_disabled",
		ListID:              opts.ListID,
		ApplicationURL:      opts.ApplicationURL,
		Action:              "portal_submission_blocked_pending_form_contract",
		DetectedState:       compatibility.detectedState(),
		PortalCompatibility: &compatibility,
	}, opts)
}

type browserResult struct {
	OK                  bool                         `json:"ok"`
	Command             string                       `json:"command"`
	Provider            string                       `json:"provider"`
	Status              string                       `json:"status"`
	ListID              string                       `json:"list_id,omitempty"`
	ApplicationURL      string                       `json:"application_url,omitempty"`
	ProfileDir          string                       `json:"profile_dir,omitempty"`
	LoginConfirmed      bool                         `json:"login_confirmed,omitempty"`
	HumanGateDetected   bool                         `json:"human_gate_detected,omitempty"`
	DryRun              bool                         `json:"dry_run,omitempty"`
	DetectedState       map[string]any               `json:"detected_state,omitempty"`
	Action              string                       `json:"action,omitempty"`
	ApplyResult         map[string]any               `json:"apply_result,omitempty"`
	PortalCompatibility *dataGoKrPortalCompatibility `json:"portal_compatibility,omitempty"`
	URL                 string                       `json:"url,omitempty"`
	Error               string                       `json:"error,omitempty"`
	RegistryTrust       *registryTrustContext        `json:"registry_trust,omitempty"`
}

func newBrowserContext(opts browserWorkflowOptions) (context.Context, context.CancelFunc, error) {
	if strings.TrimSpace(opts.BrowserDebugURL) != "" {
		allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), strings.TrimSpace(opts.BrowserDebugURL))
		ctx, ctxCancel := chromedp.NewContext(allocCtx)
		cancel := func() {
			ctxCancel()
			allocCancel()
		}
		if err := chromedp.Run(ctx); err != nil {
			cancel()
			return nil, nil, err
		}
		return ctx, cancel, nil
	}
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(opts.ProfileDir),
		chromedp.Flag("headless", !opts.Headed),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.WindowSize(1280, 900),
	)
	if opts.BrowserPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(opts.BrowserPath))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	cancel := func() {
		ctxCancel()
		allocCancel()
	}
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, nil, err
	}
	return ctx, cancel, nil
}

func runBrowserLogin(ctx context.Context, opts browserWorkflowOptions, stdout io.Writer) int {
	wait := opts.ManualWait
	deadline := time.Now().Add(wait)
	var body, currentURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataGoKrLoginURL),
		chromedp.Sleep(1*time.Second),
	); err != nil {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  "login",
			Provider: "data.go.kr",
			Status:   "navigation_error",
			Error:    err.Error(),
		}, opts)
	}

	confirmed := false
	for {
		_ = chromedp.Run(ctx,
			chromedp.Location(&currentURL),
			chromedp.Text("body", &body, chromedp.ByQuery),
		)
		if isLoginConfirmed(currentURL, body) {
			confirmed = true
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(1 * time.Second)
	}
	return writeWorkflowResultForOptions(stdout, browserResult{
		OK:                confirmed,
		Command:           "login",
		Provider:          "data.go.kr",
		Status:            ternary(confirmed, "session_ready", "manual_login_timeout"),
		ProfileDir:        opts.ProfileDir,
		LoginConfirmed:    confirmed,
		HumanGateDetected: hasHumanGate(body),
		URL:               currentURL,
	}, opts)
}

func runBrowserSubmit(ctx context.Context, opts browserWorkflowOptions, stdout io.Writer) int {
	if opts.Apply {
		compatibility := blockedDataGoKrPortalSubmissionCompatibility(opts.ApplicationURL)
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK: false, Command: "submit", Provider: "data.go.kr", Status: "portal_submission_disabled",
			ListID: opts.ListID, ApplicationURL: opts.ApplicationURL,
			Action: "portal_submission_blocked_pending_form_contract", DetectedState: compatibility.detectedState(),
			PortalCompatibility: &compatibility,
		}, opts)
	}
	var body, currentURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataGoKrBaseURL),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  "submit",
			Provider: "data.go.kr",
			Status:   "navigation_error",
			Error:    err.Error(),
		}, opts)
	}
	if !isLoginConfirmed(currentURL, body) {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:                false,
			Command:           "submit",
			Provider:          "data.go.kr",
			Status:            "session_expired_or_login_required",
			ListID:            opts.ListID,
			ApplicationURL:    opts.ApplicationURL,
			ProfileDir:        opts.ProfileDir,
			LoginConfirmed:    false,
			HumanGateDetected: hasHumanGate(body),
			URL:               currentURL,
		}, opts)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(opts.ApplicationURL),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return writeWorkflowResultForOptions(stdout, browserResult{
			OK:       false,
			Command:  "submit",
			Provider: "data.go.kr",
			Status:   "application_navigation_error",
			Error:    err.Error(),
		}, opts)
	}
	controls := inspectApplicationControls(ctx)
	compatibility := inspectDataGoKrPortalCompatibility(currentURL, body, controls)
	detected := compatibility.detectedState()
	detected["apply_controls"] = controls
	result := browserResult{
		OK:                  compatibility.inspectionOK(),
		Command:             "submit",
		Provider:            "data.go.kr",
		Status:              "inspected",
		ListID:              opts.ListID,
		ApplicationURL:      opts.ApplicationURL,
		ProfileDir:          opts.ProfileDir,
		LoginConfirmed:      true,
		DryRun:              !opts.Apply,
		DetectedState:       detected,
		Action:              "dry_run_inspection",
		PortalCompatibility: &compatibility,
		URL:                 redactedDataGoKrPortalURL(currentURL),
	}
	if !opts.Apply {
		return writeWorkflowResultForOptions(stdout, result, opts)
	}
	if detected["status"] != "access_user_action_required" {
		if detected["status"] == "access_requested_not_confirmed" {
			result.Action = "access_already_requested"
		} else {
			result.Action = "not_submitted"
		}
		return writeWorkflowResultForOptions(stdout, result, opts)
	}
	applyResult := submitApplication(ctx, opts.ListID, opts.PurposeText, opts.BrowserDebugURL)
	result.Action = fmt.Sprint(applyResult["action"])
	result.ApplyResult = applyResult
	return writeWorkflowResultForOptions(stdout, result, opts)
}

func inspectApplicationControls(ctx context.Context) []map[string]string {
	var controls []map[string]string
	script := `(() => Array.from(document.querySelectorAll("a,button,input[type=button],input[type=submit]"))
  .filter((el) => el.offsetParent !== null)
  .map((el) => ({
    text: ((el.innerText || el.textContent || el.value || "") + "").trim().replace(/\s+/g, " ").slice(0, 120),
    href: el.href || "",
    id: el.id || "",
    name: el.name || "",
    class: el.className || "",
    onclick: (el.getAttribute("onclick") || "").slice(0, 240)
  }))
  .filter((item) => item.text.includes("활용신청") || item.text === "신청")
  .slice(0, 20))()`
	_ = chromedp.Run(ctx, chromedp.Evaluate(script, &controls, chromedp.EvalAsValue))
	return controls
}

func submitApplication(ctx context.Context, listID, purposeText, browserDebugURL string) map[string]any {
	// Keep the lower-level browser helper fail-closed as well. This prevents a
	// future caller from bypassing runBrowserWorkflow's submission guard.
	compatibility := blockedDataGoKrPortalSubmissionCompatibility("")
	return map[string]any{
		"action":               "portal_submission_blocked_pending_form_contract",
		"portal_compatibility": compatibility,
	}
}

func inspectSubmitControls(ctx context.Context) []map[string]string {
	var controls []map[string]string
	script := `(() => Array.from(document.querySelectorAll("button,input[type=button],input[type=submit],a.button"))
  .filter((el) => el.offsetParent !== null)
  .map((el) => ({
    text: ((el.innerText || el.textContent || el.value || "") + "").trim().replace(/\s+/g, " ").slice(0, 120),
    type: el.getAttribute("type") || "",
    id: el.id || "",
    name: el.name || "",
    class: el.className || "",
    onclick: (el.getAttribute("onclick") || "").slice(0, 240)
  }))
  .filter((item) => item.text)
  .slice(0, 40))()`
	_ = chromedp.Run(ctx, chromedp.Evaluate(script, &controls, chromedp.EvalAsValue))
	return controls
}

func inspectApplicationFormFields(ctx context.Context) []map[string]any {
	var fields []map[string]any
	script := `(() => Array.from(document.querySelectorAll("input,select,textarea"))
  .filter((el) => el.offsetParent !== null && (el.getAttribute("type") || "").toLowerCase() !== "hidden")
  .map((el) => {
    const id = el.id || "";
    const explicit = id ? document.querySelector('label[for="' + CSS.escape(id) + '"]') : null;
    const wrapped = el.closest("label");
    const container = el.closest("tr,li,div.form-group,div.row,div") || el.parentElement;
    const label = ((explicit?.innerText || wrapped?.innerText || container?.querySelector("th,label,.label,.tit")?.innerText || "") + "")
      .trim().replace(/\s+/g, " ").slice(0, 160);
    return {
      tag: el.tagName.toLowerCase(),
      type: (el.getAttribute("type") || "").toLowerCase(),
      name: el.name || "",
      id,
      label,
      placeholder: el.getAttribute("placeholder") || "",
      required: !!el.required || el.getAttribute("aria-required") === "true",
      checked: ["checkbox","radio"].includes((el.getAttribute("type") || "").toLowerCase()) ? !!el.checked : undefined,
      options: el.tagName === "SELECT" ? Array.from(el.options).map((o) => (o.textContent || "").trim()).filter(Boolean).slice(0, 30) : undefined
    };
  }).slice(0, 80))()`
	_ = chromedp.Run(ctx, chromedp.Evaluate(script, &fields, chromedp.EvalAsValue))
	return fields
}

func inspectValidationMessages(ctx context.Context) []string {
	var messages []string
	script := `(() => Array.from(document.querySelectorAll(".error,.invalid-feedback,.help-block,.text-danger,[role=alert]"))
  .filter((el) => el.offsetParent !== null)
  .map((el) => (el.innerText || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 240))
  .filter(Boolean).slice(0, 30))()`
	_ = chromedp.Run(ctx, chromedp.Evaluate(script, &messages, chromedp.EvalAsValue))
	return messages
}

func writeWorkflowResult(stdout io.Writer, result browserResult, output string) int {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stdout, `{"ok":false,"status":"json_error","error":%q}`+"\n", err.Error())
		return exitRequest
	}
	if output != "" {
		_ = os.MkdirAll(filepath.Dir(output), 0o755)
		_ = os.WriteFile(output, data, 0o600)
	}
	_, _ = stdout.Write(append(data, '\n'))
	if result.OK {
		return exitOK
	}
	return exitRequest
}

func writeWorkflowResultForOptions(stdout io.Writer, result browserResult, opts browserWorkflowOptions) int {
	// Browser navigation can add transient portal query values. Receipts keep
	// only the trusted origin and path; the registry remains the source for the
	// canonical application URL.
	result.ApplicationURL = redactedDataGoKrPortalURL(result.ApplicationURL)
	result.URL = redactedDataGoKrPortalURL(result.URL)
	result.RegistryTrust = opts.RegistryTrust
	return writeWorkflowResult(stdout, result, opts.Output)
}

func isLoginConfirmed(currentURL, pageText string) bool {
	if strings.Contains(currentURL, "auth.data.go.kr") {
		return false
	}
	return strings.Contains(pageText, "로그아웃") || strings.Contains(pageText, "마이페이지") || strings.Contains(pageText, "My Page")
}

func hasHumanGate(pageText string) bool {
	for _, term := range []string{"보안문자", "자동입력", "본인인증", "휴대폰 인증", "아이핀", "공동인증서", "captcha", "CAPTCHA"} {
		if strings.Contains(pageText, term) {
			return true
		}
	}
	return false
}

func normalizeProfileDir(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func ternary[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
