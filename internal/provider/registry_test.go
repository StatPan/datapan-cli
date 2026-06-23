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

func TestDefaultRegistryIncludesQNetAdapter(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	adapter, ok := registry.MatchHost("openapi.q-net.or.kr")
	if !ok {
		t.Fatal("expected default registry to match q-net host")
	}
	if adapter.Name() != "q-net" {
		t.Fatalf("adapter=%s", adapter.Name())
	}
	report := registry.IndexReport("2026-06-24T00:00:00Z", "test")
	if report.AdapterCount != 1 || report.HostCount != 3 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if len(report.Adapters) != 1 || report.Adapters[0].Name != "q-net" || report.Adapters[0].Status != "registered" {
		t.Fatalf("unexpected provider index adapter: %#v", report)
	}
	if strings.Join(report.Adapters[0].Hosts, ",") != "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr" {
		t.Fatalf("unexpected provider index hosts: %#v", report.Adapters[0].Hosts)
	}
}
