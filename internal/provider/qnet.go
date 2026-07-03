package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type QNetAdapter struct {
	StaticHostMatcher
}

func NewQNetAdapter() QNetAdapter {
	return QNetAdapter{StaticHostMatcher{Hosts: QNetHosts()}}
}

func QNetHosts() []string {
	return []string{
		"openapi.q-net.or.kr",
		"c.q-net.or.kr",
		"open.api.q-net.or.kr",
	}
}

func (a QNetAdapter) Name() string { return "q-net" }

func (a QNetAdapter) Hosts() []string { return QNetHosts() }

func (a QNetAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a QNetAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	params, missing := qnetVerificationParams(req.Params, req.MissingParams)
	result := datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "q-net",
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
	if qnetWADLMetadataEndpoint(req.Operation.Endpoint) {
		result.Status = "skipped"
		result.Reason = "qnet_wadl_metadata_only"
		return result
	}
	if result.EndpointHost == "c.q-net.or.kr" {
		result.Status = "skipped"
		result.Reason = "qnet_separate_service_key_required"
		return result
	}
	if len(missing) > 0 {
		result.Status = "skipped"
		result.Reason = "qnet_missing_required_params"
		return result
	}
	if strings.TrimSpace(req.Credential.Value) == "" {
		result.Status = "skipped"
		result.Reason = "missing_auth"
		return result
	}
	plan, err := qnetRequestURL(req.Operation.Endpoint, params, req.Credential.Value)
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
	result.BodyShape = qnetBodyShape(body)
	if qnetWADLMetadataBody(body) {
		result.Status = "failed"
		result.Reason = "qnet_wadl_metadata_response"
		result.SemanticStatus = "metadata_response"
		return result
	}
	if status, ok := qnetJSONMessageStatus(body); ok && !status.OK {
		result.Status = "failed"
		result.Reason = qnetFailureReason(&status, status.Message)
		result.SemanticStatus = "provider_error"
		result.ProviderStatus = &status
		return result
	}
	result.SemanticStatus = semanticStatus
	result.ProviderStatus = providerStatus
	if ok {
		result.Status = "verified"
		return result
	}
	result.Status = "failed"
	result.Reason = qnetFailureReason(providerStatus, message)
	return result
}

type providerRequestPlan struct {
	url      string
	redacted string
}

func qnetRequestURL(endpoint string, params map[string]string, key string) (providerRequestPlan, error) {
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

func qnetVerificationParams(params map[string]string, missing []string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range params {
		if strings.TrimSpace(k) != "" {
			out[k] = v
		}
	}
	remaining := make([]string, 0)
	for _, name := range missing {
		if value, ok := qnetSafeDefault(name); ok {
			out[name] = value
			continue
		}
		remaining = append(remaining, name)
	}
	return out, remaining
}

func qnetSafeDefault(name string) (string, bool) {
	switch normalizeParamName(name) {
	case "baseyy":
		return "2023", true
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "json", true
	default:
		return "", false
	}
}

func qnetBodyShape(body []byte) string {
	text := strings.ToLower(string(body))
	switch {
	case qnetWADLMetadataBody(body):
		return "wadl_metadata"
	case strings.Contains(text, "<item>"):
		return "xml_items"
	case strings.Contains(text, `"message"`):
		return "json_message"
	case strings.TrimSpace(text) == "":
		return "empty"
	case strings.HasPrefix(strings.TrimSpace(text), "<"):
		return "xml"
	case strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "["):
		return "json"
	default:
		return "text"
	}
}

func qnetWADLMetadataEndpoint(endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	_, hasWADL := u.Query()["_wadl"]
	return hasWADL
}

func qnetWADLMetadataBody(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "<application") && strings.Contains(text, "wadl")
}

func qnetJSONMessageStatus(body []byte) (datago.ProviderStatus, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return datago.ProviderStatus{}, false
	}
	value, ok := payload["message"].(string)
	if !ok {
		return datago.ProviderStatus{}, false
	}
	message := strings.TrimSpace(value)
	if message == "" {
		return datago.ProviderStatus{}, false
	}
	upper := strings.ToUpper(message)
	status := datago.ProviderStatus{
		Source:  "message",
		Message: message,
	}
	if strings.Contains(upper, "ERROR") || strings.Contains(upper, "NOT REGISTERED") {
		status.OK = false
		status.Code = "MESSAGE_ERROR"
		status.ReasonCode = qnetReasonCode(message, status.Code)
		return status, true
	}
	status.OK = true
	return status, true
}

func qnetFailureReason(status *datago.ProviderStatus, message string) string {
	if status != nil {
		if status.ReasonCode == "" {
			status.ReasonCode = qnetReasonCode(status.Message, status.Code)
		}
		if status.ReasonCode != "" {
			return status.ReasonCode
		}
	}
	if code := qnetReasonCode(message, ""); code != "" {
		return code
	}
	return message
}

