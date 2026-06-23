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

func findProviderSummary(providers []ProviderSummary, host string) *ProviderSummary {
	for i := range providers {
		if providers[i].Host == host {
			return &providers[i]
		}
	}
	return nil
}
