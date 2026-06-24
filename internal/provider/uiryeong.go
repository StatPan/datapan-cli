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

type UiryeongAdapter struct {
	StaticHostMatcher
}

func NewUiryeongAdapter() UiryeongAdapter {
	return UiryeongAdapter{StaticHostMatcher{Hosts: UiryeongHosts()}}
}

func UiryeongHosts() []string {
	return []string{
		"data.uiryeong.go.kr",
	}
}

func (a UiryeongAdapter) Name() string { return "uiryeong" }

func (a UiryeongAdapter) Hosts() []string { return UiryeongHosts() }

func (a UiryeongAdapter) Capabilities() []string { return []string{"call"} }

func (a UiryeongAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return uiryeongVerificationParams(datago.Operation{}, params, missing)
}

func (a UiryeongAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a UiryeongAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := uiryeongVerificationParams(req.Operation, req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "uiryeong",
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
		result.Reason = "uiryeong_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := uiryeongRequestURL(req.Operation, params, req.Credential.Value)
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
	result.BodyShape = uiryeongBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = uiryeongFailureReason(providerStatus, message)
	return result
}

func uiryeongRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(op.Endpoint))
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
	authName := uiryeongAuthParamName(op)
	q.Set(authName, key)
	u.RawQuery = q.Encode()
	redacted := *u
	rq := redacted.Query()
	rq.Set(authName, "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func uiryeongAuthParamName(op datago.Operation) string {
	for _, param := range op.RequestParams {
		if strings.EqualFold(strings.TrimSpace(param.Name), "ServiceKey") {
			return param.Name
		}
	}
	return "ServiceKey"
}

func uiryeongVerificationParams(op datago.Operation, params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := uiryeongSafeDefault(op, name); ok {
			if strings.TrimSpace(value) != "" {
				out[name] = value
			} else {
				delete(out, name)
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

func uiryeongSafeDefault(op datago.Operation, name string) (string, bool) {
	normalized := normalizeParamName(name)
	switch normalized {
	case "pageno", "page_no", "page", "pageindex", "page_index", "startpage", "start_page":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit", "countperpage", "count_per_page":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	}
	if uiryeongIsListOperation(op) {
		if strings.HasSuffix(normalized, "entid") ||
			normalized == "entid" ||
			strings.HasSuffix(normalized, "title") ||
			strings.HasSuffix(normalized, "addr") ||
			strings.HasSuffix(normalized, "newaddr") ||
			normalized == "roadaddr" ||
			strings.HasSuffix(normalized, "type") ||
			strings.HasSuffix(normalized, "kind") ||
			strings.HasSuffix(normalized, "classify") ||
			strings.HasSuffix(normalized, "rank") ||
			strings.HasSuffix(normalized, "num") ||
			strings.HasSuffix(normalized, "ppsdiv") ||
			strings.HasSuffix(normalized, "info") {
			return "", true
		}
	}
	if strings.HasSuffix(normalized, "id") || strings.HasSuffix(normalized, "_id") {
		return "", false
	}
	return "", false
}

func uiryeongIsListOperation(op datago.Operation) bool {
	text := strings.ToLower(strings.TrimSpace(op.Endpoint + " " + op.Name))
	if strings.Contains(text, "view") || strings.Contains(text, "file") || strings.Contains(text, "사진") || strings.Contains(text, "상세") {
		return false
	}
	return strings.Contains(text, "list") || strings.Contains(text, "목록") || strings.Contains(text, "현황")
}

func uiryeongBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.Contains(text, "<list>"):
		return "xml_list"
	case strings.Contains(text, "<data>"):
		return "xml_data"
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

func uiryeongFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "uiryeong_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := uiryeongMessageReason(status.Message); code != "" {
			return code
		}
		if code := uiryeongMessageReason(status.AuthMessage); code != "" {
			return code
		}
		if code := uiryeongMessageReason(status.ErrorMessage); code != "" {
			return code
		}
		if status.Code != "" {
			return "uiryeong_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := uiryeongMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "uiryeong_provider_error"
}

func uiryeongMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(message, "등록되지 않은 서비스키") || strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "uiryeong_service_key_not_registered"
	case strings.Contains(message, "잘못된 요청 파라메터") || strings.Contains(upper, "INVALID") && strings.Contains(upper, "PARAM"):
		return "uiryeong_bad_request_params"
	case strings.Contains(upper, "SERVICE KEY"):
		return "uiryeong_service_key_error"
	case strings.Contains(upper, "SQLMAPCLIENT") || strings.Contains(upper, "SQL"):
		return "uiryeong_provider_sql_error"
	default:
		return ""
	}
}

func (a UiryeongAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := uiryeongVerificationParams(req.Operation, req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling uiryeong operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required uiryeong params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := uiryeongRequestURL(req.Operation, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, uiryeongTransportError(err, plan)
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
		Provider:       "uiryeong",
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

func uiryeongTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("uiryeong request failed: %s", message)
}
