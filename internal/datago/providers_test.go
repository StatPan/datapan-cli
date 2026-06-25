package datago

import "testing"

func TestProviderBacklogForRegistryClassifiesHosts(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "100",
			Title:    "기관_게이트웨이",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://apis.data.go.kr/100/list",
					Source:   &Source{Raw: map[string]any{"guide_url": "https://external.example.test/docs"}},
				},
			},
		},
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://openapi.q-net.or.kr/api/list",
					Source:   &Source{Raw: map[string]any{"is_confirmed_for_prod_nm": "심의승인"}},
				},
			},
		},
		{
			ID:       "300",
			Title:    "기관_루트",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name: "목록",
					Source: &Source{Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
			},
		},
	})
	backlog := ProviderBacklogForRegistry(reg, 2)
	if backlog.Summary.Hosts != 4 {
		t.Fatalf("hosts=%d providers=%#v", backlog.Summary.Hosts, backlog.Providers)
	}
	if backlog.Summary.DataGoKrGatewayHosts != 1 {
		t.Fatalf("gateway hosts=%d", backlog.Summary.DataGoKrGatewayHosts)
	}
	if backlog.Summary.ExternalEndpointHosts != 1 {
		t.Fatalf("external endpoint hosts=%d", backlog.Summary.ExternalEndpointHosts)
	}
	if backlog.Summary.ExternalGuideHosts != 1 {
		t.Fatalf("external guide hosts=%d", backlog.Summary.ExternalGuideHosts)
	}
	if backlog.Summary.MissingAdapterHosts != 2 {
		t.Fatalf("missing adapter hosts=%d", backlog.Summary.MissingAdapterHosts)
	}
	if backlog.Summary.NeedsAdapterOperations != 1 {
		t.Fatalf("needs adapter operations=%d", backlog.Summary.NeedsAdapterOperations)
	}
	if backlog.Summary.UnsupportedProtocolOps != 1 {
		t.Fatalf("unsupported protocol operations=%d", backlog.Summary.UnsupportedProtocolOps)
	}

	qnet := findProviderSummary(backlog.Providers, "openapi.q-net.or.kr")
	if qnet == nil {
		t.Fatalf("missing q-net provider: %#v", backlog.Providers)
	}
	if qnet.AdapterStatus != "missing" || qnet.Provider != "q-net" || qnet.ExternalEndpointOperations != 1 {
		t.Fatalf("unexpected q-net summary: %#v", qnet)
	}
	if len(qnet.SampleIDs) != 1 || qnet.SampleIDs[0] != "200" {
		t.Fatalf("unexpected q-net samples: %#v", qnet.SampleIDs)
	}

	guide := findProviderSummary(backlog.Providers, "external.example.test")
	if guide == nil || guide.AdapterStatus != "guide_only" || guide.ExternalGuideSpecs != 1 {
		t.Fatalf("unexpected guide summary: %#v", guide)
	}

	gateway := findProviderSummary(backlog.Providers, "apis.data.go.kr")
	if gateway == nil || gateway.AdapterStatus != "builtin" || gateway.Operations != 1 {
		t.Fatalf("unexpected gateway summary: %#v", gateway)
	}
}

func TestProviderBacklogMarksRegisteredAdapterHosts(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://openapi.q-net.or.kr/api/list"},
				{Name: "상세", Endpoint: "https://c.q-net.or.kr/api/detail"},
			},
		},
		{
			ID:       "300",
			Title:    "기관_외부2",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://missing.example.test/api/list"},
			},
		},
	})

	backlog := ProviderBacklogForRegistryWithAdapters(reg, 2, []string{"openapi.q-net.or.kr", "c.q-net.or.kr"})
	if backlog.Summary.RegisteredAdapterHosts != 2 {
		t.Fatalf("registered adapter hosts=%d", backlog.Summary.RegisteredAdapterHosts)
	}
	if backlog.Summary.MissingAdapterHosts != 1 {
		t.Fatalf("missing adapter hosts=%d", backlog.Summary.MissingAdapterHosts)
	}
	if backlog.Summary.NeedsAdapterOperations != 1 {
		t.Fatalf("needs adapter operations=%d", backlog.Summary.NeedsAdapterOperations)
	}
	for _, host := range []string{"openapi.q-net.or.kr", "c.q-net.or.kr"} {
		qnet := findProviderSummary(backlog.Providers, host)
		if qnet == nil || qnet.AdapterStatus != "adapter" {
			t.Fatalf("expected adapter status for %s: %#v", host, qnet)
		}
	}
}

