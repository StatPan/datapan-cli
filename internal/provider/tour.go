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

type TourAdapter struct {
	StaticHostMatcher
}

func NewTourAdapter() TourAdapter {
	return TourAdapter{StaticHostMatcher{Hosts: TourHosts()}}
}

func TourHosts() []string {
	return []string{
		"openapi.tour.go.kr",
	}
}

func (a TourAdapter) Name() string { return "tour" }

func (a TourAdapter) Hosts() []string { return TourHosts() }

func (a TourAdapter) Capabilities() []string { return []string{"call"} }

func (a TourAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return tourVerificationParams(params, missing)
}

func (a TourAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	if endpoint := rawStringFromMerged(spec, op, "operation_url"); endpoint != "" && endpointHost(endpoint) != "" {
		return "external_endpoint"
	}
	return datago.OperationDependencyClass(spec, op)
}

func (a TourAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := tourVerificationParams(req.Params, req.MissingParams)
	endpoint := tourEffectiveEndpoint(req.Spec, req.Operation)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "tour",
		EndpointHost:    endpointHost(endpoint),
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
	if tourServiceRootWithoutOperationPath(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "tour_service_root_missing_operation_path"
		result.BodyShape = "service_root"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "tour_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := tourRequestURL(endpoint, params, req.Credential.Value)
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
		result.Reason = tourTransportReason(err)
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
	result.BodyShape = tourBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	if code := tourMessageReason(string(body)); code != "" {
		result.Reason = code
		return result
	}
	result.Reason = tourFailureReason(providerStatus, message)
	return result
}

func tourEffectiveEndpoint(spec datago.Spec, op datago.Operation) string {
	if endpoint := rawStringFromMerged(spec, op, "operation_url"); endpoint != "" {
		return endpoint
	}
	return op.Endpoint
}

func tourServiceRootWithoutOperationPath(spec datago.Spec, op datago.Operation) bool {
	if rawStringFromMerged(spec, op, "operation_url") != "" {
		return false
	}
	endpoint := op.Endpoint
	if endpoint == "" {
		endpoint = rawStringFromMerged(spec, op, "end_point_url")
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, "openapi.tour.go.kr") &&
		strings.Trim(strings.ToLower(parsed.Path), "/") == "openapi/service"
}

func tourRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("tour endpoint is not absolute")
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

func tourVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
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
		if value, ok := tourSafeDefault(name); ok {
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

func tourSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	case "yy", "baseyy":
		return "2024", true
	case "ym":
		return "202401", true
	case "yq":
		return "2024Q1", true
	case "activity_cd", "sido_cd", "id_cd":
		return "A", true
	case "tourtype_cd", "biztype_cd":
		return "Z", true
	case "nat_cd":
		return "100", true
	case "ed_cd":
		return "E", true
	default:
		return "", false
	}
}

func tourBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<xmlfault") || strings.Contains(text, ":xmlfault") || strings.Contains(text, "<faultstring>"):
		return "xml_fault"
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

func tourFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "tour_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := tourMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "tour_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := tourMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "tour_provider_error"
}

func tourMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED") || strings.Contains(upper, "SERVICEKEY IS NOT REGISTERED"):
		return "tour_service_key_not_registered"
	case strings.Contains(message, "인증키가 누락") || strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "tour_missing_auth"
	case strings.Contains(message, "지원하지 않는 인증") || strings.Contains(upper, "UNSUPPORTED"):
		return "tour_auth_error"
	case strings.Contains(upper, "NO OPENAPI SERVICE"):
		return "tour_service_not_registered"
	case strings.Contains(upper, "INVALID PARAMETER") || strings.Contains(message, "필수 요청"):
		return "tour_bad_request_params"
	default:
		return ""
	}
}

func tourTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "tour_request_timeout"
	default:
		return "tour_request_failed"
	}
}

func (a TourAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := tourVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling tour operation %s", req.Operation.Name)
	}
	if tourServiceRootWithoutOperationPath(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("tour service-root metadata is missing operation_url")
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required tour params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	endpoint := tourEffectiveEndpoint(req.Spec, req.Operation)
	plan, err := tourRequestURL(endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, tourTransportError(err, plan)
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
		Provider:       "tour",
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

func tourTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("tour request failed: %s", message)
}
