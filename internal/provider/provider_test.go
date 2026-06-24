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
