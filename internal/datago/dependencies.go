package datago

import (
	"slices"
	"strings"
)

type DependencyInventoryReport struct {
	GeneratedAt   string                       `json:"generated_at"`
	Provider      string                       `json:"provider"`
	Registry      string                       `json:"registry,omitempty"`
	Limit         int                          `json:"limit"`
	Truncated     bool                         `json:"truncated"`
	Filters       *DependencyInventoryFilters  `json:"filters,omitempty"`
	FilteredCount int                          `json:"filtered_count"`
	Summary       DependencyInventorySummary   `json:"summary"`
	Dependencies  []DependencyOperationSummary `json:"dependencies"`
}

type DependencyInventoryFilters struct {
	Provider string `json:"provider,omitempty"`
	Host     string `json:"host,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Status   string `json:"status,omitempty"`
}

type DependencyInventorySummary struct {
	OperationsTotal           int `json:"operations_total"`
	DataGoKrGatewayOperations int `json:"data_go_kr_gateway_operations"`
	ExternalEndpointOps       int `json:"external_endpoint_operations"`
	ServiceRootOperations     int `json:"service_root_operations"`
	NoEndpointOperations      int `json:"no_endpoint_operations"`
	MalformedEndpointOps      int `json:"malformed_endpoint_operations"`
	SOAPOperations            int `json:"soap_operations"`
	WMSOperations             int `json:"wms_operations"`
	ApprovalRequiredOps       int `json:"approval_required_operations"`
	RegisteredAdapterOps      int `json:"registered_adapter_operations"`
	MissingAdapterOps         int `json:"missing_adapter_operations"`
}

type DependencyOperationSummary struct {
	DatasetID           string   `json:"dataset_id"`
	Title               string   `json:"title"`
	Organization        string   `json:"organization,omitempty"`
	SourceCategory      string   `json:"source_category,omitempty"`
	Operation           string   `json:"operation"`
	Provider            string   `json:"provider"`
	Endpoint            string   `json:"endpoint,omitempty"`
	EndpointHost        string   `json:"endpoint_host,omitempty"`
	SourceHost          string   `json:"source_host,omitempty"`
	GuideHost           string   `json:"guide_host,omitempty"`
	DependencyClass     string   `json:"dependency_class"`
	AdapterStatus       string   `json:"adapter_status"`
	ProviderFamily      string   `json:"provider_family,omitempty"`
	APIType             string   `json:"api_type,omitempty"`
	DataFormat          string   `json:"data_format,omitempty"`
	DevApproval         string   `json:"dev_approval,omitempty"`
	ProdApproval        string   `json:"prod_approval,omitempty"`
	ApprovalRequired    bool     `json:"approval_required"`
	SkipReason          string   `json:"skip_reason,omitempty"`
	RequestParamsCount  int      `json:"request_params_count"`
	ResponseParamsCount int      `json:"response_params_count"`
	MissingParams       []string `json:"missing_params,omitempty"`
}

func DependencyInventoryForRegistry(reg Registry, adapterHosts []string) (DependencyInventorySummary, []DependencyOperationSummary) {
	adapterHostSet := normalizedHostSet(adapterHosts)
	summary := DependencyInventorySummary{}
	dependencies := make([]DependencyOperationSummary, 0)
	for _, spec := range reg.Specs() {
		for _, op := range spec.Operations {
			candidate := VerificationCandidate{
				Spec:            spec,
				Operation:       op,
				DependencyClass: OperationDependencyClass(spec, op),
			}
			candidate.EndpointHost, _ = urlHost(op.Endpoint)
			candidate.Params, candidate.MissingParams = VerificationParams(spec, op)
			candidate.SkipReason = VerificationSkipReason(candidate)
			raw := mergedRaw(spec, op)
			sourceHost, _ := urlHost(rawString(raw, "end_point_url"))
			guideHost, _ := urlHost(rawString(raw, "guide_url"))
			adapterStatus := dependencyAdapterStatus(candidate.DependencyClass, candidate.EndpointHost, sourceHost, adapterHostSet)
			skipReason := candidate.SkipReason
			if adapterStatus == "adapter" && skipReason == "external_provider_adapter_missing" {
				skipReason = ""
			}
			dep := DependencyOperationSummary{
				DatasetID:           spec.ID,
				Title:               spec.Title,
				Organization:        spec.Organization,
				SourceCategory:      spec.SourceCategory,
				Operation:           op.Name,
				Provider:            spec.Provider,
				Endpoint:            op.Endpoint,
				EndpointHost:        candidate.EndpointHost,
				SourceHost:          sourceHost,
				GuideHost:           guideHost,
				DependencyClass:     candidate.DependencyClass,
				AdapterStatus:       adapterStatus,
				ProviderFamily:      dependencyProviderFamily(candidate.EndpointHost, sourceHost, guideHost),
				APIType:             rawString(raw, "api_type"),
				DataFormat:          rawString(raw, "data_format"),
				DevApproval:         rawString(raw, "is_confirmed_for_dev_nm"),
				ProdApproval:        rawString(raw, "is_confirmed_for_prod_nm"),
				ApprovalRequired:    approvalRequired(rawString(raw, "is_confirmed_for_dev_nm")) || approvalRequired(rawString(raw, "is_confirmed_for_prod_nm")),
				SkipReason:          skipReason,
				RequestParamsCount:  len(op.RequestParams),
				ResponseParamsCount: len(op.ResponseParams),
				MissingParams:       candidate.MissingParams,
			}
			summary.add(dep)
			dependencies = append(dependencies, dep)
		}
	}
	slices.SortFunc(dependencies, func(a, b DependencyOperationSummary) int {
		if a.AdapterStatus != b.AdapterStatus {
			return dependencyAdapterStatusRank(a.AdapterStatus) - dependencyAdapterStatusRank(b.AdapterStatus)
		}
		if a.DependencyClass != b.DependencyClass {
			return strings.Compare(a.DependencyClass, b.DependencyClass)
		}
		if a.ProviderFamily != b.ProviderFamily {
			return strings.Compare(a.ProviderFamily, b.ProviderFamily)
		}
		if a.EndpointHost != b.EndpointHost {
			return strings.Compare(a.EndpointHost, b.EndpointHost)
		}
		if a.DatasetID != b.DatasetID {
			return strings.Compare(a.DatasetID, b.DatasetID)
		}
		return strings.Compare(a.Operation, b.Operation)
	})
	return summary, dependencies
}

func (s *DependencyInventorySummary) add(dep DependencyOperationSummary) {
	s.OperationsTotal++
	switch dep.DependencyClass {
	case "data_go_kr_gateway":
		s.DataGoKrGatewayOperations++
	case "external_endpoint":
		s.ExternalEndpointOps++
	case "service_root":
		s.ServiceRootOperations++
	case "no_endpoint":
		s.NoEndpointOperations++
	case "malformed_endpoint":
		s.MalformedEndpointOps++
	}
	if dep.DependencyClass == "soap" || strings.EqualFold(dep.APIType, "SOAP") {
		s.SOAPOperations++
	}
	if dep.DependencyClass == "wms" || strings.EqualFold(dep.DataFormat, "WMS") {
		s.WMSOperations++
	}
	if dep.ApprovalRequired {
		s.ApprovalRequiredOps++
	}
	switch dep.AdapterStatus {
	case "adapter":
		s.RegisteredAdapterOps++
	case "missing":
		s.MissingAdapterOps++
	}
}

func FilterDependencyOperations(deps []DependencyOperationSummary, filters *DependencyInventoryFilters) []DependencyOperationSummary {
	if filters == nil {
		return append([]DependencyOperationSummary(nil), deps...)
	}
	out := make([]DependencyOperationSummary, 0, len(deps))
	host := strings.ToLower(strings.TrimSpace(filters.Host))
	for _, dep := range deps {
		if filters.Provider != "" && !strings.Contains(strings.ToLower(dep.ProviderFamily), strings.ToLower(filters.Provider)) {
			continue
		}
		if filters.Kind != "" && dep.DependencyClass != filters.Kind {
			continue
		}
		if filters.Status != "" && dep.AdapterStatus != filters.Status {
			continue
		}
		if host != "" && !dependencyHasHost(dep, host) {
			continue
		}
		out = append(out, dep)
	}
	return out
}

func dependencyAdapterStatus(class, endpointHost, sourceHost string, adapterHosts map[string]bool) string {
	switch class {
	case "data_go_kr_gateway":
		return "builtin"
	case "external_endpoint":
		if adapterHosts[strings.ToLower(strings.TrimSpace(endpointHost))] {
			return "adapter"
		}
		return "missing"
	case "service_root":
		if adapterHosts[strings.ToLower(strings.TrimSpace(sourceHost))] {
			return "adapter"
		}
		return "missing"
	default:
		return "not_applicable"
	}
}

func dependencyAdapterStatusRank(status string) int {
	switch status {
	case "missing":
		return 0
	case "adapter":
		return 1
	case "not_applicable":
		return 2
	case "builtin":
		return 3
	default:
		return 4
	}
}

func dependencyProviderFamily(hosts ...string) string {
	for _, host := range hosts {
		if name := providerNameForHost(host); name != "" {
			return name
		}
	}
	return ""
}

func dependencyHasHost(dep DependencyOperationSummary, host string) bool {
	return strings.EqualFold(dep.EndpointHost, host) ||
		strings.EqualFold(dep.SourceHost, host) ||
		strings.EqualFold(dep.GuideHost, host)
}
