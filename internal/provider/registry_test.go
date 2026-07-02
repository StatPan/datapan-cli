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
		"openapi.airport.co.kr":    "airport",
		"www.andong.go.kr":         "andong",
		"www.culture.go.kr":        "culture",
		"data.gg.go.kr":            "data-gg",
		"www.emuseum.go.kr":        "emuseum",
		"openapi.q-net.or.kr":      "q-net",
		"openapi.epost.go.kr":      "epost",
		"data.ekape.or.kr":         "ekape",
		"www.garak.co.kr":          "garak",
		"openapi.gblib.or.kr":      "gblib",
		"data.geoje.go.kr":         "geoje",
		"data.gwanak.go.kr":        "gwanak",
		"www.happysd.or.kr":        "happysd",
		"data.humetro.busan.kr":    "humetro",
		"search.i815.or.kr":        "i815",
		"data.sisul.or.kr":         "sisul",
		"data.uiryeong.go.kr":      "uiryeong",
		"folkency.nfm.go.kr":       "folk",
		"api.forest.go.kr":         "forest",
		"open.itfind.or.kr":        "itfind",
		"data.jeju.go.kr":          "jeju",
		"openapi.jeonju.go.kr":     "jeonju",
		"www.korad.or.kr":          "korad",
		"openapi.kpx.or.kr":        "kpx",
		"openapi.ebid.lh.or.kr":    "lh-ebid",
		"data.mafra.go.kr":         "mafra",
		"data.myhome.go.kr:443":    "myhome",
		"data.naqs.go.kr":          "naqs",
		"ncpms.rda.go.kr":          "ncpms",
		"www.nfqs.go.kr":           "nfqs",
		"www.nongsaro.go.kr":       "nongsaro",
		"oneclick.law.go.kr":       "oneclick-law",
		"openapi.pqis.go.kr":       "pqis",
		"www.safetydata.go.kr":     "safetydata",
		"openapi.tour.go.kr":       "tour",
		"data.seoul.go.kr":         "seoul-open-data",
		"openapi.seoul.go.kr:8088": "seoul-open-data",
		"ws.bus.go.kr":             "seoul-bus",
		"openapi.its.ulsan.kr":     "ulsan",
		"www.work24.go.kr":         "work24",
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
	if report.AdapterCount != 39 || report.HostCount != 44 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if !report.SplitReadiness.Ready {
		t.Fatalf("provider split should be ready after forest call capability is declared: %#v", report.SplitReadiness)
	}
	if report.SplitReadiness.Status != "ready" || report.SplitReadiness.AdapterCount != 39 || report.SplitReadiness.VerificationCapableAdapters != 39 || report.SplitReadiness.CallCapableAdapters != 23 {
		t.Fatalf("unexpected split readiness: %#v", report.SplitReadiness)
	}
	if len(report.SplitReadiness.Reasons) != 0 {
		t.Fatalf("unexpected split readiness reasons: %#v", report.SplitReadiness.Reasons)
	}
	expected := []struct {
		name         string
		hosts        string
		capabilities string
	}{
		{"airport", "openapi.airport.co.kr", "verification"},
		{"andong", "www.andong.go.kr", "call,verification"},
		{"culture", "www.culture.go.kr", "verification"},
		{"data-gg", "data.gg.go.kr", "verification"},
		{"ekape", "data.ekape.or.kr", "verification"},
		{"emuseum", "www.emuseum.go.kr", "call,verification"},
		{"epost", "openapi.epost.go.kr,openapi.epost.go.kr:80", "call,verification"},
		{"folk", "folkency.nfm.go.kr", "verification"},
		{"forest", "api.forest.go.kr", "call,verification"},
		{"garak", "www.garak.co.kr", "verification"},
		{"gblib", "openapi.gblib.or.kr", "call,verification"},
		{"geoje", "data.geoje.go.kr", "call,verification"},
		{"gwanak", "data.gwanak.go.kr", "verification"},
		{"happysd", "www.happysd.or.kr", "verification"},
		{"humetro", "data.humetro.busan.kr", "call,verification"},
		{"i815", "search.i815.or.kr", "verification"},
		{"itfind", "open.itfind.or.kr", "call,verification"},
		{"jeju", "data.jeju.go.kr", "call,verification"},
		{"jeonju", "openapi.jeonju.go.kr", "verification"},
		{"korad", "www.korad.or.kr", "call,verification"},
		{"kpx", "openapi.kpx.or.kr", "call,verification"},
		{"lh-ebid", "openapi.ebid.lh.or.kr", "call,verification"},
		{"mafra", "data.mafra.go.kr", "verification"},
		{"myhome", "data.myhome.go.kr:443", "call,verification"},
		{"naqs", "data.naqs.go.kr", "call,verification"},
		{"ncpms", "ncpms.rda.go.kr", "verification"},
		{"nfqs", "www.nfqs.go.kr", "verification"},
		{"nongsaro", "www.nongsaro.go.kr", "verification"},
		{"oneclick-law", "oneclick.law.go.kr,oneclick.law.go.kr:80", "call,verification"},
		{"pqis", "openapi.pqis.go.kr", "call,verification"},
		{"q-net", "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr", "verification"},
		{"safetydata", "www.safetydata.go.kr", "call,verification"},
		{"seoul-bus", "ws.bus.go.kr", "call,verification"},
		{"seoul-open-data", "data.seoul.go.kr,openapi.seoul.go.kr:8088", "call,verification"},
		{"sisul", "data.sisul.or.kr", "call,verification"},
		{"tour", "openapi.tour.go.kr", "call,verification"},
		{"uiryeong", "data.uiryeong.go.kr", "call,verification"},
		{"ulsan", "openapi.its.ulsan.kr", "call,verification"},
		{"work24", "www.work24.go.kr", "verification"},
	}
	if len(report.Adapters) != len(expected) {
		t.Fatalf("unexpected provider index adapters: %#v", report)
	}
	for idx, want := range expected {
		got := report.Adapters[idx]
		if got.Name != want.name || got.Status != "registered" {
			t.Fatalf("unexpected provider index adapter at %d: %#v", idx, got)
		}
		if strings.Join(got.Hosts, ",") != want.hosts {
			t.Fatalf("unexpected %s provider index hosts: %#v", want.name, got.Hosts)
		}
		if strings.Join(got.Capabilities, ",") != want.capabilities {
			t.Fatalf("unexpected %s provider index capabilities: %#v", want.name, got.Capabilities)
		}
	}
}