func qnetReasonCode(message, code string) string {
	upperMessage := strings.ToUpper(strings.TrimSpace(message))
	upperCode := strings.ToUpper(strings.TrimSpace(code))
	switch {
	case strings.Contains(upperMessage, "SERVICE KEY IS NOT REGISTERED"):
		return "qnet_service_key_not_registered"
	case upperCode == "99" && strings.Contains(upperMessage, "FAILED TO VALIDATE"):
		return "qnet_connection_validation_failed"
	case upperCode == "MESSAGE_ERROR":
		return "qnet_message_error"
	default:
		return ""
	}
}

func verifiedAt(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func normalizeParamName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	replacer := strings.NewReplacer("-", "_", " ", "_")
	return replacer.Replace(name)
}

func isAuthParam(name string) bool {
	normalized := normalizeParamName(name)
	return normalized == "servicekey" ||
		normalized == "service_key" ||
		normalized == "apikey" ||
		normalized == "api_key" ||
		normalized == "authapikey" ||
		normalized == "auth_api_key" ||
		normalized == "authkey" ||
		normalized == "auth_key"
}

func qnetApprovalRequired(spec datago.Spec, op datago.Operation) bool {
	return approvalValue(rawString(spec.Source, "is_confirmed_for_dev_nm")) ||
		approvalValue(rawString(spec.Source, "is_confirmed_for_prod_nm")) ||
		approvalValue(rawString(op.Source, "is_confirmed_for_dev_nm")) ||
		approvalValue(rawString(op.Source, "is_confirmed_for_prod_nm"))
}

func approvalValue(value string) bool {
	return strings.Contains(value, "심의") || strings.Contains(value, "승인대기")
}

func rawString(source *datago.Source, key string) string {
	if source == nil || source.Raw == nil {
		return ""
	}
	value, ok := source.Raw[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (a QNetAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("q-net adapter call support is not enabled yet")
}

func DefaultRegistry() (Registry, error) {
	return NewRegistry(
		NewAirportAdapter(),
		NewAndongAdapter(),
		NewCalspiaAdapter(),
		NewCancerAdapter(),
		NewCarAdapter(),
		NewCar365Adapter(),
		NewCodilAdapter(),
		NewConsumerAdapter(),
		NewCultureAdapter(),
		NewDataGGAdapter(),
		NewDGFCAAdapter(),
		NewDongjakAdapter(),
		NewEKAPEAdapter(),
		NewEMuseumAdapter(),
		NewEPostAdapter(),
		NewEXAdapter(),
		NewEShareAdapter(),
		NewFairDataAdapter(),
		NewFolkAdapter(),
		NewFoodSafetyKoreaAdapter(),
		NewForestAdapter(),
		NewFranchiseFTCAdapter(),
		NewGarakAdapter(),
		NewGBLibAdapter(),
		NewGeojeAdapter(),
		NewGICOMSAdapter(),
		NewGimhaeAdapter(),
		NewGwanakAdapter(),
		NewGwangjinAdapter(),
		NewGwangmyeongAdapter(),
		NewHappySDAdapter(),
		NewHumetroAdapter(),
		NewI815Adapter(),
		NewIcheonAdapter(),
		NewIns24Adapter(),
		NewITSAdapter(),
		NewItfindAdapter(),
		NewJejuAirAdapter(),
		NewJejuAdapter(),
		NewJejuDataHubAdapter(),
		NewJeonnamRedtableAdapter(),
		NewJejuITSAdapter(),
		NewJejuWWWAdapter(),
		NewJeonjuAdapter(),
		NewJusoAdapter(),
		NewKISTEPAdapter(),
		NewKOFPIAdapter(),
		NewKoradAdapter(),
		NewKPXAdapter(),
		NewLHEBidAdapter(),
		NewLofin365Adapter(),
		NewMAFRAAdapter(),
		NewMNDOpenDataAdapter(),
		NewMyHomeAdapter(),
		NewNABICAdapter(),
		NewNAQSAdapter(),
		NewNCPMSAdapter(),
		NewNFQSAdapter(),
		NewNongsaroAdapter(),
		NewOneclickLawAdapter(),
		NewOpenAssemblyAdapter(),
		NewOpenLawAdapter(),
		NewPQISAdapter(),
		NewPSISAdapter(),
		NewQNetAdapter(),
		NewSafetyDataAdapter(),
		NewSafeMapAdapter(),
		NewSeoguAdapter(),
		NewSeogwipoAdapter(),
		NewSeoulBusAdapter(),
		NewSeoulOpenDataAdapter(),
		NewSexOffenderAdapter(),
		NewSisulAdapter(),
		NewSisulWWWAdapter(),
		NewSTCISAdapter(),
		NewTourAdapter(),
		NewUiryeongAdapter(),
		NewUlsanAdapter(),
		NewVWorldAdapter(),
		NewWAMISAdapter(),
		NewWorkAdapter(),
		NewWork24Adapter(),
		NewWorldJobAdapter(),
	)
}

func endpointHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

func publicParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range params {
		if isAuthParam(key) {
			continue
		}
		out[key] = value
	}
	return out
}
