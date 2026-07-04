package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type providerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f providerRoundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type fakeAdapter struct {
	StaticHostMatcher
}

func (fakeAdapter) Name() string { return "fake" }

func (fakeAdapter) Hosts() []string { return []string{"api.example.test"} }

func (fakeAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return "external_endpoint"
}

func (fakeAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Operation:       req.Operation.Name,
		Provider:        req.Spec.Provider,
		DependencyClass: "external_endpoint",
		Status:          "skipped",
		Reason:          "fake_adapter",
	}
}

func (fakeAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{
		OK:        true,
		Provider:  req.Spec.Provider,
		Dataset:   req.Spec.ID,
		Operation: req.Operation.Name,
	}, nil
}

type namedFakeAdapter struct {
	name  string
	hosts []string
}

func (a namedFakeAdapter) Name() string { return a.name }

func (a namedFakeAdapter) Hosts() []string { return a.hosts }

func (a namedFakeAdapter) MatchHost(host string) bool {
	return StaticHostMatcher{Hosts: a.hosts}.MatchHost(host)
}

func (a namedFakeAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return "external_endpoint"
}

func (a namedFakeAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return datago.VerificationResult{DatasetID: req.Spec.ID, Operation: req.Operation.Name, Provider: req.Spec.Provider, Status: "skipped"}
}

func (a namedFakeAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{OK: true, Provider: req.Spec.Provider, Dataset: req.Spec.ID, Operation: req.Operation.Name}, nil
}

type capabilityFakeAdapter struct {
	namedFakeAdapter
	capabilities []string
}

func (a capabilityFakeAdapter) Capabilities() []string {
	return a.capabilities
}

func TestStaticHostMatcher(t *testing.T) {
	matcher := StaticHostMatcher{Hosts: []string{"OPENAPI.Q-NET.OR.KR", " c.q-net.or.kr "}}
	for _, host := range []string{"openapi.q-net.or.kr", "C.Q-NET.OR.KR"} {
		if !matcher.MatchHost(host) {
			t.Fatalf("expected host %q to match", host)
		}
	}
	if matcher.MatchHost("apis.data.go.kr") {
		t.Fatal("unexpected data.go.kr host match")
	}
}

func TestAdapterContractUsesDatapanTypes(t *testing.T) {
	var adapter Adapter = fakeAdapter{StaticHostMatcher{Hosts: []string{"api.example.test"}}}
	spec := datago.Spec{ID: "100", Provider: "example"}
	op := datago.Operation{Name: "list", Endpoint: "https://api.example.test/list"}
	if !adapter.MatchHost("api.example.test") {
		t.Fatal("expected adapter host match")
	}
	verification := adapter.Verify(context.Background(), VerificationRequest{Spec: spec, Operation: op})
	if verification.DatasetID != "100" || verification.Status != "skipped" {
		t.Fatalf("unexpected verification result: %#v", verification)
	}
	response, err := adapter.Call(context.Background(), CallRequest{Spec: spec, Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Dataset != "100" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestDataGGAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewDataGGAdapter()
	if !adapter.MatchHost("data.gg.go.kr") {
		t.Fatal("expected data-gg adapter to match data.gg.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("data-gg adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected data-gg capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.gg.go.kr" {
			t.Fatalf("expected data.gg.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("data-gg should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>경기데이터드림</title></head><body>경기도 정기간행물 현황</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15005231", Title: "경기도 정기간행물 현황", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "정기간행물 현황 외부 링크 1", Endpoint: "https://data.gg.go.kr/portal/data/service/selectServicePage.do?infId=PCG359G8UAD471M0GE9K15277158&infSeq=3"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "data-gg" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected data-gg verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected data-gg URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestDataGGAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewDataGGAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15005231", Title: "경기도 정기간행물 현황", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "정기간행물 현황 외부 링크 1", Endpoint: "https://data.gg.go.kr/portal/data/service/selectServicePage.do?infId=missing"},
		HTTP:      client,
	})
	if result.Provider != "data-gg" || result.Status != "failed" || result.Reason != "data_gg_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected data-gg failure result: %#v", result)
	}
}

func TestNFQSAdapterVerifiesHTMLDetailPageWithoutAuth(t *testing.T) {
	adapter := NewNFQSAdapter()
	if !adapter.MatchHost("www.nfqs.go.kr") {
		t.Fatal("expected nfqs adapter to match www.nfqs.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("nfqs adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected nfqs capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.nfqs.go.kr" {
			t.Fatalf("expected www.nfqs.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("nfqs should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>API 상세</title></head><body>수산물재고동향</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15001668", Title: "해양수산부 국립수산물품질관리원_수산물재고동향", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "해양수산부 국립수산물품질관리원_수산물재고동향_20230629", Endpoint: "https://www.nfqs.go.kr/hpmg/api/actionApiDetail.do?apiCd=009"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "nfqs" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected nfqs verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected nfqs URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestNFQSAdapterFailsNonOKDetailPage(t *testing.T) {
	adapter := NewNFQSAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15001668", Title: "해양수산부 국립수산물품질관리원_수산물재고동향", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "해양수산부 국립수산물품질관리원_수산물재고동향_20230629", Endpoint: "https://www.nfqs.go.kr/hpmg/api/actionApiDetail.do?apiCd=missing"},
		HTTP:      client,
	})
	if result.Provider != "nfqs" || result.Status != "failed" || result.Reason != "nfqs_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected nfqs failure result: %#v", result)
	}
}

func TestNongsaroAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewNongsaroAdapter()
	if !adapter.MatchHost("www.nongsaro.go.kr") {
		t.Fatal("expected nongsaro adapter to match www.nongsaro.go.kr")
	}
	if !adapter.MatchHost("nongsaro.go.kr") {
		t.Fatal("expected nongsaro adapter to match nongsaro.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("nongsaro adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected nongsaro capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.nongsaro.go.kr" {
			t.Fatalf("expected www.nongsaro.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("nongsaro should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>공공데이터신청 | 농사로</title></head><body>농촌교육농장</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15005257", Title: "농촌진흥청_농촌교육농장 정보", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "농촌진흥청_농촌교육농장 정보_20171211 외부 링크 1", Endpoint: "https://www.nongsaro.go.kr/portal/ps/psn/psnj/openApiLst.ps?menuId=PS65428&pageIndex=1&pageSize=&sText=%EB%86%8D%EC%B4%8C%EA%B5%90%EC%9C%A1%EB%86%8D%EC%9E%A5"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "nongsaro" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected nongsaro verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected nongsaro URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestNongsaroAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewNongsaroAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15005257", Title: "농촌진흥청_농촌교육농장 정보", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "농촌진흥청_농촌교육농장 정보_20171211 외부 링크 1", Endpoint: "https://www.nongsaro.go.kr/portal/ps/psn/psnj/openApiLst.ps?menuId=PS65428&sText=missing"},
		HTTP:      client,
	})
	if result.Provider != "nongsaro" || result.Status != "failed" || result.Reason != "nongsaro_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected nongsaro failure result: %#v", result)
	}
}

func TestGwanakAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewGwanakAdapter()
	if !adapter.MatchHost("data.gwanak.go.kr") {
		t.Fatal("expected gwanak adapter to match data.gwanak.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("gwanak adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected gwanak capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.gwanak.go.kr" {
			t.Fatalf("expected data.gwanak.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("gwanak should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>Dataset 목록|Dataset|관악구 열린 데이터 광장</title></head><body>공중위생업소</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15007009", Title: "서울특별시 관악구_공중위생업소 현황", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "서울특별시 관악구_공중위생업소 현황_20220624 외부 링크 1", Endpoint: "https://data.gwanak.go.kr/openinf/sheetview.jsp?infId=OA-11496"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "gwanak" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected gwanak verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected gwanak URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestGwanakAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewGwanakAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15007009", Title: "서울특별시 관악구_공중위생업소 현황", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "서울특별시 관악구_공중위생업소 현황_20220624 외부 링크 1", Endpoint: "https://data.gwanak.go.kr/openinf/sheetview.jsp?infId=missing"},
		HTTP:      client,
	})
	if result.Provider != "gwanak" || result.Status != "failed" || result.Reason != "gwanak_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected gwanak failure result: %#v", result)
	}
}

func TestMAFRAAdapterVerifiesHTMLDetailPageWithoutAuth(t *testing.T) {
	adapter := NewMAFRAAdapter()
	if !adapter.MatchHost("data.mafra.go.kr") {
		t.Fatal("expected mafra adapter to match data.mafra.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("mafra adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected mafra capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.mafra.go.kr" {
			t.Fatalf("expected data.mafra.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("mafra should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>데이터 상세 &lt; 통합 검색 &lt; 데이터 검색 &lt; 농림축산식품 공공데이터 포털</title></head><body>유기농업자재</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15002220", Title: "농림축산식품부_유기농업자재 현황", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "유기농업자재 현황_20210402 외부 링크 1", Endpoint: "https://data.mafra.go.kr/opendata/data/indexOpenDataDetail.do?data_id=20200929000000001392&filter_ty=O"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "mafra" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected mafra verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected mafra URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestMAFRAAdapterFailsNonOKDetailPage(t *testing.T) {
	adapter := NewMAFRAAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15002220", Title: "농림축산식품부_유기농업자재 현황", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "유기농업자재 현황_20210402 외부 링크 1", Endpoint: "https://data.mafra.go.kr/opendata/data/indexOpenDataDetail.do?data_id=missing"},
		HTTP:      client,
	})
	if result.Provider != "mafra" || result.Status != "failed" || result.Reason != "mafra_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected mafra failure result: %#v", result)
	}
}

func TestGarakAdapterVerifiesHTMLPublicDataPageWithoutAuth(t *testing.T) {
	adapter := NewGarakAdapter()
	if !adapter.MatchHost("www.garak.co.kr") {
		t.Fatal("expected garak adapter to match www.garak.co.kr")
	}
	if !adapter.MatchHost("temp.garak.co.kr") {
		t.Fatal("expected garak adapter to match temp.garak.co.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("garak adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected garak capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.garak.co.kr" {
			t.Fatalf("expected www.garak.co.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("garak should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>(공공데이터 신청) - 서울시농수산식품공사</title></head><body>주요 품목 가격</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15004517", Title: "서울특별시농수산식품공사_주요 품목 가격", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "서울특별시농수산식품공사_주요 품목 가격_20210113 외부 링크 1", Endpoint: "https://www.garak.co.kr/homepage/M0000258/publicdata/selectPageListPublicData.do?publicDataRealmSn=30"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "garak" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected garak verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected garak URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestGarakAdapterFailsNonOKPublicDataPage(t *testing.T) {
	adapter := NewGarakAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15004517", Title: "서울특별시농수산식품공사_주요 품목 가격", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "서울특별시농수산식품공사_주요 품목 가격_20210113 외부 링크 1", Endpoint: "https://www.garak.co.kr/homepage/M0000258/publicdata/selectPageListPublicData.do?publicDataRealmSn=missing"},
		HTTP:      client,
	})
	if result.Provider != "garak" || result.Status != "failed" || result.Reason != "garak_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected garak failure result: %#v", result)
	}
}

func TestWork24AdapterVerifiesHTMLOpenAPIPageWithoutAuth(t *testing.T) {
	adapter := NewWork24Adapter()
	if !adapter.MatchHost("www.work24.go.kr") {
		t.Fatal("expected work24 adapter to match www.work24.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("work24 adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected work24 capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.work24.go.kr" {
			t.Fatalf("expected www.work24.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("work24 should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>서비스 소개 | OPEN-API | 고객센터</title></head><body>학과정보</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15003549", Title: "한국고용정보원_워크넷_학과정보_학과목록 및 일반학과 상세, 이색학과 상세", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "한국고용정보원_워크넷_학과정보_학과목록 및 일반학과 상세, 이색학과 상세_20210520 외부 링크 1", Endpoint: "https://www.work24.go.kr/cm/e/a/0110/selectOpenApiSvcInfo.do?apiSvcId=&upprApiSvcId=&fullApiSvcId=000000000000000000000000000034"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "work24" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected work24 verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected work24 URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestWork24AdapterFailsNonOKOpenAPIPage(t *testing.T) {
	adapter := NewWork24Adapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15003549", Title: "한국고용정보원_워크넷_학과정보_학과목록 및 일반학과 상세, 이색학과 상세", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "한국고용정보원_워크넷_학과정보_학과목록 및 일반학과 상세, 이색학과 상세_20210520 외부 링크 1", Endpoint: "https://www.work24.go.kr/cm/e/a/0110/selectOpenApiSvcInfo.do?fullApiSvcId=missing"},
		HTTP:      client,
	})
	if result.Provider != "work24" || result.Status != "failed" || result.Reason != "work24_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected work24 failure result: %#v", result)
	}
}

func TestCultureAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewCultureAdapter()
	if !adapter.MatchHost("www.culture.go.kr") {
		t.Fatal("expected culture adapter to match www.culture.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("culture adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected culture capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.culture.go.kr" {
			t.Fatalf("expected www.culture.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("culture should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>문화포털 Open API</title></head><body>도서정보</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15008517", Title: "한국체육산업개발주식회사_올림픽공원 도서정보", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "한국체육산업개발주식회사_올림픽공원 도서정보_20230918 외부 링크 1", Endpoint: "https://www.culture.go.kr/data/openapi/openapiView.do?id=405&keyword=%EB%8F%84%EC%84%9C&searchField=all&gubun=A"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "culture" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected culture verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected culture URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestCultureAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewCultureAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15008517", Title: "한국체육산업개발주식회사_올림픽공원 도서정보", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "한국체육산업개발주식회사_올림픽공원 도서정보_20230918 외부 링크 2", Endpoint: "https://www.culture.go.kr/data/openapi/openapiView.do?id=missing"},
		HTTP:      client,
	})
	if result.Provider != "culture" || result.Status != "failed" || result.Reason != "culture_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected culture failure result: %#v", result)
	}
}

func TestSafeMapAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewSafeMapAdapter()
	if !adapter.MatchHost("www.safemap.go.kr") {
		t.Fatal("expected safemap adapter to match www.safemap.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("safemap adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected safemap capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.safemap.go.kr" {
			t.Fatalf("expected www.safemap.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("safemap should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>safemap</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15101860", Title: "행정안전부_생활안전지도 어린이 아토피"},
		Operation:  datago.Operation{Name: "생활안전지도 어린이 아토피", Endpoint: "https://www.safemap.go.kr/sm/apis.do?service=safemap"},
		Params:     map[string]string{"serviceKey": "secret", "page": "1"},
		HTTP:       client,
		VerifiedAt: "2026-07-02T00:00:00Z",
	})
	if result.Provider != "safemap" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected safemap verification result: %#v", result)
	}
	if result.URL != "https://www.safemap.go.kr/sm/apis.do?page=1&service=safemap" || result.HTTPStatus != 200 {
		t.Fatalf("unexpected safemap URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestSafeMapAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewSafeMapAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>missing</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15150102", Title: "행정안전부_생활안전지도 무더위쉼터(WMS)"},
		Operation: datago.Operation{Name: "생활안전지도 무더위쉼터", Endpoint: "https://www.safemap.go.kr/sm/apis.do?service=missing"},
		HTTP:      client,
	})
	if result.Provider != "safemap" || result.Status != "failed" || result.Reason != "safemap_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected safemap failure result: %#v", result)
	}
}

func TestEShareAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewEShareAdapter()
	if !adapter.MatchHost("www.eshare.go.kr") {
		t.Fatal("expected eshare adapter to match www.eshare.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("eshare adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected eshare capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.eshare.go.kr" {
			t.Fatalf("expected www.eshare.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("eshare should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>eshare</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15077525", Title: "행정안전부_공공개방자원 교육강좌 정보"},
		Operation:  datago.Operation{Name: "행정안전부_공공개방자원 교육강좌 정보", Endpoint: "https://www.eshare.go.kr/UserPortal/Upm/UpmOprnReq/openApiInfo.do?menuNo=200009"},
		Params:     map[string]string{"serviceKey": "secret", "page": "1"},
		HTTP:       client,
		VerifiedAt: "2026-07-02T00:00:00Z",
	})
	if result.Provider != "eshare" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected eshare verification result: %#v", result)
	}
	if result.URL != "https://www.eshare.go.kr/UserPortal/Upm/UpmOprnReq/openApiInfo.do?menuNo=200009&page=1" || result.HTTPStatus != 200 {
		t.Fatalf("unexpected eshare URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestEShareAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewEShareAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>missing</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15077524", Title: "행정안전부_공공개방자원 연구실험장비 목록"},
		Operation: datago.Operation{Name: "행정안전부_공공개방자원 연구실험장비 목록", Endpoint: "https://www.eshare.go.kr/UserPortal/Upm/UpmOprnReq/missing.do"},
		HTTP:      client,
	})
	if result.Provider != "eshare" || result.Status != "failed" || result.Reason != "eshare_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected eshare failure result: %#v", result)
	}
}

func TestLofin365AdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewLofin365Adapter()
	if !adapter.MatchHost("www.lofin365.go.kr") {
		t.Fatal("expected lofin365 adapter to match www.lofin365.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("lofin365 adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected lofin365 capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.lofin365.go.kr" {
			t.Fatalf("expected www.lofin365.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("lofin365 should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>lofin365</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15138865", Title: "행정안전부_지방재정365_지방의회경비 절감률"},
		Operation:  datago.Operation{Name: "행정안전부_지방재정365_지방의회경비 절감률", Endpoint: "https://www.lofin365.go.kr/portal/openapi/service.do?svcNo=15138865"},
		Params:     map[string]string{"serviceKey": "secret", "page": "1"},
		HTTP:       client,
		VerifiedAt: "2026-07-02T00:00:00Z",
	})
	if result.Provider != "lofin365" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected lofin365 verification result: %#v", result)
	}
	if result.URL != "https://www.lofin365.go.kr/portal/openapi/service.do?page=1&svcNo=15138865" || result.HTTPStatus != 200 {
		t.Fatalf("unexpected lofin365 URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestLofin365AdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewLofin365Adapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>missing</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15138864", Title: "행정안전부_지방재정365_지방의회 관련경비"},
		Operation: datago.Operation{Name: "행정안전부_지방재정365_지방의회 관련경비", Endpoint: "https://www.lofin365.go.kr/portal/openapi/missing.do"},
		HTTP:      client,
	})
	if result.Provider != "lofin365" || result.Status != "failed" || result.Reason != "lofin365_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected lofin365 failure result: %#v", result)
	}
}

func TestJusoAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewJusoAdapter()
	if !adapter.MatchHost("www.juso.go.kr") {
		t.Fatal("expected juso adapter to match www.juso.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("juso adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected juso capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.juso.go.kr" {
			t.Fatalf("expected www.juso.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("juso should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>juso</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15057017", Title: "행정안전부_실시간 주소정보 조회(검색API)"},
		Operation:  datago.Operation{Name: "행정안전부_실시간 주소정보 조회(검색API)", Endpoint: "https://www.juso.go.kr/addrlink/addrLinkApi.do"},
		Params:     map[string]string{"serviceKey": "secret", "page": "1"},
		HTTP:       client,
		VerifiedAt: "2026-07-02T00:00:00Z",
	})
	if result.Provider != "juso" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected juso verification result: %#v", result)
	}
	if result.URL != "https://www.juso.go.kr/addrlink/addrLinkApi.do?page=1" || result.HTTPStatus != 200 {
		t.Fatalf("unexpected juso URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestJusoAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewJusoAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>missing</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15056663", Title: "행정안전부_실시간 주소별 좌표정보 조회(검색API)"},
		Operation: datago.Operation{Name: "행정안전부_실시간 주소별 좌표정보 조회(검색API)", Endpoint: "https://www.juso.go.kr/addrlink/missing.do"},
		HTTP:      client,
	})
	if result.Provider != "juso" || result.Status != "failed" || result.Reason != "juso_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected juso failure result: %#v", result)
	}
}

func TestRemainingLinkDetailAdaptersVerifyHTMLLandingPageWithoutAuth(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		host     string
		endpoint string
		adapter  Adapter
	}{
		{
			name:     "anyang",
			provider: "anyang",
			host:     "www.anyang.go.kr",
			endpoint: "https://www.anyang.go.kr/api/example",
			adapter:  NewAnyangAdapter(),
		},
		{
			name:     "chungnam",
			provider: "chungnam",
			host:     "www.chungnam.go.kr",
			endpoint: "https://www.chungnam.go.kr/cnportal/main/contents.do?menuNo=5100181",
			adapter:  NewChungnamAdapter(),
		},
		{
			name:     "chungnam-localfood",
			provider: "chungnam",
			host:     "localfood.chungnam.go.kr",
			endpoint: "https://localfood.chungnam.go.kr/api/example",
			adapter:  NewChungnamAdapter(),
		},
		{
			name:     "chungnam-alldam",
			provider: "chungnam",
			host:     "alldam.chungnam.go.kr",
			endpoint: "https://alldam.chungnam.go.kr/api/example",
			adapter:  NewChungnamAdapter(),
		},
		{
			name:     "chungnam-six",
			provider: "chungnam",
			host:     "www.xn--6-6v7en42by2es7i6jc.com",
			endpoint: "https://www.xn--6-6v7en42by2es7i6jc.com/api/example",
			adapter:  NewChungnamAdapter(),
		},
		{
			name:     "dgfca",
			provider: "dgfca",
			host:     "dgfca.or.kr",
			endpoint: "https://dgfca.or.kr/api/example",
			adapter:  NewDGFCAAdapter(),
		},
		{
			name:     "foodsafetykorea",
			provider: "foodsafetykorea",
			host:     "www.foodsafetykorea.go.kr",
			endpoint: "https://www.foodsafetykorea.go.kr/api/example",
			adapter:  NewFoodSafetyKoreaAdapter(),
		},
		{
			name:     "gwangmyeong",
			provider: "gwangmyeong",
			host:     "data.gm.go.kr",
			endpoint: "https://data.gm.go.kr/api/example",
			adapter:  NewGwangmyeongAdapter(),
		},
		{
			name:     "gwangjin",
			provider: "gwangjin",
			host:     "www.gwangjin.go.kr",
			endpoint: "https://www.gwangjin.go.kr/api/example",
			adapter:  NewGwangjinAdapter(),
		},
		{
			name:     "ins24",
			provider: "ins24",
			host:     "www.ins24.go.kr",
			endpoint: "https://www.ins24.go.kr/api/example",
			adapter:  NewIns24Adapter(),
		},
		{
			name:     "ip-navi",
			provider: "ip-navi",
			host:     "api.ip-navi.or.kr",
			endpoint: "https://api.ip-navi.or.kr/api/example",
			adapter:  NewIPNaviAdapter(),
		},
		{
			name:     "jeonnam-redtable",
			provider: "jeonnam-redtable",
			host:     "jeonnam.openapi.redtable.global",
			endpoint: "https://jeonnam.openapi.redtable.global/api/example",
			adapter:  NewJeonnamRedtableAdapter(),
		},
		{
			name:     "daegu",
			provider: "daegu",
			host:     "www.daegu.go.kr",
			endpoint: "https://www.daegu.go.kr/api/example",
			adapter:  NewDaeguAdapter(),
		},
		{
			name:     "jejudatahub",
			provider: "jejudatahub",
			host:     "www.jejudatahub.net",
			endpoint: "https://www.jejudatahub.net/api/example",
			adapter:  NewJejuDataHubAdapter(),
		},
		{
			name:     "jejuits",
			provider: "jejuits",
			host:     "www.jejuits.go.kr",
			endpoint: "https://www.jejuits.go.kr/api/example",
			adapter:  NewJejuITSAdapter(),
		},
		{
			name:     "jongno",
			provider: "jongno",
			host:     "openapi.jongno.go.kr",
			endpoint: "https://openapi.jongno.go.kr/api/example",
			adapter:  NewJongnoAdapter(),
		},
		{
			name:     "kma-apihub",
			provider: "kma-apihub",
			host:     "apihub.kma.go.kr",
			endpoint: "https://apihub.kma.go.kr/api/typ01/url/kma_sfctm3.php",
			adapter:  NewKMAAPIHubAdapter(),
		},
		{
			name:     "kric",
			provider: "kric",
			host:     "data.kric.go.kr",
			endpoint: "https://data.kric.go.kr/api/example",
			adapter:  NewKRICAdapter(),
		},
		{
			name:     "kipris-plus",
			provider: "kipris-plus",
			host:     "plus.kipris.or.kr",
			endpoint: "https://plus.kipris.or.kr/openapi/rest/patUtiModInfoSearchSevice",
			adapter:  NewKIPRISPlusAdapter(),
		},
		{
			name:     "khoa",
			provider: "khoa",
			host:     "www.khoa.go.kr",
			endpoint: "https://www.khoa.go.kr/api/example",
			adapter:  NewKHOAAdapter(),
		},
		{
			name:     "koreapost",
			provider: "koreapost",
			host:     "koreapost.go.kr",
			endpoint: "https://koreapost.go.kr/api/example",
			adapter:  NewKoreaPostAdapter(),
		},
		{
			name:     "kosmes",
			provider: "kosmes",
			host:     "kosmes.or.kr",
			endpoint: "https://kosmes.or.kr/api/example",
			adapter:  NewKOSMESAdapter(),
		},
		{
			name:     "childcare-info",
			provider: "childcare-info",
			host:     "info.childcare.go.kr",
			endpoint: "https://info.childcare.go.kr/api/example",
			adapter:  NewChildcareInfoAdapter(),
		},
		{
			name:     "chungbuk-tour",
			provider: "chungbuk-tour",
			host:     "tour.chungbuk.go.kr",
			endpoint: "https://tour.chungbuk.go.kr/api/example",
			adapter:  NewChungbukTourAdapter(),
		},
		{
			name:     "ecvam",
			provider: "ecvam",
			host:     "ecvam.neins.go.kr",
			endpoint: "https://ecvam.neins.go.kr/api/example",
			adapter:  NewECVAMAdapter(),
		},
		{
			name:     "recycling-info",
			provider: "recycling-info",
			host:     "www.recycling-info.or.kr",
			endpoint: "https://www.recycling-info.or.kr/api/example",
			adapter:  NewRecyclingInfoAdapter(),
		},
		{
			name:     "iwest",
			provider: "iwest",
			host:     "www.iwest.co.kr",
			endpoint: "https://www.iwest.co.kr/api/example",
			adapter:  NewIWestAdapter(),
		},
		{
			name:     "mnd-open-data",
			provider: "mnd-open-data",
			host:     "opendata.mnd.go.kr",
			endpoint: "https://opendata.mnd.go.kr/api/example",
			adapter:  NewMNDOpenDataAdapter(),
		},
		{
			name:     "mpva-egonghun",
			provider: "mpva-egonghun",
			host:     "e-gonghun.mpva.go.kr",
			endpoint: "https://e-gonghun.mpva.go.kr/api/example",
			adapter:  NewMPVAEgonghunAdapter(),
		},
		{
			name:     "nihc",
			provider: "nihc",
			host:     "www.nihc.go.kr",
			endpoint: "https://www.nihc.go.kr/api/example",
			adapter:  NewNIHCAdapter(),
		},
		{
			name:     "nrich",
			provider: "nrich",
			host:     "portal.nrich.go.kr",
			endpoint: "https://portal.nrich.go.kr/api/example",
			adapter:  NewNRichAdapter(),
		},
		{
			name:     "tashu",
			provider: "tashu",
			host:     "bike.tashu.or.kr",
			endpoint: "https://bike.tashu.or.kr/api/example",
			adapter:  NewTashuAdapter(),
		},
		{
			name:     "nosc",
			provider: "nosc",
			host:     "nosc.go.kr",
			endpoint: "https://nosc.go.kr/api/example",
			adapter:  NewNOSCAdapter(),
		},
		{
			name:     "nier-nesc",
			provider: "nier-nesc",
			host:     "nesc.nier.go.kr",
			endpoint: "https://nesc.nier.go.kr/api/example",
			adapter:  NewNierNescAdapter(),
		},
		{
			name:     "nie-ecobank",
			provider: "nie-ecobank",
			host:     "www.nie-ecobank.kr",
			endpoint: "https://www.nie-ecobank.kr/api/example",
			adapter:  NewNIEEcobankAdapter(),
		},
		{
			name:     "unipass",
			provider: "unipass",
			host:     "unipass.customs.go.kr",
			endpoint: "https://unipass.customs.go.kr/api/example",
			adapter:  NewUniPassAdapter(),
		},
		{
			name:     "youthcenter",
			provider: "youthcenter",
			host:     "www.youthcenter.go.kr",
			endpoint: "https://www.youthcenter.go.kr/api/example",
			adapter:  NewYouthCenterAdapter(),
		},
		{
			name:     "ulsan-www",
			provider: "ulsan-www",
			host:     "www.ulsan.go.kr",
			endpoint: "https://www.ulsan.go.kr/api/example",
			adapter:  NewUlsanWWWAdapter(),
		},
		{
			name:     "yuseong",
			provider: "yuseong",
			host:     "www.yuseong.go.kr",
			endpoint: "https://www.yuseong.go.kr/api/example",
			adapter:  NewYuseongAdapter(),
		},
		{
			name:     "daejeon",
			provider: "daejeon",
			host:     "bigdata.daejeon.go.kr",
			endpoint: "https://bigdata.daejeon.go.kr/api/example",
			adapter:  NewDaejeonAdapter(),
		},
		{
			name:     "daejeon-gis",
			provider: "daejeon",
			host:     "gis.daejeon.go.kr",
			endpoint: "https://gis.daejeon.go.kr/api/example",
			adapter:  NewDaejeonAdapter(),
		},
		{
			name:     "gims",
			provider: "gims",
			host:     "www.gims.go.kr",
			endpoint: "https://www.gims.go.kr/openapi/service/rest/ObservatoryInfoService/getObsrvInfoList",
			adapter:  NewGIMSAdapter(),
		},
		{
			name:     "mafra-legacy",
			provider: "mafra-legacy",
			host:     "211.237.50.150:7080",
			endpoint: "http://211.237.50.150:7080/openapi/sample/xml/Grid_20151230000000000339_1/1/5",
			adapter:  NewMAFRALegacyAdapter(),
		},
		{
			name:     "much",
			provider: "much",
			host:     "www.much.go.kr",
			endpoint: "https://www.much.go.kr/cop/bbs/AnyEmployInfoXml.do",
			adapter:  NewMUCHAdapter(),
		},
		{
			name:     "nabic",
			provider: "nabic",
			host:     "nabic.rda.go.kr",
			endpoint: "https://nabic.rda.go.kr/api/example",
			adapter:  NewNABICAdapter(),
		},
		{
			name:     "naa",
			provider: "naa",
			host:     "www.naa.go.kr",
			endpoint: "http://www.naa.go.kr/site/main/content/public_data_open",
			adapter:  NewNAAAdapter(),
		},
		{
			name:     "psis",
			provider: "psis",
			host:     "psis.rda.go.kr",
			endpoint: "https://psis.rda.go.kr/api/example",
			adapter:  NewPSISAdapter(),
		},
		{
			name:     "seogu",
			provider: "seogu",
			host:     "seogu.go.kr",
			endpoint: "https://seogu.go.kr/api/example",
			adapter:  NewSeoguAdapter(),
		},
		{
			name:     "seogwipo",
			provider: "seogwipo",
			host:     "www.seogwipo.go.kr",
			endpoint: "https://www.seogwipo.go.kr/api/example",
			adapter:  NewSeogwipoAdapter(),
		},
		{
			name:     "seogwipo-bare",
			provider: "seogwipo",
			host:     "seogwipo.go.kr",
			endpoint: "https://seogwipo.go.kr/api/example",
			adapter:  NewSeogwipoAdapter(),
		},
		{
			name:     "seoul-map",
			provider: "seoul-map",
			host:     "map.seoul.go.kr",
			endpoint: "https://map.seoul.go.kr/api/example",
			adapter:  NewSeoulMapAdapter(),
		},
		{
			name:     "seoul-tdata",
			provider: "seoul-tdata",
			host:     "t-data.seoul.go.kr",
			endpoint: "https://t-data.seoul.go.kr/api/example",
			adapter:  NewSeoulTDataAdapter(),
		},
		{
			name:     "wamis",
			provider: "wamis",
			host:     "www.wamis.go.kr",
			endpoint: "https://www.wamis.go.kr/api/example",
			adapter:  NewWAMISAdapter(),
		},
		{
			name:     "wamis-port",
			provider: "wamis",
			host:     "www.wamis.go.kr:8080",
			endpoint: "http://www.wamis.go.kr:8080/api/example",
			adapter:  NewWAMISAdapter(),
		},
		{
			name:     "vworld",
			provider: "vworld",
			host:     "www.vworld.kr",
			endpoint: "https://www.vworld.kr/api/example",
			adapter:  NewVWorldAdapter(),
		},
		{
			name:     "smartfarm-korea",
			provider: "smartfarm-korea",
			host:     "www.smartfarmkorea.net",
			endpoint: "https://www.smartfarmkorea.net/openApi/openApiList.do?menuId=M1104030101",
			adapter:  NewSmartFarmKoreaAdapter(),
		},
		{
			name:     "smartfarm-korea-bare",
			provider: "smartfarm-korea",
			host:     "smartfarmkorea.net",
			endpoint: "https://smartfarmkorea.net/openApi/openApiList.do?menuId=M1104030101",
			adapter:  NewSmartFarmKoreaAdapter(),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !tc.adapter.MatchHost(tc.host) {
				t.Fatalf("expected %s adapter to match %s", tc.name, tc.host)
			}
			if tc.adapter.MatchHost("apis.data.go.kr") {
				t.Fatalf("%s adapter should not match data.go.kr gateway", tc.name)
			}
			if strings.Join(adapterCapabilities(tc.adapter), ",") != "verification" {
				t.Fatalf("unexpected %s capabilities: %#v", tc.name, adapterCapabilities(tc.adapter))
			}
			client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != tc.host {
					t.Fatalf("expected %s host, got %s", tc.host, req.URL.Host)
				}
				if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
					t.Fatalf("%s should not synthesize or leak serviceKey: %s", tc.name, req.URL.RawQuery)
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>ok</body></html>`)),
				}, nil
			})
			result := tc.adapter.Verify(context.Background(), VerificationRequest{
				Spec:       datago.Spec{ID: "15100000", Title: "행정안전부_잔여 링크상세 API"},
				Operation:  datago.Operation{Name: "행정안전부_잔여 링크상세 API", Endpoint: tc.endpoint},
				Params:     map[string]string{"serviceKey": "secret", "page": "1"},
				HTTP:       client,
				VerifiedAt: "2026-07-02T00:00:00Z",
			})
			if result.Provider != tc.provider || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
				t.Fatalf("unexpected %s verification result: %#v", tc.name, result)
			}
			wantURL := tc.endpoint + "?page=1"
			if strings.Contains(tc.endpoint, "?") {
				wantURL = tc.endpoint + "&page=1"
			}
			if result.URL != wantURL || result.HTTPStatus != 200 {
				t.Fatalf("unexpected %s URL/status: url=%s status=%d", tc.name, result.URL, result.HTTPStatus)
			}
		})
	}
}

func TestOpenDARTAdapterVerifiesWithCredential(t *testing.T) {
	adapter := NewOpenDARTAdapter()
	if !adapter.MatchHost("opendart.fss.or.kr") {
		t.Fatal("expected opendart adapter to match host")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("opendart adapter should not match data.go.kr gateway")
	}
	missingAuth := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15060620", Title: "금융감독원_정기보고서 재무정보_단일회사주요계정"},
		Operation: datago.Operation{Name: "단일회사주요계정", Endpoint: "https://opendart.fss.or.kr/api/fnlttSinglAcnt.json"},
		Params:    map[string]string{"corp_code": "00126380", "bsns_year": "2024", "reprt_code": "11011"},
	})
	if missingAuth.Status != "skipped" || missingAuth.Reason != "missing_auth" {
		t.Fatalf("unexpected missing auth result: %#v", missingAuth)
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "opendart.fss.or.kr" {
			t.Fatalf("unexpected host: %s", req.URL.Host)
		}
		if req.URL.Query().Get("crtfc_key") != "secret" {
			t.Fatalf("expected crtfc_key in request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("corp_code") != "00126380" || req.URL.Query().Get("bsns_year") != "2024" || req.URL.Query().Get("reprt_code") != "11011" {
			t.Fatalf("unexpected OpenDART query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"status":"000","message":"정상","list":[{"account_nm":"자산총계"}]}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15060620", Title: "금융감독원_정기보고서 재무정보_단일회사주요계정"},
		Operation:  datago.Operation{Name: "단일회사주요계정", Endpoint: "https://opendart.fss.or.kr/api/fnlttSinglAcnt.json"},
		Params:     map[string]string{"corp_code": "00126380", "bsns_year": "2024", "reprt_code": "11011", "crtfc_key": "must-not-leak"},
		Credential: Credential{Name: "OPENDART_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "opendart" || result.Status != "verified" || result.HTTPStatus != 200 || result.BodyShape != "json" {
		t.Fatalf("unexpected OpenDART verification result: %#v", result)
	}
	if strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "crtfc_key=REDACTED") {
		t.Fatalf("OpenDART URL was not redacted: %s", result.URL)
	}
	if result.Params["crtfc_key"] != "" || result.Params["corp_code"] != "00126380" {
		t.Fatalf("unexpected public params: %#v", result.Params)
	}
}

func TestRemainingLinkDetailAdaptersFailNonOKLandingPage(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		endpoint string
		adapter  Adapter
	}{
		{name: "anyang", provider: "anyang", endpoint: "https://www.anyang.go.kr/api/missing", adapter: NewAnyangAdapter()},
		{name: "dgfca", provider: "dgfca", endpoint: "https://dgfca.or.kr/api/missing", adapter: NewDGFCAAdapter()},
		{name: "chungnam", provider: "chungnam", endpoint: "https://www.chungnam.go.kr/api/missing", adapter: NewChungnamAdapter()},
		{name: "childcare-info", provider: "childcare-info", endpoint: "https://info.childcare.go.kr/api/missing", adapter: NewChildcareInfoAdapter()},
		{name: "chungbuk-tour", provider: "chungbuk-tour", endpoint: "https://tour.chungbuk.go.kr/api/missing", adapter: NewChungbukTourAdapter()},
		{name: "foodsafetykorea", provider: "foodsafetykorea", endpoint: "https://www.foodsafetykorea.go.kr/api/missing", adapter: NewFoodSafetyKoreaAdapter()},
		{name: "gwangjin", provider: "gwangjin", endpoint: "https://www.gwangjin.go.kr/api/missing", adapter: NewGwangjinAdapter()},
		{name: "gwangmyeong", provider: "gwangmyeong", endpoint: "https://data.gm.go.kr/api/missing", adapter: NewGwangmyeongAdapter()},
		{name: "ins24", provider: "ins24", endpoint: "https://www.ins24.go.kr/api/missing", adapter: NewIns24Adapter()},
		{name: "ip-navi", provider: "ip-navi", endpoint: "https://api.ip-navi.or.kr/api/missing", adapter: NewIPNaviAdapter()},
		{name: "iwest", provider: "iwest", endpoint: "https://www.iwest.co.kr/api/missing", adapter: NewIWestAdapter()},
		{name: "jejudatahub", provider: "jejudatahub", endpoint: "https://www.jejudatahub.net/api/missing", adapter: NewJejuDataHubAdapter()},
		{name: "jejuits", provider: "jejuits", endpoint: "https://www.jejuits.go.kr/api/missing", adapter: NewJejuITSAdapter()},
		{name: "jeonnam-redtable", provider: "jeonnam-redtable", endpoint: "https://jeonnam.openapi.redtable.global/api/missing", adapter: NewJeonnamRedtableAdapter()},
		{name: "jongno", provider: "jongno", endpoint: "https://openapi.jongno.go.kr/api/missing", adapter: NewJongnoAdapter()},
		{name: "khoa", provider: "khoa", endpoint: "https://www.khoa.go.kr/api/missing", adapter: NewKHOAAdapter()},
		{name: "kma-apihub", provider: "kma-apihub", endpoint: "https://apihub.kma.go.kr/api/missing", adapter: NewKMAAPIHubAdapter()},
		{name: "kipris-plus", provider: "kipris-plus", endpoint: "https://plus.kipris.or.kr/api/missing", adapter: NewKIPRISPlusAdapter()},
		{name: "koreapost", provider: "koreapost", endpoint: "https://koreapost.go.kr/api/missing", adapter: NewKoreaPostAdapter()},
		{name: "kosmes", provider: "kosmes", endpoint: "https://kosmes.or.kr/api/missing", adapter: NewKOSMESAdapter()},
		{name: "kric", provider: "kric", endpoint: "https://data.kric.go.kr/api/missing", adapter: NewKRICAdapter()},
		{name: "ecvam", provider: "ecvam", endpoint: "https://ecvam.neins.go.kr/api/missing", adapter: NewECVAMAdapter()},
		{name: "daegu", provider: "daegu", endpoint: "https://www.daegu.go.kr/api/missing", adapter: NewDaeguAdapter()},
		{name: "daejeon", provider: "daejeon", endpoint: "https://bigdata.daejeon.go.kr/api/missing", adapter: NewDaejeonAdapter()},
		{name: "daejeon-gis", provider: "daejeon", endpoint: "https://gis.daejeon.go.kr/api/missing", adapter: NewDaejeonAdapter()},
		{name: "gims", provider: "gims", endpoint: "https://www.gims.go.kr/api/missing", adapter: NewGIMSAdapter()},
		{name: "mafra-legacy", provider: "mafra-legacy", endpoint: "http://211.237.50.150:7080/openapi/sample/xml/missing/1/5", adapter: NewMAFRALegacyAdapter()},
		{name: "mnd-open-data", provider: "mnd-open-data", endpoint: "https://opendata.mnd.go.kr/api/missing", adapter: NewMNDOpenDataAdapter()},
		{name: "mpva-egonghun", provider: "mpva-egonghun", endpoint: "https://e-gonghun.mpva.go.kr/api/missing", adapter: NewMPVAEgonghunAdapter()},
		{name: "nosc", provider: "nosc", endpoint: "https://nosc.go.kr/api/missing", adapter: NewNOSCAdapter()},
		{name: "nier-nesc", provider: "nier-nesc", endpoint: "https://nesc.nier.go.kr/api/missing", adapter: NewNierNescAdapter()},
		{name: "nie-ecobank", provider: "nie-ecobank", endpoint: "https://www.nie-ecobank.kr/api/missing", adapter: NewNIEEcobankAdapter()},
		{name: "nihc", provider: "nihc", endpoint: "https://www.nihc.go.kr/api/missing", adapter: NewNIHCAdapter()},
		{name: "nrich", provider: "nrich", endpoint: "https://portal.nrich.go.kr/api/missing", adapter: NewNRichAdapter()},
		{name: "unipass", provider: "unipass", endpoint: "https://unipass.customs.go.kr/api/missing", adapter: NewUniPassAdapter()},
		{name: "much", provider: "much", endpoint: "https://www.much.go.kr/cop/bbs/missing.do", adapter: NewMUCHAdapter()},
		{name: "nabic", provider: "nabic", endpoint: "https://nabic.rda.go.kr/api/missing", adapter: NewNABICAdapter()},
		{name: "naa", provider: "naa", endpoint: "http://www.naa.go.kr/site/main/content/missing", adapter: NewNAAAdapter()},
		{name: "psis", provider: "psis", endpoint: "https://psis.rda.go.kr/api/missing", adapter: NewPSISAdapter()},
		{name: "recycling-info", provider: "recycling-info", endpoint: "https://www.recycling-info.or.kr/api/missing", adapter: NewRecyclingInfoAdapter()},
		{name: "seogu", provider: "seogu", endpoint: "https://seogu.go.kr/api/missing", adapter: NewSeoguAdapter()},
		{name: "seogwipo", provider: "seogwipo", endpoint: "https://www.seogwipo.go.kr/api/missing", adapter: NewSeogwipoAdapter()},
		{name: "seogwipo-bare", provider: "seogwipo", endpoint: "https://seogwipo.go.kr/api/missing", adapter: NewSeogwipoAdapter()},
		{name: "seoul-map", provider: "seoul-map", endpoint: "https://map.seoul.go.kr/api/missing", adapter: NewSeoulMapAdapter()},
		{name: "seoul-tdata", provider: "seoul-tdata", endpoint: "https://t-data.seoul.go.kr/api/missing", adapter: NewSeoulTDataAdapter()},
		{name: "smartfarm-korea", provider: "smartfarm-korea", endpoint: "https://www.smartfarmkorea.net/openApi/missing.do", adapter: NewSmartFarmKoreaAdapter()},
		{name: "tashu", provider: "tashu", endpoint: "https://bike.tashu.or.kr/api/missing", adapter: NewTashuAdapter()},
		{name: "wamis", provider: "wamis", endpoint: "https://www.wamis.go.kr/api/missing", adapter: NewWAMISAdapter()},
		{name: "vworld", provider: "vworld", endpoint: "https://www.vworld.kr/api/missing", adapter: NewVWorldAdapter()},
		{name: "youthcenter", provider: "youthcenter", endpoint: "https://www.youthcenter.go.kr/api/missing", adapter: NewYouthCenterAdapter()},
		{name: "ulsan-www", provider: "ulsan-www", endpoint: "https://www.ulsan.go.kr/api/missing", adapter: NewUlsanWWWAdapter()},
		{name: "yuseong", provider: "yuseong", endpoint: "https://www.yuseong.go.kr/api/missing", adapter: NewYuseongAdapter()},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 404,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader(`<html><body>missing</body></html>`)),
				}, nil
			})
			result := tc.adapter.Verify(context.Background(), VerificationRequest{
				Spec:      datago.Spec{ID: "15100000", Title: "행정안전부_잔여 링크상세 API"},
				Operation: datago.Operation{Name: "행정안전부_잔여 링크상세 API", Endpoint: tc.endpoint},
				HTTP:      client,
			})
			if result.Provider != tc.provider || result.Status != "failed" || result.Reason != tc.provider+"_http_404" || result.BodyShape != "html" {
				t.Fatalf("unexpected %s failure result: %#v", tc.name, result)
			}
		})
	}
}

func TestHappySDAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewHappySDAdapter()
	if !adapter.MatchHost("www.happysd.or.kr") {
		t.Fatal("expected happysd adapter to match www.happysd.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("happysd adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected happysd capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.happysd.or.kr" {
			t.Fatalf("expected www.happysd.or.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("happysd should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>성동구도시관리공단</title></head><body>체육시설 강좌정보</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15007444", Title: "서울특별시성동구도시관리공단_체육시설 강좌정보 조회", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "서울특별시성동구도시관리공단_체육시설 강좌정보 조회_20220803", Endpoint: "https://www.happysd.or.kr/c_76/cnt/m_109/view.do"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "happysd" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected happysd verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected happysd URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestHappySDAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewHappySDAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15007467", Title: "서울특별시성동구도시관리공단_체육시설 대관정보 조회", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "서울특별시성동구도시관리공단_체육시설 대관정보 조회_20220804", Endpoint: "https://www.happysd.or.kr/c_76/cnt/m_109/view.do"},
		HTTP:      client,
	})
	if result.Provider != "happysd" || result.Status != "failed" || result.Reason != "happysd_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected happysd failure result: %#v", result)
	}
}

func TestNCPMSAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewNCPMSAdapter()
	if !adapter.MatchHost("ncpms.rda.go.kr") {
		t.Fatal("expected ncpms adapter to match ncpms.rda.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("ncpms adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected ncpms capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "ncpms.rda.go.kr" {
			t.Fatalf("expected ncpms.rda.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("ncpms should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>국가농작물병해충도감정보</title></head><body>Open API 정보</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15002034", Title: "농촌진흥청_국가농작물병해충도감정보", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "농촌진흥청_국가농작물병해충도감정보_20240222 외부 링크 1", Endpoint: "http://ncpms.rda.go.kr/npms/OpenApiInfo.np"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "ncpms" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected ncpms verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected ncpms URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestNCPMSAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewNCPMSAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15002034", Title: "농촌진흥청_국가농작물병해충도감정보", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "농촌진흥청_국가농작물병해충도감정보_20240222 외부 링크 1", Endpoint: "http://ncpms.rda.go.kr/npms/OpenApiInfo.np"},
		HTTP:      client,
	})
	if result.Provider != "ncpms" || result.Status != "failed" || result.Reason != "ncpms_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected ncpms failure result: %#v", result)
	}
}

func TestSafetyDataAdapterRequiresAuthAndClassifiesServiceKeyError(t *testing.T) {
	adapter := NewSafetyDataAdapter()
	if !adapter.MatchHost("www.safetydata.go.kr") {
		t.Fatal("expected safetydata adapter to match www.safetydata.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("safetydata adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "call,verification" {
		t.Fatalf("unexpected safetydata capabilities: %#v", adapterCapabilities(adapter))
	}
	operation := datago.Operation{
		Name:     "국외검사기관인정현황",
		Endpoint: "https://www.safetydata.go.kr/V2/api/DSSP-IF-20141",
	}
	missingAuth := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15154049", Title: "행정안전부_국외검사기관인정현황", Provider: "data.go.kr"},
		Operation: operation,
	})
	if missingAuth.Status != "skipped" || missingAuth.Reason != "missing_auth" {
		t.Fatalf("unexpected missing auth result: %#v", missingAuth)
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.safetydata.go.kr" {
			t.Fatalf("expected www.safetydata.go.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey to be sent, got %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("returnType") != "json" || req.URL.Query().Get("numOfRows") != "1" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("expected safe defaults, got %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"header":{"resultMsg":"SERVICE KEY IS NOT REGISTERED ERROR","resultCode":"30","errorMsg":"등록되지 않은 서비스키"},"body":null}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15154049", Title: "행정안전부_국외검사기관인정현황", Provider: "data.go.kr"},
		Operation:     operation,
		MissingParams: []string{"returnType", "numOfRows", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
		VerifiedAt:    "2026-07-03T00:00:00Z",
	})
	if result.Provider != "safetydata" || result.Status != "failed" || result.Reason != "safetydata_service_key_not_registered" || result.BodyShape != "json_envelope" {
		t.Fatalf("unexpected safetydata verification result: %#v", result)
	}
	if strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted safetydata URL, got %s", result.URL)
	}
}

func TestSafetyDataAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewSafetyDataAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey to be sent, got %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"header":{"resultMsg":"NORMAL SERVICE","resultCode":"00"},"body":{"items":[{"A":"B"}]}}`)),
		}, nil
	})
	response, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "15154052", Title: "행정안전부_국외전염병발생현황", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "국외전염병발생현황", Endpoint: "https://www.safetydata.go.kr/V2/api/DSSP-IF-20564"},
		MissingParams: []string{"returnType", "numOfRows", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Provider != "safetydata" || response.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected safetydata call response: %#v", response)
	}
	if strings.Contains(response.URL, "secret") || !strings.Contains(response.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted safetydata URL, got %s", response.URL)
	}
}

