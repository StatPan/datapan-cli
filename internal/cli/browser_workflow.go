package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	"github.com/chromedp/cdproto/page"
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
	applyResult := opts.HTTPSession.apply(opts.ListID, opts.PurposeText)
	action := fmt.Sprint(applyResult["action"])
	status := "inspected"
	ok := true
	if action == "session_expired_or_login_required" {
		status, ok = action, false
	}
	return writeWorkflowResultForOptions(stdout, browserResult{
		OK:             ok,
		Command:        "submit",
		Provider:       "data.go.kr",
		Status:         status,
		ListID:         opts.ListID,
		ApplicationURL: opts.ApplicationURL,
		LoginConfirmed: ok,
		Action:         action,
		ApplyResult:    applyResult,
	}, opts)
}

type browserResult struct {
	OK                bool                  `json:"ok"`
	Command           string                `json:"command"`
	Provider          string                `json:"provider"`
	Status            string                `json:"status"`
	ListID            string                `json:"list_id,omitempty"`
	ApplicationURL    string                `json:"application_url,omitempty"`
	ProfileDir        string                `json:"profile_dir,omitempty"`
	LoginConfirmed    bool                  `json:"login_confirmed,omitempty"`
	HumanGateDetected bool                  `json:"human_gate_detected,omitempty"`
	DryRun            bool                  `json:"dry_run,omitempty"`
	DetectedState     map[string]any        `json:"detected_state,omitempty"`
	Action            string                `json:"action,omitempty"`
	ApplyResult       map[string]any        `json:"apply_result,omitempty"`
	URL               string                `json:"url,omitempty"`
	Error             string                `json:"error,omitempty"`
	RegistryTrust     *registryTrustContext `json:"registry_trust,omitempty"`
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
	detected := detectApplicationState(body)
	detected["apply_controls"] = inspectApplicationControls(ctx)
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
	formURL := dataGoKrBaseURL + "/tcs/dss/redirectDevAcountRequestForm.do?publicDataPk=" + url.QueryEscape(strings.TrimSpace(listID))
	var body, currentURL string
	knownTargets := browserPageTargetIDs(browserDebugURL)
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, err := page.Navigate(formURL).Do(ctx)
			return err
		}),
	); err != nil {
		return map[string]any{"action": "apply_form_navigation_error", "error": err.Error()}
	}
	if resultURL := waitForNewDataGoKrResultURL(browserDebugURL, knownTargets, 8*time.Second); resultURL != "" {
		if action := classifyApplyResultAtURL(resultURL, ""); action == "access_already_requested" {
			return confirmedApplyResult(action, resultURL, nil)
		}
	}
	if err := chromedp.Run(ctx,
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return map[string]any{"action": "apply_form_inspection_error", "error": err.Error()}
	}
	if strings.Contains(currentURL, "/uim/login/") {
		return map[string]any{"action": "session_expired_or_login_required", "url": currentURL}
	}
	if hasHumanGate(body) {
		return map[string]any{"action": "access_user_action_required", "url": currentURL}
	}
	if looksRequestedOrGranted(body) {
		return map[string]any{"action": "access_requested_not_confirmed", "url": currentURL}
	}

	filled := fillApplicationForm(ctx, purposeText)
	acceptApplyConfirmationDialogs(ctx)
	clicked, err := clickFirst(ctx, []string{
		"활용신청",
		"신청",
		"등록",
		"저장",
		"확인",
	})
	if err != nil {
		return map[string]any{"action": "apply_form_submit_control_error", "error": err.Error(), "filled": filled}
	}
	if !clicked {
		return map[string]any{"action": "apply_form_submit_control_not_found", "filled": filled, "controls": inspectSubmitControls(ctx), "url": currentURL}
	}
	if resultURL := waitForNewDataGoKrResultURL(browserDebugURL, knownTargets, 8*time.Second); resultURL != "" {
		action := classifyApplyResultAtURL(resultURL, "")
		if action == "access_already_requested" {
			return confirmedApplyResult(action, resultURL, filled)
		}
	}
	_ = chromedp.Run(ctx,
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	)
	action := classifyApplyResultAtURL(currentURL, body)
	result := map[string]any{
		"action":         action,
		"filled":         filled,
		"detected_state": detectApplicationState(body),
		"url":            currentURL,
	}
	if action == "access_user_action_required" || action == "apply_result_unconfirmed" {
		result["form_fields"] = inspectApplicationFormFields(ctx)
		result["validation_messages"] = inspectValidationMessages(ctx)
	}
	return result
}

