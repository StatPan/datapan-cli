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
	if result.Params["name"] != "" || result.Params["pageNo"] != "1" || result.Params["numOfRows"] != "1" {
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
