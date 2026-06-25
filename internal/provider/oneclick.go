package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type OneclickLawAdapter struct {
	StaticHostMatcher
}

func NewOneclickLawAdapter() OneclickLawAdapter {
	return OneclickLawAdapter{StaticHostMatcher{Hosts: OneclickLawHosts()}}
}

func OneclickLawHosts() []string {
	return []string{
		"oneclick.law.go.kr",
		"oneclick.law.go.kr:80",
	}
}

func (a OneclickLawAdapter) Name() string { return "oneclick-law" }

func (a OneclickLawAdapter) Hosts() []string { return OneclickLawHosts() }

func (a OneclickLawAdapter) Capabilities() []string { return []string{"call"} }

func (a OneclickLawAdapter) PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string) {
	return oneclickVerificationParams(params, missing)
}

func (a OneclickLawAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a OneclickLawAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := oneclickVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "oneclick-law",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		VerifiedAt:      verifiedAt(req.VerifiedAt),
		Params:          publicParams(params),
		MissingParams:   missing,
	}
	if !oneclickSOAPOperation(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "oneclick_non_soap_operation"
		return result
	}
	if qnetApprovalRequired(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "approval_required"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "oneclick_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" && oneclickNeedsServiceKey(req.Spec, req.Operation) {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := oneclickSOAPPlan(req.Operation.Endpoint, req.Spec, req.Operation, params, req.Credential.Value)
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	result.URL = plan.redacted
	resp, body, contentType, statusCode, err := oneclickDoSOAP(ctx, req.HTTP, plan)
	if err != nil {
		result.Status = "failed"
		result.Reason = oneclickTransportReason(err)
		return result
	}
	_ = resp
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(statusCode, contentType, body)
	result.HTTPStatus = statusCode
	result.BodyShape = oneclickBodyShape(body)
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = oneclickFailureReason(providerStatus, message)
	return result
}

func oneclickVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
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
		if value, ok := oneclickSafeDefault(name); ok {
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

func oneclickSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "requestmsgid", "request_msg_id":
		return "datapan", true
	case "requesttime", "request_time":
		return "20000101000000", true
	case "callbackuri", "callback_uri":
		return "", true
	case "nowpageno", "now_page_no", "page", "pageno", "page_no", "pageindex", "page_index":
		return "1", true
	case "pagemg", "page_mg", "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "section", "csmseq", "csm_seq", "ccfno", "ccf_no", "ccino", "cci_no", "cnpclsno", "cnp_cls_no", "onhunque_seq", "onhunqueSeq":
		return "1", true
	case "txtquery", "txt_query", "query", "keyword", "searchkeyword", "search_keyword":
		return "법", true
	default:
		return "", false
	}
}

func oneclickSOAPOperation(spec datago.Spec, op datago.Operation) bool {
	return strings.EqualFold(rawStringFromMerged(spec, op, "api_type"), "SOAP")
}

func oneclickNeedsServiceKey(spec datago.Spec, op datago.Operation) bool {
	params := rawStringFromMerged(spec, op, "request_param_nm_en")
	for _, name := range strings.Split(params, ",") {
		if isAuthParam(strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

type oneclickSOAPRequestPlan struct {
	url       string
	redacted  string
	action    string
	body      []byte
	bodyShape string
}

func oneclickSOAPPlan(endpoint string, spec datago.Spec, op datago.Operation, params map[string]string, key string) (oneclickSOAPRequestPlan, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return oneclickSOAPRequestPlan{}, err
	}
	if u.Scheme == "" || u.Host == "" {
		return oneclickSOAPRequestPlan{}, fmt.Errorf("invalid oneclick endpoint")
	}
	action := rawStringFromMerged(spec, op, "operation_url")
	if action == "" {
		return oneclickSOAPRequestPlan{}, fmt.Errorf("oneclick missing SOAP operation_url")
	}
	body := oneclickSOAPEnvelope(action, params, key)
	return oneclickSOAPRequestPlan{
		url:       u.String(),
		redacted:  u.String(),
		action:    action,
		body:      body,
		bodyShape: "soap_envelope",
	}, nil
}

func oneclickSOAPEnvelope(action string, params map[string]string, key string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">`)
	b.WriteString(`<soapenv:Header/>`)
	b.WriteString(`<soapenv:Body>`)
	b.WriteString(`<`)
	b.WriteString(xmlName(action))
	b.WriteString(`>`)
	if strings.TrimSpace(key) != "" {
		b.WriteString(`<ServiceKey>`)
		b.WriteString(xmlEscape(key))
		b.WriteString(`</ServiceKey>`)
	}
	for _, name := range sortedParamNames(params) {
		if isAuthParam(name) {
			continue
		}
		b.WriteString(`<`)
		b.WriteString(xmlName(name))
		b.WriteString(`>`)
		b.WriteString(xmlEscape(params[name]))
		b.WriteString(`</`)
		b.WriteString(xmlName(name))
		b.WriteString(`>`)
	}
	b.WriteString(`</`)
	b.WriteString(xmlName(action))
	b.WriteString(`>`)
	b.WriteString(`</soapenv:Body></soapenv:Envelope>`)
	return []byte(b.String())
}

func sortedParamNames(params map[string]string) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sortStrings(names)
	return names
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func xmlName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "value"
	}
	return value
}

