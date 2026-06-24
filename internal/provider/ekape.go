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

type EKAPEAdapter struct {
	StaticHostMatcher
}

func NewEKAPEAdapter() EKAPEAdapter {
	return EKAPEAdapter{StaticHostMatcher{Hosts: EKAPEHosts()}}
}

func EKAPEHosts() []string {
	return []string{
		"data.ekape.or.kr",
	}
}

func (a EKAPEAdapter) Name() string { return "ekape" }

func (a EKAPEAdapter) Hosts() []string { return EKAPEHosts() }

func (a EKAPEAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a EKAPEAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := ekapeVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "ekape",
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
		result.Reason = "ekape_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := ekapeRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = err.Error()
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
	result.BodyShape = ekapeBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = ekapeFailureReason(providerStatus, message)
	return result
}

func ekapeRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
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

func ekapeVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	for _, name := range missing {
		if value, ok := ekapeSafeDefault(name); ok {
			out[name] = value
			continue
		}
		remaining = append(remaining, name)
	}
	return out, remaining
}

func ekapeSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "issuedate", "issue_date", "startymd", "start_ymd", "endymd", "end_ymd":
		return "20240101", true
	case "standym", "stand_ym", "basemonth", "base_month":
		return "202401", true
	case "issueno", "issue_no", "lotno", "lot_no", "seq", "no":
		return "1", true
	case "judgekind", "judge_kind":
		return "1", true
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

func ekapeBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html"):
		return "html"
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, "<notice"):
		return "xml_notice"
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

func ekapeFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "ekape_" + normalizeReasonCode(status.ReasonCode)
		}
		if status.Code != "" {
			if code := ekapeMessageReason(status.Message); code != "" {
				return code
			}
			return "ekape_result_code_" + normalizeReasonCode(status.Code)
		}
		if code := ekapeMessageReason(status.Message); code != "" {
			return code
		}
		if code := ekapeMessageReason(status.AuthMessage); code != "" {
			return code
		}
		if code := ekapeMessageReason(status.ErrorMessage); code != "" {
			return code
		}
	}
	if code := ekapeMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "ekape_provider_error"
}

func ekapeMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "ekape_service_key_not_registered"
	case strings.Contains(upper, "SERVICE KEY"):
		return "ekape_service_key_error"
	case strings.Contains(upper, "HTML"):
		return "ekape_html_response"
	default:
		return ""
	}
}

func (a EKAPEAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ekape adapter call support is not enabled yet")
}
