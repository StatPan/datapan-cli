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

type GwanakAdapter struct {
	StaticHostMatcher
}

func NewGwanakAdapter() GwanakAdapter {
	return GwanakAdapter{StaticHostMatcher{Hosts: GwanakHosts()}}
}

func GwanakHosts() []string {
	return []string{"data.gwanak.go.kr"}
}

func (a GwanakAdapter) Name() string { return "gwanak" }

func (a GwanakAdapter) Hosts() []string { return GwanakHosts() }

func (a GwanakAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GwanakAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params := gwanakVerificationParams(req.Params)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "gwanak",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
	}
	plan, err := gwanakRequestURL(req.Operation.Endpoint, params)
	if err != nil {
		result.Status = "failed"
		result.Reason = "gwanak_bad_endpoint"
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
		result.Reason = "gwanak_bad_request"
		return result
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "failed"
		result.Reason = gwanakTransportReason(err)
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
	result.BodyShape = gwanakBodyShape(resp.Header.Get("Content-Type"), body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "failed"
		result.SemanticStatus = "http_error"
		result.Reason = fmt.Sprintf("gwanak_http_%d", resp.StatusCode)
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
	result.Reason = "gwanak_provider_error"
	return result
}

func gwanakVerificationParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range params {
		if strings.TrimSpace(key) != "" && !isAuthParam(key) {
			out[key] = value
		}
	}
	return out
}

func gwanakRequestURL(endpoint string, params map[string]string) (providerRequestPlan, error) {
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

func gwanakBodyShape(contentType string, body []byte) string {
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

func gwanakTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "gwanak_request_timeout"
	}
	return "gwanak_request_failed"
}

func (a GwanakAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gwanak adapter call support is not enabled yet")
}
