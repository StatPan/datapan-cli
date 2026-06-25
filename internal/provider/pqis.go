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

type PQISAdapter struct {
	StaticHostMatcher
}

func NewPQISAdapter() PQISAdapter {
	return PQISAdapter{StaticHostMatcher{Hosts: PQISHosts()}}
}

func PQISHosts() []string {
	return []string{
		"openapi.pqis.go.kr",
	}
}

func (a PQISAdapter) Name() string { return "pqis" }

func (a PQISAdapter) Hosts() []string { return PQISHosts() }

func (a PQISAdapter) Capabilities() []string { return []string{"call"} }

func (a PQISAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return pqisVerificationParams(params, missing)
}

func (a PQISAdapter) PlanCall(req CallRequest) (CallPlan, error) {
	plan, err := pqisRequestURL(req.Operation, req.Params, req.Credential.Value)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{
		URL:          plan.url,
		RedactedURL:  plan.redacted,
		PublicParams: publicQueryParams(plan.redacted),
	}, nil
}

func (a PQISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a PQISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := pqisVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "pqis",
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
		result.Reason = "pqis_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := pqisRequestURL(req.Operation, params, req.Credential.Value)
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
		result.Reason = pqisTransportReason(err)
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
	result.BodyShape = pqisBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = pqisFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func pqisRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(op.Endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("pqis endpoint is not absolute")
	}
	u.RawQuery = ""
	u.Path = strings.TrimRight(u.Path, "/") + "/" + pqisOperationPath(op.Name)
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

func publicQueryParams(rawURL string) map[string]string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for k, values := range u.Query() {
		if len(values) > 0 {
			out[k] = values[0]
		}
	}
	return out
}

func pqisOperationPath(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(normalized, "국가"):
		return "nationCode"
	case strings.Contains(normalized, "식물코드"):
		return "plantCode"
	case strings.Contains(normalized, "수출"):
		return "exportStats"
	case strings.Contains(normalized, "수입"):
		return "importStats"
	default:
		return "nationCode"
	}
}

func pqisVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
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
		if value, ok := pqisSafeDefault(name); ok {
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

func pqisSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "nationname", "nation_name":
		return "한국", true
	case "plantname", "plant_name":
		return "사과", true
	case "fromyyyymm", "from_yyyymm":
		return "202501", true
	case "toyyyymm", "to_yyyymm":
		return "202501", true
	case "nationcode", "nation_code":
		return "CN", true
	case "plantcode", "plant_code":
		return "1000", true
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	default:
		return "", false
	}
}

func pqisBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<application") && strings.Contains(text, "wadl"):
		return "wadl"
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

func pqisFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "pqis_endpoint_not_found"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "pqis_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := pqisMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "pqis_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := pqisMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "pqis_provider_error"
}

func pqisMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "pqis_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "pqis_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "pqis_service_key_error"
	default:
		return ""
	}
}

func pqisTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "pqis_request_timeout"
	default:
		return "pqis_request_failed"
	}
}

func (a PQISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := pqisVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling pqis operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required pqis params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := pqisRequestURL(req.Operation, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, pqisTransportError(err, plan)
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
		Provider:       "pqis",
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

func pqisTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("pqis request failed: %s", message)
}
