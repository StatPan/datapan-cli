package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

const maxAccessResponseBytes = 4 << 20

var errDataGoKrRateLimited = errors.New("data.go.kr rate limited the access request")
var errExternalProviderRedirect = errors.New("data.go.kr access request redirected to an external provider")

type dataGoKrHTTPSession struct {
	client   *http.Client
	debugURL string
}

type dataGoKrApplicationForm struct {
	action          string
	method          string
	values          url.Values
	endpoints       []string
	operationTokens []string
}

func newDataGoKrHTTPSessionFromBrowser(debugURL string) (*dataGoKrHTTPSession, error) {
	opts := browserWorkflowOptions{BrowserDebugURL: debugURL, ProfileDir: defaultBrowserProfilePath}
	ctx, cancel, err := newBrowserContext(opts)
	if err != nil {
		return nil, err
	}
	defer cancel()
	client, err := dataGoKrHTTPClientFromBrowser(ctx)
	if err != nil {
		return nil, err
	}
	return &dataGoKrHTTPSession{client: client, debugURL: debugURL}, nil
}

func dataGoKrHTTPClientFromBrowser(ctx context.Context) (*http.Client, error) {
	var browserCookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		browserCookies, err = storage.GetCookies().Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("read browser cookies: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	byOrigin := map[string][]*http.Cookie{}
	for _, cookie := range browserCookies {
		if cookie == nil || (!strings.HasSuffix(cookie.Domain, "data.go.kr") && cookie.Domain != "data.go.kr") {
			continue
		}
		scheme := "https"
		if !cookie.Secure {
			scheme = "http"
		}
		domain := strings.TrimPrefix(cookie.Domain, ".")
		origin := scheme + "://" + domain + "/"
		byOrigin[origin] = append(byOrigin[origin], &http.Cookie{
			Name: cookie.Name, Value: cookie.Value, Path: cookie.Path,
			Domain: cookie.Domain, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
		})
	}
	for origin, cookies := range byOrigin {
		parsed, err := url.Parse(origin)
		if err == nil {
			jar.SetCookies(parsed, cookies)
		}
	}
	client := &http.Client{
		Jar:       jar,
		Timeout:   20 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !trustedDataGoKrURL(req.URL) {
				return errExternalProviderRedirect
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many data.go.kr redirects")
			}
			return nil
		},
	}
	return client, nil
}

func (s *dataGoKrHTTPSession) apply(listID, purposeText string) map[string]any {
	result := s.applyOnce(listID, purposeText)
	if fmt.Sprint(result["action"]) != "session_expired_or_login_required" {
		return result
	}
	if err := s.refreshFromBrowserSSO(); err != nil {
		result["refresh_error"] = err.Error()
		return result
	}
	return s.applyOnce(listID, purposeText)
}

func (s *dataGoKrHTTPSession) refreshFromBrowserSSO() error {
	opts := browserWorkflowOptions{BrowserDebugURL: s.debugURL, ProfileDir: defaultBrowserProfilePath}
	ctx, cancel, err := newBrowserContext(opts)
	if err != nil {
		return err
	}
	defer cancel()
	var currentURL, body string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(dataGoKrLoginURL),
		chromedp.Sleep(time.Second),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.ByQuery),
	); err != nil {
		return err
	}
	if !isLoginConfirmed(currentURL, body) {
		return fmt.Errorf("browser SSO requires manual login")
	}
	client, err := dataGoKrHTTPClientFromBrowser(ctx)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}

