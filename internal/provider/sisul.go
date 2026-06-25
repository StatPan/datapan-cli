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

type SisulAdapter struct {
	StaticHostMatcher
}

func NewSisulAdapter() SisulAdapter {
	return SisulAdapter{StaticHostMatcher{Hosts: SisulHosts()}}
}

func SisulHosts() []string {
	return []string{
		"data.sisul.or.kr",
	}
}

func (a SisulAdapter) Name() string { return "sisul" }

func (a SisulAdapter) Hosts() []string { return SisulHosts() }

func (a SisulAdapter) Capabilities() []string { return []string{"call"} }

func (a SisulAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return sisulVerificationParams(params, missing)
}

func (a SisulAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SisulAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := sisulVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "sisul",
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
	if sisulWADLMetadataEndpoint(req.Operation.Endpoint) {
		result.Status = "skipped"
		result.Reason = "sisul_wadl_metadata_only"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "sisul_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := sisulRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = sisulVerificationTransportReason(err)
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
	result.BodyShape = sisulBodyShape(body)
	if sisulWADLMetadataBody(body) {
		result.Status = "failed"
		result.Reason = "sisul_wadl_metadata_response"
		result.SemanticStatus = "metadata_response"
		return result
	}
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = sisulFailureReason(providerStatus, message)
	return result
}

func sisulRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	q := u.Query()
	q.Del("_wadl")
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	q.Set("serviceKey", key)
	u.RawQuery = q.Encode()
	redacted := *u
	rq := redacted.Query()
	rq.Set("serviceKey", "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func sisulVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := sisulSafeDefault(name); ok {
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

func sisulSafeDefault(name string) (string, bool) {
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

func sisulWADLMetadataEndpoint(endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	_, hasWADL := u.Query()["_wadl"]
	return hasWADL
}

func sisulWADLMetadataBody(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "<application") && strings.Contains(text, "wadl")
}

func sisulBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case sisulWADLMetadataBody(body):
		return "wadl_metadata"
	case strings.Contains(text, "<item>"):
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

func sisulFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "sisul_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := sisulMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "sisul_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := sisulMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "sisul_provider_error"
}

func sisulVerificationTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "sisul_request_timeout"
	default:
		return "sisul_request_failed"
	}
}

func sisulMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(message, "인증키가 누락") || strings.Contains(upper, "SERVICE KEY"):
		return "sisul_auth_required"
	case strings.Contains(message, "지원하지 않는 인증") || strings.Contains(upper, "UNSUPPORTED"):
		return "sisul_unsupported_auth"
	default:
		return ""
	}
}

func (a SisulAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := sisulVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling sisul operation %s", req.Operation.Name)
	}
	if sisulWADLMetadataEndpoint(req.Operation.Endpoint) {
		return datago.ResponseEnvelope{}, fmt.Errorf("sisul metadata endpoint is not callable")
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required sisul params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := sisulRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, sisulTransportError(err, plan)
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
		Provider:       "sisul",
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

func sisulTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("sisul request failed: %s", message)
}
