package datago

import (
	"slices"
	"strings"
)

type RouteDispositionReport struct {
	GeneratedAt string                  `json:"generated_at"`
	Provider    string                  `json:"provider"`
	Registry    string                  `json:"registry,omitempty"`
	Probe       string                  `json:"probe,omitempty"`
	Limit       int                     `json:"limit"`
	Truncated   bool                    `json:"truncated"`
	Summary     RouteDispositionSummary `json:"summary"`
	Routes      []RouteDisposition      `json:"routes"`
}

type RouteDispositionSummary struct {
	RoutesTotal            int                     `json:"routes_total"`
	Operations             int                     `json:"operations"`
	Hosts                  int                     `json:"hosts"`
	WithProbeEvidence      int                     `json:"with_probe_evidence"`
	WithoutProbeEvidence   int                     `json:"without_probe_evidence"`
	DeadRouteCandidates    int                     `json:"dead_route_candidates"`
	TransientFailures      int                     `json:"transient_failures"`
	ParameterBlockedRoutes int                     `json:"parameter_blocked_routes"`
	AdapterCandidates      int                     `json:"adapter_candidates"`
	ByDisposition          []RouteDispositionGroup `json:"by_disposition"`
	ByReason               []RouteDispositionGroup `json:"by_reason,omitempty"`
	ByHost                 []RouteDispositionGroup `json:"by_host,omitempty"`
}

type RouteDispositionGroup struct {
	Key         string `json:"key"`
	Count       int    `json:"count"`
	Disposition string `json:"disposition,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Host        string `json:"host,omitempty"`
}

type RouteDisposition struct {
	DatasetID         string   `json:"dataset_id"`
	Title             string   `json:"title"`
	Organization      string   `json:"organization,omitempty"`
	Operation         string   `json:"operation"`
	Endpoint          string   `json:"endpoint,omitempty"`
	EndpointHost      string   `json:"endpoint_host"`
	DependencyClass   string   `json:"dependency_class"`
	Disposition       string   `json:"disposition"`
	RecommendedAction string   `json:"recommended_action"`
	ProbeStatus       string   `json:"probe_status,omitempty"`
	ProbeReason       string   `json:"probe_reason,omitempty"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	BodyShape         string   `json:"body_shape,omitempty"`
	MissingParams     []string `json:"missing_params,omitempty"`
}

func RouteDispositionReportForDependencies(generatedAt, provider, registryPath, probePath string, deps []DependencyOperationSummary, probe *VerificationReport, limit int) RouteDispositionReport {
	if limit < 0 {
		limit = 0
	}
	probeByKey := map[string]VerificationResult{}
	if probe != nil {
		for _, result := range probe.Results {
			probeByKey[routeProbeKey(result.DatasetID, result.Operation, result.EndpointHost)] = result
		}
	}
	routes := make([]RouteDisposition, 0)
	hostSet := map[string]bool{}
	for _, dep := range deps {
		if dep.AdapterStatus != "missing" || (dep.DependencyClass != "external_endpoint" && dep.DependencyClass != "service_root") {
			continue
		}
		host := routeHost(dep)
		if host == "" {
			continue
		}
		probeResult, hasProbe := probeByKey[routeProbeKey(dep.DatasetID, dep.Operation, host)]
		route := routeDispositionForDependency(dep, host, probeResult, hasProbe)
		routes = append(routes, route)
		hostSet[host] = true
	}
	slices.SortFunc(routes, func(a, b RouteDisposition) int {
		if a.Disposition != b.Disposition {
			return routeDispositionRank(a.Disposition) - routeDispositionRank(b.Disposition)
		}
		if a.EndpointHost != b.EndpointHost {
			return strings.Compare(a.EndpointHost, b.EndpointHost)
		}
		if a.DatasetID != b.DatasetID {
			return strings.Compare(a.DatasetID, b.DatasetID)
		}
		return strings.Compare(a.Operation, b.Operation)
	})
	truncated := false
	outRoutes := routes
	if limit > 0 && len(outRoutes) > limit {
		outRoutes = outRoutes[:limit]
		truncated = true
	}
	return RouteDispositionReport{
		GeneratedAt: generatedAt,
		Provider:    provider,
		Registry:    registryPath,
		Probe:       probePath,
		Limit:       limit,
		Truncated:   truncated,
		Summary:     summarizeRouteDispositions(routes, len(hostSet)),
		Routes:      outRoutes,
	}
}

