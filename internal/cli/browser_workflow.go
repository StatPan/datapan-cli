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
	Command        string
	ListID         string
	ApplicationURL string
	ProfileDir     string
	PurposeText    string
	ManualWait     time.Duration
	Headed         bool
	Apply          bool
	Output         string
}

func runBrowserWorkflow(opts browserWorkflowOptions, stdout, stderr io.Writer) int {
	if opts.ProfileDir == "" {
		opts.ProfileDir = defaultBrowserProfilePath
	}
	opts.ProfileDir = normalizeProfileDir(opts.ProfileDir)
	if opts.PurposeText == "" {
		opts.PurposeText = datago.PurposeTextKO
	}
	if err := os.MkdirAll(opts.ProfileDir, 0o700); err != nil {
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "profile_dir_error",
			Error:    err.Error(),
		}, opts.Output)
	}

	ctx, cancel, err := newBrowserContext(opts)
	if err != nil {
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "browser_start_error",
			Error:    err.Error(),
		}, opts.Output)
	}
	defer cancel()

	switch opts.Command {
	case "login":
		return runBrowserLogin(ctx, opts, stdout)
	case "submit":
		return runBrowserSubmit(ctx, opts, stdout)
	default:
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  opts.Command,
			Provider: "data.go.kr",
			Status:   "unknown_browser_workflow",
		}, opts.Output)
	}
}

type browserResult struct {
	OK                bool           `json:"ok"`
	Command           string         `json:"command"`
	Provider          string         `json:"provider"`
	Status            string         `json:"status"`
	ListID            string         `json:"list_id,omitempty"`
	ApplicationURL    string         `json:"application_url,omitempty"`
	ProfileDir        string         `json:"profile_dir,omitempty"`
	LoginConfirmed    bool           `json:"login_confirmed,omitempty"`
	HumanGateDetected bool           `json:"human_gate_detected,omitempty"`
	DryRun            bool           `json:"dry_run,omitempty"`
	DetectedState     map[string]any `json:"detected_state,omitempty"`
	Action            string         `json:"action,omitempty"`
	ApplyResult       map[string]any `json:"apply_result,omitempty"`
	URL               string         `json:"url,omitempty"`
	Error             string         `json:"error,omitempty"`
}

func newBrowserContext(opts browserWorkflowOptions) (context.Context, context.CancelFunc, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(opts.ProfileDir),
		chromedp.Flag("headless", !opts.Headed),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.WindowSize(1280, 900),
	)
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
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  "login",
			Provider: "data.go.kr",
			Status:   "navigation_error",
			Error:    err.Error(),
		}, opts.Output)
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
	return writeWorkflowResult(stdout, browserResult{
		OK:                confirmed,
		Command:           "login",
		Provider:          "data.go.kr",
		Status:            ternary(confirmed, "session_ready", "manual_login_timeout"),
		ProfileDir:        opts.ProfileDir,
		LoginConfirmed:    confirmed,
		HumanGateDetected: hasHumanGate(body),
		URL:               currentURL,
	}, opts.Output)
}

func runBrowserSubmit(ctx context.Context, opts browserWorkflowOptions, stdout io.Writer) int {
	var body, currentURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataGoKrBaseURL),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  "submit",
			Provider: "data.go.kr",
			Status:   "navigation_error",
			Error:    err.Error(),
		}, opts.Output)
	}
	if !isLoginConfirmed(currentURL, body) {
		return writeWorkflowResult(stdout, browserResult{
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
		}, opts.Output)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(opts.ApplicationURL),
		chromedp.Sleep(1*time.Second),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return writeWorkflowResult(stdout, browserResult{
			OK:       false,
			Command:  "submit",
			Provider: "data.go.kr",
			Status:   "application_navigation_error",
			Error:    err.Error(),
		}, opts.Output)
	}
	detected := detectApplicationState(body)
	result := browserResult{
		OK:             true,
		Command:        "submit",
		Provider:       "data.go.kr",
		Status:         "inspected",
		ListID:         opts.ListID,
		ApplicationURL: opts.ApplicationURL,
		ProfileDir:     opts.ProfileDir,
		LoginConfirmed: true,
		DryRun:         !opts.Apply,
		DetectedState:  detected,
		Action:         "dry_run_inspection",
		URL:            currentURL,
	}
	if !opts.Apply {
		return writeWorkflowResult(stdout, result, opts.Output)
	}
	if detected["status"] != "access_user_action_required" {
		result.Action = "not_submitted"
		return writeWorkflowResult(stdout, result, opts.Output)
	}
	applyResult := submitApplication(ctx, opts.PurposeText)
	result.Action = fmt.Sprint(applyResult["action"])
	result.ApplyResult = applyResult
	return writeWorkflowResult(stdout, result, opts.Output)
}

