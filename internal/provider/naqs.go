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

type NAQSAdapter struct {
	StaticHostMatcher
}

func NewNAQSAdapter() NAQSAdapter {
	return NAQSAdapter{StaticHostMatcher{Hosts: NAQSHosts()}}
}

func NAQSHosts() []string {
	return []string{"data.naqs.go.kr"}
}

func (a NAQSAdapter) Name() string { return "naqs" }

func (a NAQSAdapter) Hosts() []string { return NAQSHosts() }

func (a NAQSAdapter) Capabilities() []string { return []string{"call"} }

func (a NAQSAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return naqsVerificationParams(params, missing)
}

func (a NAQSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NAQSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := naqsVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "naqs",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
		MissingParams:   missing,
	}
	if naqsMutationEndpoint(req.Operation.Endpoint) {
		result.Status = "skipped"
		result.Reason = "naqs_mutation_endpoint"
		result.BodyShape = "html_portal"
		return result
	}
	if qnetApprovalRequired(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "approval_required"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "naqs_missing_required_params"
		return result
	}
	plan, err := naqsRequestURL(req.Operation.Endpoint, params)
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
		result.Reason = err.Error()
		return result
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = naqsTransportReason(err)
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
	result.BodyShape = naqsBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	if resp.StatusCode == http.StatusNotFound {
		result.Reason = "naqs_endpoint_not_found"
		return result
	}
	result.Reason = naqsFailureReason(providerStatus, message)
	return result
}

func naqsVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := naqsSafeDefault(name); ok {
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

func naqsSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "certno", "stdcertno":
		return "", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	default:
		return "", false
	}
}

func naqsMutationEndpoint(endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.Trim(u.Path, "/"), "pubc")
}

func naqsRequestURL(endpoint string, params map[string]string) (providerRequestPlan, error) {
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
	u.RawQuery = q.Encode()
	return providerRequestPlan{url: u.String(), redacted: u.String()}, nil
}

func naqsBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.Contains(text, "<getenvresponse"):
		return "xml_env_response"
	case strings.Contains(text, "<item>") || strings.Contains(text, "<items>"):
		return "xml_items"
	case strings.Contains(text, "<header>"):
		return "xml_status"
	case strings.Contains(text, "<!doctype html") || strings.Contains(text, "<html"):
		return "html_portal"
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

func naqsFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "naqs_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := naqsMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "naqs_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := naqsMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "naqs_provider_error"
}

func naqsMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "NORMAL SERVICE"):
		return ""
	case strings.Contains(upper, "INVALID") && strings.Contains(upper, "PARAMETER"):
		return "naqs_bad_request_params"
	case strings.Contains(upper, "SERVICE KEY"):
		return "naqs_service_key_error"
	default:
		return ""
	}
}

func naqsTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "naqs_request_timeout"
	}
	return "naqs_request_failed"
}

func (a NAQSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := naqsVerificationParams(req.Params, req.MissingParams)
	if naqsMutationEndpoint(req.Operation.Endpoint) {
		return datago.ResponseEnvelope{}, fmt.Errorf("naqs mutation-like endpoint is not safe for automatic calls: %s", req.Operation.Name)
	}
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling naqs operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required naqs params: %s", strings.Join(missing, ", "))
	}
	plan, err := naqsRequestURL(req.Operation.Endpoint, params)
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
		return datago.ResponseEnvelope{}, fmt.Errorf("naqs request failed: %w", err)
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
		Provider:       "naqs",
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
