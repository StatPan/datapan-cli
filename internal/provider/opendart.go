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

type OpenDARTAdapter struct {
	StaticHostMatcher
}

func NewOpenDARTAdapter() OpenDARTAdapter {
	return OpenDARTAdapter{StaticHostMatcher{Hosts: OpenDARTHosts()}}
}

func OpenDARTHosts() []string {
	return []string{"opendart.fss.or.kr"}
}

func (a OpenDARTAdapter) Name() string { return "opendart" }

func (a OpenDARTAdapter) Hosts() []string { return OpenDARTHosts() }

func (a OpenDARTAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a OpenDARTAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := opendartVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "opendart",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
		MissingParams:   missing,
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "opendart_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := opendartRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
	if err != nil {
		result.Status = "failed"
		result.Reason = "opendart_bad_endpoint"
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
	result.HTTPStatus = resp.StatusCode
	result.BodyShape = landingBodyShape(resp.Header.Get("Content-Type"), body)
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
	result.Reason = "opendart_provider_error"
	return result
}

func (a OpenDARTAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("opendart adapter call support is not enabled yet")
}

func opendartVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if value, ok := opendartSafeDefault(name); ok {
			out[name] = value
			continue
		}
		if isAuthParam(name) {
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

func opendartSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	default:
		return "", false
	}
}

func opendartRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("missing endpoint host")
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" || isAuthParam(k) {
			continue
		}
		q.Set(k, v)
	}
	q.Set("crtfc_key", key)
	u.RawQuery = q.Encode()
	redacted := *u
	rq := redacted.Query()
	rq.Set("crtfc_key", "REDACTED")
	redacted.RawQuery = rq.Encode()
	return providerRequestPlan{url: u.String(), redacted: redacted.String()}, nil
}
