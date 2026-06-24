package datago

import (
	"net/url"
	"strings"
	"testing"
)

func TestSmokeCommandQuotesArgsWithSpaces(t *testing.T) {
	spec := Spec{
		ID: "999",
		Smoke: &Smoke{
			Operation: "목록 조회",
			Params: map[string]string{
				"AREA": "서울 중구",
				"PAGE": "1",
			},
		},
	}
	got := spec.SmokeCommand()
	want := `datapan get 999 --operation "목록 조회" "AREA=서울 중구" PAGE=1 --json`
	if got != want {
		t.Fatalf("SmokeCommand()=%q want %q", got, want)
	}
}

func TestCommandStringLeavesSimpleArgsUnquoted(t *testing.T) {
	got := CommandString([]string{"datapan", "get", "15126469", "LAWD_CD=11110", "--json"})
	want := "datapan get 15126469 LAWD_CD=11110 --json"
	if got != want {
		t.Fatalf("CommandString()=%q want %q", got, want)
	}
}

func TestQueryWithServiceKeyPreservesEncodedPortalKey(t *testing.T) {
	raw := QueryWithServiceKey(url.Values{"page": {"1"}}, "abc%2Bdef%2Fghi%3D")
	if !strings.Contains(raw, "serviceKey=abc%2Bdef%2Fghi%3D") {
		t.Fatalf("expected encoded serviceKey to be preserved: %s", raw)
	}
	if strings.Contains(raw, "%252B") || strings.Contains(raw, "%252F") || strings.Contains(raw, "%253D") {
		t.Fatalf("serviceKey was double encoded: %s", raw)
	}
	parsed, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Get("serviceKey"); got != "abc+def/ghi=" {
		t.Fatalf("serviceKey=%q", got)
	}
}

func TestQueryWithServiceKeyEncodesDecodedKey(t *testing.T) {
	raw := QueryWithServiceKey(url.Values{"page": {"1"}}, "abc+def/ghi=")
	if !strings.Contains(raw, "serviceKey=abc%2Bdef%2Fghi%3D") {
		t.Fatalf("expected decoded serviceKey to be encoded: %s", raw)
	}
}

func TestOperationEndpointSkipsServiceRootWithoutOperationURL(t *testing.T) {
	got := operationEndpoint("http://openapi.tour.go.kr/openapi/service", "")
	if got != "" {
		t.Fatalf("operationEndpoint()=%q; service root should not be treated as callable", got)
	}
}

func TestOperationEndpointKeepsConcreteEndpointWithoutOperationURL(t *testing.T) {
	got := operationEndpoint("https://example.test/api/items", "")
	if got != "https://example.test/api/items" {
		t.Fatalf("operationEndpoint()=%q", got)
	}
}

func TestAuditSamplesDeduplicateDatasetIDs(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "100",
			Title:    "중복 샘플",
			Provider: "data.go.kr",
			Priority: "P2",
			Operations: []Operation{
				{
					Name: "목록",
					Source: &Source{System: "data.go.kr", Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
				{
					Name: "상세",
					Source: &Source{System: "data.go.kr", Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
			},
		},
	})

	audit := AuditRegistry(reg, 5)
	if audit.Dependency.ServiceRootOnlyOperations != 2 || audit.Dependency.SOAPOperations != 2 || audit.Dependency.WMSOperations != 2 {
		t.Fatalf("operation counts should stay operation-scoped: %#v", audit.Dependency)
	}
	for name, samples := range map[string][]AuditSample{
		"service_root_only": audit.Samples.ServiceRootOnly,
		"soap":              audit.Samples.SOAP,
		"wms":               audit.Samples.WMS,
	} {
		if len(samples) != 1 || samples[0].ID != "100" {
			t.Fatalf("%s samples should be dataset-deduplicated: %#v", name, samples)
		}
	}
}

func TestCatalogErrorStatusFieldsUseDeterministicTieBreakers(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "200",
			Title:    "B",
			Provider: "data.go.kr",
			Operations: []Operation{{
				Name: "목록",
				ResponseParams: []Param{
					{Name: "resultCode", Label: "응답결과 코드"},
				},
			}},
		},
		{
			ID:       "100",
			Title:    "A",
			Provider: "data.go.kr",
			Operations: []Operation{{
				Name: "목록",
				ResponseParams: []Param{
					{Name: "resultCode", Label: "결과 코드"},
				},
			}},
		},
	})

	report := AnalyzeCatalogErrors(reg, 0)
	if len(report.StatusFields) != 2 {
		t.Fatalf("status fields=%#v", report.StatusFields)
	}
	if report.StatusFields[0].Label != "결과 코드" || report.StatusFields[1].Label != "응답결과 코드" {
		t.Fatalf("status fields not deterministically ordered by label: %#v", report.StatusFields)
	}
}
