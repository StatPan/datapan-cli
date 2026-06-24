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

func TestDefaultRegistryIncludesExternalAdapters(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for host, name := range map[string]string{
		"openapi.q-net.or.kr": "q-net",
		"openapi.epost.go.kr": "epost",
		"data.ekape.or.kr":    "ekape",
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
	if report.AdapterCount != 3 || report.HostCount != 6 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if len(report.Adapters) != 3 || report.Adapters[0].Name != "ekape" || report.Adapters[1].Name != "epost" || report.Adapters[2].Name != "q-net" {
		t.Fatalf("unexpected provider index adapter: %#v", report)
	}
	if report.Adapters[0].Status != "registered" || report.Adapters[1].Status != "registered" || report.Adapters[2].Status != "registered" {
		t.Fatalf("unexpected provider index adapter status: %#v", report)
	}
	if strings.Join(report.Adapters[0].Hosts, ",") != "data.ekape.or.kr" {
		t.Fatalf("unexpected ekape provider index hosts: %#v", report.Adapters[0].Hosts)
	}
	if strings.Join(report.Adapters[1].Hosts, ",") != "openapi.epost.go.kr,openapi.epost.go.kr:80" {
		t.Fatalf("unexpected epost provider index hosts: %#v", report.Adapters[1].Hosts)
	}
	if strings.Join(report.Adapters[2].Hosts, ",") != "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr" {
		t.Fatalf("unexpected q-net provider index hosts: %#v", report.Adapters[2].Hosts)
	}
}
