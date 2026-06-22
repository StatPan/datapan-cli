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
