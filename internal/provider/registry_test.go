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
		"openapi.airport.co.kr":           "airport",
		"www.andong.go.kr":                "andong",
		"www.calspia.go.kr":               "calspia",
		"cancer.go.kr":                    "cancer",
		"car.go.kr":                       "car",
		"www.car365.go.kr":                "car365",
		"info.childcare.go.kr":            "childcare-info",
		"tour.chungbuk.go.kr":             "chungbuk-tour",
		"alldam.chungnam.go.kr":           "chungnam",
		"localfood.chungnam.go.kr":        "chungnam",
		"www.chungnam.go.kr":              "chungnam",
		"www.xn--6-6v7en42by2es7i6jc.com": "chungnam",
		"www.codil.or.kr":                 "codil",
		"www.culture.go.kr":               "culture",
		"air.daegu.go.kr":                 "daegu",
		"happy.daegu.go.kr":               "daegu",
		"thegoodnight.daegu.go.kr":        "daegu",
		"www.daegu.go.kr":                 "daegu",
		"www.daegufood.go.kr":             "daegu",
		"bigdata.daejeon.go.kr":           "daejeon",
		"gis.daejeon.go.kr":               "daejeon",
		"data.gg.go.kr":                   "data-gg",
		"dgfca.or.kr":                     "dgfca",
		"data.dongjak.go.kr":              "dongjak",
		"ecvam.neins.go.kr":               "ecvam",
		"www.emuseum.go.kr":               "emuseum",
		"data.ex.co.kr":                   "ex",
		"openapi.q-net.or.kr":             "q-net",
		"openapi.epost.go.kr":             "epost",
		"data.ekape.or.kr":                "ekape",
		"www.eshare.go.kr":                "eshare",
		"www.foodsafetykorea.go.kr":       "foodsafetykorea",
		"www.garak.co.kr":                 "garak",
		"openapi.gblib.or.kr":             "gblib",
		"data.geoje.go.kr":                "geoje",
		"www.gicoms.go.kr":                "gicoms",
		"www.gimhae.go.kr":                "gimhae",
		"www.gims.go.kr":                  "gims",
		"data.gwanak.go.kr":               "gwanak",
		"www.gwangjin.go.kr":              "gwangjin",
		"data.gm.go.kr":                   "gwangmyeong",
		"parking.happysd.or.kr":           "happysd",
		"www.happysd.or.kr":               "happysd",
		"data.humetro.busan.kr":           "humetro",
		"search.i815.or.kr":               "i815",
		"www.icheon.go.kr":                "icheon",
		"www.ins24.go.kr":                 "ins24",
		"api.ip-navi.or.kr":               "ip-navi",
		"api.ip-navi.or.kr:8000":          "ip-navi",
		"its.go.kr":                       "its",
		"www.its.go.kr":                   "its",
		"www.iwest.co.kr":                 "iwest",
		"data.sisul.or.kr":                "sisul",
		"www.sisul.or.kr":                 "sisul-www",
		"data.uiryeong.go.kr":             "uiryeong",
		"folkency.nfm.go.kr":              "folk",
		"api.forest.go.kr":                "forest",
		"unipass.customs.go.kr":           "unipass",
		"open.itfind.or.kr":               "itfind",
		"air.jeju.go.kr":                  "jeju-air",
		"data.jeju.go.kr":                 "jeju",
		"jeonnam.openapi.redtable.global": "jeonnam-redtable",
		"www.jejudatahub.net":             "jejudatahub",
		"www.jejuits.go.kr":               "jejuits",
		"www.jeju.go.kr":                  "jeju-www",
		"openapi.jeonju.go.kr":            "jeonju",
		"openapi.jongno.go.kr":            "jongno",
		"openapi.jongno.go.kr:8088":       "jongno",
		"www.juso.go.kr":                  "juso",
		"www.khoa.go.kr":                  "khoa",
		"plus.kipris.or.kr":               "kipris-plus",
		"www.kistep.re.kr":                "kistep",
		"apihub.kma.go.kr":                "kma-apihub",
		"www.kofpi.or.kr":                 "kofpi",
		"koreapost.go.kr":                 "koreapost",
		"www.korad.or.kr":                 "korad",
		"kosmes.or.kr":                    "kosmes",
		"openapi.kpx.or.kr":               "kpx",
		"openapi.ebid.lh.or.kr":           "lh-ebid",
		"www.lofin365.go.kr":              "lofin365",
		"211.237.50.150":                  "mafra-legacy",
		"211.237.50.150:7080":             "mafra-legacy",
		"data.mafra.go.kr":                "mafra",
		"opendata.mnd.go.kr":              "mnd-open-data",
		"www.much.go.kr":                  "much",
		"data.myhome.go.kr:443":           "myhome",
		"nabic.rda.go.kr":                 "nabic",
		"www.naa.go.kr":                   "naa",
		"data.naqs.go.kr":                 "naqs",
		"ncpms.rda.go.kr":                 "ncpms",
		"nesc.nier.go.kr":                 "nier-nesc",
		"www.nie-ecobank.kr":              "nie-ecobank",
		"www.nfqs.go.kr":                  "nfqs",
		"nongsaro.go.kr":                  "nongsaro",
		"www.nongsaro.go.kr":              "nongsaro",
		"nosc.go.kr":                      "nosc",
		"oneclick.law.go.kr":              "oneclick-law",
		"opendart.fss.or.kr":              "opendart",
		"open.law.go.kr":                  "open-law",
		"www.law.go.kr":                   "open-law",
		"www.lawmaking.go.kr":             "open-law",
		"openapi.pqis.go.kr":              "pqis",
		"psis.rda.go.kr":                  "psis",
		"www.recycling-info.or.kr":        "recycling-info",
		"www.safetydata.go.kr":            "safetydata",
		"www.safemap.go.kr":               "safemap",
		"openapi.tour.go.kr":              "tour",
		"seogu.go.kr":                     "seogu",
		"www.seogwipo.go.kr":              "seogwipo",
		"data.seoul.go.kr":                "seoul-open-data",
		"map.seoul.go.kr":                 "seoul-map",
		"openapi.seoul.go.kr":             "seoul-open-data",
		"openapi.seoul.go.kr:8088":        "seoul-open-data",
		"t-data.seoul.go.kr":              "seoul-tdata",
		"ws.bus.go.kr":                    "seoul-bus",
		"smartfarmkorea.net":              "smartfarm-korea",
		"www.smartfarmkorea.net":          "smartfarm-korea",
		"stcis.go.kr":                     "stcis",
		"openapi.its.ulsan.kr":            "ulsan",
		"www.vworld.kr":                   "vworld",
		"www.wamis.go.kr":                 "wamis",
		"www.wamis.go.kr:8080":            "wamis",
		"openapi.work.go.kr":              "work",
		"www.work24.go.kr":                "work24",
		"www.worldjob.or.kr":              "worldjob",
		"www.youthcenter.go.kr":           "youthcenter",
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
	if report.AdapterCount != 111 || report.HostCount != 135 {
		t.Fatalf("unexpected provider index counts: %#v", report)
	}
	if !report.SplitReadiness.Ready {
		t.Fatalf("provider split should be ready after forest call capability is declared: %#v", report.SplitReadiness)
	}
	if report.SplitReadiness.Status != "ready" || report.SplitReadiness.AdapterCount != 111 || report.SplitReadiness.VerificationCapableAdapters != 111 || report.SplitReadiness.CallCapableAdapters != 23 {
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
		{"calspia", "www.calspia.go.kr", "verification"},
		{"cancer", "cancer.go.kr", "verification"},
		{"car", "car.go.kr", "verification"},
		{"car365", "www.car365.go.kr", "verification"},
		{"childcare-info", "info.childcare.go.kr", "verification"},
		{"chungbuk-tour", "tour.chungbuk.go.kr", "verification"},
		{"chungnam", "alldam.chungnam.go.kr,localfood.chungnam.go.kr,www.chungnam.go.kr,www.xn--6-6v7en42by2es7i6jc.com", "verification"},
		{"codil", "www.codil.or.kr", "verification"},
		{"consumer", "www.consumer.go.kr", "verification"},
		{"culture", "www.culture.go.kr", "verification"},
		{"daegu", "air.daegu.go.kr,happy.daegu.go.kr,thegoodnight.daegu.go.kr,www.daegu.go.kr,www.daegufood.go.kr", "verification"},
		{"daejeon", "bigdata.daejeon.go.kr,gis.daejeon.go.kr", "verification"},
		{"data-gg", "data.gg.go.kr", "verification"},
		{"dgfca", "dgfca.or.kr", "verification"},
		{"dongjak", "data.dongjak.go.kr", "verification"},
		{"ecvam", "ecvam.neins.go.kr", "verification"},
		{"ekape", "data.ekape.or.kr", "verification"},
		{"emuseum", "www.emuseum.go.kr", "call,verification"},
		{"epost", "openapi.epost.go.kr,openapi.epost.go.kr:80", "call,verification"},
		{"eshare", "www.eshare.go.kr", "verification"},
		{"ex", "data.ex.co.kr", "verification"},
		{"fairdata", "www.fairdata.go.kr", "verification"},
		{"folk", "folkency.nfm.go.kr", "verification"},
		{"foodsafetykorea", "www.foodsafetykorea.go.kr", "verification"},
		{"forest", "api.forest.go.kr", "call,verification"},
		{"franchise-ftc", "franchise.ftc.go.kr", "verification"},
		{"garak", "www.garak.co.kr", "verification"},
		{"gblib", "openapi.gblib.or.kr", "call,verification"},
		{"geoje", "data.geoje.go.kr", "call,verification"},
		{"gicoms", "www.gicoms.go.kr", "verification"},
		{"gimhae", "www.gimhae.go.kr", "verification"},
		{"gims", "www.gims.go.kr", "verification"},
		{"gwanak", "data.gwanak.go.kr", "verification"},
		{"gwangjin", "www.gwangjin.go.kr", "verification"},
		{"gwangmyeong", "data.gm.go.kr", "verification"},
		{"happysd", "parking.happysd.or.kr,www.happysd.or.kr", "verification"},
		{"humetro", "data.humetro.busan.kr", "call,verification"},
		{"i815", "search.i815.or.kr", "verification"},
		{"icheon", "www.icheon.go.kr", "verification"},
		{"ins24", "www.ins24.go.kr", "verification"},
		{"ip-navi", "api.ip-navi.or.kr,api.ip-navi.or.kr:8000", "verification"},
		{"itfind", "open.itfind.or.kr", "call,verification"},
		{"its", "its.go.kr,www.its.go.kr", "verification"},
		{"iwest", "www.iwest.co.kr", "verification"},
		{"jeju", "data.jeju.go.kr", "call,verification"},
		{"jeju-air", "air.jeju.go.kr", "verification"},
		{"jeju-www", "www.jeju.go.kr", "verification"},
		{"jejudatahub", "www.jejudatahub.net", "verification"},
		{"jejuits", "www.jejuits.go.kr", "verification"},
		{"jeonju", "openapi.jeonju.go.kr", "verification"},
		{"jeonnam-redtable", "jeonnam.openapi.redtable.global", "verification"},
		{"jongno", "openapi.jongno.go.kr,openapi.jongno.go.kr:8088", "verification"},
		{"juso", "www.juso.go.kr", "verification"},
		{"khoa", "www.khoa.go.kr", "verification"},
		{"kipris-plus", "plus.kipris.or.kr", "verification"},
		{"kistep", "www.kistep.re.kr", "verification"},
		{"kma-apihub", "apihub.kma.go.kr", "verification"},
		{"kofpi", "www.kofpi.or.kr", "verification"},
		{"korad", "www.korad.or.kr", "call,verification"},
		{"koreapost", "koreapost.go.kr", "verification"},
		{"kosmes", "kosmes.or.kr", "verification"},
		{"kpx", "openapi.kpx.or.kr", "call,verification"},
		{"lh-ebid", "openapi.ebid.lh.or.kr", "call,verification"},
		{"lofin365", "www.lofin365.go.kr", "verification"},
		{"mafra", "data.mafra.go.kr", "verification"},
		{"mafra-legacy", "211.237.50.150,211.237.50.150:7080", "verification"},
		{"mnd-open-data", "opendata.mnd.go.kr", "verification"},
		{"much", "www.much.go.kr", "verification"},
		{"myhome", "data.myhome.go.kr:443", "call,verification"},
		{"naa", "www.naa.go.kr", "verification"},
		{"nabic", "nabic.rda.go.kr", "verification"},
		{"naqs", "data.naqs.go.kr", "call,verification"},
		{"ncpms", "ncpms.rda.go.kr", "verification"},
		{"nfqs", "www.nfqs.go.kr", "verification"},
		{"nie-ecobank", "www.nie-ecobank.kr", "verification"},
		{"nier-nesc", "nesc.nier.go.kr", "verification"},
		{"nongsaro", "nongsaro.go.kr,www.nongsaro.go.kr", "verification"},
		{"nosc", "nosc.go.kr", "verification"},
		{"oneclick-law", "oneclick.law.go.kr,oneclick.law.go.kr:80", "call,verification"},
		{"open-assembly", "open.assembly.go.kr", "verification"},
		{"open-law", "open.law.go.kr,www.law.go.kr,www.lawmaking.go.kr", "verification"},
		{"opendart", "opendart.fss.or.kr", "verification"},
		{"pqis", "openapi.pqis.go.kr", "call,verification"},
		{"psis", "psis.rda.go.kr", "verification"},
		{"q-net", "c.q-net.or.kr,open.api.q-net.or.kr,openapi.q-net.or.kr", "verification"},
		{"recycling-info", "www.recycling-info.or.kr", "verification"},
		{"safemap", "www.safemap.go.kr", "verification"},
		{"safetydata", "www.safetydata.go.kr", "call,verification"},
		{"seogu", "seogu.go.kr", "verification"},
		{"seogwipo", "www.seogwipo.go.kr", "verification"},
		{"seoul-bus", "ws.bus.go.kr", "call,verification"},
		{"seoul-map", "map.seoul.go.kr", "verification"},
		{"seoul-open-data", "data.seoul.go.kr,openapi.seoul.go.kr,openapi.seoul.go.kr:8088", "call,verification"},
		{"seoul-tdata", "t-data.seoul.go.kr", "verification"},
		{"sexoffender", "api.sexoffender.go.kr", "verification"},
		{"sisul", "data.sisul.or.kr", "call,verification"},
		{"sisul-www", "www.sisul.or.kr", "verification"},
		{"smartfarm-korea", "smartfarmkorea.net,www.smartfarmkorea.net", "verification"},
		{"stcis", "stcis.go.kr", "verification"},
		{"tour", "openapi.tour.go.kr", "call,verification"},
		{"uiryeong", "data.uiryeong.go.kr", "call,verification"},
		{"ulsan", "openapi.its.ulsan.kr", "call,verification"},
		{"unipass", "unipass.customs.go.kr", "verification"},
		{"vworld", "www.vworld.kr", "verification"},
		{"wamis", "www.wamis.go.kr,www.wamis.go.kr:8080", "verification"},
		{"work", "openapi.work.go.kr", "verification"},
		{"work24", "www.work24.go.kr", "verification"},
		{"worldjob", "www.worldjob.or.kr", "verification"},
		{"youthcenter", "www.youthcenter.go.kr", "verification"},
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
