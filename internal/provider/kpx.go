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

type KPXAdapter struct {
	StaticHostMatcher
}

func NewKPXAdapter() KPXAdapter {
	return KPXAdapter{StaticHostMatcher{Hosts: KPXHosts()}}
}

func KPXHosts() []string {
	return []string{
		"openapi.kpx.or.kr",
	}
}

func (a KPXAdapter) Name() string { return "kpx" }

func (a KPXAdapter) Hosts() []string { return KPXHosts() }

func (a KPXAdapter) Capabilities() []string { return []string{"call"} }

func (a KPXAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return kpxVerificationParams(params, missing)
}

func (a KPXAdapter) PlanCall(req CallRequest) (CallPlan, error) {
	params, _ := kpxVerificationParams(req.Params, req.MissingParams)
	plan, err := kpxRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{
		URL:          plan.url,
		RedactedURL:  plan.redacted,
		PublicParams: publicQueryParams(plan.redacted),
	}, nil
}

func (a KPXAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KPXAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := kpxVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "kpx",
		EndpointHost:    "openapi.kpx.or.kr",
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
		result.Reason = "kpx_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := kpxRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = kpxTransportReason(err)
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
	result.BodyShape = kpxBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = kpxFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func kpxRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return providerRequestPlan{}, fmt.Errorf("kpx endpoint is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + strings.TrimLeft(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("kpx endpoint is not absolute")
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

func kpxVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	if _, ok := out["pageNo"]; !ok {
		out["pageNo"] = "1"
	}
	if _, ok := out["numOfRows"]; !ok {
		out["numOfRows"] = "1"
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if isAuthParam(name) {
			continue
		}
		if value, ok := kpxSafeDefault(name); ok {
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

func kpxSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	default:
		return "", false
	}
}

func kpxBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, "<resultcode>"):
		return "xml_status"
	case strings.HasPrefix(text, "<"):
		return "xml"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	default:
		return "text"
	}
}

func kpxFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "kpx_endpoint_not_found"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "kpx_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := kpxMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "kpx_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := kpxMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "kpx_provider_error"
}

func kpxMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "INVALID REQUEST PARAMETER"):
		return "kpx_invalid_request_parameter"
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "kpx_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "kpx_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "kpx_service_key_error"
	default:
		return ""
	}
}

func kpxTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "kpx_request_timeout"
	default:
		return "kpx_request_failed"
	}
}

func (a KPXAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := kpxVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling kpx operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required kpx params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := kpxRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, kpxTransportError(err, plan)
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
		Provider:       "kpx",
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

func kpxTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("kpx request failed: %s", message)
}