func TestProviderBacklogNamesRegisteredExternalFamilies(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:       "400",
			Title:    "산림청_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{Name: "공항", Endpoint: "http://openapi.airport.co.kr/service/rest/airportLowVisibility/getAirportLowVisibilityLast"},
				{Name: "안동", Endpoint: "https://www.andong.go.kr/openapi/service/arDevJigaService/getCode"},
				{Name: "목록", Endpoint: "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"},
				{Name: "강북도서관", Endpoint: "http://openapi.gblib.or.kr/OpenAPI/service/SearchBook/getSearchBook"},
				{Name: "축산", Endpoint: "http://data.ekape.or.kr/openapi-data/service/user/grade/confirmNo"},
				{Name: "민속", Endpoint: "https://folkency.nfm.go.kr/api/FolkTradClturMltmd/getPhotoList"},
				{Name: "부산교통", Endpoint: "http://data.humetro.busan.kr/voc/api/open_api_public.tnn"},
				{Name: "ICT", Endpoint: "http://open.itfind.or.kr/openapi/service/ResearchResultReportService/getResearchResultReport"},
				{Name: "전주", Endpoint: "http://openapi.jeonju.go.kr/rest/wifizone"},
				{Name: "원자력", Endpoint: "http://www.korad.or.kr/openapi/service/radiTakeWasteStatsSvc/getRadiTakeWasteStatsDataList"},
				{Name: "전력거래소", Endpoint: "https://openapi.kpx.or.kr/openapi/sukub5mToday/getSukub5mToday"},
				{Name: "LH전자조달", Endpoint: "http://openapi.ebid.lh.or.kr/ebid.com.openapi.service.OpenBidInfoList.dev"},
				{Name: "친환경", Endpoint: "http://data.naqs.go.kr/openapi/service/rest/naqsenv/envparam"},
				{Name: "생활법령", Endpoint: "http://oneclick.law.go.kr:80/OPENAPI/soap/LifeLawSearchService/getSearchGroupList"},
				{Name: "식물검역", Endpoint: "http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml"},
				{Name: "시설", Endpoint: "http://data.sisul.or.kr/AutoAPI/service/OpenDB/Publicgarage/getPublicgarageQry"},
				{Name: "서울버스", Endpoint: "http://ws.bus.go.kr/api/rest/buspos/getBusPosByRtid"},
				{Name: "관광", Endpoint: "http://openapi.tour.go.kr/openapi/service/EdrcntTourismBalnaceService/getTourismBalcList"},
				{Name: "의령", Endpoint: "http://data.uiryeong.go.kr/rest/uiryeongpark/getUiryeongparkList"},
				{Name: "울산", Endpoint: "http://openapi.its.ulsan.kr/UlsanAPI/RouteInfo.xo"},
			},
		},
	})

	backlog := ProviderBacklogForRegistryWithAdapters(reg, 2, []string{"api.forest.go.kr", "data.ekape.or.kr", "data.humetro.busan.kr", "data.naqs.go.kr", "data.sisul.or.kr", "data.uiryeong.go.kr", "folkency.nfm.go.kr", "oneclick.law.go.kr:80", "open.itfind.or.kr", "openapi.airport.co.kr", "openapi.ebid.lh.or.kr", "openapi.gblib.or.kr", "openapi.its.ulsan.kr", "openapi.jeonju.go.kr", "openapi.kpx.or.kr", "openapi.pqis.go.kr", "openapi.tour.go.kr", "ws.bus.go.kr", "www.andong.go.kr", "www.korad.or.kr"})
	airport := findProviderSummary(backlog.Providers, "openapi.airport.co.kr")
	if airport == nil || airport.AdapterStatus != "adapter" || airport.Provider != "airport" {
		t.Fatalf("unexpected airport summary: %#v", airport)
	}
	andong := findProviderSummary(backlog.Providers, "www.andong.go.kr")
	if andong == nil || andong.AdapterStatus != "adapter" || andong.Provider != "andong" {
		t.Fatalf("unexpected andong summary: %#v", andong)
	}
	forest := findProviderSummary(backlog.Providers, "api.forest.go.kr")
	if forest == nil || forest.AdapterStatus != "adapter" || forest.Provider != "forest" {
		t.Fatalf("unexpected forest summary: %#v", forest)
	}
	gblib := findProviderSummary(backlog.Providers, "openapi.gblib.or.kr")
	if gblib == nil || gblib.AdapterStatus != "adapter" || gblib.Provider != "gblib" {
		t.Fatalf("unexpected gblib summary: %#v", gblib)
	}
	ekape := findProviderSummary(backlog.Providers, "data.ekape.or.kr")
	if ekape == nil || ekape.AdapterStatus != "adapter" || ekape.Provider != "ekape" {
		t.Fatalf("unexpected ekape summary: %#v", ekape)
	}
	folk := findProviderSummary(backlog.Providers, "folkency.nfm.go.kr")
	if folk == nil || folk.AdapterStatus != "adapter" || folk.Provider != "folk" {
		t.Fatalf("unexpected folk summary: %#v", folk)
	}
	humetro := findProviderSummary(backlog.Providers, "data.humetro.busan.kr")
	if humetro == nil || humetro.AdapterStatus != "adapter" || humetro.Provider != "humetro" {
		t.Fatalf("unexpected humetro summary: %#v", humetro)
	}
	itfind := findProviderSummary(backlog.Providers, "open.itfind.or.kr")
	if itfind == nil || itfind.AdapterStatus != "adapter" || itfind.Provider != "itfind" {
		t.Fatalf("unexpected itfind summary: %#v", itfind)
	}
	jeonju := findProviderSummary(backlog.Providers, "openapi.jeonju.go.kr")
	if jeonju == nil || jeonju.AdapterStatus != "adapter" || jeonju.Provider != "jeonju" {
		t.Fatalf("unexpected jeonju summary: %#v", jeonju)
	}
	korad := findProviderSummary(backlog.Providers, "www.korad.or.kr")
	if korad == nil || korad.AdapterStatus != "adapter" || korad.Provider != "korad" {
		t.Fatalf("unexpected korad summary: %#v", korad)
	}
	kpx := findProviderSummary(backlog.Providers, "openapi.kpx.or.kr")
	if kpx == nil || kpx.AdapterStatus != "adapter" || kpx.Provider != "kpx" {
		t.Fatalf("unexpected kpx summary: %#v", kpx)
	}
	lhEBid := findProviderSummary(backlog.Providers, "openapi.ebid.lh.or.kr")
	if lhEBid == nil || lhEBid.AdapterStatus != "adapter" || lhEBid.Provider != "lh-ebid" {
		t.Fatalf("unexpected lh-ebid summary: %#v", lhEBid)
	}
	naqs := findProviderSummary(backlog.Providers, "data.naqs.go.kr")
	if naqs == nil || naqs.AdapterStatus != "adapter" || naqs.Provider != "naqs" {
		t.Fatalf("unexpected naqs summary: %#v", naqs)
	}
	oneclick := findProviderSummary(backlog.Providers, "oneclick.law.go.kr:80")
	if oneclick == nil || oneclick.AdapterStatus != "adapter" || oneclick.Provider != "oneclick-law" {
		t.Fatalf("unexpected oneclick summary: %#v", oneclick)
	}
	pqis := findProviderSummary(backlog.Providers, "openapi.pqis.go.kr")
	if pqis == nil || pqis.AdapterStatus != "adapter" || pqis.Provider != "pqis" {
		t.Fatalf("unexpected pqis summary: %#v", pqis)
	}
	sisul := findProviderSummary(backlog.Providers, "data.sisul.or.kr")
	if sisul == nil || sisul.AdapterStatus != "adapter" || sisul.Provider != "sisul" {
		t.Fatalf("unexpected sisul summary: %#v", sisul)
	}
	seoulBus := findProviderSummary(backlog.Providers, "ws.bus.go.kr")
	if seoulBus == nil || seoulBus.AdapterStatus != "adapter" || seoulBus.Provider != "seoul-bus" {
		t.Fatalf("unexpected seoul-bus summary: %#v", seoulBus)
	}
	tour := findProviderSummary(backlog.Providers, "openapi.tour.go.kr")
	if tour == nil || tour.AdapterStatus != "adapter" || tour.Provider != "tour" {
		t.Fatalf("unexpected tour summary: %#v", tour)
	}
	uiryeong := findProviderSummary(backlog.Providers, "data.uiryeong.go.kr")
	if uiryeong == nil || uiryeong.AdapterStatus != "adapter" || uiryeong.Provider != "uiryeong" {
		t.Fatalf("unexpected uiryeong summary: %#v", uiryeong)
	}
	ulsan := findProviderSummary(backlog.Providers, "openapi.its.ulsan.kr")
	if ulsan == nil || ulsan.AdapterStatus != "adapter" || ulsan.Provider != "ulsan" {
		t.Fatalf("unexpected ulsan summary: %#v", ulsan)
	}
}

