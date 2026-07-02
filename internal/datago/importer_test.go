package datago

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type importerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f importerRoundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExtractLinkDetailOperationURLsKeepsOnlyRequestLinks(t *testing.T) {
	html := `
		<a href="https://www.data.go.kr/" target="_blank">portal</a>
		<a href="https://twitter.com/koreadataportal" target="_blank">twitter</a>
		<a href="https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&amp;infId=ABC&amp;infSeq=3" onclick="fn_LinkApiRequest('uddi:test')">경기도</a>
		<a href="https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&amp;infId=ABC&amp;infSeq=3" onclick="fn_LinkApiRequest('uddi:test')">duplicate</a>
		<a href="https://open.assembly.go.kr/portal/data/service/selectAPIServicePage.do/OJ24" onclick="fn_LinkApiRequest('uddi:test2')">국회</a>
	`

	got := ExtractLinkDetailOperationURLs(html)
	want := []string{
		"https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&infId=ABC&infSeq=3",
		"https://open.assembly.go.kr/portal/data/service/selectAPIServicePage.do/OJ24",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("links=%#v want %#v", got, want)
	}
}

func TestEnrichLinkDetailOperationsAddsOperationsForLinkRows(t *testing.T) {
	rows := []OpenDataListRow{
		{
			APIType:    "LINK",
			ListID:     "15005231",
			ListTitle:  "경기도 정기간행물 현황",
			Title:      "정기간행물 현황",
			OrgName:    "경기도",
			DataFormat: "XML",
		},
		{
			APIType:       "REST",
			ListID:        "15000017",
			ListTitle:     "이미 커버된 API",
			OperationName: "목록",
			EndpointURL:   "https://api.example.test/items",
		},
	}
	client := importerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://www.data.go.kr/data/15005231/openapi.do" {
			t.Fatalf("unexpected detail request %s", req.URL)
		}
		body := `<a href="https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&amp;infId=ABC&amp;infSeq=3" onclick="fn_LinkApiRequest('uddi:test')">link</a>`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	enriched, result, err := EnrichLinkDetailOperations(context.Background(), client, rows, LinkDetailEnrichmentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates != 1 || result.DetailsFetched != 1 || result.OperationsAdded != 1 || result.RowsAdded != 0 {
		t.Fatalf("unexpected enrichment result %#v", result)
	}
	specs, operations := NormalizeOpenDataRows(enriched)
	if operations != 2 {
		t.Fatalf("operations=%d specs=%#v", operations, specs)
	}
	spec := specs[1]
	if spec.ID != "15005231" {
		t.Fatalf("expected enriched spec to sort second, got %#v", spec)
	}
	if len(spec.Operations) != 1 {
		t.Fatalf("operations=%#v", spec.Operations)
	}
	if got := spec.Operations[0].Endpoint; got != "https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&infId=ABC&infSeq=3" {
		t.Fatalf("endpoint=%q", got)
	}
}
