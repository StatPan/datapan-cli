package provider

import (
	"context"
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
