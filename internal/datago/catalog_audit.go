package datago

import (
	"net/url"
	"slices"
	"strings"
)

type CatalogAudit struct {
	SpecsTotal                      int             `json:"specs_total"`
	OperationsTotal                 int             `json:"operations_total"`
	CallableOperations              int             `json:"callable_operations"`
	SpecsWithoutOperations          int             `json:"specs_without_operations"`
	SpecsWithoutCallableOperation   int             `json:"specs_without_callable_operation"`
	OperationsWithoutEndpoint       int             `json:"operations_without_endpoint"`
	OperationsWithoutRequestParams  int             `json:"operations_without_request_params"`
	OperationsWithoutResponseParams int             `json:"operations_without_response_params"`
	SpecsMissingOrganization        int             `json:"specs_missing_organization"`
	SpecsMissingSourceURL           int             `json:"specs_missing_source_url"`
	SpecsMissingUpdatedAt           int             `json:"specs_missing_updated_at"`
	Dependency                      DependencyAudit `json:"dependency"`
	Samples                         AuditSamples    `json:"samples"`
}

type AuditSamples struct {
	WithoutOperations        []AuditSample `json:"without_operations,omitempty"`
	WithoutCallableOperation []AuditSample `json:"without_callable_operation,omitempty"`
	MissingOrganization      []AuditSample `json:"missing_organization,omitempty"`
	MissingUpdatedAt         []AuditSample `json:"missing_updated_at,omitempty"`
	ExternalEndpoint         []AuditSample `json:"external_endpoint,omitempty"`
	GatewayWithExternalGuide []AuditSample `json:"gateway_with_external_guide,omitempty"`
	ServiceRootOnly          []AuditSample `json:"service_root_only,omitempty"`
	SOAP                     []AuditSample `json:"soap,omitempty"`
	WMS                      []AuditSample `json:"wms,omitempty"`
}

type AuditSample struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Organization       string `json:"organization,omitempty"`
	SourceCategory     string `json:"source_category,omitempty"`
	OperationsCount    int    `json:"operations_count"`
	CallableOperations int    `json:"callable_operations"`
}

type DependencyAudit struct {
	DataGoKrGatewayOperations      int         `json:"data_go_kr_gateway_operations"`
	GatewayWithExternalGuideSpecs  int         `json:"gateway_with_external_guide_specs"`
	ExternalEndpointSpecs          int         `json:"external_endpoint_specs"`
	ExternalEndpointOperations     int         `json:"external_endpoint_operations"`
	ServiceRootOnlyOperations      int         `json:"service_root_only_operations"`
	SOAPOperations                 int         `json:"soap_operations"`
	WMSOperations                  int         `json:"wms_operations"`
	DevApprovalRequiredOperations  int         `json:"dev_approval_required_operations"`
	ProdApprovalRequiredOperations int         `json:"prod_approval_required_operations"`
	MalformedEndpointURLCount      int         `json:"malformed_endpoint_url_count"`
	MalformedGuideURLCount         int         `json:"malformed_guide_url_count"`
	TopEndpointHosts               []HostCount `json:"top_endpoint_hosts,omitempty"`
	TopExternalEndpointHosts       []HostCount `json:"top_external_endpoint_hosts,omitempty"`
	TopExternalGuideHosts          []HostCount `json:"top_external_guide_hosts,omitempty"`
}

type HostCount struct {
	Host  string `json:"host"`
	Count int    `json:"count"`
}