func routeDispositionForDependency(dep DependencyOperationSummary, host string, probe VerificationResult, hasProbe bool) RouteDisposition {
	route := RouteDisposition{
		DatasetID:       dep.DatasetID,
		Title:           dep.Title,
		Organization:    dep.Organization,
		Operation:       dep.Operation,
		Endpoint:        dep.Endpoint,
		EndpointHost:    host,
		DependencyClass: dep.DependencyClass,
		MissingParams:   append([]string(nil), dep.MissingParams...),
	}
	if hasProbe {
		route.ProbeStatus = probe.Status
		route.ProbeReason = probe.Reason
		route.HTTPStatus = probe.HTTPStatus
		route.BodyShape = probe.BodyShape
	}
	route.Disposition, route.RecommendedAction = routeDisposition(probe, hasProbe, dep.MissingParams)
	return route
}

func routeDisposition(probe VerificationResult, hasProbe bool, missingParams []string) (string, string) {
	if hasProbe {
		switch {
		case probe.Reason == "unadapted_probe_http_404":
			return "dead_route_candidate", "confirm upstream metadata or mark the route stale before adapter work"
		case probe.Reason == "unadapted_probe_http_503" || strings.HasPrefix(probe.Reason, "unadapted_probe_http_5") ||
			probe.Reason == "unadapted_probe_timeout" ||
			probe.Reason == "unadapted_probe_dns" ||
			probe.Reason == "unadapted_probe_connection_refused" ||
			probe.Reason == "unadapted_probe_request_error":
			return "transient_failure", "retry with bounded evidence before building a provider adapter"
		}
	}
	if len(missingParams) > 0 {
		return "parameter_blocked", "add safe default or smoke parameters before adapter work"
	}
	if hasProbe {
		return "adapter_candidate", "build a provider adapter using the captured probe evidence"
	}
	return "adapter_candidate", "probe the route or build a provider adapter"
}

func summarizeRouteDispositions(routes []RouteDisposition, hosts int) RouteDispositionSummary {
	summary := RouteDispositionSummary{
		RoutesTotal: len(routes),
		Operations:  len(routes),
		Hosts:       hosts,
	}
	for _, route := range routes {
		if route.ProbeStatus != "" || route.ProbeReason != "" {
			summary.WithProbeEvidence++
		} else {
			summary.WithoutProbeEvidence++
		}
		switch route.Disposition {
		case "dead_route_candidate":
			summary.DeadRouteCandidates++
		case "transient_failure":
			summary.TransientFailures++
		case "parameter_blocked":
			summary.ParameterBlockedRoutes++
		case "adapter_candidate":
			summary.AdapterCandidates++
		}
	}
	summary.ByDisposition = routeDispositionGroups(routes, func(route RouteDisposition) RouteDispositionGroup {
		return RouteDispositionGroup{Key: route.Disposition, Disposition: route.Disposition}
	})
	summary.ByReason = routeDispositionGroups(routes, func(route RouteDisposition) RouteDispositionGroup {
		if route.ProbeReason == "" {
			return RouteDispositionGroup{}
		}
		return RouteDispositionGroup{Key: route.ProbeReason, Reason: route.ProbeReason}
	})
	summary.ByHost = routeDispositionGroups(routes, func(route RouteDisposition) RouteDispositionGroup {
		return RouteDispositionGroup{Key: route.EndpointHost, Host: route.EndpointHost}
	})
	return summary
}

func routeDispositionGroups(routes []RouteDisposition, keyFunc func(RouteDisposition) RouteDispositionGroup) []RouteDispositionGroup {
	counts := map[string]RouteDispositionGroup{}
	for _, route := range routes {
		group := keyFunc(route)
		if strings.TrimSpace(group.Key) == "" {
			continue
		}
		group.Count = counts[group.Key].Count + 1
		counts[group.Key] = group
	}
	groups := make([]RouteDispositionGroup, 0, len(counts))
	for _, group := range counts {
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b RouteDispositionGroup) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Key, b.Key)
	})
	return groups
}

func routeProbeKey(datasetID, operation, host string) string {
	return strings.TrimSpace(datasetID) + "\x00" + strings.TrimSpace(operation) + "\x00" + strings.ToLower(strings.TrimSpace(host))
}

func routeHost(dep DependencyOperationSummary) string {
	if dep.EndpointHost != "" {
		return dep.EndpointHost
	}
	return dep.SourceHost
}

func routeDispositionRank(disposition string) int {
	switch disposition {
	case "dead_route_candidate":
		return 0
	case "transient_failure":
		return 1
	case "parameter_blocked":
		return 2
	case "adapter_candidate":
		return 3
	default:
		return 4
	}
}
