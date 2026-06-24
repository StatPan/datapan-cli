package provider

import (
	"strings"
	"testing"
)

func TestRegistryMatchesAdaptersByHost(t *testing.T) {
	registry, err := NewRegistry(
		fakeAdapter{StaticHostMatcher{Hosts: []string{"api.example.test"}}},
		namedFakeAdapter{name: "second", hosts: []string{"second.example.test"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter, ok := registry.MatchHost("API.EXAMPLE.TEST")
	if !ok {
		t.Fatal("expected adapter match")
	}
	if adapter.Name() != "fake" {
		t.Fatalf("adapter=%s", adapter.Name())
	}
	if _, ok := registry.MatchHost("missing.example.test"); ok {
		t.Fatal("unexpected adapter match")
	}
	hosts := strings.Join(registry.Hosts(), ",")
	if hosts != "api.example.test,second.example.test" {
		t.Fatalf("hosts=%s", hosts)
	}
}

func TestRegistryRejectsDuplicateHostAcrossAdapters(t *testing.T) {
	_, err := NewRegistry(
		fakeAdapter{StaticHostMatcher{Hosts: []string{"api.example.test"}}},
		namedFakeAdapter{name: "duplicate", hosts: []string{"API.EXAMPLE.TEST"}},
	)
	if err == nil {
		t.Fatal("expected duplicate host error")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryRejectsEmptyAdapterName(t *testing.T) {
	_, err := NewRegistry(namedFakeAdapter{name: "", hosts: []string{"api.example.test"}})
	if err == nil {
		t.Fatal("expected empty adapter name error")
	}
}

func TestRegistryIndexMergesDeclaredCapabilitiesWithVerification(t *testing.T) {
	registry, err := NewRegistry(
		capabilityFakeAdapter{
			namedFakeAdapter: namedFakeAdapter{name: "callish", hosts: []string{"call.example.test"}},
			capabilities:     []string{"call", " verification ", "call"},
		},
		namedFakeAdapter{name: "plain", hosts: []string{"plain.example.test"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	report := registry.IndexReport("2026-06-24T00:00:00Z", "test")
	if strings.Join(report.Adapters[0].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected merged capabilities: %#v", report.Adapters[0].Capabilities)
	}
	if !report.SplitReadiness.Ready || report.SplitReadiness.CallCapableAdapters != 1 || report.SplitReadiness.VerificationCapableAdapters != 2 {
		t.Fatalf("unexpected split readiness: %#v", report.SplitReadiness)
	}
}

func TestDefaultRegistryIncludesExternalAdapters(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for host, name := range map[string]string{
		"openapi.airport.co.kr": "airport",
		"openapi.q-net.or.kr":   "q-net",
		"openapi.epost.go.kr":   "epost",
		"data.ekape.or.kr":      "ekape",
		"data.geoje.go.kr":      "geoje",
		"folkency.nfm.go.kr":    "folk",
		"api.forest.go.kr":      "forest",
		"openapi.jeonju.go.kr":  "jeonju",
	} {
		adapter, ok := registry.MatchHost(host)
		if !ok {
			t.Fatalf("expected default registry to match %s", host)
		}
		if adapter.Name() != name {
			t.Fatalf("adapter for %s=%s", host, adapter.Name())
		}
	}
	report := registry.IndexReport("2026-06-24T00:00:00Z", "test")
	if report.AdapterCount != 8 || report.HostCount != 11 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if !report.SplitReadiness.Ready {
		t.Fatalf("provider split should be ready after forest call capability is declared: %#v", report.SplitReadiness)
	}
	if report.SplitReadiness.Status != "ready" || report.SplitReadiness.AdapterCount != 8 || report.SplitReadiness.VerificationCapableAdapters != 8 || report.SplitReadiness.CallCapableAdapters != 3 {
		t.Fatalf("unexpected split readiness: %#v", report.SplitReadiness)
	}
	if len(report.SplitReadiness.Reasons) != 0 {
		t.Fatalf("unexpected split readiness reasons: %#v", report.SplitReadiness.Reasons)
	}
	if len(report.Adapters) != 8 || report.Adapters[0].Name != "airport" || report.Adapters[1].Name != "ekape" || report.Adapters[2].Name != "epost" || report.Adapters[3].Name != "folk" || report.Adapters[4].Name != "forest" || report.Adapters[5].Name != "geoje" || report.Adapters[6].Name != "jeonju" || report.Adapters[7].Name != "q-net" {
		t.Fatalf("unexpected provider index adapter: %#v", report)
	}
	if report.Adapters[0].Status != "registered" || report.Adapters[1].Status != "registered" || report.Adapters[2].Status != "registered" || report.Adapters[3].Status != "registered" || report.Adapters[4].Status != "registered" || report.Adapters[5].Status != "registered" || report.Adapters[6].Status != "registered" || report.Adapters[7].Status != "registered" {
		t.Fatalf("unexpected provider index adapter status: %#v", report)
	}
	if strings.Join(report.Adapters[0].Hosts, ",") != "openapi.airport.co.kr" {
		t.Fatalf("unexpected airport provider index hosts: %#v", report.Adapters[0].Hosts)
	}
	if strings.Join(report.Adapters[1].Hosts, ",") != "data.ekape.or.kr" {
		t.Fatalf("unexpected ekape provider index hosts: %#v", report.Adapters[0].Hosts)
	}
	if strings.Join(report.Adapters[2].Hosts, ",") != "openapi.epost.go.kr,openapi.epost.go.kr:80" {
		t.Fatalf("unexpected epost provider index hosts: %#v", report.Adapters[2].Hosts)
	}
	if strings.Join(report.Adapters[2].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected epost provider index capabilities: %#v", report.Adapters[2].Capabilities)
	}
	if strings.Join(report.Adapters[3].Hosts, ",") != "folkency.nfm.go.kr" {
		t.Fatalf("unexpected folk provider index hosts: %#v", report.Adapters[3].Hosts)
	}
	if strings.Join(report.Adapters[4].Hosts, ",") != "api.forest.go.kr" {
		t.Fatalf("unexpected forest provider index hosts: %#v", report.Adapters[4].Hosts)
	}
	if strings.Join(report.Adapters[4].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected forest provider index capabilities: %#v", report.Adapters[4].Capabilities)
	}
	if strings.Join(report.Adapters[5].Hosts, ",") != "data.geoje.go.kr" {
		t.Fatalf("unexpected geoje provider index hosts: %#v", report.Adapters[5].Hosts)
	}
	if strings.Join(report.Adapters[5].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected geoje provider index capabilities: %#v", report.Adapters[5].Capabilities)
	}
	if strings.Join(report.Adapters[6].Hosts, ",") != "openapi.jeonju.go.kr" {
		t.Fatalf("unexpected jeonju provider index hosts: %#v", report.Adapters[6].Hosts)
	}
	if strings.Join(report.Adapters[6].Capabilities, ",") != "verification" {
		t.Fatalf("unexpected jeonju provider index capabilities: %#v", report.Adapters[6].Capabilities)
	}
	if strings.Join(report.Adapters[7].Hosts, ",") != "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr" {
		t.Fatalf("unexpected q-net provider index hosts: %#v", report.Adapters[7].Hosts)
	}
}