func confirmedApplyResult(action, resultURL string, filled any) map[string]any {
	return map[string]any{
		"action": action,
		"filled": filled,
		"detected_state": map[string]any{
			"status": "access_requested_not_confirmed",
		},
		"url": resultURL,
	}
}

type browserPageTarget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

func browserPageTargets(browserDebugURL string) []browserPageTarget {
	endpoint := strings.TrimRight(strings.TrimSpace(browserDebugURL), "/") + "/json/list"
	if strings.TrimSpace(browserDebugURL) == "" {
		return nil
	}
	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var targets []browserPageTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&targets); err != nil {
		return nil
	}
	return targets
}

func browserPageTargetIDs(browserDebugURL string) map[string]bool {
	known := map[string]bool{}
	for _, info := range browserPageTargets(browserDebugURL) {
		known[info.ID] = true
		if classifyApplyResultAtURL(info.URL, "") == "access_already_requested" {
			known["duplicate-result-present"] = true
		}
	}
	return known
}

func waitForNewDataGoKrResultURL(browserDebugURL string, known map[string]bool, wait time.Duration) string {
	deadline := time.Now().Add(wait)
	for {
		for _, info := range browserPageTargets(browserDebugURL) {
			if info.Type != "page" || !strings.HasPrefix(info.URL, dataGoKrBaseURL+"/") {
				continue
			}
			if !known["duplicate-result-present"] && classifyApplyResultAtURL(info.URL, "") == "access_already_requested" {
				return info.URL
			}
			if !known[info.ID] {
				return info.URL
			}
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func acceptApplyConfirmationDialogs(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(event any) {
		if _, ok := event.(*page.EventJavascriptDialogOpening); !ok {
			return
		}
		go func() {
			_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true))
		}()
	})
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
      const matched = label === "신청"
        ? ["신청", "신청하기"].includes(text)
        : label === "활용신청"
          ? text === "활용신청"
          : (text === label || text.includes(label));
      if (matched && el.offsetParent !== null) {
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
		"has_approved_text":   containsAny(pageText, "승인완료", "승인대기", "심사중", "이미 신청", "활용중", "사용중"),
		"has_login_text":      strings.Contains(pageText, "로그인"),
		"human_gate_detected": hasHumanGate(pageText),
	}
	switch {
	case markers["human_gate_detected"].(bool):
		markers["status"] = "human_gate"
	case markers["has_cancel_text"].(bool):
		markers["status"] = "access_requested_not_confirmed"
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

func writeWorkflowResultForOptions(stdout io.Writer, result browserResult, opts browserWorkflowOptions) int {
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

func looksRequestedOrGranted(pageText string) bool {
	for _, term := range []string{"신청취소", "이미 신청", "승인완료", "승인대기", "심사중", "활용중", "사용중"} {
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
	return "apply_result_unconfirmed"
}

func classifyApplyResultAtURL(currentURL, pageText string) string {
	if parsed, err := url.Parse(currentURL); err == nil && parsed.Host == "www.data.go.kr" {
		if parsed.Path == "/iim/api/selectAcountList.do" && parsed.Query().Get("status") == "dupReq" {
			return "access_already_requested"
		}
	}
	return classifyApplyResult(pageText)
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