func TestI815AdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewI815Adapter()
	if !adapter.MatchHost("search.i815.or.kr") {
		t.Fatal("expected i815 adapter to match search.i815.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("i815 adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected i815 capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "search.i815.or.kr" {
			t.Fatalf("expected search.i815.or.kr host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" || strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("i815 should not synthesize or leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>한국독립운동사 소장자료 정보 DB</title></head><body>openApi</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15006273", Title: "독립기념관_한국독립운동사 소장자료 정보 DB", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "독립기념관_한국독립운동사 소장자료 정보 DB_20200703", Endpoint: "https://search.i815.or.kr/openApi.do"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "i815" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected i815 verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected i815 URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestI815AdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewI815Adapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15006273", Title: "독립기념관_한국독립운동사 소장자료 정보 DB", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "독립기념관_한국독립운동사 소장자료 정보 DB_20200703", Endpoint: "https://search.i815.or.kr/openApi.do"},
		HTTP:      client,
	})
	if result.Provider != "i815" || result.Status != "failed" || result.Reason != "i815_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected i815 failure result: %#v", result)
	}
}

func TestOpenAssemblyAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewOpenAssemblyAdapter()
	if !adapter.MatchHost("open.assembly.go.kr") {
		t.Fatal("expected open-assembly adapter to match open.assembly.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("open-assembly adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected open-assembly capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "open.assembly.go.kr" {
			t.Fatalf("expected open.assembly.go.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("open-assembly should not leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>열린국회정보</title></head><body>국회 국회사무처</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15152558", Title: "국회 국회사무처_법률안 제안이유 및 주요내용", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "법률안 제안이유 및 주요내용", Endpoint: "https://open.assembly.go.kr/portal/data/service/selectAPIServicePage.do/OJ24"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "open-assembly" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected open-assembly verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected open-assembly URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestOpenAssemblyAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewOpenAssemblyAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15152558", Title: "국회 국회사무처_법률안 제안이유 및 주요내용", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "법률안 제안이유 및 주요내용", Endpoint: "https://open.assembly.go.kr/portal/data/service/selectAPIServicePage.do/OJ24"},
		HTTP:      client,
	})
	if result.Provider != "open-assembly" || result.Status != "failed" || result.Reason != "open-assembly_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected open-assembly failure result: %#v", result)
	}
}

func TestSexOffenderAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewSexOffenderAdapter()
	if !adapter.MatchHost("api.sexoffender.go.kr") {
		t.Fatal("expected sexoffender adapter to match api.sexoffender.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("sexoffender adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected sexoffender capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.sexoffender.go.kr" {
			t.Fatalf("expected api.sexoffender.go.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("sexoffender should not leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>성범죄자 알림e</title></head><body>지역별 통계</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "3072018", Title: "성평등가족부_성범죄자 지역별 통계", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "성범죄자 지역별 통계", Endpoint: "https://api.sexoffender.go.kr/openapi/SOCitysStats/"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "sexoffender" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected sexoffender verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected sexoffender URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestSexOffenderAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewSexOffenderAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "3072018", Title: "성평등가족부_성범죄자 지역별 통계", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "성범죄자 지역별 통계", Endpoint: "https://api.sexoffender.go.kr/openapi/SOCitysStats/"},
		HTTP:      client,
	})
	if result.Provider != "sexoffender" || result.Status != "failed" || result.Reason != "sexoffender_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected sexoffender failure result: %#v", result)
	}
}

