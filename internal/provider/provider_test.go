package provider

import (
	"context"
	"testing"

	"github.com/StatPan/datapan-cli/internal/datago"
)

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
