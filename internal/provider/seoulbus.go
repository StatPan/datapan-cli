package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SeoulBusAdapter struct {
	StaticHostMatcher
}

func NewSeoulBusAdapter() SeoulBusAdapter {
	return SeoulBusAdapter{StaticHostMatcher{Hosts: SeoulBusHosts()}}
}

func SeoulBusHosts() []string {
	return []string{
		"ws.bus.go.kr",
	}
}

func (a SeoulBusAdapter) Name() string { return "seoul-bus" }

func (a SeoulBusAdapter) Hosts() []string { return SeoulBusHosts() }

func (a SeoulBusAdapter) Capabilities() []string { return []string{"call"} }

func (a SeoulBusAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return seoulBusVerificationParams(params, missing)
}

func (a SeoulBusAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeoulBusAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := seoulBusVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "seoul-bus",
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
		result.Reason = "seoul_bus_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := seoulBusRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = seoulBusTransportReason(err)
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	ok, semanticStatus, message, providerStatus := seoulBusClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = seoulBusBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = seoulBusFailureReason(providerStatus, message)
	return result
}

func seoulBusRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("seoul-bus endpoint is not absolute")
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

func seoulBusVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
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
		if value, ok := seoulBusSafeDefault(name); ok {
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

func seoulBusSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "busrouteid", "bus_route_id", "routeid", "route_id":
		return "100100118", true
	case "startord", "start_ord":
		return "1", true
	case "endord", "end_ord":
		return "5", true
	default:
		return "", false
	}
}

func seoulBusClassifyResponse(statusCode int, contentType string, body []byte) (bool, string, string, *datago.ProviderStatus) {
	if status, ok := seoulBusServiceStatus(body); ok {
		if seoulBusCodeOK(status.Code) {
			status.OK = true
			return true, "provider_ok", status.Message, &status
		}
		status.OK = false
		msg := status.Message
		if msg == "" {
			msg = "provider returned headerCd " + status.Code
		}
		return false, "provider_error", msg, &status
	}
	return datago.ClassifyResponse(statusCode, contentType, body)
}

func seoulBusServiceStatus(body []byte) (datago.ProviderStatus, bool) {
	code := seoulBusXMLTagValue(body, "headerCd")
	message := seoulBusXMLTagValue(body, "headerMsg")
	if code == "" && message == "" {
		return datago.ProviderStatus{}, false
	}
	return datago.ProviderStatus{
		Source:  "ServiceResult/msgHeader",
		Code:    code,
		Message: message,
	}, true
}

func seoulBusXMLTagValue(body []byte, tag string) string {
	pattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `>\s*([^<]+?)\s*</` + regexp.QuoteMeta(tag) + `>`)
	match := pattern.FindSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}

func seoulBusCodeOK(code string) bool {
	code = strings.TrimSpace(strings.ToUpper(code))
	return code == "0" || code == "00" || code == "NORMAL_SERVICE"
}

func seoulBusBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<serviceresult"):
		if strings.Contains(text, "<itemlist>") {
			return "xml_service_items"
		}
		return "xml_service_status"
	case strings.Contains(text, "<item>") || strings.Contains(text, "<items>"):
		return "xml_items"
	case strings.Contains(text, "<header>") || strings.Contains(text, "<resultcode>"):
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

func seoulBusFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "seoul_bus_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := seoulBusMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "seoul_bus_header_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := seoulBusMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "seoul_bus_provider_error"
}

func seoulBusMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED") || strings.Contains(upper, "SERVICEKEY IS NOT REGISTERED"):
		return "seoul_bus_service_key_not_registered"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY") || strings.Contains(message, "인증"):
		return "seoul_bus_auth_error"
	case strings.Contains(upper, "INVALID") || strings.Contains(message, "필수"):
		return "seoul_bus_bad_request_params"
	default:
		return ""
	}
}

func seoulBusTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "seoul_bus_request_timeout"
	default:
		return "seoul_bus_request_failed"
	}
}

func (a SeoulBusAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := seoulBusVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling seoul-bus operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required seoul-bus params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := seoulBusRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, seoulBusTransportError(err, plan)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	ok, semanticStatus, message, providerStatus := seoulBusClassifyResponse(resp.StatusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "seoul-bus",
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

func seoulBusTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("seoul-bus request failed: %s", message)
}
