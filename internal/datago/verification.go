package datago

import (
	"slices"
	"strings"
)

type VerificationReport struct {
	GeneratedAt   string                     `json:"generated_at"`
	Provider      string                     `json:"provider"`
	Registry      string                     `json:"registry,omitempty"`
	Ref           string                     `json:"ref,omitempty"`
	Operation     string                     `json:"operation,omitempty"`
	Limit         int                        `json:"limit"`
	Timeout       string                     `json:"timeout,omitempty"`
	ExcludeInput  string                     `json:"exclude_input,omitempty"`
	Truncated     bool                       `json:"truncated"`
	Filters       *VerificationReportFilters `json:"filters,omitempty"`
	FilteredCount int                        `json:"filtered_count"`
	Summary       VerificationSummary        `json:"summary"`
	Results       []VerificationResult       `json:"results"`
}

type VerificationSummaryReport struct {
	GeneratedAt string                    `json:"generated_at"`
	Source      string                    `json:"source,omitempty"`
	Provider    string                    `json:"provider"`
	Registry    string                    `json:"registry,omitempty"`
	Limit       int                       `json:"limit"`
	Truncated   bool                      `json:"truncated"`
	Summary     VerificationSummary       `json:"summary"`
	Groups      VerificationSummaryGroups `json:"groups"`
}

type VerificationSummaryGroups struct {
	ByStatus       []VerificationGroup `json:"by_status"`
	ByReason       []VerificationGroup `json:"by_reason,omitempty"`
	ByProvider     []VerificationGroup `json:"by_provider,omitempty"`
	ByEndpointHost []VerificationGroup `json:"by_endpoint_host,omitempty"`
	ByKind         []VerificationGroup `json:"by_kind,omitempty"`
}

type VerificationGroup struct {
	Key      string `json:"key"`
	Count    int    `json:"count"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Provider string `json:"provider,omitempty"`
	Host     string `json:"host,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type VerificationReportFilters struct {
	Status       string `json:"status,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Organization string `json:"organization,omitempty"`
	Host         string `json:"host,omitempty"`
	Kind         string `json:"kind,omitempty"`
}

type VerificationSummary struct {
	Total    int `json:"total"`
	Verified int `json:"verified"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`
	Unknown  int `json:"unknown"`
}

type VerificationResult struct {
	DatasetID       string            `json:"dataset_id"`
	Title           string            `json:"title"`
	Operation       string            `json:"operation"`
	Provider        string            `json:"provider"`
	EndpointHost    string            `json:"endpoint_host,omitempty"`
	DependencyClass string            `json:"dependency_class"`
	Status          string            `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	VerifiedAt      string            `json:"verified_at,omitempty"`
	HTTPStatus      int               `json:"http_status,omitempty"`
	SemanticStatus  string            `json:"semantic_status,omitempty"`
	ProviderStatus  *ProviderStatus   `json:"provider_status,omitempty"`
	URL             string            `json:"url,omitempty"`
	Params          map[string]string `json:"params,omitempty"`
	MissingParams   []string          `json:"missing_params,omitempty"`
	BodyShape       string            `json:"body_shape,omitempty"`
	DurationMS      int64             `json:"duration_ms,omitempty"`
}

type VerificationCandidate struct {
	Spec            Spec
	Operation       Operation
	EndpointHost    string
	DependencyClass string
	Params          map[string]string
	MissingParams   []string
	SkipReason      string
}

type VerificationCandidateFilters struct {
	Hosts        []string
	Kind         string
	Organization string
}

func VerificationCandidates(reg Registry, ref string, operation string, limit int) ([]VerificationCandidate, bool, error) {
	return VerificationCandidatesWithFilters(reg, ref, operation, limit, VerificationCandidateFilters{})
}

func VerificationCandidatesWithFilters(reg Registry, ref string, operation string, limit int, filters VerificationCandidateFilters) ([]VerificationCandidate, bool, error) {
	specs := reg.Specs()
	if strings.TrimSpace(ref) != "" {
		resolved := reg.Resolve(ref, 10)
		if resolved.Status != ResolveFound {
			return nil, false, VerificationResolveError{status: resolved.Status, ref: ref, candidates: resolved.Candidates}
		}
		specs = []Spec{resolved.Spec}
	}
	if strings.TrimSpace(operation) != "" && strings.TrimSpace(ref) == "" {
		return nil, false, VerificationResolveError{status: ResolveNotFound, ref: "--operation requires --ref"}
	}
	candidates := make([]VerificationCandidate, 0)
	truncated := false
	hostSet := verificationHostSet(filters.Hosts)
	kind := strings.TrimSpace(filters.Kind)
	organization := strings.TrimSpace(filters.Organization)
	for _, spec := range specs {
		if organization != "" && !containsFold(spec.Organization, organization) {
			continue
		}
		for _, op := range spec.Operations {
			if operation != "" && op.Name != operation {
				continue
			}
			candidate := VerificationCandidate{
				Spec:            spec,
				Operation:       op,
				DependencyClass: OperationDependencyClass(spec, op),
			}
			candidate.EndpointHost, _ = urlHost(op.Endpoint)
			if candidate.EndpointHost == "" && candidate.DependencyClass == "service_root" {
				candidate.EndpointHost, _ = urlHost(rawString(mergedRaw(spec, op), "end_point_url"))
			}
			candidate.Params, candidate.MissingParams = VerificationParams(spec, op)
			candidate.SkipReason = VerificationSkipReason(candidate)
			if !candidateMatchesFilters(candidate, hostSet, kind) {
				continue
			}
			if limit > 0 && len(candidates) >= limit {
				truncated = true
				return candidates, truncated, nil
			}
			candidates = append(candidates, candidate)
		}
	}
	return candidates, truncated, nil
}