func AuditRegistry(reg Registry, sampleLimit int) CatalogAudit {
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	audit := CatalogAudit{}
	endpointHosts := map[string]int{}
	externalEndpointHosts := map[string]int{}
	externalGuideHosts := map[string]int{}
	for _, spec := range reg.Specs() {
		audit.SpecsTotal++
		if strings.TrimSpace(spec.Organization) == "" {
			audit.SpecsMissingOrganization++
			audit.Samples.MissingOrganization = appendSample(audit.Samples.MissingOrganization, spec, sampleLimit)
		}
		if spec.Source == nil || strings.TrimSpace(spec.Source.URL) == "" {
			audit.SpecsMissingSourceURL++
		}
		if !hasUpdatedAt(spec) {
			audit.SpecsMissingUpdatedAt++
			audit.Samples.MissingUpdatedAt = appendSample(audit.Samples.MissingUpdatedAt, spec, sampleLimit)
		}
		if len(spec.Operations) == 0 {
			audit.SpecsWithoutOperations++
			audit.SpecsWithoutCallableOperation++
			audit.Samples.WithoutOperations = appendSample(audit.Samples.WithoutOperations, spec, sampleLimit)
			audit.Samples.WithoutCallableOperation = appendSample(audit.Samples.WithoutCallableOperation, spec, sampleLimit)
			continue
		}
		callable := false
		specHasExternalEndpoint := false
		specHasGatewayWithExternalGuide := false
		for _, op := range spec.Operations {
			audit.OperationsTotal++
			raw := mergedRaw(spec, op)
			apiType := strings.ToUpper(rawString(raw, "api_type"))
			dataFormat := strings.ToUpper(rawString(raw, "data_format"))
			if apiType == "SOAP" {
				audit.Dependency.SOAPOperations++
				audit.Samples.SOAP = appendSample(audit.Samples.SOAP, spec, sampleLimit)
			}
			if dataFormat == "WMS" {
				audit.Dependency.WMSOperations++
				audit.Samples.WMS = appendSample(audit.Samples.WMS, spec, sampleLimit)
			}
			if approvalRequired(rawString(raw, "is_confirmed_for_dev_nm")) {
				audit.Dependency.DevApprovalRequiredOperations++
			}
			if approvalRequired(rawString(raw, "is_confirmed_for_prod_nm")) {
				audit.Dependency.ProdApprovalRequiredOperations++
			}
			if strings.TrimSpace(op.Endpoint) == "" {
				audit.OperationsWithoutEndpoint++
				if serviceRootOnly(rawString(raw, "end_point_url")) {
					audit.Dependency.ServiceRootOnlyOperations++
					audit.Samples.ServiceRootOnly = appendSample(audit.Samples.ServiceRootOnly, spec, sampleLimit)
				}
			} else {
				callable = true
				audit.CallableOperations++
				endpointHost, endpointMalformed := urlHost(op.Endpoint)
				if endpointMalformed {
					audit.Dependency.MalformedEndpointURLCount++
				}
				if endpointHost != "" {
					endpointHosts[endpointHost]++
					if isDataGoKrGateway(endpointHost) {
						audit.Dependency.DataGoKrGatewayOperations++
					} else {
						audit.Dependency.ExternalEndpointOperations++
						externalEndpointHosts[endpointHost]++
						specHasExternalEndpoint = true
					}
				}
			}
			guideHost, guideMalformed := urlHost(rawString(raw, "guide_url"))
			if guideMalformed {
				audit.Dependency.MalformedGuideURLCount++
			}
			if guideHost != "" && !isDataGoKrGateway(guideHost) && !strings.Contains(guideHost, "data.go.kr") {
				externalGuideHosts[guideHost]++
				endpointHost, _ := urlHost(op.Endpoint)
				if isDataGoKrGateway(endpointHost) {
					specHasGatewayWithExternalGuide = true
				}
			}
			if len(op.RequestParams) == 0 {
				audit.OperationsWithoutRequestParams++
			}
			if len(op.ResponseParams) == 0 {
				audit.OperationsWithoutResponseParams++
			}
		}
		if !callable {
			audit.SpecsWithoutCallableOperation++
			audit.Samples.WithoutCallableOperation = appendSample(audit.Samples.WithoutCallableOperation, spec, sampleLimit)
		}
		if specHasExternalEndpoint {
			audit.Dependency.ExternalEndpointSpecs++
			audit.Samples.ExternalEndpoint = appendSample(audit.Samples.ExternalEndpoint, spec, sampleLimit)
		}
		if specHasGatewayWithExternalGuide {
			audit.Dependency.GatewayWithExternalGuideSpecs++
			audit.Samples.GatewayWithExternalGuide = appendSample(audit.Samples.GatewayWithExternalGuide, spec, sampleLimit)
		}
	}
	audit.Dependency.TopEndpointHosts = topHosts(endpointHosts, 20)
	audit.Dependency.TopExternalEndpointHosts = topHosts(externalEndpointHosts, 20)
	audit.Dependency.TopExternalGuideHosts = topHosts(externalGuideHosts, 20)
	return audit
}

func appendSample(samples []AuditSample, spec Spec, limit int) []AuditSample {
	if limit == 0 || len(samples) >= limit {
		return samples
	}
	for _, sample := range samples {
		if sample.ID == spec.ID {
			return samples
		}
	}
	return append(samples, auditSample(spec))
}

func auditSample(spec Spec) AuditSample {
	callable := 0
	for _, op := range spec.Operations {
		if strings.TrimSpace(op.Endpoint) != "" {
			callable++
		}
	}
	return AuditSample{
		ID:                 spec.ID,
		Title:              spec.Title,
		Organization:       spec.Organization,
		SourceCategory:     spec.SourceCategory,
		OperationsCount:    len(spec.Operations),
		CallableOperations: callable,
	}
}

func hasUpdatedAt(spec Spec) bool {
	if spec.Source != nil && rawString(spec.Source.Raw, "updated_at") != "" {
		return true
	}
	for _, op := range spec.Operations {
		if op.Source != nil && rawString(op.Source.Raw, "updated_at") != "" {
			return true
		}
	}
	return false
}

func rawString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	value, ok := raw[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(toString(value))
}

func mergedRaw(spec Spec, op Operation) map[string]any {
	out := map[string]any{}
	if spec.Source != nil {
		for k, v := range spec.Source.Raw {
			out[k] = v
		}
	}
	if op.Source != nil {
		for k, v := range op.Source.Raw {
			out[k] = v
		}
	}
	return out
}

func urlHost(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", true
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", true
	}
	return strings.ToLower(parsed.Host), false
}

func isDataGoKrGateway(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "apis.data.go.kr" || host == "api.odcloud.kr" || strings.HasSuffix(host, ".data.go.kr")
}

func serviceRootOnly(raw string) bool {
	parsed, malformed := urlHost(raw)
	if malformed || parsed == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	return path == "/openapi/service"
}

func approvalRequired(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "심의") || strings.Contains(value, "승인대기")
}

func topHosts(counts map[string]int, limit int) []HostCount {
	hosts := make([]HostCount, 0, len(counts))
	for host, count := range counts {
		hosts = append(hosts, HostCount{Host: host, Count: count})
	}
	slices.SortFunc(hosts, func(a, b HostCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Host, b.Host)
	})
	if limit > 0 && len(hosts) > limit {
		return hosts[:limit]
	}
	return hosts
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
