package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GBLibAdapter struct {
	StaticHostMatcher
}

func NewGBLibAdapter() GBLibAdapter {
	return GBLibAdapter{StaticHostMatcher{Hosts: GBLibHosts()}}
}

func GBLibHosts() []string {
	return []string{
		"openapi.gblib.or.kr",
	}
}

func (a GBLibAdapter) Name() string { return "gblib" }

func (a GBLibAdapter) Hosts() []string { return GBLibHosts() }

func (a GBLibAdapter) Capabilities() []string { return []string{"call"} }

func (a GBLibAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return gblibVerificationParams(params, missing)
}

func (a GBLibAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GBLibAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := gblibVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "gblib",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
		MissingParams:   missing,
	}
	if qnetApprovalRequired(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "approval_required"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "gblib_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := gblibRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	result.URL = plan.redacted
	client := req.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.url, nil)
	if err != nil {
		result.Status = "failed"
		result.Reason = redactProviderError(err, plan, req.Credential.Value)
		return result
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = gblibTransportReason(err)
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = gblibBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = gblibFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func gblibRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("gblib endpoint is not absolute")
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = datago.QueryWithServiceKey(q, key)
	redacted := *u
	rq := redacted.Query()
	rq.Set("serviceKey", "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func gblibVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if isAuthParam(name) {
			continue
		}
		if value, ok := gblibSafeDefault(name); ok {
			out[name] = value
			continue
		}
		normalized := normalizeParamName(name)
		if normalized != "" && seenRemaining[normalized] {
			continue
		}
		seenRemaining[normalized] = true
		remaining = append(remaining, name)
	}
	return out, remaining
}

func gblibSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "keyword", "searchword", "search_word":
		return "공공", true
	case "pub", "publisher":
		return "도서", true
	case "lib", "library", "managecd", "manage_cd":
		return "MA", true
	case "org", "organization", "comcd", "com_cd":
		return "1", true
	case "date", "dt":
		return "20260625", true
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	default:
		return "", false
	}
}

func gblibBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, "<items/>") || strings.Contains(text, "<items></items>"):
		return "xml_empty_items"
	case strings.Contains(text, "<resultcode>"):
		return "xml_status"
	case strings.HasPrefix(text, "<"):
		return "xml"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	case strings.Contains(text, "<html"):
		return "html"
	default:
		return "text"
	}
}

func gblibFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "gblib_endpoint_not_found"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "gblib_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := gblibMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "gblib_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := gblibMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "gblib_provider_error"
}

func gblibMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "gblib_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "gblib_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "gblib_service_key_error"
	default:
		return ""
	}
}

func gblibTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "gblib_request_timeout"
	default:
		return "gblib_request_failed"
	}
}

func (a GBLibAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := gblibVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling gblib operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required gblib params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := gblibRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	client := req.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.url, nil)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return datago.ResponseEnvelope{}, gblibTransportError(err, plan)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(resp.StatusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "gblib",
		Dataset:        req.Spec.ID,
		Operation:      req.Operation.Name,
		StatusCode:     resp.StatusCode,
		ContentType:    contentType,
		SemanticStatus: semanticStatus,
		Message:        message,
		ProviderStatus: providerStatus,
		URL:            plan.redacted,
		Body:           string(body),
	}, nil
}

func gblibTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("gblib request failed: %s", message)
}
