package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SeoulOpenDataAdapter struct {
	StaticHostMatcher
}

func NewSeoulOpenDataAdapter() SeoulOpenDataAdapter {
	return SeoulOpenDataAdapter{StaticHostMatcher{Hosts: SeoulOpenDataHosts()}}
}

func SeoulOpenDataHosts() []string {
	return []string{
		"data.seoul.go.kr",
		"openapi.seoul.go.kr",
		"openapi.seoul.go.kr:8088",
	}
}

func (a SeoulOpenDataAdapter) Name() string { return "seoul-open-data" }

func (a SeoulOpenDataAdapter) Hosts() []string { return SeoulOpenDataHosts() }

func (a SeoulOpenDataAdapter) Capabilities() []string { return []string{"call"} }

func (a SeoulOpenDataAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return seoulOpenDataVerificationParams(params, missing)
}

func (a SeoulOpenDataAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeoulOpenDataAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := seoulOpenDataVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "seoul-open-data",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
		MissingParams:   missing,
	}
	plan, needsCredential, err := seoulOpenDataRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
	if err != nil {
		result.Status = "failed"
		result.Reason = "seoul_open_data_bad_endpoint"
		return result
	}
	if needsCredential && strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		result.URL = plan.redacted
		return result
	}
	if needsCredential && len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "seoul_open_data_missing_required_params"
		result.URL = plan.redacted
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
		result.Reason = seoulOpenDataTransportReason(err)
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = seoulOpenDataBodyShape(resp.Header.Get("Content-Type"), body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "failed"
		result.SemanticStatus = "http_error"
		result.Reason = fmt.Sprintf("seoul_open_data_http_%d", resp.StatusCode)
		return result
	}
	if result.BodyShape == "html" && !needsCredential {
		result.Status = "verified"
		result.SemanticStatus = "html_landing_page"
		return result
	}
	ok, semanticStatus, message, providerStatus := seoulOpenDataClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = seoulOpenDataFailureReason(providerStatus, message)
	return result
}

func seoulOpenDataRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, bool, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, false, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, false, fmt.Errorf("seoul-open-data endpoint is not absolute")
	}
	if !strings.EqualFold(u.Host, "openapi.seoul.go.kr:8088") {
		q := u.Query()
		for k, v := range params {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
				continue
			}
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		return providerRequestPlan{url: u.String(), redacted: u.String()}, false, nil
	}
	format := seoulOpenDataParam(params, "format", "type")
	service := seoulOpenDataParam(params, "service", "service_name", "api_name")
	start := seoulOpenDataParam(params, "start_index", "startindex", "start")
	end := seoulOpenDataParam(params, "end_index", "endindex", "end")
	if format == "" {
		format = "json"
	}
	if start == "" {
		start = "1"
	}
	if end == "" {
		end = "5"
	}
	requestKey := strings.TrimSpace(key)
	if requestKey == "" {
		requestKey = "REDACTED"
	}
	path := strings.Trim(strings.TrimSpace(u.Path), "/")
	if strings.Contains(path, "{") || path == "" {
		path = strings.Join([]string{
			url.PathEscape(requestKey),
			url.PathEscape(format),
			url.PathEscape(service),
			url.PathEscape(start),
			url.PathEscape(end),
		}, "/")
	}
	u.Path = "/" + path
	u.RawQuery = ""
	redacted := *u
	redacted.Path = strings.ReplaceAll(redacted.Path, url.PathEscape(requestKey), "REDACTED")
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, true, nil
}

func seoulOpenDataVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" && !isAuthParam(k) {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if isAuthParam(name) {
			continue
		}
		if value, ok := seoulOpenDataSafeDefault(name); ok {
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

func seoulOpenDataSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "format", "type":
		return "json", true
	case "start_index", "startindex", "start":
		return "1", true
	case "end_index", "endindex", "end":
		return "5", true
	default:
		return "", false
	}
}

func seoulOpenDataParam(params map[string]string, names ...string) string {
	for _, want := range names {
		normalizedWant := normalizeParamName(want)
		for key, value := range params {
			if normalizeParamName(key) == normalizedWant {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func seoulOpenDataBodyShape(contentType string, body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	lowerContentType := strings.ToLower(contentType)
	switch {
	case text == "":
		return "empty"
	case strings.Contains(lowerContentType, "html") || strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html"):
		return "html"
	case strings.Contains(lowerContentType, "json") || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	case strings.HasPrefix(text, "<"):
		return "xml"
	default:
		return "text"
	}
}

func seoulOpenDataClassifyResponse(statusCode int, contentType string, body []byte) (bool, string, string, *datago.ProviderStatus) {
	if status, ok := seoulOpenDataResultStatus(body); ok {
		if strings.EqualFold(status.Code, "INFO-000") {
			status.OK = true
			return true, "provider_ok", status.Message, &status
		}
		status.OK = false
		msg := status.Message
		if msg == "" {
			msg = "provider returned RESULT.CODE " + status.Code
		}
		return false, "provider_error", msg, &status
	}
	return datago.ClassifyResponse(statusCode, contentType, body)
}

func seoulOpenDataResultStatus(body []byte) (datago.ProviderStatus, bool) {
	text := strings.TrimSpace(string(body))
	if text == "" || !strings.Contains(text, `"RESULT"`) {
		return datago.ProviderStatus{}, false
	}
	code := seoulOpenDataJSONField(text, "CODE")
	message := seoulOpenDataJSONField(text, "MESSAGE")
	if code == "" && message == "" {
		return datago.ProviderStatus{}, false
	}
	return datago.ProviderStatus{
		Source:  "RESULT",
		Code:    code,
		Message: message,
	}, true
}

func seoulOpenDataJSONField(text, field string) string {
	pattern := `"` + field + `"\s*:\s*"([^"]*)"`
	matches := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func seoulOpenDataFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if code := strings.TrimSpace(status.Code); code != "" {
			return "seoul_open_data_result_code_" + normalizeReasonCode(code)
		}
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "seoul_open_data_provider_error"
}

func seoulOpenDataTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "seoul_open_data_request_timeout"
	}
	return "seoul_open_data_request_failed"
}

func (a SeoulOpenDataAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := a.PrepareCallParams(req.Params, req.MissingParams)
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required seoul-open-data params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, _, err := seoulOpenDataRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, seoulOpenDataTransportError(err, plan)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	ok, semanticStatus, message, providerStatus := seoulOpenDataClassifyResponse(resp.StatusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "seoul-open-data",
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

func seoulOpenDataTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("seoul-open-data request failed: %s", message)
}
