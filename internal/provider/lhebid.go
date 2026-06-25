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

type LHEBidAdapter struct {
	StaticHostMatcher
}

func NewLHEBidAdapter() LHEBidAdapter {
	return LHEBidAdapter{StaticHostMatcher{Hosts: LHEBidHosts()}}
}

func LHEBidHosts() []string {
	return []string{
		"openapi.ebid.lh.or.kr",
	}
}

func (a LHEBidAdapter) Name() string { return "lh-ebid" }

func (a LHEBidAdapter) Hosts() []string { return LHEBidHosts() }

func (a LHEBidAdapter) Capabilities() []string { return []string{"call"} }

func (a LHEBidAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return lhEBidVerificationParams(params, missing)
}

func (a LHEBidAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a LHEBidAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := lhEBidVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "lh-ebid",
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
		result.Reason = "lh_ebid_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := lhEBidRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		result.Reason = lhEBidTransportReason(err)
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
	result.BodyShape = lhEBidBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = lhEBidFailureReason(providerStatus, message)
	return result
}

func lhEBidRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return providerRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return providerRequestPlan{}, fmt.Errorf("lh-ebid endpoint is not absolute")
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

func lhEBidVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	seenRemaining := map[string]bool{}
	for _, name := range missing {
		if isAuthParam(name) {
			continue
		}
		if value, ok := lhEBidSafeDefault(name); ok {
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

func lhEBidSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "tndrbidregdtstart", "contractdtstart", "opinionregenddtmstart", "opendtmstart":
		return "20240101", true
	case "tndrbidregdtend", "contractdtend", "opinionregenddtmend", "opendtmend":
		return "20240131", true
	case "orderexpectymstart":
		return "202401", true
	case "orderexpectymend":
		return "202412", true
	default:
		return "", false
	}
}

func lhEBidBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case text == "":
		return "empty"
	case strings.Contains(text, "<item>") || strings.Contains(text, "<items>"):
		return "xml_items"
	case strings.Contains(text, "<header>") || strings.Contains(text, "<resultcode>"):
		return "xml_status"
	case strings.HasPrefix(text, "<"):
		return "xml"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "json"
	case strings.Contains(text, "<html"):
		return "html"
	default:
		return "text"
	}
}

func lhEBidFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "lh_ebid_" + normalizeReasonCode(status.ReasonCode)
		}
		if code := lhEBidMessageReason(status.Message); code != "" {
			return code
		}
		if code := lhEBidMessageReason(status.AuthMessage); code != "" {
			return code
		}
		if code := lhEBidMessageReason(status.ErrorMessage); code != "" {
			return code
		}
		if status.Code != "" {
			return "lh_ebid_result_code_" + normalizeReasonCode(status.Code)
		}
	}
	if code := lhEBidMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "lh_ebid_provider_error"
}

func lhEBidMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY IS NOT REGISTERED") || strings.Contains(upper, "SERVICEKEY IS NOT REGISTERED"):
		return "lh_ebid_service_key_not_registered"
	case strings.Contains(message, "인증키가 누락") || strings.Contains(upper, "SERVICE KEY") || strings.Contains(upper, "SERVICEKEY"):
		return "lh_ebid_missing_auth"
	case strings.Contains(upper, "INVALID PARAMETER") || strings.Contains(message, "필수 요청"):
		return "lh_ebid_bad_request_params"
	default:
		return ""
	}
}

func lhEBidTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "lh_ebid_request_timeout"
	default:
		return "lh_ebid_request_failed"
	}
}

func (a LHEBidAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := lhEBidVerificationParams(req.Params, req.MissingParams)
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling lh-ebid operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required lh-ebid params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := lhEBidRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
		return datago.ResponseEnvelope{}, lhEBidTransportError(err, plan)
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
		Provider:       "lh-ebid",
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

func lhEBidTransportError(err error, plan providerRequestPlan) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), plan.url, plan.redacted)
	return fmt.Errorf("lh-ebid request failed: %s", message)
}
