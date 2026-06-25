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

type EMuseumAdapter struct {
	StaticHostMatcher
}

const eMuseumUserAgent = "datapan-cli/0.1 (+https://github.com/StatPan/datapan-cli)"

func NewEMuseumAdapter() EMuseumAdapter {
	return EMuseumAdapter{StaticHostMatcher{Hosts: EMuseumHosts()}}
}

func EMuseumHosts() []string {
	return []string{
		"www.emuseum.go.kr",
	}
}

func (a EMuseumAdapter) Name() string { return "emuseum" }

func (a EMuseumAdapter) Hosts() []string { return EMuseumHosts() }

func (a EMuseumAdapter) Capabilities() []string { return []string{"call"} }

func (a EMuseumAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return eMuseumVerificationParams(params, missing)
}

func (a EMuseumAdapter) PlanCall(req CallRequest) (CallPlan, error) {
	params, _ := eMuseumOperationParams(req.Operation, req.Params, req.MissingParams)
	plan, err := eMuseumRequestURL(req.Operation, params, req.Credential.Value)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{
		URL:          plan.url,
		RedactedURL:  plan.redacted,
		PublicParams: publicQueryParams(plan.redacted),
	}, nil
}

func (a EMuseumAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a EMuseumAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := eMuseumOperationParams(req.Operation, req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "emuseum",
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
		result.Reason = "emuseum_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := eMuseumRequestURL(req.Operation, params, req.Credential.Value)
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
	httpReq.Header.Set("User-Agent", eMuseumUserAgent)
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = eMuseumTransportReason(err)
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
	result.BodyShape = eMuseumBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = eMuseumFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func eMuseumRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(op.Endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("emuseum endpoint is not absolute")
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

func eMuseumVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	return eMuseumOperationParams(datago.Operation{}, params, missing)
}

func eMuseumOperationParams(op datago.Operation, params map[string]string, missing []string) (map[string]string, []string) {
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
		if eMuseumOptionalParam(op, name) {
			continue
		}
		if value, ok := eMuseumSafeDefault(name); ok {
			if strings.TrimSpace(value) != "" {
				out[name] = value
			}
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

func eMuseumOptionalParam(op datago.Operation, name string) bool {
	normalizedName := normalizeParamName(name)
	if normalizedName == "" {
		return true
	}
	normalizedOperation := strings.ToLower(strings.TrimSpace(op.Name))
	if strings.Contains(normalizedOperation, "상세") {
		return normalizedName != "id"
	}
	if strings.Contains(normalizedOperation, "코드") {
		return normalizedName != "parentcode" && normalizedName != "parent_code"
	}
	switch normalizedName {
	case "id", "museumcode", "museum_code", "name", "namekr", "name_kr", "nameen", "name_en", "namecn", "name_cn", "author", "nationalitycode", "nationality_code", "materialcode", "material_code", "purposecode", "purpose_code", "sizerangecode", "size_range_code", "placelandcode", "place_land_code", "designationcode", "designation_code", "indexword", "index_word":
		return true
	default:
		return false
	}
}

func eMuseumSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	default:
		return "", false
	}
}

func eMuseumBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<resultcode>"):
		return "xml_status"
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, "<html"):
		return "html"
	case strings.HasPrefix(text, "<"):
		return "xml"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	default:
		return "text"
	}
}

func eMuseumFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "emuseum_endpoint_not_found"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "emuseum_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := eMuseumMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "emuseum_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := eMuseumMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "emuseum_provider_error"
}

func eMuseumMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "emuseum_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "emuseum_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "emuseum_service_key_error"
	default:
		return ""
	}
}

func eMuseumTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "emuseum_request_timeout"
	default:
		return "emuseum_request_failed"
	}
}

func (a EMuseumAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := eMuseumOperationParams(req.Operation, req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling emuseum operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required emuseum params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := eMuseumRequestURL(req.Operation, params, req.Credential.Value)
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
	httpReq.Header.Set("User-Agent", eMuseumUserAgent)
	resp, err := client.Do(httpReq)
	if err != nil {
		return datago.ResponseEnvelope{}, eMuseumTransportError(err, plan)
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
		Provider:       "emuseum",
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

func eMuseumTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("emuseum request failed: %s", message)
}