func (s *dataGoKrHTTPSession) applyOnce(listID, purposeText string) map[string]any {
	formURL := dataGoKrBaseURL + "/tcs/dss/redirectDevAcountRequestForm.do?publicDataPk=" + url.QueryEscape(strings.TrimSpace(listID))
	resp, body, err := s.request(http.MethodGet, formURL, "", "")
	if err != nil {
		if errors.Is(err, errDataGoKrRateLimited) {
			return map[string]any{"action": "portal_rate_limited"}
		}
		if errors.Is(err, errExternalProviderRedirect) {
			return map[string]any{"action": "external_provider_redirect_blocked"}
		}
		return map[string]any{"action": "apply_form_navigation_error", "error": err.Error()}
	}
	currentURL := resp.Request.URL.String()
	if isHTTPLoginResult(resp, body) {
		return map[string]any{"action": "session_expired_or_login_required", "url": currentURL}
	}
	if action := classifyApplyResultAtURL(currentURL, ""); action == "access_already_requested" {
		return confirmedApplyResult(action, currentURL, nil)
	}
	if parsed, parseErr := url.Parse(currentURL); parseErr == nil && parsed.Path == "/iim/api/selectAcountList.do" && looksRequestedOrGranted(body) {
		return confirmedApplyResult("access_already_requested", currentURL, nil)
	}
	form, err := parseDataGoKrApplicationForm(currentURL, body, purposeText)
	if err != nil {
		if parsed, parseErr := url.Parse(currentURL); parseErr == nil && parsed.Path == "/index.do" {
			return map[string]any{"action": "session_expired_or_login_required", "url": currentURL}
		}
		return map[string]any{"action": "apply_form_parse_error", "error": err.Error(), "url": currentURL}
	}
	if form.method != http.MethodPost {
		return map[string]any{"action": "apply_form_unsafe_method", "method": form.method, "url": currentURL}
	}
	resp, resultBody, err := s.request(http.MethodPost, form.action, "application/x-www-form-urlencoded", form.values.Encode())
	if err != nil {
		if errors.Is(err, errDataGoKrRateLimited) {
			return map[string]any{"action": "portal_rate_limited"}
		}
		return map[string]any{"action": "apply_form_submit_error", "error": err.Error()}
	}
	resultURL := resp.Request.URL.String()
	if isHTTPLoginResult(resp, resultBody) {
		return map[string]any{"action": "session_expired_or_login_required", "url": resultURL}
	}
	action := classifyPortalSaveResponse(resultBody)
	if action == "apply_result_unconfirmed" {
		action = classifyApplyResultAtURL(resultURL, resultBody)
	}
	result := map[string]any{
		"action":         action,
		"detected_state": detectApplicationState(resultBody),
		"transport":      "authenticated_http",
		"url":            resultURL,
		"response":       safePortalResponse(resultBody),
	}
	if action != "access_requested_not_confirmed" && action != "access_already_requested" {
		result["form_action"] = form.action
		result["form_fields"] = sortedValueKeys(form.values)
		result["operation_tokens"] = form.operationTokens
		result["form_endpoints"] = form.endpoints
		result["validation_messages"] = dataGoKrAlertMessages(resultBody)
	}
	return result
}

func trustedDataGoKrURL(candidate *url.URL) bool {
	if candidate == nil || !strings.EqualFold(candidate.Scheme, "https") {
		return false
	}
	host := strings.ToLower(candidate.Hostname())
	return host == "data.go.kr" || strings.HasSuffix(host, ".data.go.kr")
}

func classifyPortalSaveResponse(body string) string {
	var result struct {
		Success *bool `json:"success"`
		Result  *bool `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || result.Success == nil {
		return "apply_result_unconfirmed"
	}
	if !*result.Success {
		return "apply_result_rejected"
	}
	if result.Result == nil {
		return "apply_result_unconfirmed"
	}
	if *result.Result {
		return "access_requested_not_confirmed"
	}
	return "access_already_requested"
}

func safePortalResponse(body string) map[string]any {
	var object map[string]any
	if err := json.Unmarshal([]byte(body), &object); err != nil {
		return map[string]any{"format": "non_json", "bytes": len(body)}
	}
	result := map[string]any{"format": "json"}
	allowed := map[string]bool{"result": true, "success": true, "code": true, "message": true, "msg": true, "status": true}
	for key, value := range object {
		if !allowed[strings.ToLower(key)] {
			continue
		}
		switch value := value.(type) {
		case string:
			if len(value) > 300 {
				value = value[:300]
			}
			result[key] = value
		case bool, float64, nil:
			result[key] = value
		}
	}
	return result
}

func sortedValueKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var dataGoKrAlertPattern = regexp.MustCompile(`(?i)alert\s*\(\s*["']([^"']{1,300})["']\s*\)`)
var dataGoKrAcountEndpointPattern = regexp.MustCompile(`["'](/[^"']*Acount[^"']*\.do(?:\?[^"']*)?)["']`)
var dataGoKrOperationTokenPattern = regexp.MustCompile(`(?i)\b[A-Za-z_$][A-Za-z0-9_$]*(?:operation|opertn|oprtn)[A-Za-z0-9_$]*\b`)

func dataGoKrAlertMessages(document string) []string {
	matches := dataGoKrAlertPattern.FindAllStringSubmatch(document, 10)
	messages := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			messages = append(messages, strings.TrimSpace(match[1]))
		}
	}
	return messages
}

func dataGoKrOperationTokens(document string) []string {
	seen := map[string]bool{}
	var tokens []string
	for _, token := range dataGoKrOperationTokenPattern.FindAllString(document, -1) {
		if seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

func dataGoKrAcountEndpoints(document string) []string {
	seen := map[string]bool{}
	var endpoints []string
	for _, match := range dataGoKrAcountEndpointPattern.FindAllStringSubmatch(document, -1) {
		if len(match) != 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		endpoints = append(endpoints, match[1])
	}
	sort.Strings(endpoints)
	return endpoints
}

func (s *dataGoKrHTTPSession) request(method, endpoint, contentType, body string) (*http.Response, string, error) {
	req, err := http.NewRequest(method, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "datapan-cli/data-go-kr-access")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAccessResponseBytes))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return resp, string(data), errDataGoKrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return resp, string(data), fmt.Errorf("portal returned HTTP %d", resp.StatusCode)
	}
	return resp, string(data), nil
}

func isHTTPLoginResult(resp *http.Response, body string) bool {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return true
	}
	return strings.Contains(resp.Request.URL.Path, "/login") || strings.Contains(resp.Request.URL.Host, "auth.data.go.kr") || strings.Contains(body, "통합 로그인")
}

