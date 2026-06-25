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

type JejuAdapter struct {
	StaticHostMatcher
}

const jejuUserAgent = "datapan-cli/0.1 (+https://github.com/StatPan/datapan-cli)"

func NewJejuAdapter() JejuAdapter {
	return JejuAdapter{StaticHostMatcher{Hosts: JejuHosts()}}
}

func JejuHosts() []string {
	return []string{
		"data.jeju.go.kr",
	}
}

func (a JejuAdapter) Name() string { return "jeju" }

func (a JejuAdapter) Hosts() []string { return JejuHosts() }

func (a JejuAdapter) Capabilities() []string { return []string{"call"} }

func (a JejuAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return jejuVerificationParams(datago.Operation{}, params, missing)
}

func (a JejuAdapter) PlanCall(req CallRequest) (CallPlan, error) {
	params, _ := jejuVerificationParams(req.Operation, req.Params, req.MissingParams)
	plan, err := jejuRequestURL(req.Operation, params, req.Credential.Value)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{
		URL:          plan.url,
		RedactedURL:  plan.redacted,
		PublicParams: publicQueryParams(plan.redacted),
	}, nil
}

func (a JejuAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JejuAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := jejuVerificationParams(req.Operation, req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "jeju",
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
		result.Reason = "jeju_missing_required_params"
		return result
	}
	plan, err := jejuRequestURL(req.Operation, params, req.Credential.Value)
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
	httpReq.Header.Set("User-Agent", jejuUserAgent)
	httpReq.Header.Set("Accept", "application/xml, application/json, */*")
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = jejuTransportReason(err)
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
	result.BodyShape = jejuBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = jejuFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func jejuRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(jejuOperationEndpoint(op))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("jeju endpoint is not absolute")
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	if strings.TrimSpace(key) != "" {
		u.RawQuery = datago.QueryWithServiceKey(q, key)
	} else {
		u.RawQuery = q.Encode()
	}
	redacted := *u
	rq := redacted.Query()
	for existing := range rq {
		if isAuthParam(existing) {
			rq.Del(existing)
		}
	}
	if strings.TrimSpace(key) != "" {
		rq.Set("serviceKey", "REDACTED")
	}
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func jejuOperationEndpoint(op datago.Operation) string {
	raw := strings.ReplaceAll(strings.TrimSpace(op.Endpoint), "/ ", "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if !strings.EqualFold(u.Host, "data.jeju.go.kr") {
		return raw
	}
	path := strings.TrimRight(u.Path, "/")
	name := strings.ToLower(strings.TrimSpace(op.Name))
	switch path {
	case "/rest/nightpharmacy":
		switch {
		case strings.Contains(name, "첨부") || strings.Contains(name, "file"):
			u.Path = "/rest/nightpharmacy/getNightPharmacyFile"
		case strings.Contains(name, "리스트") || strings.Contains(name, "목록") || strings.Contains(name, "list"):
			u.Path = "/rest/nightpharmacy/getNightPharmacyList"
		}
	}
	return u.String()
}

func jejuVerificationParams(op datago.Operation, params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if isAuthParam(name) {
			continue
		}
		if value, ok := jejuSafeDefault(op, name); ok {
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

func jejuSafeDefault(op datago.Operation, name string) (string, bool) {
	normalized := normalizeParamName(name)
	switch normalized {
	case "pageno", "page_no", "page", "pageindex", "page_index", "startpage", "start_page":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit", "countperpage", "count_per_page":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "xml", true
	case "checkindate", "check_in_date":
		return "20260701", true
	}
	if jejuIsListOperation(op) {
		switch normalized {
		case "datatitle", "data_title", "searchword", "keyword", "query":
			return "", true
		}
	}
	return "", false
}

func jejuIsListOperation(op datago.Operation) bool {
	text := strings.ToLower(strings.TrimSpace(op.Endpoint + " " + op.Name))
	return strings.Contains(text, "list") || strings.Contains(text, "목록") || strings.Contains(text, "리스트")
}

func jejuBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<rfcopenapi>"):
		if strings.Contains(text, "<list>") {
			return "xml_rfcopenapi_list"
		}
		return "xml_rfcopenapi"
	case strings.Contains(text, "<resultcode>"):
		return "xml_status"
	case strings.Contains(text, "<list>"):
		return "xml_list"
	case strings.Contains(text, "<data>"):
		return "xml_data"
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

func jejuFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "jeju_endpoint_not_found"
	}
	if statusCode == http.StatusMethodNotAllowed {
		return "jeju_method_not_allowed"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "jeju_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := jejuMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "jeju_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := jejuMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "jeju_provider_error"
}

func jejuMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "jeju_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "jeju_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "jeju_service_key_error"
	default:
		return ""
	}
}

func jejuTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "jeju_request_timeout"
	default:
		return "jeju_request_failed"
	}
}

func (a JejuAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := jejuVerificationParams(req.Operation, req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling jeju operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required jeju params: %s", strings.Join(missing, ", "))
	}
	plan, err := jejuRequestURL(req.Operation, params, req.Credential.Value)
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
	httpReq.Header.Set("User-Agent", jejuUserAgent)
	httpReq.Header.Set("Accept", "application/xml, application/json, */*")
	resp, err := client.Do(httpReq)
	if err != nil {
		return datago.ResponseEnvelope{}, jejuTransportError(err, plan)
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
		Provider:       "jeju",
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

func jejuTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("jeju request failed: %s", message)
}