func TestFTCLinkDetailAdaptersVerifyHTMLLandingPageWithoutAuth(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		host     string
		endpoint string
		adapter  Adapter
	}{
		{name: "consumer", provider: "consumer", host: "www.consumer.go.kr", endpoint: "https://www.consumer.go.kr/openapi/example", adapter: NewConsumerAdapter()},
		{name: "fairdata", provider: "fairdata", host: "www.fairdata.go.kr", endpoint: "https://www.fairdata.go.kr/openapi/example", adapter: NewFairDataAdapter()},
		{name: "franchise-ftc", provider: "franchise-ftc", host: "franchise.ftc.go.kr", endpoint: "https://franchise.ftc.go.kr/openapi/example", adapter: NewFranchiseFTCAdapter()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.adapter.MatchHost(tc.host) {
				t.Fatalf("expected %s adapter to match %s", tc.name, tc.host)
			}
			if tc.adapter.MatchHost("apis.data.go.kr") {
				t.Fatalf("%s adapter should not match data.go.kr gateway", tc.name)
			}
			if strings.Join(adapterCapabilities(tc.adapter), ",") != "verification" {
				t.Fatalf("unexpected %s capabilities: %#v", tc.name, adapterCapabilities(tc.adapter))
			}
			client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != tc.host {
					t.Fatalf("expected %s host, got %s", tc.host, req.URL.Host)
				}
				if strings.Contains(req.URL.RawQuery, "secret") {
					t.Fatalf("%s should not leak serviceKey: %s", tc.name, req.URL.RawQuery)
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
					Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>공정거래위원회</title></head><body>open api</body></html>`)),
				}, nil
			})
			result := tc.adapter.Verify(context.Background(), VerificationRequest{
				Spec:       datago.Spec{ID: "15144425", Title: "공정거래위원회 API", Provider: "data.go.kr"},
				Operation:  datago.Operation{Name: "공정거래위원회 API", Endpoint: tc.endpoint},
				Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
				HTTP:       client,
				VerifiedAt: "2026-07-04T00:00:00Z",
			})
			if result.Provider != tc.provider || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
				t.Fatalf("unexpected %s verification result: %#v", tc.name, result)
			}
			if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
				t.Fatalf("unexpected %s URL/status: url=%s status=%d", tc.name, result.URL, result.HTTPStatus)
			}
		})
	}
}

func TestFTCLinkDetailAdaptersFailNonOKLandingPage(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		endpoint string
		adapter  Adapter
	}{
		{name: "consumer", provider: "consumer", endpoint: "https://www.consumer.go.kr/openapi/example", adapter: NewConsumerAdapter()},
		{name: "fairdata", provider: "fairdata", endpoint: "https://www.fairdata.go.kr/openapi/example", adapter: NewFairDataAdapter()},
		{name: "franchise-ftc", provider: "franchise-ftc", endpoint: "https://franchise.ftc.go.kr/openapi/example", adapter: NewFranchiseFTCAdapter()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 404,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
				}, nil
			})
			result := tc.adapter.Verify(context.Background(), VerificationRequest{
				Spec:      datago.Spec{ID: "15144425", Title: "공정거래위원회 API", Provider: "data.go.kr"},
				Operation: datago.Operation{Name: "공정거래위원회 API", Endpoint: tc.endpoint},
				HTTP:      client,
			})
			if result.Provider != tc.provider || result.Status != "failed" || result.Reason != tc.provider+"_http_404" || result.BodyShape != "html" {
				t.Fatalf("unexpected %s failure result: %#v", tc.name, result)
			}
		})
	}
}

func TestWorldJobAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewWorldJobAdapter()
	if !adapter.MatchHost("www.worldjob.or.kr") {
		t.Fatal("expected worldjob adapter to match www.worldjob.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("worldjob adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected worldjob capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.worldjob.or.kr" {
			t.Fatalf("expected www.worldjob.or.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("worldjob should not leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>월드잡플러스</title></head><body>open api</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "3045136", Title: "[산업인력] 해외취업 통계정보", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "[산업인력] 해외취업 통계정보", Endpoint: "https://www.worldjob.or.kr/openapi/example"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-04T00:00:00Z",
	})
	if result.Provider != "worldjob" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected worldjob verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected worldjob URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestWorldJobAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewWorldJobAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "3045136", Title: "[산업인력] 해외취업 통계정보", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "[산업인력] 해외취업 통계정보", Endpoint: "https://www.worldjob.or.kr/openapi/example"},
		HTTP:      client,
	})
	if result.Provider != "worldjob" || result.Status != "failed" || result.Reason != "worldjob_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected worldjob failure result: %#v", result)
	}
}

func TestCancerAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewCancerAdapter()
	if !adapter.MatchHost("cancer.go.kr") {
		t.Fatal("expected cancer adapter to match cancer.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("cancer adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected cancer capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cancer.go.kr" {
			t.Fatalf("expected cancer.go.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("cancer should not leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>국가암정보센터</title></head><body>open api</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15122235", Title: "국립암센터_국가암정보센터 내가 알고 싶은 암(100대암)", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "국가암정보센터", Endpoint: "https://cancer.go.kr/openapi/example"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-04T00:00:00Z",
	})
	if result.Provider != "cancer" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected cancer verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected cancer URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestCancerAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewCancerAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15122235", Title: "국립암센터_국가암정보센터 내가 알고 싶은 암(100대암)", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "국가암정보센터", Endpoint: "https://cancer.go.kr/openapi/example"},
		HTTP:      client,
	})
	if result.Provider != "cancer" || result.Status != "failed" || result.Reason != "cancer_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected cancer failure result: %#v", result)
	}
}

func TestGICOMSAdapterVerifiesHTMLLandingPageWithoutAuth(t *testing.T) {
	adapter := NewGICOMSAdapter()
	if !adapter.MatchHost("www.gicoms.go.kr") {
		t.Fatal("expected gicoms adapter to match www.gicoms.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("gicoms adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "verification" {
		t.Fatalf("unexpected gicoms capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.gicoms.go.kr" {
			t.Fatalf("expected www.gicoms.go.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("gicoms should not leak serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>GICOMS</title></head><body>ship location</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15084033", Title: "해양수산부_선박위치정보(연안AIS) 통계정보", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "선박위치정보 통계", Endpoint: "https://www.gicoms.go.kr/openapi/example"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-04T00:00:00Z",
	})
	if result.Provider != "gicoms" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected gicoms verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected gicoms URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestGICOMSAdapterFailsNonOKLandingPage(t *testing.T) {
	adapter := NewGICOMSAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>not found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "15084033", Title: "해양수산부_선박위치정보(연안AIS) 통계정보", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "선박위치정보 통계", Endpoint: "https://www.gicoms.go.kr/openapi/example"},
		HTTP:      client,
	})
	if result.Provider != "gicoms" || result.Status != "failed" || result.Reason != "gicoms_http_404" || result.BodyShape != "html" {
		t.Fatalf("unexpected gicoms failure result: %#v", result)
	}
}

func TestOpenLawAdapterVerifiesExpandedHTMLLandingHostsWithoutAuth(t *testing.T) {
	adapter := NewOpenLawAdapter()
	tests := []struct {
		name     string
		endpoint string
		host     string
	}{
		{name: "open", endpoint: "https://open.law.go.kr/example", host: "open.law.go.kr"},
		{name: "www-law", endpoint: "https://www.law.go.kr/example", host: "www.law.go.kr"},
		{name: "lawmaking", endpoint: "https://www.lawmaking.go.kr/example", host: "www.lawmaking.go.kr"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !adapter.MatchHost(tc.host) {
				t.Fatalf("expected open-law adapter to match %s", tc.host)
			}
			if adapter.MatchHost("apis.data.go.kr") {
				t.Fatal("open-law adapter should not match data.go.kr gateway")
			}
			client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != tc.host {
					t.Fatalf("expected %s host, got %s", tc.host, req.URL.Host)
				}
				if strings.Contains(req.URL.RawQuery, "secret") {
					t.Fatalf("open-law should not leak serviceKey: %s", req.URL.RawQuery)
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
					Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>법제처</title></head><body>open api</body></html>`)),
				}, nil
			})
			result := adapter.Verify(context.Background(), VerificationRequest{
				Spec:       datago.Spec{ID: "15056821", Title: "법제처_현행법령 목록 조회", Provider: "data.go.kr"},
				Operation:  datago.Operation{Name: "법제처 API", Endpoint: tc.endpoint},
				Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
				HTTP:       client,
				VerifiedAt: "2026-07-04T00:00:00Z",
			})
			if result.Provider != "open-law" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
				t.Fatalf("unexpected open-law verification result: %#v", result)
			}
			if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
				t.Fatalf("unexpected open-law URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
			}
		})
	}
}

func TestSeoulOpenDataAdapterVerifiesDatasetPageWithoutAuth(t *testing.T) {
	adapter := NewSeoulOpenDataAdapter()
	if !adapter.MatchHost("data.seoul.go.kr") {
		t.Fatal("expected seoul-open-data adapter to match data.seoul.go.kr")
	}
	if !adapter.MatchHost("openapi.seoul.go.kr:8088") {
		t.Fatal("expected seoul-open-data adapter to match openapi.seoul.go.kr:8088")
	}
	if adapter.MatchHost("ws.bus.go.kr") {
		t.Fatal("seoul-open-data adapter should not match Seoul bus API")
	}
	if strings.Join(adapterCapabilities(adapter), ",") != "call,verification" {
		t.Fatalf("unexpected seoul-open-data capabilities: %#v", adapterCapabilities(adapter))
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.seoul.go.kr" {
			t.Fatalf("expected data.seoul.go.kr host, got %s", req.URL.Host)
		}
		if strings.Contains(req.URL.RawQuery, "secret") {
			t.Fatalf("dataset page verification should not leak credential: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>서울 열린데이터광장</title></head><body>역사 건축 현황</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15003164", Title: "서울교통공사_역사 건축 현황", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "서울교통공사_역사 건축 현황_20240331 외부 링크 1", Endpoint: "http://data.seoul.go.kr/dataList/datasetView.do?infId=OA-11572&srvType=F&serviceKind=1&currentPageNo=1"},
		Credential: Credential{Name: "SEOUL_OPEN_DATA_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-07-03T00:00:00Z",
	})
	if result.Provider != "seoul-open-data" || result.Status != "verified" || result.SemanticStatus != "html_landing_page" || result.BodyShape != "html" {
		t.Fatalf("unexpected seoul-open-data verification result: %#v", result)
	}
	if result.HTTPStatus != 200 || result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("unexpected seoul-open-data URL/status: url=%s status=%d", result.URL, result.HTTPStatus)
	}
}

func TestSeoulOpenDataAdapterSkipsPathAPIWithoutAuth(t *testing.T) {
	adapter := NewSeoulOpenDataAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "seoul-open-data-subway-station-list", Title: "Seoul Open Data subway station bounded query", Provider: "data.seoul.go.kr"},
		Operation: datago.Operation{Name: "bounded sample", Endpoint: "http://openapi.seoul.go.kr:8088/{KEY}/{format}/{service}/{start_index}/{end_index}"},
		Params: map[string]string{
			"format":      "json",
			"service":     "SearchSTNBySubwayLineInfo",
			"start_index": "1",
			"end_index":   "5",
		},
	})
	if result.Provider != "seoul-open-data" || result.Status != "skipped" || result.Reason != "missing_auth" {
		t.Fatalf("unexpected seoul-open-data missing auth result: %#v", result)
	}
	if strings.Contains(result.URL, "{KEY}") {
		t.Fatalf("expected redacted planned URL, got %s", result.URL)
	}
}

func TestSeoulOpenDataAdapterCallsPathAPIWithCredential(t *testing.T) {
	adapter := NewSeoulOpenDataAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.seoul.go.kr:8088" {
			t.Fatalf("expected openapi.seoul.go.kr:8088 host, got %s", req.URL.Host)
		}
		if req.URL.Path != "/secret/json/SearchSTNBySubwayLineInfo/1/5" {
			t.Fatalf("unexpected Seoul Open Data path: %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`{"SearchSTNBySubwayLineInfo":{"RESULT":{"CODE":"INFO-000","MESSAGE":"정상 처리되었습니다"},"row":[{"STATION_NM":"서울"}]}}`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:      datago.Spec{ID: "seoul-open-data-subway-station-list", Title: "Seoul Open Data subway station bounded query", Provider: "data.seoul.go.kr"},
		Operation: datago.Operation{Name: "bounded sample", Endpoint: "http://openapi.seoul.go.kr:8088/{KEY}/{format}/{service}/{start_index}/{end_index}"},
		Params: map[string]string{
			"format":      "json",
			"service":     "SearchSTNBySubwayLineInfo",
			"start_index": "1",
			"end_index":   "5",
		},
		Credential: Credential{Name: "SEOUL_OPEN_DATA_KEY", Value: "secret"},
		HTTP:       client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "seoul-open-data" || envelope.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected seoul-open-data call envelope: %#v", envelope)
	}
	if !strings.Contains(envelope.URL, "/REDACTED/json/SearchSTNBySubwayLineInfo/1/5") || strings.Contains(envelope.URL, "secret") {
		t.Fatalf("expected redacted seoul-open-data call URL: %s", envelope.URL)
	}
}

func TestQNetAdapterOwnsKnownHostsConservatively(t *testing.T) {
	adapter := NewQNetAdapter()
	for _, host := range []string{"openapi.q-net.or.kr", "c.q-net.or.kr", "open.api.q-net.or.kr"} {
		if !adapter.MatchHost(host) {
			t.Fatalf("expected q-net adapter to match %s", host)
		}
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("q-net adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "200", Title: "Q-Net 샘플", Provider: "data.go.kr"}
	op := datago.Operation{Name: "목록", Endpoint: "https://openapi.q-net.or.kr/api/list"}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in provider request")
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><name>ok</name></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       spec,
		Operation:  op,
		Params:     map[string]string{"serviceKey": "secret", "pageNo": "1"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       httpClient,
		VerifiedAt: "2026-06-24T00:00:00Z",
	})
	if result.Provider != "q-net" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected q-net verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted q-net URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected q-net public params: %#v", result.Params)
	}
}

func TestEPostAdapterOwnsKnownHostsConservatively(t *testing.T) {
	adapter := NewEPostAdapter()
	for _, host := range []string{"openapi.epost.go.kr", "openapi.epost.go.kr:80"} {
		if !adapter.MatchHost(host) {
			t.Fatalf("expected epost adapter to match %s", host)
		}
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("epost adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected epost capabilities: %#v", adapter.Capabilities())
	}
	spec := datago.Spec{ID: "300", Title: "EPost 샘플", Provider: "data.go.kr"}
	op := datago.Operation{Name: "우편번호", Endpoint: "http://openapi.epost.go.kr/postal/retrieveNewZipCdService/retrieveNewZipCdService/getNewZipCdList"}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in provider request")
		}
		if req.URL.Query().Get("srchwrd") != "서울" || req.URL.Query().Get("countPerPage") != "1" {
			t.Fatalf("unexpected epost query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><zipNo>04524</zipNo></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		Params:        map[string]string{"serviceKey": "secret", "srchwrd": "서울"},
		MissingParams: []string{"countPerPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "epost" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected epost verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted epost URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["srchwrd"] != "서울" || result.Params["countPerPage"] != "1" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected epost public params: %#v", result.Params)
	}
}

func TestEPostAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewEPostAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.epost.go.kr" {
			t.Fatalf("expected epost host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in epost call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("srchwrd") != "서울" || req.URL.Query().Get("countPerPage") != "1" {
			t.Fatalf("unexpected epost call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><zipNo>04524</zipNo></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "300", Title: "EPost 샘플", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "우편번호", Endpoint: "http://openapi.epost.go.kr/postal/retrieveNewZipCdService/retrieveNewZipCdService/getNewZipCdList"},
		Params:        map[string]string{"srchwrd": "서울"},
		MissingParams: []string{"countPerPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "epost" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected epost call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected epost provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.Body, "<zipNo>04524</zipNo>") {
		t.Fatalf("unexpected epost call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestEPostAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewEPostAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "300", Title: "EPost 샘플", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "우편번호", Endpoint: "http://openapi.epost.go.kr/postal/retrieveNewZipCdService/retrieveNewZipCdService/getNewZipCdList"},
		Params:        map[string]string{"srchwrd": "서울"},
		MissingParams: []string{"countPerPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted transport error, got %v", err)
	}
}

func TestEPostAdapterSkipsWADLMetadataEndpoints(t *testing.T) {
	adapter := NewEPostAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "301", Title: "EPost WADL", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "메타데이터", Endpoint: "http://openapi.epost.go.kr:80/postal/retrieveNewAdressAreaCdService?_wadl&type=xml"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "epost_wadl_metadata_only" {
		t.Fatalf("unexpected epost WADL skip result: %#v", result)
	}
}

func TestEPostAdapterSkipsUnsupportedSOAP(t *testing.T) {
	adapter := NewEPostAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "302", Title: "EPost SOAP", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "SOAP",
			Endpoint: "http://openapi.epost.go.kr:80/postal/RegisterEMSService",
			Source:   &datago.Source{Raw: map[string]any{"api_type": "SOAP"}},
		},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "epost_unsupported_protocol" {
		t.Fatalf("unexpected epost SOAP skip result: %#v", result)
	}
}

func TestEPostAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewEPostAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "303", Title: "EPost 상세", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "상세", Endpoint: "http://openapi.epost.go.kr/postal/detail"},
		MissingParams: []string{"srchwrd"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "epost_missing_required_params" || len(result.MissingParams) != 1 {
		t.Fatalf("unexpected epost skip result: %#v", result)
	}
}

func TestEPostAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewEPostAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<OpenAPI_ServiceResponse><cmmMsgHeader><errMsg>SERVICE ERROR</errMsg><returnAuthMsg>SERVICE KEY IS NOT REGISTERED ERROR.</returnAuthMsg></cmmMsgHeader></OpenAPI_ServiceResponse>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "304", Title: "EPost key", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "키 오류", Endpoint: "http://openapi.epost.go.kr/postal/list"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "epost_service_key_not_registered" {
		t.Fatalf("unexpected epost service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.AuthMessage == "" {
		t.Fatalf("unexpected epost provider status: %#v", result.ProviderStatus)
	}
}

func TestEKAPEAdapterOwnsKnownHostsConservatively(t *testing.T) {
	adapter := NewEKAPEAdapter()
	if !adapter.MatchHost("data.ekape.or.kr") {
		t.Fatal("expected ekape adapter to match data.ekape.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("ekape adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "400", Title: "EKAPE 샘플", Provider: "data.go.kr"}
	op := datago.Operation{Name: "목록", Endpoint: "http://data.ekape.or.kr/openapi-data/service/user/grade/confirmNo"}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in provider request")
		}
		if req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected ekape query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><issueNo>1</issueNo></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "ekape" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected ekape verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted ekape URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected ekape public params: %#v", result.Params)
	}
}

func TestEKAPEAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewEKAPEAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("issueDate") != "20240101" || req.URL.Query().Get("issueNo") != "1" {
			t.Fatalf("unexpected ekape query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>99</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header><notice/></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "401", Title: "EKAPE key", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "키 오류", Endpoint: "http://data.ekape.or.kr/openapi-data/service/user/grade/confirmNo"},
		MissingParams: []string{"issueDate", "issueNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "ekape_service_key_not_registered" {
		t.Fatalf("unexpected ekape service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "99" {
		t.Fatalf("unexpected ekape provider status: %#v", result.ProviderStatus)
	}
	if result.BodyShape != "xml_notice" {
		t.Fatalf("unexpected ekape body shape: %s", result.BodyShape)
	}
}

func TestForestAdapterOwnsKnownHostsConservatively(t *testing.T) {
	adapter := NewForestAdapter()
	if !adapter.MatchHost("api.forest.go.kr") {
		t.Fatal("expected forest adapter to match api.forest.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("forest adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected forest capabilities: %#v", adapter.Capabilities())
	}
	spec := datago.Spec{ID: "500", Title: "Forest 샘플", Provider: "data.go.kr"}
	op := datago.Operation{Name: "숲 이야기", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in provider request")
		}
		if req.URL.Query().Get("searchWrd") != "소나무" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected forest query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><fsname>소나무</fsname></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"searchWrd", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "forest" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected forest verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted forest URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["searchWrd"] != "소나무" || result.Params["pageNo"] != "1" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected forest public params: %#v", result.Params)
	}
}

func TestForestAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewForestAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.forest.go.kr" {
			t.Fatalf("expected forest host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in forest call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("searchWrd") != "소나무" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected forest call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><fsname>소나무</fsname></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "500", Title: "Forest 샘플", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "숲 이야기", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"},
		MissingParams: []string{"searchWrd", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "forest" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected forest call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected forest provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.Body, "<fsname>소나무</fsname>") {
		t.Fatalf("unexpected forest call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestForestAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewForestAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "500", Title: "Forest 샘플", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "숲 이야기", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"},
		MissingParams: []string{"searchWrd", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted transport error, got %v", err)
	}
}

func TestForestAdapterVerifyRedactsTransportErrors(t *testing.T) {
	adapter := NewForestAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "500", Title: "Forest 샘플", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "숲 이야기", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"},
		MissingParams: []string{"searchWrd", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" {
		t.Fatalf("expected failed transport result: %#v", result)
	}
	if strings.Contains(result.Reason, "secret") || !strings.Contains(result.Reason, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted transport reason, got %s", result.Reason)
	}
}

func TestForestAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewForestAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("searchMtNm") != "북한산" || req.URL.Query().Get("searchArNm") != "서울" {
			t.Fatalf("unexpected forest trail query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>30</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "501", Title: "Forest key", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "키 오류", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/gdTrailInfoOpenAPI"},
		MissingParams: []string{"searchMtNm", "searchArNm"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "forest_service_key_not_registered" {
		t.Fatalf("unexpected forest service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "30" {
		t.Fatalf("unexpected forest provider status: %#v", result.ProviderStatus)
	}
}

func TestJeonjuAdapterOwnsKnownHostAndAuthParam(t *testing.T) {
	adapter := NewJeonjuAdapter()
	if !adapter.MatchHost("openapi.jeonju.go.kr") {
		t.Fatal("expected jeonju adapter to match openapi.jeonju.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("jeonju adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "600", Title: "Jeonju 샘플", Provider: "data.go.kr"}
	op := datago.Operation{
		Name:     "무선인터넷존",
		Endpoint: "http://openapi.jeonju.go.kr/rest/wifizone",
		RequestParams: []datago.Param{
			{Name: "authApiKey"},
			{Name: "posy"},
			{Name: "posx"},
			{Name: "searchDts"},
		},
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("authApiKey") != "secret" {
			t.Fatalf("expected authApiKey in jeonju request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("posy") != "35.8242" || req.URL.Query().Get("posx") != "127.1480" || req.URL.Query().Get("searchDts") != "1000" {
			t.Fatalf("unexpected jeonju query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"items":[{"name":"wifi"}],"resultCode":"00","resultMsg":"NORMAL SERVICE."}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"posy", "posx", "searchDts"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "jeonju" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected jeonju verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "authApiKey=REDACTED") {
		t.Fatalf("expected redacted jeonju URL: %s", result.URL)
	}
	if result.Params["authApiKey"] != "" || result.Params["posy"] != "35.8242" || result.Params["posx"] != "127.1480" || result.Params["searchDts"] != "1000" || result.BodyShape != "json_items" {
		t.Fatalf("unexpected jeonju public params: %#v", result.Params)
	}
}

func TestGeojeAdapterOwnsKnownHostAndVerifiesList(t *testing.T) {
	adapter := NewGeojeAdapter()
	if !adapter.MatchHost("data.geoje.go.kr") {
		t.Fatal("expected geoje adapter to match data.geoje.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("geoje adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "650", Title: "Geoje 의료", Provider: "data.go.kr"}
	op := datago.Operation{
		Name:     "거제시 의료기관 목록정보",
		Endpoint: "http://data.geoje.go.kr/rfcapi/rest/geojemedical/getGeojemedicalList",
		RequestParams: []datago.Param{
			{Name: "geojemedicalNm"},
			{Name: "geojemedicalCd"},
			{Name: "pageSize"},
			{Name: "startPage"},
		},
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in geoje request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("startPage") != "1" || req.URL.Query().Get("pageSize") != "1" {
			t.Fatalf("unexpected geoje paging query: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Has("geojemedicalNm") || req.URL.Query().Has("geojemedicalCd") {
			t.Fatalf("empty optional geoje filters should not be sent: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rfcOpenApi><header><resultCode>00</resultCode><resultMsg>success</resultMsg></header><body><pageSize>1</pageSize><data><list><geojemedicalNm>다임치과의원</geojemedicalNm></list></data></body></rfcOpenApi>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"geojemedicalNm", "geojemedicalCd", "pageSize", "startPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "geoje" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected geoje verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted geoje URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["startPage"] != "1" || result.Params["pageSize"] != "1" || result.BodyShape != "xml_list" {
		t.Fatalf("unexpected geoje public params: %#v", result.Params)
	}
}

func TestGeojeAdapterSkipsDetailIDsWithoutInventingValues(t *testing.T) {
	adapter := NewGeojeAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "651", Title: "Geoje 상세", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "거제시 의료기관 상세정보", Endpoint: "http://data.geoje.go.kr/rfcapi/rest/geojemedical/getGeojemedicalList"},
		MissingParams: []string{"geojemedicalId"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "geoje_missing_required_params" || len(result.MissingParams) != 1 {
		t.Fatalf("unexpected geoje skip result: %#v", result)
	}
}

func TestGeojeAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewGeojeAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.geoje.go.kr" {
			t.Fatalf("expected geoje host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in geoje call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("startPage") != "1" || req.URL.Query().Get("pageSize") != "1" {
			t.Fatalf("unexpected geoje call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<rfcOpenApi><header><resultCode>00</resultCode><resultMsg>success</resultMsg></header><body><data><list><goodshopNm>착한식당</goodshopNm></list></data></body></rfcOpenApi>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "652", Title: "Geoje 착한가격업소", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "거제시 착한가격업소 목록정보",
			Endpoint: "http://data.geoje.go.kr/rfcapi/rest/geojegoodshop/getGeojegoodshopList",
			RequestParams: []datago.Param{
				{Name: "goodshopNm"},
				{Name: "pageSize"},
				{Name: "startPage"},
			},
		},
		MissingParams: []string{"goodshopNm", "pageSize", "startPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "geoje" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected geoje call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected geoje provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "착한식당") {
		t.Fatalf("unexpected geoje call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestGeojeAdapterClassifiesCommonErrors(t *testing.T) {
	adapter := NewGeojeAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rfcOpenApi><header><resultCode>99</resultCode><resultMsg>Common Error</resultMsg></header></rfcOpenApi>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "653", Title: "Geoje error", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "거제시 의료기관 목록정보", Endpoint: "http://data.geoje.go.kr/rfcapi/rest/geojemedical/getGeojemedicalList"},
		MissingParams: []string{"startPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "geoje_common_error" {
		t.Fatalf("unexpected geoje common error result: %#v", result)
	}
}

func TestGeojeAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewGeojeAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "654", Title: "Geoje transport", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "거제시 의료기관 목록정보",
			Endpoint: "http://data.geoje.go.kr/rfcapi/rest/geojemedical/getGeojemedicalList",
		},
		MissingParams: []string{"startPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted geoje transport error, got %v", err)
	}
}

func TestUiryeongAdapterOwnsKnownHostAndClassifiesServiceKeyRegistration(t *testing.T) {
	adapter := NewUiryeongAdapter()
	if !adapter.MatchHost("data.uiryeong.go.kr") {
		t.Fatal("expected uiryeong adapter to match data.uiryeong.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("uiryeong adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "660", Title: "Uiryeong park", Provider: "data.go.kr"}
	op := datago.Operation{
		Name:     "도시공원정보 목록",
		Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongpark/getUiryeongparkList",
		RequestParams: []datago.Param{
			{Name: "ServiceKey"},
			{Name: "pageNo"},
			{Name: "numOfRows"},
			{Name: "uiryeongparkEntId"},
			{Name: "uiryeongparkType"},
			{Name: "uiryeongparkTitle"},
			{Name: "uiryeongparkNewAddr"},
			{Name: "uiryeongparkAddr"},
		},
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("ServiceKey") != "secret" {
			t.Fatalf("expected ServiceKey in uiryeong request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected uiryeong paging query: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Has("uiryeongparkEntId") || req.URL.Query().Has("uiryeongparkTitle") || req.URL.Query().Has("uiryeongparkAddr") {
			t.Fatalf("empty optional uiryeong filters should not be sent: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rfcOpenApi><header><resultCode>99</resultCode><resultMsg>등록되지 않은 서비스키입니다.</resultMsg></header></rfcOpenApi>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"pageNo", "numOfRows", "uiryeongparkEntId", "uiryeongparkType", "uiryeongparkTitle", "uiryeongparkNewAddr", "uiryeongparkAddr"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "uiryeong" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "uiryeong_service_key_not_registered" {
		t.Fatalf("unexpected uiryeong verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted uiryeong URL: %s", result.URL)
	}
	if result.Params["ServiceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected uiryeong public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestUiryeongAdapterSkipsOpaqueIDsForDetailAndFileOperations(t *testing.T) {
	adapter := NewUiryeongAdapter()
	for _, op := range []datago.Operation{
		{Name: "체육시설 상세정보", Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongphysical/uiryeongphysicalView"},
		{Name: "민박/펜션업소 사진", Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongstay/uiryeongstayFile"},
	} {
		result := adapter.Verify(context.Background(), VerificationRequest{
			Spec:          datago.Spec{ID: "661", Title: "Uiryeong detail", Provider: "data.go.kr"},
			Operation:     op,
			MissingParams: []string{"uiryeongphysicalEntId"},
			Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		})
		if result.Status != "skipped" || result.Reason != "uiryeong_missing_required_params" || len(result.MissingParams) != 1 {
			t.Fatalf("unexpected uiryeong skip result for %s: %#v", op.Name, result)
		}
	}
}

func TestUiryeongAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewUiryeongAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.uiryeong.go.kr" {
			t.Fatalf("expected uiryeong host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("ServiceKey") != "secret" {
			t.Fatalf("expected ServiceKey in uiryeong call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected uiryeong call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<rfcOpenApi><header><resultCode>00</resultCode><resultMsg>success</resultMsg></header><body><data><list><title>의령 도시공원</title></list></data></body></rfcOpenApi>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "662", Title: "Uiryeong park", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "도시공원정보 목록",
			Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongpark/getUiryeongparkList",
			RequestParams: []datago.Param{
				{Name: "ServiceKey"},
				{Name: "pageNo"},
				{Name: "numOfRows"},
				{Name: "uiryeongparkEntId"},
				{Name: "uiryeongparkTitle"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows", "uiryeongparkEntId", "uiryeongparkTitle"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "uiryeong" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected uiryeong call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected uiryeong provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "ServiceKey=REDACTED") || !strings.Contains(envelope.Body, "의령 도시공원") {
		t.Fatalf("unexpected uiryeong call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestUiryeongAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewUiryeongAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "663", Title: "Uiryeong transport", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "도시공원정보 목록",
			Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongpark/getUiryeongparkList",
			RequestParams: []datago.Param{
				{Name: "ServiceKey"},
				{Name: "pageNo"},
			},
		},
		MissingParams: []string{"pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted uiryeong transport error, got %v", err)
	}
}

func TestAndongAdapterAddsServiceKeyAndClassifiesServiceErrors(t *testing.T) {
	adapter := NewAndongAdapter()
	if !adapter.MatchHost("www.andong.go.kr") {
		t.Fatal("expected andong adapter to match www.andong.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("andong adapter should not match data.go.kr gateway")
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.andong.go.kr" {
			t.Fatalf("expected andong host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in andong request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("numOfRowns") != "1" {
			t.Fatalf("expected numOfRowns default in andong request: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>99</resultCode><resultMsg>NO OPENAPI SERVICE ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "920", Title: "경상북도 안동시_의료기관 현황", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "의무관리현황 목록 조회",
			Endpoint: "https://www.andong.go.kr/openapi/service/mediHsptService/getList",
			RequestParams: []datago.Param{
				{Name: "numOfRowns"},
			},
		},
		MissingParams: []string{"numOfRowns"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "andong" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "andong_service_not_registered" {
		t.Fatalf("unexpected andong verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted andong URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["numOfRowns"] != "1" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected andong public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestAndongAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewAndongAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "921", Title: "경상북도 안동시_개별공시지가자료", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "개별공시지가 목록 조회",
			Endpoint: "https://www.andong.go.kr/openapi/service/arDevJigaService/getList",
		},
		MissingParams: []string{"dongCode", "langKind", "bonbun", "bubun", "numOfRowns"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "andong_missing_required_params" || strings.Join(result.MissingParams, ",") != "dongCode,langKind,bonbun,bubun" {
		t.Fatalf("unexpected andong skip result: %#v", result)
	}
}

func TestAndongAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewAndongAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in andong call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("numOfRowns") != "1" {
			t.Fatalf("unexpected andong call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><name>안동병원</name></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "922", Title: "경상북도 안동시_의료기관 현황", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "의무관리현황 목록 조회",
			Endpoint: "https://www.andong.go.kr/openapi/service/mediHsptService/getList",
			RequestParams: []datago.Param{
				{Name: "numOfRowns"},
			},
		},
		MissingParams: []string{"numOfRowns"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "andong" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected andong call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected andong provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "안동병원") {
		t.Fatalf("unexpected andong call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestAndongAdapterVerifyNormalizesTransportTimeouts(t *testing.T) {
	adapter := NewAndongAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "923", Title: "경상북도 안동시 timeout", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "의무관리현황 목록 조회",
			Endpoint: "https://www.andong.go.kr/openapi/service/mediHsptService/getList",
		},
		MissingParams: []string{"numOfRowns"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "andong_request_timeout" {
		t.Fatalf("unexpected andong transport result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted andong URL after timeout: %s", result.URL)
	}
}

func TestAndongAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewAndongAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "924", Title: "경상북도 안동시 transport", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "의무관리현황 목록 조회",
			Endpoint: "https://www.andong.go.kr/openapi/service/mediHsptService/getList",
		},
		MissingParams: []string{"numOfRowns"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted andong transport error, got %v", err)
	}
}

func TestItfindAdapterAddsServiceKeyAndVerifiesNormalService(t *testing.T) {
	adapter := NewItfindAdapter()
	if !adapter.MatchHost("open.itfind.or.kr") {
		t.Fatal("expected itfind adapter to match open.itfind.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("itfind adapter should not match data.go.kr gateway")
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "open.itfind.or.kr" {
			t.Fatalf("expected itfind host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in itfind request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected itfind paging query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><identifier>02-001-260618-000005</identifier></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "930", Title: "한국연구재단_연구결과보고서 정보", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "연구결과보고서 정보 조회",
			Endpoint: "http://open.itfind.or.kr/openapi/service/ResearchResultReportService/getResearchResultReport",
			RequestParams: []datago.Param{
				{Name: "pageNo"},
				{Name: "numOfRows"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "itfind" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected itfind verification result: %#v", result)
	}
	if result.ProviderStatus == nil || !result.ProviderStatus.OK || result.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected itfind provider status: %#v", result.ProviderStatus)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted itfind URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected itfind public params: %#v", result.Params)
	}
}

func TestItfindAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewItfindAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "931", Title: "한국연구재단_ICT 정기간행물 상세", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "ICT정기간행물 목록별 상세 정보 조회",
			Endpoint: "http://open.itfind.or.kr/openapi/service/ITPeriodicalsService/getITPdicalListCntntDetailInfo",
		},
		MissingParams: []string{"identifier", "pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "itfind_missing_required_params" || strings.Join(result.MissingParams, ",") != "identifier" {
		t.Fatalf("unexpected itfind skip result: %#v", result)
	}
}

func TestItfindAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewItfindAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in itfind call: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><publisher>정보통신기획평가원</publisher></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "932", Title: "한국연구재단_연구결과보고서 정보", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "연구결과보고서 정보 조회",
			Endpoint: "http://open.itfind.or.kr/openapi/service/ResearchResultReportService/getResearchResultReport",
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "itfind" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected itfind call envelope: %#v", envelope)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "정보통신기획평가원") {
		t.Fatalf("unexpected itfind call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestItfindAdapterClassifiesServiceKeyRegistrationErrors(t *testing.T) {
	adapter := NewItfindAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>30</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "933", Title: "한국연구재단 service key", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "연구결과보고서 정보 조회",
			Endpoint: "http://open.itfind.or.kr/openapi/service/ResearchResultReportService/getResearchResultReport",
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "itfind_service_key_not_registered" || result.SemanticStatus != "provider_error" {
		t.Fatalf("unexpected itfind service key result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted itfind URL: %s", result.URL)
	}
}

func TestItfindAdapterClassifiesMissingEndpoints(t *testing.T) {
	adapter := NewItfindAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<html>Not Found</html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "934", Title: "한국연구재단 missing endpoint", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "SW Insight 리포트 정보 조회",
			Endpoint: "http://open.itfind.or.kr/openapi/service/ITPeriodicalsService/getSWInsightReportInfo",
		},
		MissingParams: []string{"pageNo", "numOfRows", "searchWord"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "itfind_endpoint_not_found" || result.HTTPStatus != 404 {
		t.Fatalf("unexpected itfind missing endpoint result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted itfind URL: %s", result.URL)
	}
}

func TestKoradAdapterSkipsWADLMetadataEndpoints(t *testing.T) {
	adapter := NewKoradAdapter()
	if !adapter.MatchHost("www.korad.or.kr") {
		t.Fatal("expected korad adapter to match www.korad.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("korad adapter should not match data.go.kr gateway")
	}
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "940", Title: "한국원자력환경공단 WADL", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "RI폐기물 누적 현황",
			Endpoint: "http://www.korad.or.kr/openapi/service/radiRiWasteOpenStatsSvc?_wadl&type=xml",
		},
		MissingParams: []string{"N/A"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Provider != "korad" || result.Status != "skipped" || result.Reason != "korad_wadl_metadata_only" || result.BodyShape != "wadl_metadata" {
		t.Fatalf("unexpected korad WADL result: %#v", result)
	}
}

func TestKoradAdapterAddsServiceKeyAndClassifiesRegistrationErrors(t *testing.T) {
	adapter := NewKoradAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.korad.or.kr" {
			t.Fatalf("expected korad host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in korad request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("yyyy") != "2024" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected korad query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>99</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "941", Title: "한국원자력환경공단_방사성폐기물 인수현황", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "getRadiTakeWasteStatsDataList",
			Endpoint: "http://www.korad.or.kr/openapi/service/radiTakeWasteStatsSvc/getRadiTakeWasteStatsDataList",
			RequestParams: []datago.Param{
				{Name: "serviceKey"},
				{Name: "pageNo"},
				{Name: "numOfRows"},
				{Name: "yyyy"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows", "yyyy"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "korad_service_key_not_registered" || result.SemanticStatus != "provider_error" {
		t.Fatalf("unexpected korad registration result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted korad URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["yyyy"] != "2024" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected korad public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestKoradAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewKoradAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("yyyymm") != "202401" {
			t.Fatalf("unexpected korad call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><insttNm>한국원자력환경공단</insttNm></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "942", Title: "한국원자력환경공단_중저준위방폐물 저장현황", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "getRadiRadiationStorsStatsDataList",
			Endpoint: "http://www.korad.or.kr/openapi/service/radiationStorsSvc/getRadiRadiationStorsStatsDataList",
		},
		MissingParams: []string{"pageNo", "numOfRows", "yyyymm"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "korad" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected korad call envelope: %#v", envelope)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "한국원자력환경공단") {
		t.Fatalf("unexpected korad call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestKoradAdapterRejectsWADLCall(t *testing.T) {
	adapter := NewKoradAdapter()
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "943", Title: "한국원자력환경공단 WADL", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "RI폐기물 누적 현황",
			Endpoint: "http://www.korad.or.kr/openapi/service/radiRiWasteOpenStatsSvc?_wadl&type=xml",
		},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "WADL metadata endpoint") {
		t.Fatalf("expected WADL call rejection, got %v", err)
	}
}

func TestSisulAdapterSkipsWADLMetadataEndpoints(t *testing.T) {
	adapter := NewSisulAdapter()
	if !adapter.MatchHost("data.sisul.or.kr") {
		t.Fatal("expected sisul adapter to match data.sisul.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("sisul adapter should not match data.go.kr gateway")
	}
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "900", Title: "서울시설공단 retaining walls", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "옹벽 현황 WADL",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/RetainingWalls?_wadl&type=xml",
		},
		MissingParams: []string{"proadlinename"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Provider != "sisul" || result.Status != "skipped" || result.Reason != "sisul_wadl_metadata_only" {
		t.Fatalf("unexpected sisul WADL skip result: %#v", result)
	}
}

func TestSisulAdapterAddsServiceKeyAndClassifiesAuthErrors(t *testing.T) {
	adapter := NewSisulAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.sisul.or.kr" {
			t.Fatalf("expected sisul host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in sisul request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected sisul paging query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>99</resultCode><resultMsg>지원하지 않는 인증 방식이거나 인증키가 누락되었습니다. PUBC 인증(?serviceKey=) 또는 Gateway 인증(?SG_APIM=)을 사용하세요.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "901", Title: "서울시설공단 공영차고지", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "공영차고지 조회",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/Publicgarage/getPublicgarageQry",
			RequestParams: []datago.Param{
				{Name: "pageNo"},
				{Name: "numOfRows"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "sisul" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "sisul_auth_required" {
		t.Fatalf("unexpected sisul verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted sisul URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected sisul public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestSisulAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewSisulAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "902", Title: "서울시설공단 화장 연령 통계", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "화장 연령 통계 조회",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/CremationAgeStat/getCremationAgeStatQry",
		},
		MissingParams: []string{"pfromym", "ptoym", "pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "sisul_missing_required_params" || strings.Join(result.MissingParams, ",") != "pfromym,ptoym" {
		t.Fatalf("unexpected sisul skip result: %#v", result)
	}
}

func TestSisulAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewSisulAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in sisul call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected sisul call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><garageName>장안공영차고지</garageName></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "903", Title: "서울시설공단 공영차고지", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "공영차고지 조회",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/Publicgarage/getPublicgarageQry",
			RequestParams: []datago.Param{
				{Name: "pageNo"},
				{Name: "numOfRows"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "sisul" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected sisul call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected sisul provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "장안공영차고지") {
		t.Fatalf("unexpected sisul call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestSisulAdapterVerifyNormalizesTransportTimeouts(t *testing.T) {
	adapter := NewSisulAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "904", Title: "서울시설공단 timeout", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "공영차고지 조회",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/Publicgarage/getPublicgarageQry",
		},
		MissingParams: []string{"pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "sisul_request_timeout" {
		t.Fatalf("unexpected sisul transport result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted sisul URL after timeout: %s", result.URL)
	}
}

func TestSisulAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewSisulAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "905", Title: "서울시설공단 transport", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "공영차고지 조회",
			Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/Publicgarage/getPublicgarageQry",
		},
		MissingParams: []string{"pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted sisul transport error, got %v", err)
	}
}

func TestTourAdapterSkipsServiceRootWithoutOperationPath(t *testing.T) {
	adapter := NewTourAdapter()
	called := false
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "950", Title: "관광 서비스 루트", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "관광 통계",
			Endpoint: "http://openapi.tour.go.kr/openapi/service",
			Source: &datago.Source{Raw: map[string]any{
				"end_point_url": "http://openapi.tour.go.kr/openapi/service",
				"operation_url": "",
			}},
		},
		MissingParams: []string{"YY"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	})
	if called {
		t.Fatal("tour service-root operation should skip before HTTP")
	}
	if result.Provider != "tour" || result.Status != "skipped" || result.Reason != "tour_service_root_missing_operation_path" || result.BodyShape != "service_root" {
		t.Fatalf("unexpected tour service-root skip: %#v", result)
	}
	if result.Params["YY"] != "2024" {
		t.Fatalf("expected safe YY default in public params: %#v", result.Params)
	}
}

func TestLHEBidAdapterAddsServiceKeyAndDateDefaults(t *testing.T) {
	adapter := NewLHEBidAdapter()
	if !adapter.MatchHost("openapi.ebid.lh.or.kr") {
		t.Fatal("expected lh-ebid adapter to match openapi.ebid.lh.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("lh-ebid adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected lh-ebid capabilities: %#v", adapter.Capabilities())
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.ebid.lh.or.kr" {
			t.Fatalf("expected lh-ebid host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in lh-ebid request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("tndrbidRegDtStart") != "20240101" || req.URL.Query().Get("tndrbidRegDtEnd") != "20240131" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected lh-ebid query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><bidNum>1</bidNum></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15021183", Title: "LH 입찰", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "입찰정보 조회", Endpoint: "http://openapi.ebid.lh.or.kr/ebid.com.openapi.service.OpenBidInfoList.dev"},
		MissingParams: []string{"tndrbidRegDtStart", "tndrbidRegDtEnd", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "lh-ebid" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected lh-ebid verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted lh-ebid URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["tndrbidRegDtStart"] != "20240101" || result.Params["tndrbidRegDtEnd"] != "20240131" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected lh-ebid public params: %#v", result.Params)
	}
}

func TestLHEBidAdapterSkipsUnknownRequiredIdentifiers(t *testing.T) {
	adapter := NewLHEBidAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15021184", Title: "LH 계약", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "계약현황 조회", Endpoint: "http://openapi.ebid.lh.or.kr/ebid.com.openapi.service.OpenContractInfoList.dev"},
		MissingParams: []string{"contractDtStart", "contractDtEnd", "bidNum"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "lh_ebid_missing_required_params" || len(result.MissingParams) != 1 || result.MissingParams[0] != "bidNum" {
		t.Fatalf("unexpected lh-ebid identifier skip result: %#v", result)
	}
	if result.Params["contractDtStart"] != "20240101" || result.Params["contractDtEnd"] != "20240131" {
		t.Fatalf("expected contract date defaults: %#v", result.Params)
	}
}

func TestLHEBidAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewLHEBidAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("orderExpectYmStart") != "202401" || req.URL.Query().Get("orderExpectYmEnd") != "202412" {
			t.Fatalf("unexpected lh-ebid order query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>30</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15042795", Title: "LH 발주계획", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "발주계획정보 조회", Endpoint: "http://openapi.ebid.lh.or.kr/ebid.com.openapi.service.OpenOrdergPlanList.dev"},
		MissingParams: []string{"orderExpectYmStart", "orderExpectYmEnd"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "lh_ebid_service_key_not_registered" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected lh-ebid service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "30" {
		t.Fatalf("unexpected lh-ebid provider status: %#v", result.ProviderStatus)
	}
}

func TestLHEBidAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewLHEBidAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("openDtmStart") != "20240101" || req.URL.Query().Get("openDtmEnd") != "20240131" {
			t.Fatalf("unexpected lh-ebid call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><openDtm>20240101</openDtm></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "15058826", Title: "LH 개찰결과", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "개찰결과정보", Endpoint: "http://openapi.ebid.lh.or.kr/ebid.com.openapi.service.OpenTenderopenList.dev"},
		MissingParams: []string{"openDtmStart", "openDtmEnd"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "lh-ebid" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected lh-ebid call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected lh-ebid provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted lh-ebid call URL: %s", envelope.URL)
	}
}

func TestSeoulBusAdapterAddsServiceKeyAndRouteDefaults(t *testing.T) {
	adapter := NewSeoulBusAdapter()
	if !adapter.MatchHost("ws.bus.go.kr") {
		t.Fatal("expected seoul-bus adapter to match ws.bus.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("seoul-bus adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected seoul-bus capabilities: %#v", adapter.Capabilities())
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "ws.bus.go.kr" {
			t.Fatalf("expected seoul-bus host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in seoul-bus request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("busRouteId") != "100100118" || req.URL.Query().Get("startOrd") != "1" || req.URL.Query().Get("endOrd") != "5" {
			t.Fatalf("unexpected seoul-bus query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<ServiceResult><comMsgHeader/><msgHeader><headerCd>0</headerCd><headerMsg>정상적으로 처리되었습니다.</headerMsg><itemCount>1</itemCount></msgHeader><msgBody><itemList><vehId>1</vehId></itemList></msgBody></ServiceResult>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15000332", Title: "서울 버스", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "getBusPosByRouteStList", Endpoint: "http://ws.bus.go.kr/api/rest/buspos/getBusPosByRouteSt"},
		MissingParams: []string{"busRouteId", "startOrd", "endOrd"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "seoul-bus" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_service_items" {
		t.Fatalf("unexpected seoul-bus verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted seoul-bus URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["busRouteId"] != "100100118" || result.Params["startOrd"] != "1" || result.Params["endOrd"] != "5" {
		t.Fatalf("unexpected seoul-bus public params: %#v", result.Params)
	}
}

func TestSeoulBusAdapterSkipsVehicleIdentifier(t *testing.T) {
	adapter := NewSeoulBusAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15000332", Title: "서울 버스", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "getBusPosByVehIdItem", Endpoint: "http://ws.bus.go.kr/api/rest/buspos/getBusPosByVehId"},
		MissingParams: []string{"vehId"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "seoul_bus_missing_required_params" || len(result.MissingParams) != 1 || result.MissingParams[0] != "vehId" {
		t.Fatalf("unexpected seoul-bus vehicle skip result: %#v", result)
	}
}

func TestSeoulBusAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewSeoulBusAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<ServiceResult><comMsgHeader/><msgHeader><headerCd>7</headerCd><headerMsg>Key인증실패: SERVICE KEY IS NOT REGISTERED ERROR.[인증모듈 에러코드(30)]</headerMsg><itemCount>0</itemCount></msgHeader><msgBody/></ServiceResult>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15000332", Title: "서울 버스", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "getBusPosByRtidList", Endpoint: "http://ws.bus.go.kr/api/rest/buspos/getBusPosByRtid"},
		MissingParams: []string{"busRouteId"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "seoul_bus_service_key_not_registered" || result.BodyShape != "xml_service_status" {
		t.Fatalf("unexpected seoul-bus key-registration result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "7" || result.ProviderStatus.Source != "ServiceResult/msgHeader" {
		t.Fatalf("unexpected seoul-bus provider status: %#v", result.ProviderStatus)
	}
}

func TestSeoulBusAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewSeoulBusAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("busRouteId") != "100100118" {
			t.Fatalf("unexpected seoul-bus call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<ServiceResult><msgHeader><headerCd>0</headerCd><headerMsg>OK</headerMsg></msgHeader><msgBody><itemList><vehId>1</vehId></itemList></msgBody></ServiceResult>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "15000332", Title: "서울 버스", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "getBusPosByRtidList", Endpoint: "http://ws.bus.go.kr/api/rest/buspos/getBusPosByRtid"},
		MissingParams: []string{"busRouteId"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "seoul-bus" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected seoul-bus call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "0" {
		t.Fatalf("unexpected seoul-bus provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted seoul-bus call URL: %s", envelope.URL)
	}
}

func TestGBLibAdapterAddsServiceKeyAndDefaults(t *testing.T) {
	adapter := NewGBLibAdapter()
	if !adapter.MatchHost("openapi.gblib.or.kr") {
		t.Fatal("expected gblib adapter to match openapi.gblib.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("gblib adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected gblib capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.gblib.or.kr" {
			t.Fatalf("expected gblib host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in gblib request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("keyword") != "공공" || req.URL.Query().Get("pub") != "도서" || req.URL.Query().Get("lib") != "MA" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected gblib query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><title>공공</title></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "3075291", Title: "강북문화정보도서관", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "도서자료검색", Endpoint: "http://openapi.gblib.or.kr/OpenAPI/service/SearchBook/getSearchBook"},
		MissingParams: []string{"keyword", "pub", "lib", "numOfRows", "pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "gblib" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected gblib verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted gblib URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["keyword"] != "공공" || result.Params["lib"] != "MA" {
		t.Fatalf("unexpected gblib public params: %#v", result.Params)
	}
}

func TestGBLibAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewGBLibAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>99</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "15003489", Title: "강북 스포츠시설", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "이용현황검색", Endpoint: "http://openapi.gblib.or.kr/OpenAPI/service/SportsCenterNow/getNow"},
		MissingParams: []string{"org", "date"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "gblib_service_key_not_registered" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected gblib service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "99" {
		t.Fatalf("unexpected gblib provider status: %#v", result.ProviderStatus)
	}
}

func TestGBLibAdapterClassifiesNotFoundEndpoint(t *testing.T) {
	adapter := NewGBLibAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>Not Found</body></html>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "3075292", Title: "일반열람실", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "일반열람실 목록 조회", Endpoint: "http://openapi.gblib.or.kr/OpenAPI/service/ReadingRoomInfoService"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if result.Status != "failed" || result.SemanticStatus != "http_error" || result.Reason != "gblib_endpoint_not_found" || result.HTTPStatus != 404 {
		t.Fatalf("unexpected gblib not found result: %#v", result)
	}
}

func TestGBLibAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewGBLibAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("org") != "1" || req.URL.Query().Get("date") != "20260625" {
			t.Fatalf("unexpected gblib call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><cnt>1</cnt></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "15003489", Title: "강북 스포츠시설", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "이용현황검색", Endpoint: "http://openapi.gblib.or.kr/OpenAPI/service/SportsCenterNow/getNow"},
		MissingParams: []string{"org", "date"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "gblib" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected gblib call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected gblib provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted gblib call URL: %s", envelope.URL)
	}
}

func TestKPXAdapterNormalizesEndpointAndDefaults(t *testing.T) {
	adapter := NewKPXAdapter()
	if !adapter.MatchHost("openapi.kpx.or.kr") {
		t.Fatal("expected kpx adapter to match openapi.kpx.or.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("kpx adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected kpx capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "openapi.kpx.or.kr" {
			t.Fatalf("expected normalized kpx https endpoint, got %s", req.URL.String())
		}
		if req.URL.Path != "/openapi/sukub5mMaxDatetime/getSukub5mMaxDatetime" {
			t.Fatalf("unexpected kpx path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected kpx query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><supply>1</supply></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15043670", Title: "현재전력수급현황", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "현재전력수급현황조회", Endpoint: "openapi.kpx.or.kr/openapi/sukub5mMaxDatetime/getSukub5mMaxDatetime"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-06-25T00:00:00Z",
	})
	if result.Provider != "kpx" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected kpx verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") || !strings.HasPrefix(result.URL, "https://openapi.kpx.or.kr/") {
		t.Fatalf("expected redacted normalized kpx URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected kpx public params: %#v", result.Params)
	}
}

func TestKPXAdapterClassifiesInvalidRequestParameter(t *testing.T) {
	adapter := NewKPXAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>10</resultCode><resultMsg>INVALID REQUEST PARAMETER ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "15051436", Title: "전력수급예보조회", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "전력수급예보조회", Endpoint: "https://openapi.kpx.or.kr/openapi/forecast1dMaxBaseDate/getForecast1dMaxBaseDate"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "kpx_invalid_request_parameter" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected kpx invalid request result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "10" {
		t.Fatalf("unexpected kpx provider status: %#v", result.ProviderStatus)
	}
}

func TestKPXAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewKPXAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.kpx.or.kr" {
			t.Fatalf("expected kpx host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected kpx call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><smp>1</smp></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:       datago.Spec{ID: "15065266", Title: "계통한계가격조회", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "계통한계가격조회", Endpoint: "https://openapi.kpx.or.kr/openapi/smp1hToday/getSmp1hToday"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "kpx" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected kpx call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected kpx provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted kpx call URL: %s", envelope.URL)
	}
}

func TestMyHomeAdapterClassifiesJSONStatusDespiteHTMLContentType(t *testing.T) {
	adapter := NewMyHomeAdapter()
	if !adapter.MatchHost("data.myhome.go.kr:443") {
		t.Fatal("expected myhome adapter to match data.myhome.go.kr:443")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("myhome adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected myhome capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "data.myhome.go.kr:443" || req.URL.Path != "/rentalHouseList" {
			t.Fatalf("unexpected myhome endpoint: %s", req.URL.String())
		}
		if req.URL.Query().Get("ServiceKey") != "secret" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected myhome query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"30","msg":"SERVICE KEY IS NOT REGISTERED ERROR."}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "15058476", Title: "공공임대주택 단지정보", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "임대주택목록 조회",
			Endpoint: "https://data.myhome.go.kr:443/rentalHouseList",
			RequestParams: []datago.Param{
				{Name: "ServiceKey"},
				{Name: "numOfRows"},
				{Name: "pageNo"},
			},
		},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-06-25T00:00:00Z",
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "myhome_service_key_not_registered" || result.BodyShape != "json" {
		t.Fatalf("unexpected myhome verification result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Source != "code/msg" || result.ProviderStatus.Code != "30" {
		t.Fatalf("unexpected myhome provider status: %#v", result.ProviderStatus)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted myhome URL: %s", result.URL)
	}
	if result.Params["ServiceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected myhome public params: %#v", result.Params)
	}
}

func TestMyHomeAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewMyHomeAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("ServiceKey") != "secret" || req.URL.Query().Get("brtcCode") != "11" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected myhome call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"00","msg":"NORMAL SERVICE.","data":[{"hsmpNm":"테스트"}]}`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "15058476", Title: "공공임대주택 단지정보", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "임대주택목록 조회",
			Endpoint: "https://data.myhome.go.kr:443/rentalHouseList",
			RequestParams: []datago.Param{
				{Name: "ServiceKey"},
				{Name: "numOfRows"},
				{Name: "pageNo"},
			},
		},
		Params:     map[string]string{"brtcCode": "11"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "myhome" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected myhome call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected myhome provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted myhome call URL: %s", envelope.URL)
	}
}

func TestEMuseumAdapterClassifiesXMLServiceKeyStatus(t *testing.T) {
	adapter := NewEMuseumAdapter()
	if !adapter.MatchHost("www.emuseum.go.kr") {
		t.Fatal("expected emuseum adapter to match www.emuseum.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("emuseum adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected emuseum capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.emuseum.go.kr" || req.URL.Path != "/openapi/relic/list" {
			t.Fatalf("unexpected emuseum endpoint: %s", req.URL.String())
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected emuseum query: %s", req.URL.RawQuery)
		}
		if req.Header.Get("User-Agent") != eMuseumUserAgent {
			t.Fatalf("expected emuseum user agent, got %q", req.Header.Get("User-Agent"))
		}
		if _, ok := req.URL.Query()["name"]; ok {
			t.Fatalf("expected empty optional filter to be omitted: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<result><params><item key="serviceKey" value="secret"/></params><numOfRows>10</numOfRows><pageNo>1</pageNo><totalCount>0</totalCount><resultCode>4030</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></result>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "3036708", Title: "eMuseum 샘플", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "소장품 목록 조회",
			Endpoint: "http://www.emuseum.go.kr/openapi/relic/list",
		},
		Params: map[string]string{"name": ""},
		MissingParams: []string{
			"id", "museumCode", "name", "nameKr", "nameEn", "nameCn", "author", "nationalityCode",
			"materialCode", "purposeCode", "sizeRangeCode", "placeLandCode", "designationCode", "indexWord",
		},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
		VerifiedAt: "2026-06-25T00:00:00Z",
	})
	if result.Provider != "emuseum" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "emuseum_service_key_not_registered" {
		t.Fatalf("unexpected emuseum verification result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.Code != "4030" || result.ProviderStatus.Source != "resultCode/resultMsg" {
		t.Fatalf("unexpected emuseum provider status: %#v", result.ProviderStatus)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || result.BodyShape != "xml_status" {
		t.Fatalf("expected redacted emuseum XML status URL: %#v", result)
	}
	if _, ok := result.Params["name"]; ok {
		t.Fatalf("expected empty emuseum filter to be omitted: %#v", result.Params)
	}
	if result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected emuseum public params: %#v", result.Params)
	}
}

func TestEMuseumAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewEMuseumAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "www.emuseum.go.kr" {
			t.Fatalf("expected emuseum host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected emuseum call query: %s", req.URL.RawQuery)
		}
		if req.Header.Get("User-Agent") != eMuseumUserAgent {
			t.Fatalf("expected emuseum user agent, got %q", req.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<result><numOfRows>1</numOfRows><pageNo>1</pageNo><totalCount>1</totalCount><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg><item><name>금동반가사유상</name></item></result>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "3036708", Title: "eMuseum 샘플", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "소장품 목록 조회",
			Endpoint: "http://www.emuseum.go.kr/openapi/relic/list",
		},
		MissingParams: []string{"id", "museumCode", "name", "nameKr", "nameEn", "nameCn", "author", "nationalityCode", "materialCode", "purposeCode", "sizeRangeCode", "placeLandCode", "designationCode", "indexWord"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "emuseum" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected emuseum call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected emuseum provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "금동반가사유상") {
		t.Fatalf("unexpected emuseum call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestJejuAdapterRewritesNightPharmacyListEndpoint(t *testing.T) {
	adapter := NewJejuAdapter()
	if !adapter.MatchHost("data.jeju.go.kr") {
		t.Fatal("expected jeju adapter to match data.jeju.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("jeju adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected jeju capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.jeju.go.kr" || req.URL.Path != "/rest/nightpharmacy/getNightPharmacyList" {
			t.Fatalf("unexpected jeju endpoint: %s", req.URL.String())
		}
		if req.URL.Query().Get("pageSize") != "1" || req.URL.Query().Get("startPage") != "1" {
			t.Fatalf("unexpected jeju query: %s", req.URL.RawQuery)
		}
		if _, ok := req.URL.Query()["dataTitle"]; ok {
			t.Fatalf("expected empty jeju filter to be omitted: %s", req.URL.RawQuery)
		}
		if req.Header.Get("User-Agent") != jejuUserAgent {
			t.Fatalf("expected jeju user agent, got %q", req.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><rfcOpenApi><header><resultCode>00</resultCode><resultMsg>success</resultMsg></header><body><pageSize>1</pageSize><startPage>1</startPage><totalCount>1</totalCount><data><list><dataSid>1</dataSid><dataTitle>현재약국</dataTitle></list></data></body></rfcOpenApi>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "15043696", Title: "제주 심야약국", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "심야약국 리스트 조회",
			Endpoint: "http://data.jeju.go.kr/rest/nightpharmacy",
		},
		MissingParams: []string{"dataTitle", "pageSize", "startPage"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "jeju" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_rfcopenapi_list" {
		t.Fatalf("unexpected jeju verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "/getNightPharmacyList") {
		t.Fatalf("expected redacted jeju list URL: %#v", result)
	}
	if result.Params["pageSize"] != "1" || result.Params["startPage"] != "1" {
		t.Fatalf("unexpected jeju public params: %#v", result.Params)
	}
	if _, ok := result.Params["dataTitle"]; ok {
		t.Fatalf("expected empty jeju filter to be omitted: %#v", result.Params)
	}
}

func TestJejuAdapterCallNormalizesSpacedEndpoint(t *testing.T) {
	adapter := NewJejuAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/rest/jejurest" {
			t.Fatalf("expected normalized jeju endpoint, got %s", req.URL.String())
		}
		if req.URL.Query().Get("checkIndate") != "20260701" {
			t.Fatalf("unexpected jeju call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 405,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<html><body>Method Not Allowed</body></html>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "15001189", Title: "제주교래자연휴양림", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "예약된 방 목록 조회",
			Endpoint: "http://data.jeju.go.kr/rest/ jejurest",
		},
		MissingParams: []string{"checkIndate"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Provider != "jeju" || envelope.StatusCode != 405 || envelope.SemanticStatus != "http_error" {
		t.Fatalf("unexpected jeju call envelope: %#v", envelope)
	}
	if !strings.Contains(envelope.URL, "/rest/jejurest") || !strings.Contains(envelope.Body, "Method Not Allowed") {
		t.Fatalf("unexpected jeju call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestPQISAdapterRewritesWADLEndpointAndDefaults(t *testing.T) {
	adapter := NewPQISAdapter()
	if !adapter.MatchHost("openapi.pqis.go.kr") {
		t.Fatal("expected pqis adapter to match openapi.pqis.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("pqis adapter should not match data.go.kr gateway")
	}
	if strings.Join(adapter.Capabilities(), ",") != "call" {
		t.Fatalf("unexpected pqis capabilities: %#v", adapter.Capabilities())
	}
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.pqis.go.kr" {
			t.Fatalf("expected pqis host, got %s", req.URL.Host)
		}
		if req.URL.Path != "/openapi/service/plntQrantStats/nationCode" {
			t.Fatalf("expected pqis nationCode path, got %s", req.URL.Path)
		}
		if req.URL.Query().Get("_wadl") != "" || req.URL.Query().Get("type") != "" {
			t.Fatalf("did not expect registry WADL query in provider call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("nationName") != "한국" || req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected pqis query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><nationCode>KR</nationCode></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "3055528", Title: "식물검역정보", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "국가코드", Endpoint: "http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml"},
		MissingParams: []string{"nationName"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "pqis" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected pqis verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "/nationCode") {
		t.Fatalf("expected redacted pqis URL with operation path: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["nationName"] != "한국" || result.Params["pageNo"] != "1" {
		t.Fatalf("unexpected pqis public params: %#v", result.Params)
	}
}

func TestPQISAdapterClassifiesServiceKeyFailures(t *testing.T) {
	adapter := NewPQISAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/openapi/service/plntQrantStats/plantCode" {
			t.Fatalf("expected pqis plantCode path, got %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><response><header><resultCode>99</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "3055528", Title: "식물검역정보", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "식물코드", Endpoint: "http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml"},
		MissingParams: []string{"plantName"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "pqis_service_key_not_registered" || result.BodyShape != "xml_status" {
		t.Fatalf("unexpected pqis service key result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "99" {
		t.Fatalf("unexpected pqis provider status: %#v", result.ProviderStatus)
	}
}

func TestPQISAdapterClassifiesNotFoundEndpoint(t *testing.T) {
	adapter := NewPQISAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(``)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "3055528", Title: "식물검역정보", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "수입식물검역통계", Endpoint: "http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml"},
		MissingParams: []string{"fromYYYYMM", "toYYYYMM", "nationCode", "plantCode"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "http_error" || result.Reason != "pqis_endpoint_not_found" || result.HTTPStatus != 404 {
		t.Fatalf("unexpected pqis not found result: %#v", result)
	}
}

func TestPQISAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewPQISAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/openapi/service/plntQrantStats/importStats" {
			t.Fatalf("expected pqis importStats path, got %s", req.URL.Path)
		}
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("fromYYYYMM") != "202501" || req.URL.Query().Get("toYYYYMM") != "202501" || req.URL.Query().Get("nationCode") != "CN" || req.URL.Query().Get("plantCode") != "1000" {
			t.Fatalf("unexpected pqis call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><plantNm>사과</plantNm></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:          datago.Spec{ID: "3055528", Title: "식물검역정보", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "수입식물검역통계", Endpoint: "http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml"},
		MissingParams: []string{"fromYYYYMM", "toYYYYMM", "nationCode", "plantCode"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "pqis" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected pqis call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected pqis provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.URL, "/importStats") {
		t.Fatalf("expected redacted pqis call URL: %s", envelope.URL)
	}
}

func TestTourAdapterAddsServiceKeyAndDefaults(t *testing.T) {
	adapter := NewTourAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.tour.go.kr" {
			t.Fatalf("expected tour host, got %s", req.URL.Host)
		}
		if req.URL.Path != "/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList" {
			t.Fatalf("unexpected tour path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in tour request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("YM") != "202401" {
			t.Fatalf("unexpected tour YM query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><ym>202401</ym></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "951", Title: "출입국 관광수지 서비스", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name: "getTourismBalcList",
			Source: &datago.Source{Raw: map[string]any{
				"operation_url": "http://openapi.tour.go.kr/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList",
			}},
		},
		MissingParams: []string{"serviceKey", "YM"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "tour" || result.EndpointHost != "openapi.tour.go.kr" || result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected tour verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted tour URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["YM"] != "202401" {
		t.Fatalf("unexpected tour public params: %#v", result.Params)
	}
}

func TestTourAdapterClassifiesXMLFault(t *testing.T) {
	adapter := NewTourAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Header:     http.Header{"Content-Type": []string{"text/xml; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(`<ns1:XMLFault><faultstring>java.lang.IllegalArgumentException: 지원하지 않는 인증 방식이거나 인증키가 누락되었습니다. PUBC 인증(?serviceKey=) 또는 Gateway 인증(?SG_APIM=)을 사용하세요.</faultstring></ns1:XMLFault>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "952", Title: "출입국 관광수지 서비스", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name: "getTourismBalcList",
			Source: &datago.Source{Raw: map[string]any{
				"operation_url": "http://openapi.tour.go.kr/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList",
			}},
		},
		MissingParams: []string{"YM"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "tour_missing_auth" || result.BodyShape != "xml_fault" || result.HTTPStatus != 500 {
		t.Fatalf("unexpected tour XMLFault result: %#v", result)
	}
}

func TestTourAdapterClassifiesUnregisteredServiceKey(t *testing.T) {
	adapter := NewTourAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>30</resultCode><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "952-1", Title: "출입국 관광수지 서비스", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name: "getTourismBalcList",
			Source: &datago.Source{Raw: map[string]any{
				"operation_url": "http://openapi.tour.go.kr/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList",
			}},
		},
		MissingParams: []string{"YM"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if result.Status != "failed" || result.Reason != "tour_service_key_not_registered" || result.SemanticStatus != "provider_error" {
		t.Fatalf("unexpected tour key-registration result: %#v", result)
	}
}

func TestTourAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewTourAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" || req.URL.Query().Get("YM") != "202401" {
			t.Fatalf("unexpected tour call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><balance>1</balance></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "953", Title: "출입국 관광수지 서비스", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name: "getTourismBalcList",
			Source: &datago.Source{Raw: map[string]any{
				"operation_url": "http://openapi.tour.go.kr/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList",
			}},
		},
		MissingParams: []string{"YM"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "tour" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected tour call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected tour provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "balance") {
		t.Fatalf("unexpected tour call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestUlsanAdapterAddsServiceKeyAndClassifiesRegistrationErrors(t *testing.T) {
	adapter := NewUlsanAdapter()
	if !adapter.MatchHost("openapi.its.ulsan.kr") {
		t.Fatal("expected ulsan adapter to match openapi.its.ulsan.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("ulsan adapter should not match data.go.kr gateway")
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.its.ulsan.kr" {
			t.Fatalf("expected ulsan host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected synthesized serviceKey in ulsan request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected ulsan paging query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<Response><error><resultMsg>SERVICE KEY IS NOT REGISTERED ERROR.</resultMsg><resultCode>30</resultCode></error></Response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "800", Title: "울산광역시 BIS 정보", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "노선정보 조회",
			Endpoint: "http://openapi.its.ulsan.kr/UlsanAPI/RouteInfo.xo",
			RequestParams: []datago.Param{
				{Name: "pageNo"},
				{Name: "numOfRows"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-25T00:00:00Z",
	})
	if result.Provider != "ulsan" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "ulsan_service_key_not_registered" {
		t.Fatalf("unexpected ulsan verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted ulsan URL: %s", result.URL)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" || result.BodyShape != "xml_error" {
		t.Fatalf("unexpected ulsan public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestUlsanAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewUlsanAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "801", Title: "울산 노선별정류장", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "노선별정류장정보 조회",
			Endpoint: "http://openapi.its.ulsan.kr/UlsanAPI/AllRouteDetailInfo.xo",
		},
		MissingParams: []string{"Routeid", "pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "ulsan_missing_required_params" || len(result.MissingParams) != 1 || result.MissingParams[0] != "Routeid" {
		t.Fatalf("unexpected ulsan skip result: %#v", result)
	}
}

func TestUlsanAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewUlsanAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in ulsan call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" {
			t.Fatalf("unexpected ulsan call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<Response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><routeNo>401</routeNo></item></items></body></Response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "802", Title: "울산 노선", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "노선정보 조회",
			Endpoint: "http://openapi.its.ulsan.kr/UlsanAPI/RouteInfo.xo",
			RequestParams: []datago.Param{
				{Name: "pageNo"},
				{Name: "numOfRows"},
			},
		},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "ulsan" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected ulsan call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected ulsan provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "serviceKey=REDACTED") || !strings.Contains(envelope.Body, "<routeNo>401</routeNo>") {
		t.Fatalf("unexpected ulsan call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestUlsanAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewUlsanAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "803", Title: "울산 transport", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "노선정보 조회",
			Endpoint: "http://openapi.its.ulsan.kr/UlsanAPI/RouteInfo.xo",
		},
		MissingParams: []string{"pageNo"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "serviceKey=REDACTED") {
		t.Fatalf("expected redacted ulsan transport error, got %v", err)
	}
}

func TestJeonjuAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewJeonjuAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.jeonju.go.kr" {
			t.Fatalf("expected jeonju host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("ServiceKey") != "secret" {
			t.Fatalf("expected ServiceKey in jeonju call: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("loadResult") != "백제대로" {
			t.Fatalf("unexpected jeonju call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><name>백제대로</name></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "601", Title: "Jeonju 도로", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "도로별 현황 상세 정보",
			Endpoint: "http://openapi.jeonju.go.kr/jeonjubus/openApi/traffic",
			RequestParams: []datago.Param{
				{Name: "ServiceKey"},
				{Name: "loadResult"},
			},
		},
		MissingParams: []string{"loadResult"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "jeonju" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected jeonju call envelope: %#v", envelope)
	}
	if envelope.ProviderStatus == nil || !envelope.ProviderStatus.OK || envelope.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected jeonju provider status: %#v", envelope.ProviderStatus)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "ServiceKey=REDACTED") || !strings.Contains(envelope.Body, "<name>백제대로</name>") {
		t.Fatalf("unexpected jeonju call URL/body: url=%s body=%s", envelope.URL, envelope.Body)
	}
}

func TestJeonjuAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewJeonjuAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "602", Title: "Jeonju 상세", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "상세", Endpoint: "http://openapi.jeonju.go.kr/rest/detail"},
		MissingParams: []string{"unknownRequired"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "jeonju_missing_required_params" || len(result.MissingParams) != 1 {
		t.Fatalf("unexpected jeonju skip result: %#v", result)
	}
}

func TestJeonjuAdapterClassifiesHTTPMethodFailures(t *testing.T) {
	adapter := NewJeonjuAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 405,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "604", Title: "Jeonju method", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "무선인터넷존",
			Endpoint: "http://openapi.jeonju.go.kr/rest/wifizone",
			RequestParams: []datago.Param{
				{Name: "authApiKey"},
				{Name: "posy"},
				{Name: "posx"},
				{Name: "searchDts"},
			},
		},
		MissingParams: []string{"posy", "posx", "searchDts"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "http_error" || result.Reason != "jeonju_http_405_method_not_allowed" {
		t.Fatalf("unexpected jeonju method failure: %#v", result)
	}
}

func TestJeonjuAdapterCallRedactsTransportErrors(t *testing.T) {
	adapter := NewJeonjuAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: context deadline exceeded", req.URL.String())
	})
	_, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "603", Title: "Jeonju error", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "무선인터넷존",
			Endpoint: "http://openapi.jeonju.go.kr/rest/wifizone",
			RequestParams: []datago.Param{
				{Name: "authApiKey"},
				{Name: "posy"},
				{Name: "posx"},
				{Name: "searchDts"},
			},
		},
		MissingParams: []string{"posy", "posx", "searchDts"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "authApiKey=REDACTED") {
		t.Fatalf("expected redacted jeonju transport error, got %v", err)
	}
}

func TestFolkAdapterOwnsKnownHostsConservatively(t *testing.T) {
	adapter := NewFolkAdapter()
	if !adapter.MatchHost("folkency.nfm.go.kr") {
		t.Fatal("expected folk adapter to match folkency.nfm.go.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("folk adapter should not match data.go.kr gateway")
	}
	spec := datago.Spec{ID: "600", Title: "Folk 샘플", Provider: "data.go.kr"}
	op := datago.Operation{Name: "사진목록", Endpoint: "https://folkency.nfm.go.kr/api/FolkTradClturMltmd/getPhotoList"}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in provider request")
		}
		if req.URL.Query().Get("korname") != "소나무" || req.URL.Query().Get("page") != "1" || req.URL.Query().Get("size") != "1" {
			t.Fatalf("unexpected folk query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":{"total":1,"list":[{"tit_idx":10039,"korname":"소나무"}]},"message":null,"result_code":200}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          spec,
		Operation:     op,
		MissingParams: []string{"dictionary", "tit_idx", "korname", "summary", "page", "size"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Provider != "folk" || result.Status != "verified" || result.SemanticStatus != "provider_ok" {
		t.Fatalf("unexpected folk verification result: %#v", result)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") {
		t.Fatalf("expected redacted folk URL: %s", result.URL)
	}
	if result.ProviderStatus == nil || !result.ProviderStatus.OK || result.ProviderStatus.Code != "200" {
		t.Fatalf("unexpected folk provider status: %#v", result.ProviderStatus)
	}
	if result.Params["serviceKey"] != "" || result.Params["korname"] != "소나무" || result.Params["size"] != "1" || result.BodyShape != "json_items" {
		t.Fatalf("unexpected folk public params/body shape: params=%#v shape=%s", result.Params, result.BodyShape)
	}
}

func TestAirportAdapterClassifiesServiceKeyRegistrationErrors(t *testing.T) {
	adapter := NewAirportAdapter()
	if !adapter.MatchHost("openapi.airport.co.kr") {
		t.Fatal("expected airport adapter to match openapi.airport.co.kr")
	}
	if adapter.MatchHost("apis.data.go.kr") {
		t.Fatal("airport adapter should not match data.go.kr gateway")
	}
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("expected serviceKey in airport request: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":99,"resultMsg":"SERVICE KEY IS NOT REGISTERED ERROR."}}}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "700", Title: "Airport low visibility", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "latest", Endpoint: "http://openapi.airport.co.kr/service/rest/airportLowVisibility/getAirportLowVisibilityLast"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       httpClient,
		VerifiedAt: "2026-06-24T00:00:00Z",
	})
	if result.Provider != "airport" || result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "airport_service_key_not_registered" {
		t.Fatalf("unexpected airport result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.OK || result.ProviderStatus.Code != "99" {
		t.Fatalf("unexpected airport provider status: %#v", result.ProviderStatus)
	}
	if result.URL == "" || strings.Contains(result.URL, "secret") || result.BodyShape != "json_status" {
		t.Fatalf("unexpected airport URL/body shape: url=%s shape=%s", result.URL, result.BodyShape)
	}
}

func TestAirportAdapterSuppliesSafePagingDefaults(t *testing.T) {
	adapter := NewAirportAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("pageNo") != "1" || req.URL.Query().Get("numOfRows") != "1" || req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("unexpected airport query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><airport>GMP</airport></item></items></body></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "701", Title: "Airport list", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "list", Endpoint: "http://openapi.airport.co.kr/service/rest/airportLowVisibility/getAirportLowVisibility"},
		MissingParams: []string{"pageNo", "numOfRows"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_items" {
		t.Fatalf("unexpected airport success result: %#v", result)
	}
	if result.Params["serviceKey"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
		t.Fatalf("unexpected airport params: %#v", result.Params)
	}
}

func TestFolkAdapterSkipsDetailEndpointsWithoutIdentifiers(t *testing.T) {
	adapter := NewFolkAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "601", Title: "Folk detail", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "사진상세", Endpoint: "https://folkency.nfm.go.kr/api/FolkTradClturMltmd/getPhotoItemList"},
		MissingParams: []string{"tit_idx", "group_name", "md_idx"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "folk_missing_required_params" {
		t.Fatalf("unexpected folk detail skip result: %#v", result)
	}
}

func TestFolkAdapterDoesNotSendEmptySoundFilters(t *testing.T) {
	adapter := NewFolkAdapter()
	httpClient := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("korname") != "" || req.URL.Query().Get("dictionary") != "" || req.URL.Query().Get("tit_idx") != "" || req.URL.Query().Get("summary") != "" {
			t.Fatalf("unexpected empty folk filter query params: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("page") != "1" || req.URL.Query().Get("size") != "1" || req.URL.Query().Get("serviceKey") != "secret" {
			t.Fatalf("unexpected folk sound query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":{"total":1,"list":[{"title":"sample"}]},"message":null,"result_code":200}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "602", Title: "Folk sound", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "음원목록", Endpoint: "https://folkency.nfm.go.kr/api/FolkTradClturMltmd/getSoundList"},
		MissingParams: []string{"dictionary", "tit_idx", "korname", "summary", "page", "size"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          httpClient,
		VerifiedAt:    "2026-06-24T00:00:00Z",
	})
	if result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "json_items" {
		t.Fatalf("unexpected folk sound result: %#v", result)
	}
	if _, ok := result.Params["korname"]; !ok || result.Params["korname"] != "" {
		t.Fatalf("expected public params to record empty korname default: %#v", result.Params)
	}
}

func TestQNetAdapterSkipsUnknownRequiredParams(t *testing.T) {
	adapter := NewQNetAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "201", Title: "Q-Net 상세", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "상세", Endpoint: "https://openapi.q-net.or.kr/api/detail"},
		MissingParams: []string{"jmCd"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "qnet_missing_required_params" || len(result.MissingParams) != 1 {
		t.Fatalf("unexpected q-net skip result: %#v", result)
	}
}

func TestQNetAdapterSkipsApprovalRequiredOperations(t *testing.T) {
	adapter := NewQNetAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "202", Title: "Q-Net 승인", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "승인 필요",
			Endpoint: "https://openapi.q-net.or.kr/api/list",
			Source:   &datago.Source{Raw: map[string]any{"is_confirmed_for_prod_nm": "심의승인"}},
		},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "approval_required" {
		t.Fatalf("unexpected approval skip result: %#v", result)
	}
}

func TestQNetAdapterSkipsWADLMetadataEndpoints(t *testing.T) {
	adapter := NewQNetAdapter()
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "203", Title: "Q-Net WADL", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "메타데이터", Endpoint: "http://openapi.q-net.or.kr/api/service/rest/InquiryListNationalQualifcationSVC?_wadl&_type=xml"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
	})
	if result.Status != "skipped" || result.Reason != "qnet_wadl_metadata_only" {
		t.Fatalf("unexpected WADL skip result: %#v", result)
	}
}

func TestQNetAdapterSkipsCNetUntilSeparateKeyEvidence(t *testing.T) {
	adapter := NewQNetAdapter()
	called := false
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "204", Title: "Q-Net C", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "C계열", Endpoint: "https://c.q-net.or.kr/openapi/cwyearlcslist.do"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	})
	if called {
		t.Fatal("c.q-net verification should skip before HTTP until credential evidence exists")
	}
	if result.Status != "skipped" || result.Reason != "qnet_separate_service_key_required" {
		t.Fatalf("unexpected c.q-net skip result: %#v", result)
	}
}

func TestQNetAdapterFailsJSONMessageErrors(t *testing.T) {
	adapter := NewQNetAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"message":"SERVICE KEY IS NOT REGISTERED ERROR."}`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "205", Title: "Q-Net JSON", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "JSON", Endpoint: "https://open.api.q-net.or.kr/api/list"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "qnet_service_key_not_registered" {
		t.Fatalf("unexpected JSON message result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.Source != "message" || result.ProviderStatus.OK || result.ProviderStatus.ReasonCode != "qnet_service_key_not_registered" {
		t.Fatalf("unexpected provider status: %#v", result.ProviderStatus)
	}
}

func TestQNetAdapterClassifiesConnectionValidationFailures(t *testing.T) {
	adapter := NewQNetAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>99</resultCode><resultMsg>Failed to validate a newly established connection.</resultMsg></header><body/></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:       datago.Spec{ID: "206", Title: "Q-Net 99", Provider: "data.go.kr"},
		Operation:  datago.Operation{Name: "99", Endpoint: "https://openapi.q-net.or.kr/api/list"},
		Credential: Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:       client,
	})
	if result.Status != "failed" || result.Reason != "qnet_connection_validation_failed" {
		t.Fatalf("unexpected connection validation result: %#v", result)
	}
	if result.ProviderStatus == nil || result.ProviderStatus.Code != "99" || result.ProviderStatus.ReasonCode != "qnet_connection_validation_failed" {
		t.Fatalf("unexpected provider status: %#v", result.ProviderStatus)
	}
}

func TestNAQSAdapterSkipsMutationEndpoint(t *testing.T) {
	adapter := NewNAQSAdapter()
	called := false
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:      datago.Spec{ID: "700", Title: "NAQS pubc", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "상품속성정보연계", Endpoint: "http://data.naqs.go.kr/pubc"},
		HTTP: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	})
	if called {
		t.Fatal("naqs pubc endpoint should skip before HTTP")
	}
	if result.Status != "skipped" || result.Reason != "naqs_mutation_endpoint" || result.BodyShape != "html_portal" {
		t.Fatalf("unexpected naqs mutation skip: %#v", result)
	}
}

func TestNAQSAdapterVerifiesEnvResponseWithoutAuth(t *testing.T) {
	adapter := NewNAQSAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.naqs.go.kr" {
			t.Fatalf("expected naqs host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "" {
			t.Fatalf("naqs should not synthesize serviceKey: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<?xml version='1.0' encoding='UTF-8'?><GetEnvResponse><body/><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header></GetEnvResponse>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec:          datago.Spec{ID: "701", Title: "NAQS env", Provider: "data.go.kr"},
		Operation:     datago.Operation{Name: "친환경인증정보", Endpoint: "http://data.naqs.go.kr/openapi/service/rest/naqsenv/envparam"},
		MissingParams: []string{"certno"},
		HTTP:          client,
	})
	if result.Status != "verified" || result.SemanticStatus != "provider_ok" || result.BodyShape != "xml_env_response" {
		t.Fatalf("unexpected naqs verification result: %#v", result)
	}
	if result.ProviderStatus == nil || !result.ProviderStatus.OK || result.ProviderStatus.Code != "00" {
		t.Fatalf("unexpected naqs provider status: %#v", result.ProviderStatus)
	}
}

func TestNAQSAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewNAQSAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("certno") != "1" {
			t.Fatalf("expected certno in naqs call: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<GetEnvResponse><body/><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header></GetEnvResponse>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec:      datago.Spec{ID: "702", Title: "NAQS env", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "친환경인증정보", Endpoint: "http://data.naqs.go.kr/openapi/service/rest/naqsenv/envparam"},
		Params:    map[string]string{"certno": "1"},
		HTTP:      client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "naqs" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected naqs call envelope: %#v", envelope)
	}
	if !strings.Contains(envelope.URL, "certno=1") || strings.Contains(envelope.URL, "serviceKey") {
		t.Fatalf("unexpected naqs call URL: %s", envelope.URL)
	}
}

func TestHumetroAdapterAddsServiceKeyAndClassifiesAccessDenied(t *testing.T) {
	adapter := NewHumetroAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "data.humetro.busan.kr" {
			t.Fatalf("expected humetro host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("ServiceKey") != "secret" || req.URL.Query().Get("act") != "xml" || req.URL.Query().Get("scode") != "101" {
			t.Fatalf("unexpected humetro query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>20</resultCode><resultMsg>SERVICE ACCESS DENIED ERROR.</resultMsg></header></response>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "800", Title: "Humetro", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "부산 도시철도 공공 시설물 정보",
			Endpoint: "http://data.humetro.busan.kr/voc/api/open_api_public.tnn",
		},
		MissingParams: []string{"act", "scode"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "failed" || result.SemanticStatus != "provider_error" || result.Reason != "humetro_service_access_denied" {
		t.Fatalf("unexpected humetro verification result: %#v", result)
	}
	if strings.Contains(result.URL, "secret") || !strings.Contains(result.URL, "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted humetro URL: %s", result.URL)
	}
}

func TestHumetroAdapterCallExecutesProviderRequest(t *testing.T) {
	adapter := NewHumetroAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("kind") != "1" || req.URL.Query().Get("c_page") != "1" || req.URL.Query().Get("c_size") != "1" {
			t.Fatalf("unexpected humetro call query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><name>sample</name></item></items></body></response>`)),
		}, nil
	})
	envelope, err := adapter.Call(context.Background(), CallRequest{
		Spec: datago.Spec{ID: "801", Title: "Humetro contract", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "부산 도시철도 계약현황 정보",
			Endpoint: "http://data.humetro.busan.kr/voc/api/open_api_contractinfo.tnn",
		},
		MissingParams: []string{"act", "kind", "c_page", "c_size"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Provider != "humetro" || envelope.SemanticStatus != "provider_ok" || envelope.StatusCode != 200 {
		t.Fatalf("unexpected humetro call envelope: %#v", envelope)
	}
	if strings.Contains(envelope.URL, "secret") || !strings.Contains(envelope.URL, "ServiceKey=REDACTED") {
		t.Fatalf("expected redacted humetro call URL: %s", envelope.URL)
	}
}

func TestOneclickLawAdapterPostsSOAPEnvelope(t *testing.T) {
	adapter := NewOneclickLawAdapter()
	client := providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected SOAP POST, got %s", req.Method)
		}
		if req.URL.Host != "oneclick.law.go.kr:80" {
			t.Fatalf("expected oneclick host, got %s", req.URL.Host)
		}
		if req.Header.Get("SOAPAction") != "getSearchGroupList" {
			t.Fatalf("unexpected SOAPAction: %s", req.Header.Get("SOAPAction"))
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range []string{"<getSearchGroupList>", "<ServiceKey>secret</ServiceKey>", "<RequestMsgID>datapan</RequestMsgID>", "<RequestTime>20000101000000</RequestTime>"} {
			if !strings.Contains(text, want) {
				t.Fatalf("expected SOAP body to contain %q: %s", want, text)
			}
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<soapenv:Envelope><soapenv:Body><getSearchGroupListResponse><ReturnCode>0</ReturnCode><ErrMsg></ErrMsg></getSearchGroupListResponse></soapenv:Body></soapenv:Envelope>`)),
		}, nil
	})
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "900", Title: "Oneclick search", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "검색분류목록조회",
			Endpoint: "http://oneclick.law.go.kr:80/OPENAPI/soap/LifeLawSearchService",
			Source: &datago.Source{Raw: map[string]any{
				"api_type":            "SOAP",
				"operation_url":       "getSearchGroupList",
				"request_param_nm_en": "RequestMsgID,ServiceKey,RequestTime,CallBackURI",
			}},
		},
		MissingParams: []string{"RequestMsgID", "ServiceKey", "RequestTime", "CallBackURI"},
		Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
		HTTP:          client,
	})
	if result.Status != "verified" || result.SemanticStatus != "xml_response" || result.BodyShape != "soap_envelope" {
		t.Fatalf("unexpected oneclick verification result: %#v", result)
	}
	if strings.Contains(result.URL, "secret") || result.URL != "http://oneclick.law.go.kr:80/OPENAPI/soap/LifeLawSearchService" {
		t.Fatalf("unexpected oneclick URL: %s", result.URL)
	}
}

func TestOneclickLawAdapterSkipsApprovalRequired(t *testing.T) {
	adapter := NewOneclickLawAdapter()
	called := false
	result := adapter.Verify(context.Background(), VerificationRequest{
		Spec: datago.Spec{ID: "901", Title: "Oneclick approval", Provider: "data.go.kr"},
		Operation: datago.Operation{
			Name:     "생활분야목록조회",
			Endpoint: "http://oneclick.law.go.kr:80/OPENAPI/soap/LifeLawInfoService",
			Source: &datago.Source{Raw: map[string]any{
				"api_type":                 "SOAP",
				"operation_url":            "getLifeAreaList",
				"is_confirmed_for_dev_nm":  "심의승인",
				"is_confirmed_for_prod_nm": "심의승인",
			}},
		},
		HTTP: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	})
	if called {
		t.Fatal("oneclick approval-required operation should skip before HTTP")
	}
	if result.Status != "skipped" || result.Reason != "approval_required" {
		t.Fatalf("unexpected oneclick approval skip: %#v", result)
	}
}

func TestOneclickLawAdapterClassifiesConnectionRefused(t *testing.T) {
	adapter := NewOneclickLawAdapter()
	for _, message := range []string{
		"dial tcp: connection refused",
		"dial tcp: No connection could be made because the target machine actively refused it.",
	} {
		result := adapter.Verify(context.Background(), VerificationRequest{
			Spec: datago.Spec{ID: "902", Title: "Oneclick down", Provider: "data.go.kr"},
			Operation: datago.Operation{
				Name:     "검색분류목록조회",
				Endpoint: "http://oneclick.law.go.kr:80/OPENAPI/soap/LifeLawSearchService",
				Source: &datago.Source{Raw: map[string]any{
					"api_type":            "SOAP",
					"operation_url":       "getSearchGroupList",
					"request_param_nm_en": "RequestMsgID,ServiceKey,RequestTime,CallBackURI",
				}},
			},
			MissingParams: []string{"RequestMsgID", "ServiceKey", "RequestTime", "CallBackURI"},
			Credential:    Credential{Name: "DATA_PORTAL_API_KEY", Value: "secret"},
			HTTP: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("%s", message)
			}),
		})
		if result.Status != "failed" || result.Reason != "oneclick_connection_refused" {
			t.Fatalf("unexpected oneclick transport result for %q: %#v", message, result)
		}
	}
}