func TestDependencyInventoryClassifiesOperations(t *testing.T) {
	reg := NewRegistry([]Spec{
		{
			ID:             "100",
			Title:          "기관_게이트웨이",
			Provider:       "data.go.kr",
			Organization:   "기관",
			SourceCategory: "교통",
			Operations: []Operation{
				{Name: "목록", Endpoint: "https://apis.data.go.kr/100/list"},
			},
		},
		{
			ID:       "200",
			Title:    "기관_외부",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name:     "목록",
					Endpoint: "https://openapi.q-net.or.kr/api/list",
					Source: &Source{Raw: map[string]any{
						"is_confirmed_for_dev_nm": "심의승인",
						"guide_url":               "https://www.q-net.or.kr/docs",
					}},
				},
			},
		},
		{
			ID:       "300",
			Title:    "기관_루트",
			Provider: "data.go.kr",
			Operations: []Operation{
				{
					Name: "목록",
					Source: &Source{Raw: map[string]any{
						"end_point_url": "http://openapi.tour.go.kr/openapi/service",
						"api_type":      "SOAP",
						"data_format":   "WMS",
					}},
				},
			},
		},
	})

	summary, deps := DependencyInventoryForRegistry(reg, []string{"openapi.q-net.or.kr"})
	if summary.OperationsTotal != 3 || summary.DataGoKrGatewayOperations != 1 || summary.ExternalEndpointOps != 1 || summary.ServiceRootOperations != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.RegisteredAdapterOps != 1 || summary.MissingAdapterOps != 1 || summary.ApprovalRequiredOps != 1 {
		t.Fatalf("unexpected adapter/approval summary: %#v", summary)
	}
	qnet := findDependency(deps, "200", "목록")
	if qnet == nil {
		t.Fatalf("missing q-net dependency: %#v", deps)
	}
	if qnet.DependencyClass != "external_endpoint" || qnet.AdapterStatus != "adapter" || qnet.ProviderFamily != "q-net" || !qnet.ApprovalRequired || qnet.SkipReason != "" {
		t.Fatalf("unexpected q-net dependency: %#v", qnet)
	}
	root := findDependency(deps, "300", "목록")
	if root == nil || root.DependencyClass != "service_root" || root.AdapterStatus != "missing" || root.SourceHost != "openapi.tour.go.kr" {
		t.Fatalf("unexpected service root dependency: %#v", root)
	}
	filtered := FilterDependencyOperations(deps, &DependencyInventoryFilters{Status: "missing"})
	if len(filtered) != 1 || filtered[0].DatasetID != "300" {
		t.Fatalf("unexpected missing filter: %#v", filtered)
	}
}

