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

type HumetroAdapter struct {
	StaticHostMatcher
}

func NewHumetroAdapter() HumetroAdapter {
	return HumetroAdapter{StaticHostMatcher{Hosts: HumetroHosts()}}
}

func HumetroHosts() []string {
	return []string{"data.humetro.busan.kr"}
}

func (a HumetroAdapter) Name() string { return "humetro" }

func (a HumetroAdapter) Hosts() []string { return HumetroHosts() }

func (a HumetroAdapter) Capabilities() []string { return []string{"call"} }

func (a HumetroAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return humetroVerificationParams(params, missing)
}

func (a HumetroAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a HumetroAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := humetroVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "humetro",
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
		result.Reason = "humetro_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := humetroRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = humetroTransportReason(err)
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
	result.BodyShape = humetroBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = humetroFailureReason(providerStatus, message)
	return result
}

func humetroVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := humetroSafeDefault(name); ok {
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

func humetroSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "act":
		return "xml", true
	case "scode":
		return "101", true
	case "kind":
		return "1", true
	case "sdate":
		return "2024-01-01", true
	case "edate":
		return "2024-12-31", true
	case "year":
		return "2024", true
	case "pageno", "page_no", "c_page", "cpage":
		return "1", true
	case "numofrows", "num_of_rows", "c_size", "csize":
		return "1", true
	default:
		return "", false
	}
}

func humetroRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	q.Set("ServiceKey", key)
	u.RawQuery = q.Encode()
	redacted := *u
	rq := redacted.Query()
	rq.Set("ServiceKey", "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func humetroBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.Contains(text, "<item>") || strings.Contains(text, "<items>"):
		return "xml_items"
	case strings.Contains(text, "<header>"):
		return "xml_status"
	case text == "":
		return "empty"
	case strings.HasPrefix(text, "<"):
		return "xml"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	default:
		return "text"
	}
}

func humetroFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "humetro_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := humetroMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "humetro_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := humetroMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "humetro_provider_error"
}

func humetroMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "humetro_service_access_denied"
	case strings.Contains(upper, "DEADLINE HAS EXPIRED"):
		return "humetro_deadline_expired"
	case strings.Contains(upper, "INVALID") && strings.Contains(upper, "PARAMETER"):
		return "humetro_invalid_request_parameter"
	case strings.Contains(upper, "SERVICE KEY"):
		return "humetro_service_key_error"
	default:
		return ""
	}
}

func humetroTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "humetro_request_timeout"
	}
	return "humetro_request_failed"
}

func (a HumetroAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := humetroVerificationParams(req.Params, req.MissingParams)
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required humetro params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := humetroRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, fmt.Errorf("humetro request failed: %w", err)
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
		Provider:       "humetro",
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