func parseDataGoKrApplicationForm(pageURL, document, purposeText string) (dataGoKrApplicationForm, error) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return dataGoKrApplicationForm{}, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return dataGoKrApplicationForm{}, err
	}
	var selected *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if selected != nil {
			return
		}
		if node.Type == html.ElementNode && node.Data == "form" && formHasPurposeField(node) {
			selected = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if selected == nil {
		return dataGoKrApplicationForm{}, fmt.Errorf("application form not found")
	}
	action := attr(selected, "action")
	if discovered := discoverDataGoKrSubmitAction(document); discovered != "" {
		action = discovered
	}
	if action == "" {
		return dataGoKrApplicationForm{}, fmt.Errorf("application submit action not found")
	}
	actionURL, err := base.Parse(action)
	if err != nil || actionURL.Host != "www.data.go.kr" {
		return dataGoKrApplicationForm{}, fmt.Errorf("untrusted application submit action")
	}
	method := strings.ToUpper(attr(selected, "method"))
	if method == "" {
		method = http.MethodGet
	}
	values := url.Values{}
	collectFormValues(selected, values, purposeText)
	collectOperationAuthorizations(selected, values)
	return dataGoKrApplicationForm{
		action: actionURL.String(), method: method, values: values,
		endpoints: dataGoKrAcountEndpoints(document), operationTokens: dataGoKrOperationTokens(document),
	}, nil
}

func collectOperationAuthorizations(form *html.Node, values url.Values) {
	index := 0
	for node := form.FirstChild; node != nil; node = nextNode(form, node) {
		if node.Type != html.ElementNode || node.Data != "input" || hasAttr(node, "disabled") {
			continue
		}
		seq := attr(node, "data-oprtin-seq-no")
		use := attr(node, "data-dily-use-expect-co")
		if !digitsOnly(seq) || !digitsOnly(use) {
			continue
		}
		values.Set(fmt.Sprintf("oprtinAuthorList[%d].oprtinSeqNo", index), seq)
		values.Set(fmt.Sprintf("oprtinAuthorList[%d].dilyUseExpectCo", index), use)
		index++
	}
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func formHasPurposeField(form *html.Node) bool {
	for node := form.FirstChild; node != nil; node = nextNode(form, node) {
		if node.Type == html.ElementNode && node.Data == "textarea" && attr(node, "name") != "" {
			return true
		}
	}
	return false
}

func collectFormValues(form *html.Node, values url.Values, purposeText string) {
	for node := form.FirstChild; node != nil; node = nextNode(form, node) {
		if node.Type != html.ElementNode {
			continue
		}
		name := attr(node, "name")
		if name == "" || hasAttr(node, "disabled") {
			continue
		}
		switch node.Data {
		case "textarea":
			values.Set(name, purposeText)
		case "select":
			if value, ok := selectedOptionValue(node); ok {
				values.Add(name, value)
			}
		case "input":
			typeName := strings.ToLower(attr(node, "type"))
			if typeName == "submit" || typeName == "button" || typeName == "file" {
				continue
			}
			if (typeName == "checkbox" || typeName == "radio") && !hasAttr(node, "checked") {
				if typeName == "checkbox" {
					values.Add(name, ternary(attr(node, "value") == "", "on", attr(node, "value")))
				}
				continue
			}
			values.Add(name, attr(node, "value"))
		}
	}
}

func selectedOptionValue(selectNode *html.Node) (string, bool) {
	var fallback string
	foundFallback := false
	for node := selectNode.FirstChild; node != nil; node = nextNode(selectNode, node) {
		if node.Type != html.ElementNode || node.Data != "option" || hasAttr(node, "disabled") {
			continue
		}
		value := attr(node, "value")
		if !foundFallback {
			fallback, foundFallback = value, true
		}
		if hasAttr(node, "selected") {
			return value, true
		}
	}
	return fallback, foundFallback
}

func nextNode(root, node *html.Node) *html.Node {
	if node.FirstChild != nil {
		return node.FirstChild
	}
	for node != nil && node != root {
		if node.NextSibling != nil {
			return node.NextSibling
		}
		node = node.Parent
	}
	return nil
}

func attr(node *html.Node, name string) string {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, name) {
			return strings.TrimSpace(item.Val)
		}
	}
	return ""
}

func hasAttr(node *html.Node, name string) bool {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, name) {
			return true
		}
	}
	return false
}

var dataGoKrSubmitActionPattern = regexp.MustCompile(`["'](/[^"']*(?:insert|save)[^"']*Acount[^"']*\.do)["']`)

func discoverDataGoKrSubmitAction(document string) string {
	match := dataGoKrSubmitActionPattern.FindStringSubmatch(document)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}
