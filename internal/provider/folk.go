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

type FolkAdapter struct {
	StaticHostMatcher
}

func NewFolkAdapter() FolkAdapter {
	return FolkAdapter{StaticHostMatcher{Hosts: FolkHosts()}}
}

func FolkHosts() []string {
	return []string{
		"folkency.nfm.go.kr",
	}
}

func (a FolkAdapter) Name() string { return "folk" }

func (a FolkAdapter) Hosts() []string { return FolkHosts() }

func (a FolkAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a FolkAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := folkVerificationParams(req.Operation, req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "folk",
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
	if folkDetailEndpoint(req.Operation.Endpoint) {
		result.Status = "skipped"
		result.Reason = "folk_missing_required_params"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "folk_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := folkRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
	ok, semanticStatus, message, providerStatus := folkClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = folkBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = folkFailureReason(providerStatus, message)
	return result
}

func folkRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
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
	u.RawQuery = datago.QueryWithServiceKey(q, key)
	redacted := *u
	rq := redacted.Query()
	rq.Set("serviceKey", "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}

func folkVerificationParams(op datago.Operation, params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	for _, name := range missing {
		if value, ok := folkSafeDefault(op, name); ok {
			out[name] = value
			continue
		}
		remaining = append(remaining, name)
	}
	return out, remaining
}

func folkSafeDefault(op datago.Operation, name string) (string, bool) {
	if folkDetailEndpoint(op.Endpoint) {
		return "", false
	}
	endpoint := strings.ToLower(op.Endpoint)
	switch normalizeParamName(name) {
	case "dictionary", "tit_idx", "summary":
		return "", true
	case "korname":
		if strings.Contains(endpoint, "getsoundlist") {
			return "", true
		}
		return "소나무", true
	case "page", "pageno", "page_no", "pageindex", "page_index":
		return "1", true
	case "size", "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	default:
		return "", false
	}
}

func folkDetailEndpoint(endpoint string) bool {
	return strings.Contains(strings.ToLower(endpoint), "getitemlist")
}

func folkClassifyResponse(statusCode int, contentType string, body []byte) (bool, string, string, *datago.ProviderStatus) {
	if statusCode < 200 || statusCode >= 300 {
		return false, "http_error", fmt.Sprintf("HTTP %d", statusCode), nil
	}
	var payload struct {
		ResultCode any    `json:"result_code"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.ResultCode != nil {
		code := folkResultCode(payload.ResultCode)
		status := datago.ProviderStatus{
			Source:  "result_code/message",
			Code:    code,
			Message: strings.TrimSpace(payload.Message),
		}
		if code == "200" {
			status.OK = true
			return true, "provider_ok", status.Message, &status
		}
		status.OK = false
		return false, "provider_error", status.Message, &status
	}
	return datago.ClassifyResponse(statusCode, contentType, body)
}

func folkResultCode(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return fmt.Sprint(value)
	}
}

func folkBodyShape(body []byte) string {
	var payload struct {
		Data struct {
			Total int   `json:"total"`
			List  []any `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		switch {
		case payload.Data.Total > 0 && len(payload.Data.List) > 0:
			return "json_items"
		case payload.Data.Total == 0:
			return "json_empty_items"
		default:
			return "json"
		}
	}
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	case strings.HasPrefix(text, "<"):
		return "xml"
	default:
		return "text"
	}
}

func folkFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "folk_" + normalizeReasonCode(status.ReasonCode)
		}
		if status.Code != "" {
			return "folk_result_code_" + normalizeReasonCode(status.Code)
		}
		if strings.TrimSpace(status.Message) != "" {
			return status.Message
		}
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "folk_provider_error"
}

func (a FolkAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("folk adapter call support is not enabled yet")
}