func submitApplication(ctx context.Context, purposeText string) map[string]any {
	clicked, err := clickFirst(ctx, []string{
		"활용신청",
	})
	if err != nil {
		return map[string]any{"action": "apply_control_error", "error": err.Error()}
	}
	if !clicked {
		return map[string]any{"action": "apply_control_not_found"}
	}
	_ = chromedp.Run(ctx, chromedp.Sleep(1*time.Second))

	var body string
	_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery))
	if hasHumanGate(body) {
		return map[string]any{"action": "access_user_action_required"}
	}
	if looksRequestedOrGranted(body) {
		return map[string]any{"action": "access_requested_not_confirmed"}
	}

	filled := fillApplicationForm(ctx, purposeText)
	clicked, err = clickFirst(ctx, []string{
		"신청",
		"등록",
		"저장",
		"확인",
	})
	if err != nil {
		return map[string]any{"action": "apply_form_submit_control_error", "error": err.Error(), "filled": filled}
	}
	if !clicked {
		return map[string]any{"action": "apply_form_submit_control_not_found", "filled": filled}
	}
	_ = chromedp.Run(ctx, chromedp.Sleep(1*time.Second), chromedp.Text("body", &body, chromedp.ByQuery))
	return map[string]any{"action": classifyApplyResult(body), "filled": filled}
}

func fillApplicationForm(ctx context.Context, purposeText string) map[string]int {
	filled := map[string]int{"textarea": 0, "text_input": 0, "checkbox": 0}
	var counts map[string]int
	purposeJSON, _ := json.Marshal(purposeText)
	script := fmt.Sprintf(`(() => {
  const purpose = %s;
  const isPurposeField = (el) => {
    const label = [el.name, el.id, el.placeholder, el.title].filter(Boolean).join(" ").toLowerCase();
    return ["활용","목적","사용","내용","사유","비고","설명","purpose","reason","use","usage","cont"].some((term) => label.includes(term));
  };
  const counts = {textarea: 0, text_input: 0, checkbox: 0};
  for (const el of Array.from(document.querySelectorAll("textarea"))) {
    if (el.offsetParent !== null && !el.value.trim()) {
      el.value = purpose;
      el.dispatchEvent(new Event("input", {bubbles: true}));
      counts.textarea++;
    }
  }
  for (const el of Array.from(document.querySelectorAll("input"))) {
    const type = (el.getAttribute("type") || "text").toLowerCase();
    if (el.offsetParent !== null && ["text","search"].includes(type) && isPurposeField(el) && !el.value.trim()) {
      el.value = purpose;
      el.dispatchEvent(new Event("input", {bubbles: true}));
      counts.text_input++;
    }
    if (el.offsetParent !== null && type === "checkbox" && !el.checked) {
      el.click();
      counts.checkbox++;
    }
  }
  return counts;
})()`, string(purposeJSON))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &counts, chromedp.EvalAsValue)); err == nil {
		for key, value := range counts {
			filled[key] = value
		}
	}
	return filled
}

func clickFirst(ctx context.Context, labels []string) (bool, error) {
	labelsJSON, _ := json.Marshal(labels)
	script := fmt.Sprintf(`(() => {
  const labels = %s;
  const controls = Array.from(document.querySelectorAll("button,a,input[type=button],input[type=submit]"));
  for (const label of labels) {
    for (const el of controls) {
      const text = ((el.innerText || el.textContent || el.value || "") + "").trim();
      if (label === "신청" && text.includes("활용신청")) {
        continue;
      }
      if ((text === label || text.includes(label)) && el.offsetParent !== null) {
        el.click();
        return true;
      }
    }
  }
  return false;
})()`, string(labelsJSON))
	var clicked bool
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &clicked, chromedp.EvalAsValue))
	return clicked, err
}

func detectApplicationState(pageText string) map[string]any {
	markers := map[string]any{
		"has_apply_text":      strings.Contains(pageText, "활용신청"),
		"has_cancel_text":     strings.Contains(pageText, "신청취소"),
		"has_approved_text":   strings.Contains(pageText, "승인"),
		"has_login_text":      strings.Contains(pageText, "로그인"),
		"human_gate_detected": hasHumanGate(pageText),
	}
	switch {
	case markers["human_gate_detected"].(bool):
		markers["status"] = "human_gate"
	case markers["has_cancel_text"].(bool):
		markers["status"] = "access_requested_not_confirmed"
	case markers["has_apply_text"].(bool) && markers["has_approved_text"].(bool):
		markers["status"] = "ambiguous_manual_review"
	case markers["has_approved_text"].(bool):
		markers["status"] = "access_requested_not_confirmed"
	case markers["has_apply_text"].(bool):
		markers["status"] = "access_user_action_required"
	case markers["has_login_text"].(bool):
		markers["status"] = "not_logged_in_or_session_expired"
	default:
		markers["status"] = "unknown"
	}
	return markers
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

func looksRequestedOrGranted(pageText string) bool {
	for _, term := range []string{"신청취소", "이미 신청", "승인", "활용중", "사용중"} {
		if strings.Contains(pageText, term) {
			return true
		}
	}
	return false
}

func classifyApplyResult(pageText string) string {
	if hasHumanGate(pageText) {
		return "access_user_action_required"
	}
	for _, term := range []string{"신청완료", "신청되었습니다", "승인대기"} {
		if strings.Contains(pageText, term) {
			return "access_requested_not_confirmed"
		}
	}
	if looksRequestedOrGranted(pageText) {
		return "access_requested_not_confirmed"
	}
	if strings.Contains(pageText, "필수") || strings.Contains(pageText, "입력") {
		return "access_user_action_required"
	}
	return "apply_submitted_review_required"
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
