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

type JeonjuAdapter struct {
	StaticHostMatcher
}

func NewJeonjuAdapter() JeonjuAdapter {
	return JeonjuAdapter{StaticHostMatcher{Hosts: JeonjuHosts()}}
}

func JeonjuHosts() []string {
	return []string{
		"openapi.jeonju.go.kr",
	}
}

func (a JeonjuAdapter) Name() string { return "jeonju" }

func (a JeonjuAdapter) Hosts() []string { return JeonjuHosts() }

func (a JeonjuAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return jeonjuVerificationParams(params, missing)
}

func (a JeonjuAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JeonjuAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := jeonjuVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "jeonju",
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
		result.Reason = "jeonju_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := jeonjuRequestURL(req.Operation, params, req.Credential.Value)
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
	result.BodyShape = jeonjuBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = jeonjuFailureReason(providerStatus, message)
	return result
}

func jeonjuRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(op.Endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	authName := jeonjuAuthParamName(op)
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = datago.QueryWithCredentialParam(q, authName, key)
	redacted := *u
	rq := redacted.Query()
	for existing := range rq {
		if strings.EqualFold(existing, authName) {
			rq.Del(existing)
		}
	}
	rq.Set(authName, "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func jeonjuAuthParamName(op datago.Operation) string {
	for _, param := range op.RequestParams {
		name := strings.TrimSpace(param.Name)
		if isAuthParam(name) {
			return name
		}
	}
	return "serviceKey"
}

func jeonjuVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := jeonjuSafeDefault(name); ok {
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

func jeonjuSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index", "startpage", "start_page", "indexnum", "index_num":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit", "countperpage", "count_per_page":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "json", true
	case "posx", "x", "longitude", "lng", "lon":
		return "127.1480", true
	case "posy", "y", "latitude", "lat":
		return "35.8242", true
	case "searchdts", "search_dts", "distance", "radius":
		return "1000", true
	case "loadresult":
		return "백제대로", true
	case "dong", "patroldong":
		return "중앙동", true
	case "gu":
		return "완산구", true
	case "subject", "instplacenm", "instfactype", "roadadd", "patroltitle", "patrolsid", "insarea", "newaddr", "oldaddr", "pacnm", "totalcount", "centernm", "shopnm", "shopaddr":
		return "", true
	default:
		return "", false
	}
}

func jeonjuBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, `"item"`), strings.Contains(text, `"items"`), strings.Contains(text, `"data"`):
		return "json_items"
	case strings.Contains(text, "<items/>") || strings.Contains(text, "<items></items>"):
		return "xml_empty_items"
	case strings.Contains(text, `"resultcode"`), strings.Contains(text, `"result_code"`), strings.Contains(text, `"code"`):
		return "json_status"
	case strings.Contains(text, "<resultcode>"):
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

func jeonjuFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "jeonju_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := jeonjuMessageReason(status.Message); code != "" {
			return code
		}
		if code := jeonjuMessageReason(status.AuthMessage); code != "" {
			return code
		}
		if code := jeonjuMessageReason(status.ErrorMessage); code != "" {
			return code
		}
		if status.Code != "" {
			return "jeonju_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := jeonjuMessageReason(message); code != "" {
		return code
	}
	switch strings.ToUpper(strings.TrimSpace(message)) {
	case "HTTP 404":
		return "jeonju_http_404_not_found"
	case "HTTP 405":
		return "jeonju_http_405_method_not_allowed"
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "jeonju_provider_error"
}

func jeonjuMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "jeonju_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "jeonju_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY"):
		return "jeonju_service_key_error"
	default:
		return ""
	}
}

func (a JeonjuAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := a.PrepareCallParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling jeonju operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required jeonju params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := jeonjuRequestURL(req.Operation, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, jeonjuTransportError(err, plan)
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
		Provider:       "jeonju",
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

func jeonjuTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("jeonju request failed: %s", message)
}