func TestAdapterTargetsPrioritizeMissingAdapterHosts(t *testing.T) {
	deps := []DependencyOperationSummary{
		{
			DatasetID:       "100",
			Title:           "외부 A",
			Organization:    "기관",
			Operation:       "목록",
			Endpoint:        "https://missing.example.test/list",
			EndpointHost:    "missing.example.test",
			DependencyClass: "external_endpoint",
			AdapterStatus:   "missing",
			DataFormat:      "JSON",
			MissingParams:   []string{"q"},
		},
		{
			DatasetID:        "101",
			Title:            "외부 B",
			Organization:     "기관",
			Operation:        "상세",
			Endpoint:         "https://missing.example.test/detail",
			EndpointHost:     "missing.example.test",
			DependencyClass:  "external_endpoint",
			AdapterStatus:    "missing",
			ApprovalRequired: true,
			DataFormat:       "XML",
		},
		{
			DatasetID:       "200",
			Title:           "루트",
			Operation:       "목록",
			SourceHost:      "root.example.test",
			DependencyClass: "service_root",
			AdapterStatus:   "missing",
			APIType:         "SOAP",
		},
		{
			DatasetID:       "300",
			Title:           "등록됨",
			Operation:       "목록",
			EndpointHost:    "openapi.q-net.or.kr",
			DependencyClass: "external_endpoint",
			AdapterStatus:   "adapter",
		},
	}

	summary, targets := AdapterTargetsFromDependencies(deps, 1)
	if summary.TargetHosts != 2 || summary.TargetOperations != 3 || summary.ExternalEndpointOperations != 2 || summary.ServiceRootOperations != 1 {
		t.Fatalf("unexpected target summary: %#v", summary)
	}
	if summary.ApprovalRequiredOperations != 1 || summary.MissingParamOperations != 1 || summary.UnsupportedProtocolOperations != 1 {
		t.Fatalf("unexpected target gaps: %#v", summary)
	}
	if len(targets) != 2 || targets[0].Host != "missing.example.test" || targets[0].Rank != 1 || targets[0].Operations != 2 || targets[0].Specs != 2 {
		t.Fatalf("unexpected top target: %#v", targets)
	}
	if len(targets[0].SampleOperations) != 1 || targets[0].SampleOperations[0].DatasetID != "100" {
		t.Fatalf("unexpected samples: %#v", targets[0].SampleOperations)
	}
	filtered := FilterAdapterTargets(targets, &AdapterTargetFilters{Kind: "service_root"})
	if len(filtered) != 1 || filtered[0].Host != "root.example.test" || filtered[0].Rank != 1 {
		t.Fatalf("unexpected service_root filter: %#v", filtered)
	}
}

func findProviderSummary(providers []ProviderSummary, host string) *ProviderSummary {
	for i := range providers {
		if providers[i].Host == host {
			return &providers[i]
		}
	}
	return nil
}

func findDependency(deps []DependencyOperationSummary, datasetID, operation string) *DependencyOperationSummary {
	for i := range deps {
		if deps[i].DatasetID == datasetID && deps[i].Operation == operation {
			return &deps[i]
		}
	}
	return nil
}
