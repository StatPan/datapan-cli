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

type EPostAdapter struct {
	StaticHostMatcher
}

func NewEPostAdapter() EPostAdapter {
	return EPostAdapter{StaticHostMatcher{Hosts: EPostHosts()}}
}

func EPostHosts() []string {
	return []string{
		"openapi.epost.go.kr",
		"openapi.epost.go.kr:80",
	}
}

func (a EPostAdapter) Name() string { return "epost" }

func (a EPostAdapter) Hosts() []string { return EPostHosts() }

func (a EPostAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a EPostAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := epostVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "epost",
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
	if epostUnsupportedProtocol(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "epost_unsupported_protocol"
		return result
	}
	if epostWADLMetadataEndpoint(req.Operation.Endpoint) {
		result.Status = "skipped"
		result.Reason = "epost_wadl_metadata_only"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "epost_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := epostRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = redactProviderError(err, plan, req.Credential.Value)
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
	result.BodyShape = epostBodyShape(body)
	if epostWADLMetadataBody(body) {
		result.Status = "failed"
		result.Reason = "epost_wadl_metadata_response"
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
	result.Reason = epostFailureReason(providerStatus, message)
	return result
}

func epostRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || isAuthParam(k) {
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

func epostVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	for _, name := range missing {
		if value, ok := epostSafeDefault(name); ok {
			out[name] = value
			continue
		}
		remaining = append(remaining, name)
	}
	return out, remaining
}

func epostSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "countperpage", "count_per_page", "pageno", "page_no", "page", "pageindex", "page_index", "currentpage", "current_page":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	default:
		return "", false
	}
}

func epostUnsupportedProtocol(spec datago.Spec, op datago.Operation) bool {
	apiType := strings.ToUpper(rawStringFromMerged(spec, op, "api_type"))
	return apiType == "SOAP"
}

func epostWADLMetadataEndpoint(endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	_, hasWADL := u.Query()["_wadl"]
	return hasWADL
}

func epostWADLMetadataBody(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "<application") && strings.Contains(text, "wadl")
}

func epostBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case epostWADLMetadataBody(body):
		return "wadl_metadata"
	case strings.Contains(text, "<item>"):
		return "xml_items"
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

func epostFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "epost_" + normalizeReasonCode(status.ReasonCode)
		}
		if status.Code != "" {
			return "epost_result_code_" + normalizeReasonCode(status.Code)
		}
		if code := epostMessageReason(status.Message); code != "" {
			return code
		}
		if code := epostMessageReason(status.AuthMessage); code != "" {
			return code
		}
		if code := epostMessageReason(status.ErrorMessage); code != "" {
			return code
		}
	}
	if code := epostMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "epost_provider_error"
}

func epostMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "epost_service_key_not_registered"
	case strings.Contains(upper, "SERVICE KEY"):
		return "epost_service_key_error"
	default:
		return ""
	}
}

func normalizeReasonCode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer(" ", "_", "-", "_", ".", "_", "/", "_")
	value = replacer.Replace(value)
	value = strings.Trim(value, "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func rawStringFromMerged(spec datago.Spec, op datago.Operation, key string) string {
	if value := rawString(op.Source, key); value != "" {
		return value
	}
	return rawString(spec.Source, key)
}

func (a EPostAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("epost adapter call support is not enabled yet")
}
