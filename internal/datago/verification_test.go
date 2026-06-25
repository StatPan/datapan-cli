package datago

import "testing"

func TestVerificationCandidatesClassifyAndSkip(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "100",
			Title:    "기관_게이트웨이",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://apis.data.go.kr/100/list",
					RequestParams: []Param{
						{Name: "serviceKey"},
						{Name: "pageNo"},
						{Name: "base_date"},
					},
				},
			},
		},
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://openapi.q-net.or.kr/api/list"},
			},
		},
		{
			ID:       "300",
			Title:    "기관_서비스루트",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "통계", Source: &Source{Raw: map[string]any{"end_point_url": "http://openapi.tour.go.kr/openapi/service"}}},
			},
		},
	})
	candidates, truncated, err := VerificationCandidates(reg, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates=%d", len(candidates))
	}
	first := candidates[0]
	if first.DependencyClass != "data_go_kr_gateway" {
		t.Fatalf("dependency=%s", first.DependencyClass)
	}
	if first.Params["pageNo"] != "1" {
		t.Fatalf("expected safe page default: %#v", first.Params)
	}
	if first.SkipReason != "missing_required_params" {
		t.Fatalf("skip reason=%q", first.SkipReason)
	}
	if len(first.MissingParams) != 1 || first.MissingParams[0] != "base_date" {
		t.Fatalf("missing params=%#v", first.MissingParams)
	}
	second := candidates[1]
	if second.DependencyClass != "external_endpoint" || second.SkipReason != "external_provider_adapter_missing" {
		t.Fatalf("unexpected external candidate: %#v", second)
	}
	third := candidates[2]
	if third.DependencyClass != "service_root" || third.EndpointHost != "openapi.tour.go.kr" || third.SkipReason != "service_root_only" {
		t.Fatalf("unexpected service-root candidate: %#v", third)
	}

	limited, truncated, err := VerificationCandidates(reg, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(limited) != 3 {
		t.Fatalf("exact limit should not be truncated: truncated=%v len=%d", truncated, len(limited))
	}
	limited, truncated, err = VerificationCandidates(reg, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(limited) != 1 {
		t.Fatalf("expected truncation: truncated=%v len=%d", truncated, len(limited))
	}
}

func TestVerificationParamsUsesSmokeParams(t *testing.T) {
	spec := Spec{
		ID:       "100",
		Provider: "data.go.kr",
		Smoke: &Smoke{
			Operation: "목록",
			Params: map[string]string{
				"base_date": "20260624",
			},
		},
	}
	op := Operation{
		Name:     "목록",
		Endpoint: "https://apis.data.go.kr/100/list",
		RequestParams: []Param{
			{Name: "serviceKey"},
			{Name: "base_date"},
			{Name: "numOfRows"},
		},
	}
	params, missing := VerificationParams(spec, op)
	if len(missing) != 0 {
		t.Fatalf("missing=%#v", missing)
	}
	if params["base_date"] != "20260624" || params["numOfRows"] != "1" {
		t.Fatalf("params=%#v", params)
	}
}

func TestVerificationCandidatesFilterBeforeLimit(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "100",
			Title:    "게이트웨이",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://apis.data.go.kr/100/list"},
			},
		},
		{
			ID:       "200",
			Title:    "Q-Net 1",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://openapi.q-net.or.kr/api/list"},
			},
		},
		{
			ID:       "300",
			Title:    "Q-Net 2",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://c.q-net.or.kr/api/list"},
			},
		},
	})

	candidates, truncated, err := VerificationCandidatesWithFilters(reg, "", "", 1, VerificationCandidateFilters{
		Hosts: []string{"openapi.q-net.or.kr", "c.q-net.or.kr"},
		Kind:  "external_endpoint",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(candidates) != 1 {
		t.Fatalf("expected filtered truncation: truncated=%v len=%d", truncated, len(candidates))
	}
	if candidates[0].Spec.ID != "200" {
		t.Fatalf("filter should apply before limit: %#v", candidates[0])
	}
}

func TestSummarizeVerificationReportGroupsEvidence(t *testing.T) {
	report := VerificationReport{
		GeneratedAt: "2026-06-24T00:00:00Z",
		Provider:    "data.go.kr",
		Registry:    "registry.json",
		Results: []VerificationResult{
			{DatasetID: "100", Provider: "q-net", EndpointHost: "openapi.q-net.or.kr", DependencyClass: "external_endpoint", Status: "verified"},
			{DatasetID: "101", Provider: "q-net", EndpointHost: "openapi.q-net.or.kr", DependencyClass: "external_endpoint", Status: "failed", Reason: "qnet_connection_validation_failed"},
			{DatasetID: "102", Provider: "q-net", EndpointHost: "openapi.q-net.or.kr", DependencyClass: "external_endpoint", Status: "failed", ProviderStatus: &ProviderStatus{ReasonCode: "qnet_service_key_not_registered"}},
			{DatasetID: "103", Provider: "data.go.kr", EndpointHost: "apis.data.go.kr", DependencyClass: "data_go_kr_gateway", Status: "skipped", Reason: "missing_required_params"},
		},
	}

	summary := SummarizeVerificationReport(report, "verification.json", 1)
	if summary.Source != "verification.json" || summary.Summary.Total != 4 || summary.Summary.Failed != 2 || !summary.Truncated {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(summary.Groups.ByStatus) != 3 || summary.Groups.ByStatus[0].Key != "failed" || summary.Groups.ByStatus[0].Count != 2 {
		t.Fatalf("unexpected status groups: %#v", summary.Groups.ByStatus)
	}
	if len(summary.Groups.ByReason) != 1 || summary.Groups.ByReason[0].Key != "missing_required_params" {
		t.Fatalf("expected reason groups to be limited and sorted: %#v", summary.Groups.ByReason)
	}
	if len(summary.Groups.ByProvider) != 1 || summary.Groups.ByProvider[0].Key != "q-net" || summary.Groups.ByProvider[0].Count != 3 {
		t.Fatalf("unexpected provider groups: %#v", summary.Groups.ByProvider)
	}
}
