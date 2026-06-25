package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MyHomeAdapter struct {
	StaticHostMatcher
}

func NewMyHomeAdapter() MyHomeAdapter {
	return MyHomeAdapter{StaticHostMatcher{Hosts: MyHomeHosts()}}
}

func MyHomeHosts() []string {
	return []string{
		"data.myhome.go.kr:443",
	}
}

func (a MyHomeAdapter) Name() string { return "myhome" }

func (a MyHomeAdapter) Hosts() []string { return MyHomeHosts() }

func (a MyHomeAdapter) Capabilities() []string { return []string{"call"} }

func (a MyHomeAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return myHomeVerificationParams(params, missing)
}

func (a MyHomeAdapter) PlanCall(req CallRequest) (CallPlan, error) {
	params, _ := myHomeVerificationParams(req.Params, req.MissingParams)
	plan, err := myHomeRequestURL(req.Operation, params, req.Credential.Value)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{
		URL:          plan.url,
		RedactedURL:  plan.redacted,
		PublicParams: publicQueryParams(plan.redacted),
	}, nil
}

func (a MyHomeAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MyHomeAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := myHomeVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "myhome",
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
		result.Reason = "myhome_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := myHomeRequestURL(req.Operation, params, req.Credential.Value)
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
		result.Reason = myHomeTransportReason(err)
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	ok, semanticStatus, message, providerStatus := myHomeClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = myHomeBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = myHomeFailureReason(providerStatus, message, resp.StatusCode)
	return result
}

func myHomeRequestURL(op datago.Operation, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(op.Endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("myhome endpoint is not absolute")
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	authName := myHomeAuthParamName(op)
	u.RawQuery = datago.QueryWithCredentialParam(q, authName, key)
	redacted := *u
	rq := redacted.Query()
	rq.Set(authName, "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func myHomeAuthParamName(op datago.Operation) string {
	for _, param := range op.RequestParams {
		if strings.EqualFold(strings.TrimSpace(param.Name), "ServiceKey") {
			return param.Name
		}
	}
	return "ServiceKey"
}

func myHomeVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
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
		if value, ok := myHomeSafeDefault(name); ok {
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

func myHomeSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "brtcCode", "brtccode", "brtc_code", "signgucode", "signgu_code":
		return "", true
	default:
		return "", false
	}
}

func myHomeClassifyResponse(statusCode int, contentType string, body []byte) (bool, string, string, *datago.ProviderStatus) {
	if status, ok := myHomeJSONStatus(body); ok {
		if myHomeCodeOK(status.Code) {
			status.OK = true
			return true, "provider_ok", status.Message, &status
		}
		status.OK = false
		message := status.Message
		if message == "" {
			message = "provider returned code " + status.Code
		}
		return false, "provider_error", message, &status
	}
	return datago.ClassifyResponse(statusCode, contentType, body)
}

func myHomeJSONStatus(body []byte) (datago.ProviderStatus, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return datago.ProviderStatus{}, false
	}
	code, _ := payload["code"].(string)
	msg, _ := payload["msg"].(string)
	code = strings.TrimSpace(code)
	msg = strings.TrimSpace(msg)
	if code == "" && msg == "" {
		return datago.ProviderStatus{}, false
	}
	return datago.ProviderStatus{
		Source:  "code/msg",
		Code:    code,
		Message: msg,
	}, true
}

func myHomeCodeOK(code string) bool {
	code = strings.TrimSpace(strings.ToUpper(code))
	return code == "00" || code == "0" || code == "NORMAL_SERVICE"
}

func myHomeBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	case strings.Contains(text, "<html"):
		return "html"
	case strings.HasPrefix(text, "<"):
		return "xml"
	default:
		return "text"
	}
}

func myHomeFailureReason(status *datago.ProviderStatus, message string, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "myhome_endpoint_not_found"
	}
	if status != nil {
		if status.ReasonCode != "" {
			return "myhome_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := myHomeMessageReason(status.Message); code != "" {
			return code
		}
		if status.Code != "" {
			return "myhome_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := myHomeMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "myhome_provider_error"
}

func myHomeMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED"):
		return "myhome_service_key_not_registered"
	case strings.Contains(upper, "SERVICE ACCESS DENIED"):
		return "myhome_service_access_denied"
	case strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "myhome_service_key_error"
	default:
		return ""
	}
}

func myHomeTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "myhome_request_timeout"
	default:
		return "myhome_request_failed"
	}
}

func (a MyHomeAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := myHomeVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling myhome operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required myhome params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := myHomeRequestURL(req.Operation, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, myHomeTransportError(err, plan)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	ok, semanticStatus, message, providerStatus := myHomeClassifyResponse(resp.StatusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "myhome",
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

func myHomeTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("myhome request failed: %s", message)
}
