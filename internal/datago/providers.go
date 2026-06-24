package datago

import (
	"slices"
	"strings"
)

type ProviderBacklog struct {
	Providers []ProviderSummary `json:"providers"`
	Summary   ProviderCounts    `json:"summary"`
}

type ProviderBacklogReport struct {
	GeneratedAt   string                  `json:"generated_at"`
	Provider      string                  `json:"provider"`
	Registry      string                  `json:"registry,omitempty"`
	Limit         int                     `json:"limit"`
	Truncated     bool                    `json:"truncated"`
	Filters       *ProviderBacklogFilters `json:"filters,omitempty"`
	FilteredCount int                     `json:"filtered_count"`
	Summary       ProviderCounts          `json:"summary"`
	Providers     []ProviderSummary       `json:"providers"`
}

type ProviderBacklogFilters struct {
	Status   string `json:"status,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type ProviderCounts struct {
	Hosts                   int `json:"hosts"`
	DataGoKrGatewayHosts    int `json:"data_go_kr_gateway_hosts"`
	ExternalEndpointHosts   int `json:"external_endpoint_hosts"`
	ExternalGuideHosts      int `json:"external_guide_hosts"`
	RegisteredAdapterHosts  int `json:"registered_adapter_hosts"`
	OperationHosts          int `json:"operation_hosts"`
	GuideOnlyHosts          int `json:"guide_only_hosts"`
	MissingAdapterHosts     int `json:"missing_adapter_hosts"`
	NeedsAdapterOperations  int `json:"needs_adapter_operations"`
	ServiceRootOperations   int `json:"service_root_operations"`
	UnsupportedProtocolOps  int `json:"unsupported_protocol_operations"`
	ApprovalRequiredOps     int `json:"approval_required_operations"`
	MalformedSourceURLCount int `json:"malformed_source_url_count"`
}

type ProviderSummary struct {
	Host                       string   `json:"host"`
	Provider                   string   `json:"provider,omitempty"`
	Kinds                      []string `json:"kinds"`
	AdapterStatus              string   `json:"adapter_status"`
	Specs                      int      `json:"specs"`
	Operations                 int      `json:"operations"`
	ExternalEndpointOperations int      `json:"external_endpoint_operations,omitempty"`
	ExternalGuideSpecs         int      `json:"external_guide_specs,omitempty"`
	ServiceRootOperations      int      `json:"service_root_operations,omitempty"`
	SOAPOperations             int      `json:"soap_operations,omitempty"`
	WMSOperations              int      `json:"wms_operations,omitempty"`
	ApprovalRequiredOperations int      `json:"approval_required_operations,omitempty"`
	SampleIDs                  []string `json:"sample_ids,omitempty"`
}

type providerAccumulator struct {
	summary     ProviderSummary
	specIDs     map[string]bool
	kindSet     map[string]bool
	sampleIDSet map[string]bool
}

func ProviderBacklogForRegistry(reg Registry, sampleLimit int) ProviderBacklog {
	return ProviderBacklogForRegistryWithAdapters(reg, sampleLimit, nil)
}

func ProviderBacklogForRegistryWithAdapters(reg Registry, sampleLimit int, adapterHosts []string) ProviderBacklog {
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	adapterHostSet := normalizedHostSet(adapterHosts)
	accs := map[string]*providerAccumulator{}
	var counts ProviderCounts
	for _, spec := range reg.Specs() {
		specExternalGuideHosts := map[string]bool{}
		for _, op := range spec.Operations {
			raw := mergedRaw(spec, op)
			apiType := strings.ToUpper(rawString(raw, "api_type"))
			dataFormat := strings.ToUpper(rawString(raw, "data_format"))
			unsupportedProtocol := apiType == "SOAP" || dataFormat == "WMS"
			approval := approvalRequired(rawString(raw, "is_confirmed_for_dev_nm")) ||
				approvalRequired(rawString(raw, "is_confirmed_for_prod_nm"))
			if approval {
				counts.ApprovalRequiredOps++
			}
			if unsupportedProtocol {
				counts.UnsupportedProtocolOps++
			}

			if endpointHost, malformed := urlHost(op.Endpoint); malformed {
				counts.MalformedSourceURLCount++
			} else if endpointHost != "" {
				acc := providerAcc(accs, endpointHost)
				addKind(acc, operationHostKind(endpointHost))
				addSpec(acc, spec, sampleLimit)
				acc.summary.Operations++
				if !isDataGoKrGateway(endpointHost) {
					acc.summary.ExternalEndpointOperations++
					if !adapterHostSet[endpointHost] {
						counts.NeedsAdapterOperations++
					}
				}
				if apiType == "SOAP" {
					acc.summary.SOAPOperations++
				}
				if dataFormat == "WMS" {
					acc.summary.WMSOperations++
				}
				if approval {
					acc.summary.ApprovalRequiredOperations++
				}
			} else if serviceRootOnly(rawString(raw, "end_point_url")) {
				counts.ServiceRootOperations++
				if rootHost, malformed := urlHost(rawString(raw, "end_point_url")); malformed {
					counts.MalformedSourceURLCount++
				} else if rootHost != "" {
					acc := providerAcc(accs, rootHost)
					addKind(acc, "service_root")
					addSpec(acc, spec, sampleLimit)
					acc.summary.ServiceRootOperations++
					if apiType == "SOAP" {
						acc.summary.SOAPOperations++
					}
					if dataFormat == "WMS" {
						acc.summary.WMSOperations++
					}
					if approval {
						acc.summary.ApprovalRequiredOperations++
					}
				}
			}

			guideHost, malformed := urlHost(rawString(raw, "guide_url"))
			if malformed {
				counts.MalformedSourceURLCount++
			}
			if guideHost != "" && !isDataGoKrGateway(guideHost) && !strings.Contains(guideHost, "data.go.kr") {
				specExternalGuideHosts[guideHost] = true
			}
		}
		for guideHost := range specExternalGuideHosts {
			acc := providerAcc(accs, guideHost)
			addKind(acc, "external_guide")
			addSpec(acc, spec, sampleLimit)
			acc.summary.ExternalGuideSpecs++
		}
	}

	providers := make([]ProviderSummary, 0, len(accs))
	for _, acc := range accs {
		acc.summary.Kinds = sortedKinds(acc.kindSet)
		acc.summary.Specs = len(acc.specIDs)
		acc.summary.Provider = providerNameForHost(acc.summary.Host)
		acc.summary.AdapterStatus = adapterStatus(acc.summary.Host, acc.kindSet, adapterHostSet)
		providers = append(providers, acc.summary)
		counts.Hosts++
		if acc.kindSet["data_go_kr_gateway"] {
			counts.DataGoKrGatewayHosts++
		}
		if acc.kindSet["external_endpoint"] {
			counts.ExternalEndpointHosts++
		}
		if acc.kindSet["external_guide"] {
			counts.ExternalGuideHosts++
		}
		if acc.kindSet["data_go_kr_gateway"] || acc.kindSet["external_endpoint"] || acc.kindSet["service_root"] {
			counts.OperationHosts++
		}
		if acc.kindSet["external_guide"] && !acc.kindSet["data_go_kr_gateway"] && !acc.kindSet["external_endpoint"] && !acc.kindSet["service_root"] {
			counts.GuideOnlyHosts++
		}
		if acc.summary.AdapterStatus == "adapter" {
			counts.RegisteredAdapterHosts++
		}
		if acc.summary.AdapterStatus == "missing" {
			counts.MissingAdapterHosts++
		}
	}
	slices.SortFunc(providers, func(a, b ProviderSummary) int {
		if a.AdapterStatus != b.AdapterStatus {
			return adapterStatusRank(a.AdapterStatus) - adapterStatusRank(b.AdapterStatus)
		}
		if a.ExternalEndpointOperations != b.ExternalEndpointOperations {
			return b.ExternalEndpointOperations - a.ExternalEndpointOperations
		}
		if a.Operations != b.Operations {
			return b.Operations - a.Operations
		}
		if a.ExternalGuideSpecs != b.ExternalGuideSpecs {
			return b.ExternalGuideSpecs - a.ExternalGuideSpecs
		}
		return strings.Compare(a.Host, b.Host)
	})
	return ProviderBacklog{Providers: providers, Summary: counts}
}

func normalizedHostSet(hosts []string) map[string]bool {
	out := map[string]bool{}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			out[host] = true
		}
	}
	return out
}

func providerAcc(accs map[string]*providerAccumulator, host string) *providerAccumulator {
	host = strings.ToLower(strings.TrimSpace(host))
	if acc, ok := accs[host]; ok {
		return acc
	}
	acc := &providerAccumulator{
		summary:     ProviderSummary{Host: host},
		specIDs:     map[string]bool{},
		kindSet:     map[string]bool{},
		sampleIDSet: map[string]bool{},
	}
	accs[host] = acc
	return acc
}

func addSpec(acc *providerAccumulator, spec Spec, sampleLimit int) {
	if strings.TrimSpace(spec.ID) != "" {
		acc.specIDs[spec.ID] = true
	}
	if sampleLimit == 0 || len(acc.summary.SampleIDs) >= sampleLimit || acc.sampleIDSet[spec.ID] {
		return
	}
	acc.summary.SampleIDs = append(acc.summary.SampleIDs, spec.ID)
	acc.sampleIDSet[spec.ID] = true
}

func addKind(acc *providerAccumulator, kind string) {
	if kind != "" {
		acc.kindSet[kind] = true
	}
}

func operationHostKind(host string) string {
	if isDataGoKrGateway(host) {
		return "data_go_kr_gateway"
	}
	return "external_endpoint"
}

func sortedKinds(kinds map[string]bool) []string {
	out := make([]string, 0, len(kinds))
	for kind := range kinds {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

func adapterStatus(host string, kinds map[string]bool, adapterHosts map[string]bool) string {
	if isDataGoKrGateway(host) {
		return "builtin"
	}
	if kinds["external_endpoint"] || kinds["service_root"] {
		if adapterHosts[strings.ToLower(strings.TrimSpace(host))] {
			return "adapter"
		}
		return "missing"
	}
	return "guide_only"
}

func adapterStatusRank(status string) int {
	switch status {
	case "missing":
		return 0
	case "adapter":
		return 1
	case "guide_only":
		return 2
	case "builtin":
		return 3
	default:
		return 4
	}
}

func providerNameForHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "apis.data.go.kr" || host == "api.odcloud.kr" || strings.HasSuffix(host, ".data.go.kr"):
		return "data.go.kr"
	case strings.Contains(host, "airport.co.kr"):
		return "airport"
	case strings.Contains(host, "q-net.or.kr"):
		return "q-net"
	case strings.Contains(host, "epost.go.kr"):
		return "epost"
	case strings.Contains(host, "ekape.or.kr"):
		return "ekape"
	case strings.Contains(host, "forest.go.kr"):
		return "forest"
	case strings.Contains(host, "folkency.nfm.go.kr"):
		return "folk"
	case strings.Contains(host, "mfds.go.kr"):
		return "mfds"
	case strings.Contains(host, "visitkorea.or.kr"):
		return "visitkorea"
	case strings.Contains(host, "assembly.go.kr"):
		return "open-assembly"
	default:
		return ""
	}
}