func verificationHostSet(hosts []string) map[string]bool {
	if len(hosts) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			out[host] = true
		}
	}
	return out
}

func candidateMatchesFilters(candidate VerificationCandidate, hosts map[string]bool, kind string) bool {
	if kind != "" && candidate.DependencyClass != kind {
		return false
	}
	if len(hosts) > 0 && !hosts[strings.ToLower(strings.TrimSpace(candidate.EndpointHost))] {
		return false
	}
	return true
}

func OperationDependencyClass(spec Spec, op Operation) string {
	raw := mergedRaw(spec, op)
	apiType := strings.ToUpper(rawString(raw, "api_type"))
	dataFormat := strings.ToUpper(rawString(raw, "data_format"))
	if strings.TrimSpace(op.Endpoint) == "" {
		if serviceRootOnly(rawString(raw, "end_point_url")) {
			return "service_root"
		}
		return "no_endpoint"
	}
	host, malformed := urlHost(op.Endpoint)
	if malformed {
		return "malformed_endpoint"
	}
	if host != "" && !isDataGoKrGateway(host) {
		return "external_endpoint"
	}
	if apiType == "SOAP" {
		return "soap"
	}
	if dataFormat == "WMS" {
		return "wms"
	}
	return "data_go_kr_gateway"
}

func VerificationParams(spec Spec, op Operation) (map[string]string, []string) {
	params := map[string]string{}
	for key, value := range op.DefaultParams {
		if strings.TrimSpace(key) != "" {
			params[key] = value
		}
	}
	if spec.Smoke != nil && (spec.Smoke.Operation == "" || spec.Smoke.Operation == op.Name) {
		for key, value := range spec.Smoke.Params {
			if strings.TrimSpace(key) != "" {
				params[key] = value
			}
		}
	}
	for _, param := range op.RequestParams {
		name := strings.TrimSpace(param.Name)
		if name == "" || isAuthParam(name) || params[name] != "" {
			continue
		}
		if value, ok := safeVerificationDefault(name); ok {
			params[name] = value
			continue
		}
		if !isProbablyOptionalParam(name) {
			params[name] = ""
		}
	}
	missing := make([]string, 0)
	for _, param := range op.RequestParams {
		name := strings.TrimSpace(param.Name)
		if name == "" || isAuthParam(name) || isProbablyOptionalParam(name) {
			continue
		}
		if strings.TrimSpace(params[name]) == "" {
			missing = append(missing, name)
		}
	}
	return params, missing
}

func VerificationSkipReason(candidate VerificationCandidate) string {
	switch candidate.DependencyClass {
	case "data_go_kr_gateway":
	case "external_endpoint":
		return "external_provider_adapter_missing"
	case "service_root":
		return "service_root_only"
	case "no_endpoint":
		return "missing_endpoint"
	case "malformed_endpoint":
		return "malformed_endpoint"
	case "soap":
		return "unsupported_protocol_soap"
	case "wms":
		return "unsupported_protocol_wms"
	default:
		return "unknown_dependency_class"
	}
	if approvalRequired(rawString(mergedRaw(candidate.Spec, candidate.Operation), "is_confirmed_for_dev_nm")) ||
		approvalRequired(rawString(mergedRaw(candidate.Spec, candidate.Operation), "is_confirmed_for_prod_nm")) {
		return "approval_required"
	}
	if len(candidate.MissingParams) > 0 {
		return "missing_required_params"
	}
	return ""
}

func NewSkippedVerificationResult(candidate VerificationCandidate, reason string) VerificationResult {
	if reason == "" {
		reason = candidate.SkipReason
	}
	return VerificationResult{
		DatasetID:       candidate.Spec.ID,
		Title:           candidate.Spec.Title,
		Operation:       candidate.Operation.Name,
		Provider:        candidate.Spec.Provider,
		EndpointHost:    candidate.EndpointHost,
		DependencyClass: candidate.DependencyClass,
		Status:          "skipped",
		Reason:          reason,
		Params:          publicVerificationParams(candidate.Params),
		MissingParams:   candidate.MissingParams,
	}
}

