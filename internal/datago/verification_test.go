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
	})
	candidates, truncated, err := VerificationCandidates(reg, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if len(candidates) != 2 {
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

	limited, truncated, err := VerificationCandidates(reg, "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(limited) != 2 {
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
