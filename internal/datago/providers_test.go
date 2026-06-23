package datago

import "testing"

func TestProviderBacklogForRegistryClassifiesHosts(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "100",
			Title:    "기관_게이트웨이",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://apis.data.go.kr/100/list",
					Source:   &Source{Raw: map[string]any{"guide_url": "https://external.example.test/docs"}},
				},
			},
		},
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://openapi.q-net.or.kr/api/list",
					Source:   &Source{Raw: map[string]any{"is_confirmed_for_prod_nm": "심의승인"}},
				},
			},
		},
		{
			ID:       "300",
			Title:    "기관_루트",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name: "목록",
					Source: &Source{Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
			},
		},
	})
	backlog := ProviderBacklogForRegistry(reg, 2)
	if backlog.Summary.Hosts != 4 {
		t.Fatalf("hosts=%d providers=%#v", backlog.Summary.Hosts, backlog.Providers)
	}
	if backlog.Summary.DataGoKrGatewayHosts != 1 {
		t.Fatalf("gateway hosts=%d", backlog.Summary.DataGoKrGatewayHosts)
	}
	if backlog.Summary.ExternalEndpointHosts != 1 {
		t.Fatalf("external endpoint hosts=%d", backlog.Summary.ExternalEndpointHosts)
	}
	if backlog.Summary.ExternalGuideHosts != 1 {
		t.Fatalf("external guide hosts=%d", backlog.Summary.ExternalGuideHosts)
	}
	if backlog.Summary.MissingAdapterHosts != 2 {
		t.Fatalf("missing adapter hosts=%d", backlog.Summary.MissingAdapterHosts)
	}
	if backlog.Summary.NeedsAdapterOperations != 1 {
		t.Fatalf("needs adapter operations=%d", backlog.Summary.NeedsAdapterOperations)
	}
	if backlog.Summary.UnsupportedProtocolOps != 1 {
		t.Fatalf("unsupported protocol operations=%d", backlog.Summary.UnsupportedProtocolOps)
	}

	qnet := findProviderSummary(backlog.Providers, "openapi.q-net.or.kr")
	if qnet == nil {
		t.Fatalf("missing q-net provider: %#v", backlog.Providers)
	}
	if qnet.AdapterStatus != "missing" || qnet.Provider != "q-net" || qnet.ExternalEndpointOperations != 1 {
		t.Fatalf("unexpected q-net summary: %#v", qnet)
	}
	if len(qnet.SampleIDs) != 1 || qnet.SampleIDs[0] != "200" {
		t.Fatalf("unexpected q-net samples: %#v", qnet.SampleIDs)
	}

	guide := findProviderSummary(backlog.Providers, "external.example.test")
	if guide == nil || guide.AdapterStatus != "guide_only" || guide.ExternalGuideSpecs != 1 {
		t.Fatalf("unexpected guide summary: %#v", guide)
	}

	gateway := findProviderSummary(backlog.Providers, "apis.data.go.kr")
	if gateway == nil || gateway.AdapterStatus != "builtin" || gateway.Operations != 1 {
		t.Fatalf("unexpected gateway summary: %#v", gateway)
	}
}

func TestProviderBacklogMarksRegisteredAdapterHosts(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://openapi.q-net.or.kr/api/list"},
				{Name: "상세", Endpoint: "https://c.q-net.or.kr/api/detail"},
			},
		},
		{
			ID:       "300",
			Title:    "기관_외부2",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://missing.example.test/api/list"},
			},
		},
	})

	backlog := ProviderBacklogForRegistryWithAdapters(reg, 2, []string{"openapi.q-net.or.kr", "c.q-net.or.kr"})
	if backlog.Summary.RegisteredAdapterHosts != 2 {
		t.Fatalf("registered adapter hosts=%d", backlog.Summary.RegisteredAdapterHosts)
	}
	if backlog.Summary.MissingAdapterHosts != 1 {
		t.Fatalf("missing adapter hosts=%d", backlog.Summary.MissingAdapterHosts)
	}
	if backlog.Summary.NeedsAdapterOperations != 1 {
		t.Fatalf("needs adapter operations=%d", backlog.Summary.NeedsAdapterOperations)
	}
	for _, host := range []string{"openapi.q-net.or.kr", "c.q-net.or.kr"} {
		qnet := findProviderSummary(backlog.Providers, host)
		if qnet == nil || qnet.AdapterStatus != "adapter" {
			t.Fatalf("expected adapter status for %s: %#v", host, qnet)
		}
	}
}

func TestDependencyInventoryClassifiesOperations(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:             "100",
			Title:          "기관_게이트웨이",
			Provider:       "data.go.kr",
			Organization:   "기관",
			SourceCategory: "교통",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://apis.data.go.kr/100/list"},
			},
		},
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://openapi.q-net.or.kr/api/list",
					Source: &Source{Raw: map[string]any{
						"is_confirmed_for_dev_nm": "심의승인",
						"guide_url":               "https://www.q-net.or.kr/docs",
					}},
				},
			},
		},
		{
			ID:       "300",
			Title:    "기관_루트",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name: "목록",
					Source: &Source{Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
			},
		},
	})

	summary, deps := DependencyInventoryForRegistry(reg, []string{"openapi.q-net.or.kr"})
	if summary.OperationsTotal != 3 || summary.DataGoKrGatewayOperations != 1 || summary.ExternalEndpointOps != 1 || summary.ServiceRootOperations != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.RegisteredAdapterOps != 1 || summary.MissingAdapterOps != 1 || summary.ApprovalRequiredOps != 1 {
		t.Fatalf("unexpected adapter/approval summary: %#v", summary)
	}
	qnet := findDependency(deps, "200", "목록")
	if qnet == nil {
		t.Fatalf("missing q-net dependency: %#v", deps)
	}
	if qnet.DependencyClass != "external_endpoint" || qnet.AdapterStatus != "adapter" || qnet.ProviderFamily != "q-net" || !qnet.ApprovalRequired || qnet.SkipReason != "" {
		t.Fatalf("unexpected q-net dependency: %#v", qnet)
	}
	root := findDependency(deps, "300", "목록")
	if root == nil || root.DependencyClass != "service_root" || root.AdapterStatus != "missing" || root.SourceHost != "openapi.tour.go.kr" {
		t.Fatalf("unexpected service root dependency: %#v", root)
	}
	filtered := FilterDependencyOperations(deps, &DependencyInventoryFilters{Status: "missing"})
	if len(filtered) != 1 || filtered[0].DatasetID != "300" {
		t.Fatalf("unexpected missing filter: %#v", filtered)
	}
}

func findProviderSummary(providers []ProviderSummary, host string) *ProviderSummary {
	for i := range providers {
		if providers[i].Host == host {
			return &providers[i]
		}
	}
	return nil
}

func findDependency(deps []DependencyOperationSummary, datasetID, operation string) *DependencyOperationSummary {
	for i := range deps {
		if deps[i].DatasetID == datasetID && deps[i].Operation == operation {
			return &deps[i]
		}
	}
	return nil
}
