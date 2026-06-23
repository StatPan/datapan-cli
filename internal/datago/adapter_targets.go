package datago

import (
	"slices"
	"strings"
)

type AdapterTargetReport struct {
	GeneratedAt   string                `json:"generated_at"`
	Provider      string                `json:"provider"`
	Registry      string                `json:"registry,omitempty"`
	Limit         int                   `json:"limit"`
	Truncated     bool                  `json:"truncated"`
	Filters       *AdapterTargetFilters `json:"filters,omitempty"`
	FilteredCount int                   `json:"filtered_count"`
	Summary       AdapterTargetSummary  `json:"summary"`
	Targets       []AdapterTarget       `json:"targets"`
}

type AdapterTargetFilters struct {
	Provider string `json:"provider,omitempty"`
	Host     string `json:"host,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type AdapterTargetSummary struct {
	TargetHosts                   int `json:"target_hosts"`
	TargetOperations              int `json:"target_operations"`
	ExternalEndpointOperations    int `json:"external_endpoint_operations"`
	ServiceRootOperations         int `json:"service_root_operations"`
	ApprovalRequiredOperations    int `json:"approval_required_operations"`
	MissingParamOperations        int `json:"missing_param_operations"`
	UnsupportedProtocolOperations int `json:"unsupported_protocol_operations"`
}

type AdapterTarget struct {
	Rank                       int                   `json:"rank"`
	Host                       string                `json:"host"`
	ProviderFamily             string                `json:"provider_family,omitempty"`
	Kinds                      []string              `json:"kinds"`
	Operations                 int                   `json:"operations"`
	Specs                      int                   `json:"specs"`
	Organizations              []string              `json:"organizations,omitempty"`
	SourceCategories           []string              `json:"source_categories,omitempty"`
	APITypes                   []string              `json:"api_types,omitempty"`
	DataFormats                []string              `json:"data_formats,omitempty"`
	ApprovalRequiredOperations int                   `json:"approval_required_operations,omitempty"`
	MissingParamOperations     int                   `json:"missing_param_operations,omitempty"`
	UnsupportedProtocolOps     int                   `json:"unsupported_protocol_operations,omitempty"`
	SampleOperations           []AdapterTargetSample `json:"sample_operations,omitempty"`
}

type AdapterTargetSample struct {
	DatasetID       string   `json:"dataset_id"`
	Title           string   `json:"title"`
	Organization    string   `json:"organization,omitempty"`
	Operation       string   `json:"operation"`
	DependencyClass string   `json:"dependency_class"`
	Endpoint        string   `json:"endpoint,omitempty"`
	MissingParams   []string `json:"missing_params,omitempty"`
}

type adapterTargetAccumulator struct {
	target             AdapterTarget
	specIDs            map[string]bool
	organizationSet    map[string]bool
	sourceCategorySet  map[string]bool
	kindSet            map[string]bool
	apiTypeSet         map[string]bool
	dataFormatSet      map[string]bool
	sampleOperationSet map[string]bool
}

func AdapterTargetsForRegistry(reg Registry, adapterHosts []string, sampleLimit int) (AdapterTargetSummary, []AdapterTarget) {
	_, deps := DependencyInventoryForRegistry(reg, adapterHosts)
	return AdapterTargetsFromDependencies(deps, sampleLimit)
}

func AdapterTargetsFromDependencies(deps []DependencyOperationSummary, sampleLimit int) (AdapterTargetSummary, []AdapterTarget) {
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	accs := map[string]*adapterTargetAccumulator{}
	summary := AdapterTargetSummary{}
	for _, dep := range deps {
		if dep.AdapterStatus != "missing" || (dep.DependencyClass != "external_endpoint" && dep.DependencyClass != "service_root") {
			continue
		}
		host := adapterTargetHost(dep)
		if host == "" {
			continue
		}
		acc := adapterTargetAcc(accs, host)
		addAdapterTargetDependency(acc, dep, sampleLimit)
		summary.TargetOperations++
		switch dep.DependencyClass {
		case "external_endpoint":
			summary.ExternalEndpointOperations++
		case "service_root":
			summary.ServiceRootOperations++
		}
		if dep.ApprovalRequired {
			summary.ApprovalRequiredOperations++
		}
		if len(dep.MissingParams) > 0 {
			summary.MissingParamOperations++
		}
		if strings.EqualFold(dep.APIType, "SOAP") || strings.EqualFold(dep.DataFormat, "WMS") {
			summary.UnsupportedProtocolOperations++
		}
	}
	targets := make([]AdapterTarget, 0, len(accs))
	for _, acc := range accs {
		acc.target.Specs = len(acc.specIDs)
		acc.target.Kinds = sortedKinds(acc.kindSet)
		acc.target.Organizations = sortedStringSet(acc.organizationSet)
		acc.target.SourceCategories = sortedStringSet(acc.sourceCategorySet)
		acc.target.APITypes = sortedStringSet(acc.apiTypeSet)
		acc.target.DataFormats = sortedStringSet(acc.dataFormatSet)
		targets = append(targets, acc.target)
	}
	slices.SortFunc(targets, func(a, b AdapterTarget) int {
		if a.Operations != b.Operations {
			return b.Operations - a.Operations
		}
		if a.Specs != b.Specs {
			return b.Specs - a.Specs
		}
		if a.ApprovalRequiredOperations != b.ApprovalRequiredOperations {
			return b.ApprovalRequiredOperations - a.ApprovalRequiredOperations
		}
		return strings.Compare(a.Host, b.Host)
	})
	for i := range targets {
		targets[i].Rank = i + 1
	}
	summary.TargetHosts = len(targets)
	return summary, targets
}

func FilterAdapterTargets(targets []AdapterTarget, filters *AdapterTargetFilters) []AdapterTarget {
	if filters == nil {
		return append([]AdapterTarget(nil), targets...)
	}
	out := make([]AdapterTarget, 0, len(targets))
	for _, target := range targets {
		if filters.Provider != "" && !strings.Contains(strings.ToLower(target.ProviderFamily), strings.ToLower(filters.Provider)) {
			continue
		}
		if filters.Host != "" && !strings.EqualFold(target.Host, filters.Host) {
			continue
		}
		if filters.Kind != "" && !adapterTargetHasKind(target, filters.Kind) {
			continue
		}
		out = append(out, target)
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func adapterTargetHost(dep DependencyOperationSummary) string {
	if dep.EndpointHost != "" {
		return dep.EndpointHost
	}
	return dep.SourceHost
}

func adapterTargetAcc(accs map[string]*adapterTargetAccumulator, host string) *adapterTargetAccumulator {
	host = strings.ToLower(strings.TrimSpace(host))
	if acc, ok := accs[host]; ok {
		return acc
	}
	acc := &adapterTargetAccumulator{
		target:             AdapterTarget{Host: host, ProviderFamily: providerNameForHost(host)},
		specIDs:            map[string]bool{},
		organizationSet:    map[string]bool{},
		sourceCategorySet:  map[string]bool{},
		kindSet:            map[string]bool{},
		apiTypeSet:         map[string]bool{},
		dataFormatSet:      map[string]bool{},
		sampleOperationSet: map[string]bool{},
	}
	accs[host] = acc
	return acc
}

func addAdapterTargetDependency(acc *adapterTargetAccumulator, dep DependencyOperationSummary, sampleLimit int) {
	acc.target.Operations++
	addSetValue(acc.specIDs, dep.DatasetID)
	addSetValue(acc.organizationSet, dep.Organization)
	addSetValue(acc.sourceCategorySet, dep.SourceCategory)
	addSetValue(acc.kindSet, dep.DependencyClass)
	addSetValue(acc.apiTypeSet, dep.APIType)
	addSetValue(acc.dataFormatSet, dep.DataFormat)
	if dep.ApprovalRequired {
		acc.target.ApprovalRequiredOperations++
	}
	if len(dep.MissingParams) > 0 {
		acc.target.MissingParamOperations++
	}
	if strings.EqualFold(dep.APIType, "SOAP") || strings.EqualFold(dep.DataFormat, "WMS") {
		acc.target.UnsupportedProtocolOps++
	}
	if sampleLimit == 0 || len(acc.target.SampleOperations) >= sampleLimit {
		return
	}
	sampleKey := dep.DatasetID + "\x00" + dep.Operation
	if acc.sampleOperationSet[sampleKey] {
		return
	}
	acc.target.SampleOperations = append(acc.target.SampleOperations, AdapterTargetSample{
		DatasetID:       dep.DatasetID,
		Title:           dep.Title,
		Organization:    dep.Organization,
		Operation:       dep.Operation,
		DependencyClass: dep.DependencyClass,
		Endpoint:        dep.Endpoint,
		MissingParams:   dep.MissingParams,
	})
	acc.sampleOperationSet[sampleKey] = true
}

func addSetValue(set map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		set[value] = true
	}
}

func sortedStringSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func adapterTargetHasKind(target AdapterTarget, kind string) bool {
	for _, candidate := range target.Kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}
