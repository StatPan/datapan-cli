package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeEnv map[string]string

func (f fakeEnv) LookupEnv(name string) (string, bool) {
	value, ok := f[name]
	return value, ok
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func runTest(args []string, env fakeEnv, client HTTPClient) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, env, client)
	return code, stdout.String(), stderr.String()
}

func TestSearchJSON(t *testing.T) {
	code, stdout, stderr := runTest([]string{"search", "아파트", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "15126469"`) {
		t.Fatalf("expected apartment trade spec in output: %s", stdout)
	}
}

func TestSearchFiltersByOrganization(t *testing.T) {
	code, stdout, stderr := runTest([]string{"search", "실거래", "--org", "국토교통부", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"organization": "국토교통부"`) {
		t.Fatalf("expected organization metadata in output: %s", stdout)
	}
	if !strings.Contains(stdout, `"id": "15126469"`) {
		t.Fatalf("expected apartment trade spec in output: %s", stdout)
	}
}

func TestSearchAllowsFilterOnly(t *testing.T) {
	code, stdout, stderr := runTest([]string{"search", "--org", "기상청", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "15084084"`) {
		t.Fatalf("expected KMA forecast spec in output: %s", stdout)
	}
}

func TestShowResolvesURLAndTitle(t *testing.T) {
	code, stdout, stderr := runTest([]string{"show", "https://www.data.go.kr/data/15126469/openapi.do", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "15126469"`) {
		t.Fatalf("expected URL ref to resolve: %s", stdout)
	}
	if !strings.Contains(stdout, `"get": "datapan get 15126469 --operation getRTMSDataSvcAptTrade DEAL_YMD=202501 LAWD_CD=11110 --json"`) {
		t.Fatalf("expected concrete show example: %s", stdout)
	}

	code, stdout, stderr = runTest([]string{"show", "국토교통부_아파트 매매 실거래가 자료", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "15126469"`) {
		t.Fatalf("expected title ref to resolve: %s", stdout)
	}
}

func TestShowIncludesImportedParamsAccessAndExample(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id": "999",
			"title": "테스트기관_테스트 API",
			"provider": "data.go.kr",
			"organization": "테스트기관",
			"source_category": "테스트분류",
			"priority": "P2",
			"operations": [
				{
					"name": "목록 조회",
					"endpoint": "https://example.test/api",
					"request_params": [
						{"name": "serviceKey", "label": "인증키"},
						{"name": "PAGE", "label": "페이지"},
						{"name": "ROWS", "label": "행수"}
					],
					"response_params": [
						{"name": "resultCode", "label": "결과코드"}
					]
				}
			],
			"source": {
				"system": "data.go.kr",
				"url": "https://www.data.go.kr/data/999/openapi.do",
				"raw": {
					"is_confirmed_for_dev_nm": "신청가능",
					"is_confirmed_for_prod_nm": "운영신청가능",
					"is_charged": "무료",
					"register_status": "정상",
					"request_cnt": 421,
					"data_format": "JSON",
					"updated_at": "2026-06-22"
				}
			}
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"show", "테스트기관_테스트 API", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": tmp}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		`"dev_approval": "신청가능"`,
		`"request_count": 421`,
		`"request_params":`,
		`"auth_params":`,
		`"name": "serviceKey"`,
		`"name": "PAGE"`,
		`"label": "페이지"`,
		`"response_params_count": 1`,
		`"example": "datapan get 999 --operation \"목록 조회\" PAGE=VALUE ROWS=VALUE --json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `serviceKey=VALUE`) {
		t.Fatalf("show example should not ask users to pass serviceKey: %s", stdout)
	}
}

func TestShowMarksImportedServiceRootNotCallable(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id": "998",
			"title": "테스트기관_루트 API",
			"provider": "data.go.kr",
			"organization": "테스트기관",
			"priority": "P2",
			"operations": [
				{
					"name": "목록 조회",
					"request_params": [
						{"name": "YY", "label": "년도"}
					]
				}
			]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"show", "테스트기관_루트 API", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": tmp}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"callable": false`) {
		t.Fatalf("expected not-callable operation: %s", stdout)
	}
	if strings.Contains(stdout, `datapan get 998`) {
		t.Fatalf("show should not generate get example without callable endpoint: %s", stdout)
	}
}

func TestShowAmbiguousQueryReturnsCandidates(t *testing.T) {
	code, stdout, stderr := runTest([]string{"show", "정보", "--json"}, nil, nil)
	if code != exitAmbiguous {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "ambiguous_ref"`) || !strings.Contains(stdout, `"candidates"`) {
		t.Fatalf("expected ambiguous candidates: %s", stdout)
	}
}

func TestShowUnknownRefJSONReturnsNotFound(t *testing.T) {
	code, stdout, stderr := runTest([]string{"show", "missing-dataset", "--json"}, nil, nil)
	if code != exitNotFound {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "not_found"`) || !strings.Contains(stdout, `"ref": "missing-dataset"`) {
		t.Fatalf("expected not_found JSON: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
}

func TestSearchRejectsInventedSectorFilter(t *testing.T) {
	code, _, stderr := runTest([]string{"search", "실거래", "--sector", "realestate"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--sector is not a source metadata field") {
		t.Fatalf("expected sector rejection: %s", stderr)
	}
}

func TestCatalogImportWritesRegistry(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("serviceKey"); got != "secret-value" {
			t.Fatalf("serviceKey=%q", got)
		}
		body := `{
			"currentCount": 2,
			"data": [
				{
					"list_id": "999",
					"list_title": "테스트기관_테스트 API",
					"title": "테스트 API",
					"org_nm": "테스트기관",
					"new_category_nm": "테스트분류",
					"keywords": "테스트,샘플",
					"desc": "테스트 설명",
					"meta_url": "https://www.data.go.kr/catalog/999/openapi.json",
					"end_point_url": "https://example.test/api",
					"operation_nm": "목록조회",
					"request_param_nm_en": "PAGE,ROWS",
					"request_param_nm": "\"페이지\",\"행수\"",
					"response_param_nm_en": "resultCode,resultMsg",
					"response_param_nm": "\"결과코드\",\"결과메시지\""
				},
				{
					"list_id": "999",
					"list_title": "테스트기관_테스트 API",
					"title": "테스트 API",
					"org_nm": "테스트기관",
					"new_category_nm": "테스트분류",
					"keywords": "테스트,샘플",
					"end_point_url": "https://example.test/api",
					"operation_nm": "상세조회",
					"request_param_nm_en": "ID",
					"request_param_nm": "\"식별자\""
				},
				{
					"list_id": "998",
					"list_title": "테스트기관_빈 API",
					"title": "빈 API",
					"org_nm": "테스트기관"
				}
			],
			"page": 1,
			"perPage": 3,
			"totalCount": 3
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "3", "--pages", "1", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"specs_written": 2`) || !strings.Contains(stdout, `"operations": 2`) {
		t.Fatalf("expected import summary: %s", stdout)
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"id": "999"`,
		`"source_category": "테스트분류"`,
		`"source_keywords"`,
		`"request_params"`,
		`"source"`,
		`"operations": []`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in registry: %s", want, data)
		}
	}
	code, stdout, stderr = runTest([]string{"search", "테스트", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": tmp}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "999"`) {
		t.Fatalf("expected imported registry search result: %s", stdout)
	}
	if strings.Contains(stdout, `"raw"`) {
		t.Fatalf("search output should stay compact and omit source raw: %s", stdout)
	}
}

func TestCatalogImportAllFetchesUntilTotalCount(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	var pages []string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		pages = append(pages, page)
		var body string
		switch page {
		case "1":
			body = `{
				"currentCount": 1,
				"data": [
					{"list_id": "100", "list_title": "기관_A", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/a"}
				],
				"page": 1,
				"perPage": 1,
				"totalCount": 2
			}`
		case "2":
			body = `{
				"currentCount": 1,
				"data": [
					{"list_id": "101", "list_title": "기관_B", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/b"}
				],
				"page": 2,
				"perPage": 1,
				"totalCount": 2
			}`
		default:
			t.Fatalf("unexpected page %s", page)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "1", "--all", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Fatalf("pages=%v", pages)
	}
	for _, want := range []string{`"pages_fetched": 2`, `"rows_fetched": 2`, `"specs_written": 2`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in import summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id": "100"`) || !strings.Contains(string(data), `"id": "101"`) {
		t.Fatalf("expected both pages in registry: %s", data)
	}
}

func TestCatalogImportAllStopsAtMaxPages(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		page := req.URL.Query().Get("page")
		body := `{
			"currentCount": 1,
			"data": [
				{"list_id": "` + page + `", "list_title": "기관_` + page + `", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/api"}
			],
			"page": ` + page + `,
			"perPage": 1,
			"totalCount": 99
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "1", "--all", "--max-pages", "2", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"error": "request_failed"`,
		`"max_pages": 2`,
		`"pages_fetched": 2`,
		`increase --max-pages`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogImportPreservesEncodedServiceKey(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.RawQuery, "%252B") || strings.Contains(req.URL.RawQuery, "%252F") || strings.Contains(req.URL.RawQuery, "%253D") {
			t.Fatalf("serviceKey was double encoded: %s", req.URL.RawQuery)
		}
		if !strings.Contains(req.URL.RawQuery, "serviceKey=abc%2Bdef%2Fghi%3D") {
			t.Fatalf("expected encoded serviceKey in raw query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"currentCount":0,"data":[],"page":1,"perPage":1,"totalCount":0}`)),
			Header:     make(http.Header),
		}, nil
	})
	code, _, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "1", "--pages", "1", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "abc%2Bdef%2Fghi%3D"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestCatalogImportMissingAuthJSON(t *testing.T) {
	code, stdout, stderr := runTest([]string{"catalog", "import", "data-go-kr", "--output", "registry.json", "--json"}, nil, nil)
	if code != exitAuth {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "missing_auth"`) || !strings.Contains(stdout, `"DATAPAN_DATA_GO_KR_KEY"`) {
		t.Fatalf("expected missing_auth JSON: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
}

func TestCatalogImportRequestFailureJSON(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 503,
			Body:       io.NopCloser(strings.NewReader(`portal unavailable`)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", "registry.json", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"error": "request_failed"`,
		`"message": "data.go.kr catalog import failed: HTTP 503 portal unavailable"`,
		`"pages_fetched": 0`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogImportOutputFailureJSON(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"currentCount":0,"data":[],"page":1,"perPage":1,"totalCount":0}`)),
			Header:     make(http.Header),
		}, nil
	})
	blocker := t.TempDir() + "/not-a-dir"
	if err := osWriteFile(blocker, []byte("file")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", blocker + "/registry.json", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "request_failed"`) || !strings.Contains(stdout, `"message":`) {
		t.Fatalf("expected output failure JSON: %s", stdout)
	}
}

func TestCatalogDiffJSON(t *testing.T) {
	dir := t.TempDir()
	oldPath := dir + "/old.json"
	newPath := dir + "/new.json"
	if err := osWriteFile(oldPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://old.test"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(newPath, []byte(`[
		{"id":"200","title":"기관_B 변경","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://new.test"}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","operations":[]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "diff", "--old", oldPath, "--new", newPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"added": 1`,
		`"removed": 1`,
		`"changed": 1`,
		`"id": "300"`,
		`"id": "100"`,
		`"fields":`,
		`"title"`,
		`"operations"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestAuthCheckMissing(t *testing.T) {
	code, stdout, stderr := runTest([]string{"auth", "check"}, nil, nil)
	if code != exitAuth {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "DATAPAN_DATA_GO_KR_KEY") {
		t.Fatalf("expected env var hint: %s", stdout)
	}
}

func TestAuthCheckMissingJSONKeepsExitCode(t *testing.T) {
	code, stdout, stderr := runTest([]string{"auth", "check", "--json"}, nil, nil)
	if code != exitAuth {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"credential_present": false`) {
		t.Fatalf("expected machine-readable missing credential status: %s", stdout)
	}
}

func TestCallDryRunRedactsKey(t *testing.T) {
	code, stdout, stderr := runTest(
		[]string{"call", "15084084", "--dry-run", "--json", "--param", "base_date=20260622", "--param", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("secret leaked in output: %s", stdout)
	}
	if !strings.Contains(stdout, "serviceKey=REDACTED") {
		t.Fatalf("expected redacted URL: %s", stdout)
	}
}

func TestGetAcceptsPositionalParams(t *testing.T) {
	code, stdout, stderr := runTest(
		[]string{"get", "기상청_단기예보 조회서비스", "--dry-run", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("secret leaked in output: %s", stdout)
	}
	if !strings.Contains(stdout, `"dataset": "15084084"`) || !strings.Contains(stdout, `"base_date": "20260622"`) {
		t.Fatalf("expected resolved dry-run with positional params: %s", stdout)
	}
}

func TestGetAmbiguousRefJSONReturnsCandidates(t *testing.T) {
	code, stdout, stderr := runTest([]string{"get", "정보", "--dry-run", "--json"}, nil, nil)
	if code != exitAmbiguous {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"error": "ambiguous_ref"`,
		`"candidates":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestGetUnknownRefJSONReturnsNotFound(t *testing.T) {
	code, stdout, stderr := runTest([]string{"get", "missing-dataset", "--dry-run", "--json"}, nil, nil)
	if code != exitNotFound {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "not_found"`) || !strings.Contains(stdout, `"ref": "missing-dataset"`) {
		t.Fatalf("expected not_found JSON: %s", stdout)
	}
}

func TestGetMissingAuthJSON(t *testing.T) {
	code, stdout, stderr := runTest([]string{"get", "15084084", "--json"}, nil, nil)
	if code != exitAuth {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "missing_auth"`) || !strings.Contains(stdout, `"DATAPAN_DATA_GO_KR_KEY"`) {
		t.Fatalf("expected missing_auth JSON: %s", stdout)
	}
}

func TestGetRequestFailureJSON(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"error": "request_failed"`,
		`"dataset": "15084084"`,
		`"message": "network down"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestGetProviderErrorJSONReturnsRequestExit(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"30","resultMsg":"SERVICE KEY IS NOT REGISTERED ERROR."}}}`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"status_code": 200`,
		`"semantic_status": "provider_error"`,
		`"message": "SERVICE KEY IS NOT REGISTERED ERROR."`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestGetHTMLResponseJSONReturnsRequestExit(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/html; charset=utf-8")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>service listing</body></html>`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"semantic_status": "html_response"`) {
		t.Fatalf("expected html semantic failure: %s", stdout)
	}
}

func TestSaveJSONForwardsRefError(t *testing.T) {
	tmp := t.TempDir() + "/rows.csv"
	code, stdout, stderr := runTest([]string{"save", "정보", "--output", tmp, "--json"}, nil, nil)
	if code != exitAmbiguous {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "ambiguous_ref"`) || !strings.Contains(stdout, `"candidates"`) {
		t.Fatalf("expected forwarded ambiguous JSON: %s", stdout)
	}
}

func TestSaveWritesCSVFromGet(t *testing.T) {
	tmp := t.TempDir() + "/rows.csv"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"response":{"body":{"items":{"item":[{"name":"alpha","count":2}]}}}}`)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"save", "15084084", "--output", tmp, "--format", "csv", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"format": "csv"`) || !strings.Contains(stdout, `"count": 1`) {
		t.Fatalf("expected save summary: %s", stdout)
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "count,name") || !strings.Contains(string(data), "2,alpha") {
		t.Fatalf("unexpected CSV: %s", data)
	}
}

func TestAccessJSONIncludesGuidedNextSteps(t *testing.T) {
	code, stdout, stderr := runTest([]string{"access", "15126469", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		`"application_url": "https://www.data.go.kr/data/15126469/openapi.do"`,
		`"purpose_text":`,
		`"smoke_command": "datapan get 15126469 --operation getRTMSDataSvcAptTrade DEAL_YMD=202501 LAWD_CD=11110 --json"`,
		`"next_steps":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestAccessOpenFailureJSON(t *testing.T) {
	original := openURLFunc
	openURLFunc = func(target string) error { return errors.New("browser unavailable") }
	defer func() { openURLFunc = original }()

	code, stdout, stderr := runTest([]string{"access", "15126469", "--open", "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"error": "open_failed"`,
		`"message": "browser unavailable"`,
		`"application_url": "https://www.data.go.kr/data/15126469/openapi.do"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
}

func TestAccessCopyFailureJSON(t *testing.T) {
	original := copyToClipboardFunc
	copyToClipboardFunc = func(text string) error { return errors.New("clipboard unavailable") }
	defer func() { copyToClipboardFunc = original }()

	code, stdout, stderr := runTest([]string{"access", "15126469", "--copy-purpose", "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"error": "copy_failed"`,
		`"message": "clipboard unavailable"`,
		`"purpose_copied": false`,
		`"purpose_text":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
}

func TestAccessSynthesizesSmokeCommandFromImportedRegistry(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id": "999",
			"title": "테스트기관_테스트 API",
			"provider": "data.go.kr",
			"organization": "테스트기관",
			"priority": "P2",
			"operations": [
				{
					"name": "목록 조회",
					"endpoint": "https://example.test/api",
					"request_params": [
						{"name": "PAGE", "label": "페이지"},
						{"name": "ROWS", "label": "행수"}
					]
				}
			]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"access", "테스트기관_테스트 API", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": tmp}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	want := `"smoke_command": "datapan get 999 --operation \"목록 조회\" PAGE=VALUE ROWS=VALUE --json"`
	if !strings.Contains(stdout, want) {
		t.Fatalf("expected synthesized smoke command %q in output: %s", want, stdout)
	}
	if !strings.Contains(stdout, `After approval, run: datapan get 999 --operation \"목록 조회\" PAGE=VALUE ROWS=VALUE --json`) {
		t.Fatalf("expected next step to include synthesized smoke command: %s", stdout)
	}
}

func TestAccessRejectsApplyAndDryRunTogether(t *testing.T) {
	code, _, stderr := runTest([]string{"access", "15126469", "--apply", "--dry-run"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "either --dry-run or --apply") {
		t.Fatalf("expected conflict message: %s", stderr)
	}
}

func TestAccessUnknownSpecDoesNotStartBrowser(t *testing.T) {
	code, _, stderr := runTest([]string{"access", "missing", "--dry-run"}, nil, nil)
	if code != exitNotFound {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, `unknown data.go.kr ref "missing"`) {
		t.Fatalf("expected unknown spec message: %s", stderr)
	}
}

func TestAccessUnknownSpecJSONReturnsNotFound(t *testing.T) {
	code, stdout, stderr := runTest([]string{"access", "missing", "--json"}, nil, nil)
	if code != exitNotFound {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"error": "not_found"`) || !strings.Contains(stdout, `"ref": "missing"`) {
		t.Fatalf("expected not_found JSON: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
}

func TestApplyAliasStillWorks(t *testing.T) {
	code, stdout, stderr := runTest([]string{"apply", "15126469", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"application_url": "https://www.data.go.kr/data/15126469/openapi.do"`) {
		t.Fatalf("expected compatibility alias output: %s", stdout)
	}
}

func TestAccessRequestAliasStillWorks(t *testing.T) {
	code, _, stderr := runTest([]string{"access", "request", "missing", "--dry-run"}, nil, nil)
	if code != exitNotFound {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, `unknown data.go.kr ref "missing"`) {
		t.Fatalf("expected unknown spec message: %s", stderr)
	}
}

func TestCallUsesHTTPClient(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("serviceKey"); got != "secret-value" {
			t.Fatalf("serviceKey=%q", got)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"response":{"body":{"items":{"item":[{"a":"b"}]}}}}`)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"call", "15084084", "--json", "--param", "base_date=20260622", "--param", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("expected response envelope: %s", stdout)
	}
}

func TestCallPreservesEncodedServiceKey(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.RawQuery, "%252B") || strings.Contains(req.URL.RawQuery, "%252F") || strings.Contains(req.URL.RawQuery, "%253D") {
			t.Fatalf("serviceKey was double encoded: %s", req.URL.RawQuery)
		}
		if !strings.Contains(req.URL.RawQuery, "serviceKey=abc%2Bdef%2Fghi%3D") {
			t.Fatalf("expected encoded serviceKey in raw query: %s", req.URL.RawQuery)
		}
		if got := req.URL.Query().Get("serviceKey"); got != "abc+def/ghi=" {
			t.Fatalf("serviceKey=%q", got)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"rows":[{"ok":true}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	code, _, stderr := runTest(
		[]string{"call", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "abc%2Bdef%2Fghi%3D"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestCallHTTPFailureKeepsJSONAndExitRequest(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"status_code": 404`,
		`"body": "{\"error\":\"not found\"}"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestSaveJSONForwardsHTTPFailure(t *testing.T) {
	tmp := t.TempDir() + "/rows.csv"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader(`server error`)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"save", "15084084", "--output", tmp, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"status_code": 500`) {
		t.Fatalf("expected forwarded HTTP failure JSON: %s", stdout)
	}
}

func TestExportInputCSV(t *testing.T) {
	tmp := t.TempDir() + "/rows.json"
	if err := osWriteFile(tmp, []byte(`{"rows":[{"name":"alpha","count":2}]}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"export", "--input", tmp}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "count,name") || !strings.Contains(stdout, "2,alpha") {
		t.Fatalf("unexpected CSV: %s", stdout)
	}
}