func SummarizeVerification(results []VerificationResult) VerificationSummary {
	var summary VerificationSummary
	for _, result := range results {
		summary.Total++
		switch result.Status {
		case "verified":
			summary.Verified++
		case "failed":
			summary.Failed++
		case "skipped":
			summary.Skipped++
		default:
			summary.Unknown++
		}
	}
	return summary
}

func SummarizeVerificationReport(source VerificationReport, sourcePath string, limit int) VerificationSummaryReport {
	results := source.Results
	truncated := false
	statusGroups := verificationGroups(results, func(result VerificationResult) VerificationGroup {
		return VerificationGroup{Key: result.Status, Status: result.Status}
	}, 0, nil)
	reasonGroups := verificationGroups(results, func(result VerificationResult) VerificationGroup {
		key := strings.TrimSpace(result.Reason)
		if key == "" && result.ProviderStatus != nil {
			key = strings.TrimSpace(result.ProviderStatus.ReasonCode)
		}
		return VerificationGroup{Key: key, Reason: key}
	}, limit, &truncated)
	providerGroups := verificationGroups(results, func(result VerificationResult) VerificationGroup {
		return VerificationGroup{Key: result.Provider, Provider: result.Provider}
	}, limit, &truncated)
	hostGroups := verificationGroups(results, func(result VerificationResult) VerificationGroup {
		return VerificationGroup{Key: result.EndpointHost, Host: result.EndpointHost}
	}, limit, &truncated)
	kindGroups := verificationGroups(results, func(result VerificationResult) VerificationGroup {
		return VerificationGroup{Key: result.DependencyClass, Kind: result.DependencyClass}
	}, limit, &truncated)
	return VerificationSummaryReport{
		GeneratedAt: source.GeneratedAt,
		Source:      sourcePath,
		Provider:    source.Provider,
		Registry:    source.Registry,
		Limit:       limit,
		Truncated:   truncated,
		Summary:     SummarizeVerification(results),
		Groups: VerificationSummaryGroups{
			ByStatus:       statusGroups,
			ByReason:       reasonGroups,
			ByProvider:     providerGroups,
			ByEndpointHost: hostGroups,
			ByKind:         kindGroups,
		},
	}
}

func verificationGroups(results []VerificationResult, keyFunc func(VerificationResult) VerificationGroup, limit int, truncated *bool) []VerificationGroup {
	counts := map[string]VerificationGroup{}
	for _, result := range results {
		group := keyFunc(result)
		group.Key = strings.TrimSpace(group.Key)
		if group.Key == "" {
			continue
		}
		existing := counts[group.Key]
		if existing.Key == "" {
			existing = group
		}
		existing.Count++
		counts[group.Key] = existing
	}
	groups := make([]VerificationGroup, 0, len(counts))
	for _, group := range counts {
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b VerificationGroup) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Key, b.Key)
	})
	if limit > 0 && len(groups) > limit {
		if truncated != nil {
			*truncated = true
		}
		return groups[:limit]
	}
	return groups
}

func publicVerificationParams(params map[string]string) map[string]string {
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

func safeVerificationDefault(name string) (string, bool) {
	normalized := normalizeParamName(name)
	switch normalized {
	case "pageno", "page_no", "page", "pageindex", "page_index":
		return "1", true
	case "numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit":
		return "1", true
	case "type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return "json", true
	case "acyear", "ac_year", "year", "yyyy", "stdyear", "std_year":
		return "2024", true
	default:
		return "", false
	}
}

func isProbablyOptionalParam(name string) bool {
	normalized := normalizeParamName(name)
	switch normalized {
	case "pageno", "page_no", "page", "pageindex", "page_index",
		"numofrows", "num_of_rows", "rows", "perpage", "per_page", "pagesize", "page_size", "limit",
		"type", "_type", "datatype", "data_type", "returntype", "return_type", "resulttype", "result_type":
		return true
	default:
		return false
	}
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

func normalizeParamName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	replacer := strings.NewReplacer("-", "_", " ", "_")
	return replacer.Replace(name)
}

type VerificationResolveError struct {
	status     ResolveStatus
	ref        string
	candidates []Spec
}

func (e VerificationResolveError) Error() string {
	if e.status == ResolveAmbiguous {
		return "ambiguous: " + e.ref
	}
	return "not found: " + e.ref
}

func (e VerificationResolveError) Status() ResolveStatus { return e.status }

func (e VerificationResolveError) Ref() string { return e.ref }

func (e VerificationResolveError) Candidates() []Spec { return e.candidates }
