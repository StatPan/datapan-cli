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

type DataGGAdapter struct {
	StaticHostMatcher
}

func NewDataGGAdapter() DataGGAdapter {
	return DataGGAdapter{StaticHostMatcher{Hosts: DataGGHosts()}}
}

func DataGGHosts() []string {
	return []string{"data.gg.go.kr"}
}

func (a DataGGAdapter) Name() string { return "data-gg" }

func (a DataGGAdapter) Hosts() []string { return DataGGHosts() }

func (a DataGGAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a DataGGAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params := dataGGVerificationParams(req.Params)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "data-gg",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
	}
	plan, err := dataGGRequestURL(req.Operation.Endpoint, params)
	if err != nil {
		result.Status = "failed"
		result.Reason = "data_gg_bad_endpoint"
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
		result.Reason = "data_gg_bad_request"
		return result
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = dataGGTransportReason(err)
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
	result.BodyShape = dataGGBodyShape(resp.Header.Get("Content-Type"), body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "failed"
		result.SemanticStatus = "http_error"
		result.Reason = fmt.Sprintf("data_gg_http_%d", resp.StatusCode)
		return result
	}
	if result.BodyShape == "html" {
		result.Status = "verified"
		result.SemanticStatus = "html_landing_page"
		return result
	}
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	if strings.TrimSpace(message) != "" {
		result.Reason = message
		return result
	}
	result.Reason = "data_gg_provider_error"
	return result
}

func dataGGVerificationParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range params {
		if strings.TrimSpace(key) != "" && !isAuthParam(key) {
			out[key] = value
		}
	}
	return out
}

func dataGGRequestURL(endpoint string, params map[string]string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("missing endpoint host")
	}
	q := u.Query()
	for key, value := range params {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" || isAuthParam(key) {
			continue
		}
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return providerRequestPlan{url: u.String(), redacted: u.String()}, nil
}

func dataGGBodyShape(contentType string, body []byte) string {
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

func dataGGTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "data_gg_request_timeout"
	}
	return "data_gg_request_failed"
}

func (a DataGGAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("data-gg adapter call support is not enabled yet")
}
