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
		"www.andong.go.kr":      "andong",
		"openapi.q-net.or.kr":   "q-net",
		"openapi.epost.go.kr":   "epost",
		"data.ekape.or.kr":      "ekape",
		"data.geoje.go.kr":      "geoje",
		"data.sisul.or.kr":      "sisul",
		"data.uiryeong.go.kr":   "uiryeong",
		"folkency.nfm.go.kr":    "folk",
		"api.forest.go.kr":      "forest",
		"open.itfind.or.kr":     "itfind",
		"openapi.jeonju.go.kr":  "jeonju",
		"www.korad.or.kr":       "korad",
		"data.naqs.go.kr":       "naqs",
		"openapi.its.ulsan.kr":  "ulsan",
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
	if report.AdapterCount != 15 || report.HostCount != 18 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if !report.SplitReadiness.Ready {
		t.Fatalf("provider split should be ready after forest call capability is declared: %#v", report.SplitReadiness)
	}
	if report.SplitReadiness.Status != "ready" || report.SplitReadiness.AdapterCount != 15 || report.SplitReadiness.VerificationCapableAdapters != 15 || report.SplitReadiness.CallCapableAdapters != 10 {
		t.Fatalf("unexpected split readiness: %#v", report.SplitReadiness)
	}
	if len(report.SplitReadiness.Reasons) != 0 {
		t.Fatalf("unexpected split readiness reasons: %#v", report.SplitReadiness.Reasons)
	}
	if len(report.Adapters) != 15 || report.Adapters[0].Name != "airport" || report.Adapters[1].Name != "andong" || report.Adapters[2].Name != "ekape" || report.Adapters[3].Name != "epost" || report.Adapters[4].Name != "folk" || report.Adapters[5].Name != "forest" || report.Adapters[6].Name != "geoje" || report.Adapters[7].Name != "itfind" || report.Adapters[8].Name != "jeonju" || report.Adapters[9].Name != "korad" || report.Adapters[10].Name != "naqs" || report.Adapters[11].Name != "q-net" || report.Adapters[12].Name != "sisul" || report.Adapters[13].Name != "uiryeong" || report.Adapters[14].Name != "ulsan" {
		t.Fatalf("unexpected provider index adapter: %#v", report)
	}
	if report.Adapters[0].Status != "registered" || report.Adapters[1].Status != "registered" || report.Adapters[2].Status != "registered" || report.Adapters[3].Status != "registered" || report.Adapters[4].Status != "registered" || report.Adapters[5].Status != "registered" || report.Adapters[6].Status != "registered" || report.Adapters[7].Status != "registered" || report.Adapters[8].Status != "registered" || report.Adapters[9].Status != "registered" || report.Adapters[10].Status != "registered" || report.Adapters[11].Status != "registered" || report.Adapters[12].Status != "registered" || report.Adapters[13].Status != "registered" || report.Adapters[14].Status != "registered" {
		t.Fatalf("unexpected provider index adapter status: %#v", report)
	}
	if strings.Join(report.Adapters[0].Hosts, ",") != "openapi.airport.co.kr" {
		t.Fatalf("unexpected airport provider index hosts: %#v", report.Adapters[0].Hosts)
	}
	if strings.Join(report.Adapters[1].Hosts, ",") != "www.andong.go.kr" {
		t.Fatalf("unexpected andong provider index hosts: %#v", report.Adapters[1].Hosts)
	}
	if strings.Join(report.Adapters[1].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected andong provider index capabilities: %#v", report.Adapters[1].Capabilities)
	}
	if strings.Join(report.Adapters[2].Hosts, ",") != "data.ekape.or.kr" {
		t.Fatalf("unexpected ekape provider index hosts: %#v", report.Adapters[2].Hosts)
	}
	if strings.Join(report.Adapters[3].Hosts, ",") != "openapi.epost.go.kr,openapi.epost.go.kr:80" {
		t.Fatalf("unexpected epost provider index hosts: %#v", report.Adapters[3].Hosts)
	}
	if strings.Join(report.Adapters[3].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected epost provider index capabilities: %#v", report.Adapters[3].Capabilities)
	}
	if strings.Join(report.Adapters[4].Hosts, ",") != "folkency.nfm.go.kr" {
		t.Fatalf("unexpected folk provider index hosts: %#v", report.Adapters[4].Hosts)
	}
	if strings.Join(report.Adapters[5].Hosts, ",") != "api.forest.go.kr" {
		t.Fatalf("unexpected forest provider index hosts: %#v", report.Adapters[5].Hosts)
	}
	if strings.Join(report.Adapters[5].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected forest provider index capabilities: %#v", report.Adapters[5].Capabilities)
	}
	if strings.Join(report.Adapters[6].Hosts, ",") != "data.geoje.go.kr" {
		t.Fatalf("unexpected geoje provider index hosts: %#v", report.Adapters[6].Hosts)
	}
	if strings.Join(report.Adapters[6].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected geoje provider index capabilities: %#v", report.Adapters[6].Capabilities)
	}
	if strings.Join(report.Adapters[7].Hosts, ",") != "open.itfind.or.kr" {
		t.Fatalf("unexpected itfind provider index hosts: %#v", report.Adapters[7].Hosts)
	}
	if strings.Join(report.Adapters[7].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected itfind provider index capabilities: %#v", report.Adapters[7].Capabilities)
	}
	if strings.Join(report.Adapters[8].Hosts, ",") != "openapi.jeonju.go.kr" {
		t.Fatalf("unexpected jeonju provider index hosts: %#v", report.Adapters[8].Hosts)
	}
	if strings.Join(report.Adapters[8].Capabilities, ",") != "verification" {
		t.Fatalf("unexpected jeonju provider index capabilities: %#v", report.Adapters[8].Capabilities)
	}
	if strings.Join(report.Adapters[9].Hosts, ",") != "www.korad.or.kr" {
		t.Fatalf("unexpected korad provider index hosts: %#v", report.Adapters[9].Hosts)
	}
	if strings.Join(report.Adapters[9].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected korad provider index capabilities: %#v", report.Adapters[9].Capabilities)
	}
	if strings.Join(report.Adapters[10].Hosts, ",") != "data.naqs.go.kr" {
		t.Fatalf("unexpected naqs provider index hosts: %#v", report.Adapters[10].Hosts)
	}
	if strings.Join(report.Adapters[10].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected naqs provider index capabilities: %#v", report.Adapters[10].Capabilities)
	}
	if strings.Join(report.Adapters[11].Hosts, ",") != "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr" {
		t.Fatalf("unexpected q-net provider index hosts: %#v", report.Adapters[11].Hosts)
	}
	if strings.Join(report.Adapters[12].Hosts, ",") != "data.sisul.or.kr" {
		t.Fatalf("unexpected sisul provider index hosts: %#v", report.Adapters[12].Hosts)
	}
	if strings.Join(report.Adapters[12].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected sisul provider index capabilities: %#v", report.Adapters[12].Capabilities)
	}
	if strings.Join(report.Adapters[13].Hosts, ",") != "data.uiryeong.go.kr" {
		t.Fatalf("unexpected uiryeong provider index hosts: %#v", report.Adapters[13].Hosts)
	}
	if strings.Join(report.Adapters[13].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected uiryeong provider index capabilities: %#v", report.Adapters[13].Capabilities)
	}
	if strings.Join(report.Adapters[14].Hosts, ",") != "openapi.its.ulsan.kr" {
		t.Fatalf("unexpected ulsan provider index hosts: %#v", report.Adapters[14].Hosts)
	}
	if strings.Join(report.Adapters[14].Capabilities, ",") != "call,verification" {
		t.Fatalf("unexpected ulsan provider index capabilities: %#v", report.Adapters[14].Capabilities)
	}
}