func xmlEscape(value string) string {
	var b bytes.Buffer
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func oneclickDoSOAP(ctx context.Context, client HTTPDoer, plan oneclickSOAPRequestPlan) (*http.Response, []byte, string, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.url, bytes.NewReader(plan.body))
	if err != nil {
		return nil, nil, "", 0, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", plan.action)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp, nil, resp.Header.Get("Content-Type"), resp.StatusCode, err
	}
	return resp, body, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func oneclickBodyShape(body []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	switch {
	case strings.Contains(text, "<soap:envelope") || strings.Contains(text, "<soapenv:envelope"):
		return "soap_envelope"
	case strings.Contains(text, "<returncode>") || strings.Contains(text, "<err_msg>") || strings.Contains(text, "<errmsg>"):
		return "xml_status"
	case strings.Contains(text, "<!doctype html") || strings.Contains(text, "<html"):
		return "html"
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

func oneclickFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode != "" {
			return "oneclick_" + normalizeReasonCode(status.ReasonCode)
		}
		if status.Code != "" {
			return "oneclick_result_code_" + normalizeReasonCode(status.Code)
		}
		if code := oneclickMessageReason(status.Message); code != "" {
			return code
		}
		if code := oneclickMessageReason(status.ErrorMessage); code != "" {
			return code
		}
	}
	if code := oneclickMessageReason(message); code != "" {
		return code
	}
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "oneclick_provider_error"
}

func oneclickMessageReason(message string) string {
	upper := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(upper, "SERVICE KEY"):
		return "oneclick_service_key_error"
	case strings.Contains(upper, "NO SUCH OPERATION"):
		return "oneclick_unknown_soap_operation"
	case strings.Contains(upper, "RETURN CODE") && strings.Contains(upper, "1"):
		return "oneclick_return_code_error"
	default:
		return ""
	}
}

func oneclickTransportReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "actively refused") ||
		strings.Contains(lower, "refused it") ||
		strings.Contains(lower, "연결을 거부"):
		return "oneclick_connection_refused"
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "oneclick_request_timeout"
	default:
		return "oneclick_request_failed"
	}
}

func (a OneclickLawAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	params, missing := oneclickVerificationParams(req.Params, req.MissingParams)
	if !oneclickSOAPOperation(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("oneclick operation is not SOAP: %s", req.Operation.Name)
	}
	if qnetApprovalRequired(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("approval required before calling oneclick operation %s", req.Operation.Name)
	}
	if len(missing) > 0 {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing required oneclick params: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(req.Credential.Value) == "" && oneclickNeedsServiceKey(req.Spec, req.Operation) {
		return datago.ResponseEnvelope{}, fmt.Errorf("missing auth")
	}
	plan, err := oneclickSOAPPlan(req.Operation.Endpoint, req.Spec, req.Operation, params, req.Credential.Value)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	_, body, contentType, statusCode, err := oneclickDoSOAP(ctx, req.HTTP, plan)
	if err != nil {
		return datago.ResponseEnvelope{}, fmt.Errorf("oneclick request failed: %s", oneclickTransportReason(err))
	}
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(statusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "oneclick-law",
		Dataset:        req.Spec.ID,
		Operation:      req.Operation.Name,
		StatusCode:     statusCode,
		ContentType:    contentType,
		SemanticStatus: semanticStatus,
		Message:        message,
		ProviderStatus: providerStatus,
		URL:            plan.redacted,
		Body:           string(body),
	}, nil
}
