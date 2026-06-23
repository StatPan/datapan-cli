package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestReadDotEnvTrimsShellQuotes(t *testing.T) {
	path := t.TempDir() + "/.env"
	if err := osWriteFile(path, []byte("DATA_PORTAL_API_KEY='encoded-key'\nexport DATA_GO_KR_USER_ID=user\nPLAIN=value\n")); err != nil {
		t.Fatal(err)
	}
	values, err := readDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["DATA_PORTAL_API_KEY"] != "encoded-key" {
		t.Fatalf("unexpected quoted value: %#v", values["DATA_PORTAL_API_KEY"])
	}
	if values["DATA_GO_KR_USER_ID"] != "user" {
		t.Fatalf("unexpected export value: %#v", values["DATA_GO_KR_USER_ID"])
	}
	if values["PLAIN"] != "value" {
		t.Fatalf("unexpected plain value: %#v", values["PLAIN"])
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
		[]string{"catalog", "import", "data-go-kr", "--output", "registry.json", "--retries", "0", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"error": "request_failed"`,
		`HTTP 503 portal unavailable`,
		`"failed_page": 1`,
		`"pages_fetched": 0`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogImportRetriesTransientFailure(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	attempts := 0
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: 400,
				Body:       io.NopCloser(strings.NewReader(`{"code":-999,"msg":"UNKNOWN"}`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`{
				"currentCount": 1,
				"data": [
					{"list_id": "999", "list_title": "기관_A", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/api"}
				],
				"page": 1,
				"perPage": 1,
				"totalCount": 1
			}`)),
			Header: make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "1", "--pages", "1", "--retries", "1", "--retry-delay-ms", "1", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
	if !strings.Contains(stdout, `"retries": 1`) {
		t.Fatalf("expected retry count in output: %s", stdout)
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
	outputPath := dir + "/catalog-diff.json"
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
	code, stdout, stderr := runTest([]string{"catalog", "diff", "--old", oldPath, "--new", newPath, "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"output": "` + jsonEscaped(outputPath) + `"`,
		`"report":`,
		`"generated_at":`,
		`"provider": "data.go.kr"`,
		`"old": "` + jsonEscaped(oldPath) + `"`,
		`"new": "` + jsonEscaped(newPath) + `"`,
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
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"generated_at":`,
		`"provider": "data.go.kr"`,
		`"old": "` + jsonEscaped(oldPath) + `"`,
		`"new": "` + jsonEscaped(newPath) + `"`,
		`"truncated": false`,
		`"counts":`,
		`"old": 2`,
		`"new": 2`,
		`"id": "300"`,
		`"id": "100"`,
		`"fields":`,
		`"operations"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in report: %s", want, data)
		}
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.catalog-diff.v1.schema.json", data); err != nil {
		t.Fatalf("expected catalog diff report to match schema: %v\n%s", err, data)
	}
}

func TestCatalogAuditJSONReportsCoverageGaps(t *testing.T) {
	dir := t.TempDir()
	tmp := dir + "/registry.json"
	outputPath := dir + "/catalog-audit.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","source":{"system":"data.go.kr","raw":{"end_point_url":"http://openapi.tour.go.kr/openapi/service","api_type":"SOAP","data_format":"WMS","is_confirmed_for_dev_nm":"심의승인","is_confirmed_for_prod_nm":"심의승인"}}}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/123/service/list","request_params":[{"name":"page"}],"response_params":[{"name":"resultCode"}],"source":{"system":"data.go.kr","raw":{"guide_url":"https://external.example.test/docs"}}}],"source":{"system":"data.go.kr","url":"https://www.data.go.kr/data/300/openapi.do","raw":{"updated_at":"2026-06-23"}}},
		{"id":"400","title":"기관_D","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://external.example.test/api","request_params":[{"name":"page"}],"response_params":[{"name":"resultCode"}],"source":{"system":"data.go.kr","raw":{"updated_at":"2026-06-23"}}}],"source":{"system":"data.go.kr","url":"https://www.data.go.kr/data/400/openapi.do","raw":{"updated_at":"2026-06-23"}}}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "audit", "--registry", tmp, "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"output": "` + jsonEscaped(outputPath) + `"`,
		`"report":`,
		`"generated_at":`,
		`"sample_limit": 5`,
		`"specs_total": 4`,
		`"operations_total": 3`,
		`"callable_operations": 2`,
		`"specs_without_operations": 1`,
		`"specs_without_callable_operation": 2`,
		`"operations_without_endpoint": 1`,
		`"specs_missing_organization": 1`,
		`"data_go_kr_gateway_operations": 1`,
		`"gateway_with_external_guide_specs": 1`,
		`"external_endpoint_specs": 1`,
		`"external_endpoint_operations": 1`,
		`"service_root_only_operations": 1`,
		`"soap_operations": 1`,
		`"wms_operations": 1`,
		`"dev_approval_required_operations": 1`,
		`"prod_approval_required_operations": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"provider": "data.go.kr"`,
		`"registry": "` + jsonEscaped(tmp) + `"`,
		`"audit":`,
		`"samples":`,
		`"top_endpoint_hosts":`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in audit report: %s", want, data)
		}
	}
}

func TestCatalogErrorsJSONReportsProviderStatusFields(t *testing.T) {
	dir := t.TempDir()
	tmp := dir + "/registry.json"
	outputPath := dir + "/error-catalog.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","response_params":[{"name":"resultCode","label":"결과코드"},{"name":"resultMsg","label":"결과메시지"},{"name":"body","label":"본문"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"오류","endpoint":"https://external.example.test/api","response_params":[{"name":"returnReasonCode","label":"오류코드"},{"name":"returnAuthMsg","label":"인증오류메시지"},{"name":"errMsg","label":"오류메시지"}]}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"데이터","endpoint":"https://apis.data.go.kr/300/list","response_params":[{"name":"name","label":"이름"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "errors", "--registry", tmp, "--limit", "1", "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"output": "` + jsonEscaped(outputPath) + `"`,
		`"report":`,
		`"operations_with_status_fields": 2`,
		`"operations_with_result_code": 1`,
		`"operations_with_result_message": 1`,
		`"operations_with_reason_code": 1`,
		`"operations_with_auth_message": 1`,
		`"operations_with_error_message": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"limit": 1`,
		`"truncated": true`,
		`"name": "resultCode"`,
		`"role": "result_code"`,
		`"name": "returnAuthMsg"`,
		`"role": "auth_message"`,
		`"operations": [`,
		`"dataset_id": "100"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in report: %s", want, data)
		}
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.error-catalog.v1.schema.json", data); err != nil {
		t.Fatalf("expected error catalog report to match schema: %v\n%s", err, data)
	}
}

func TestCatalogProvidersJSONReportsAdapterBacklog(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/123/service/list","source":{"system":"data.go.kr","raw":{"guide_url":"https://external.example.test/docs"}}}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list","source":{"system":"data.go.kr","raw":{"is_confirmed_for_prod_nm":"심의승인"}}}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","source":{"system":"data.go.kr","raw":{"end_point_url":"http://openapi.tour.go.kr/openapi/service","api_type":"SOAP","data_format":"WMS"}}}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "providers", "--registry", tmp, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"external_endpoint_hosts": 1`,
		`"external_guide_hosts": 1`,
		`"registered_adapter_hosts": 1`,
		`"missing_adapter_hosts": 1`,
		`"needs_adapter_operations": 0`,
		`"unsupported_protocol_operations": 1`,
		`"host": "openapi.q-net.or.kr"`,
		`"provider": "q-net"`,
		`"adapter_status": "adapter"`,
		`"external_endpoint_operations": 1`,
		`"sample_ids":`,
		`"200"`,
		`"host": "apis.data.go.kr"`,
		`"adapter_status": "builtin"`,
		`"host": "external.example.test"`,
		`"adapter_status": "guide_only"`,
		`"report":`,
		`"generated_at":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogProvidersWritesReportOutput(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	outputPath := dir + "/providers.json"
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "providers", "--registry", registryPath, "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"output": "`+jsonEscaped(outputPath)+`"`) || !strings.Contains(stdout, `"report":`) {
		t.Fatalf("expected output path and report in stdout: %s", stdout)
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"generated_at":`,
		`"provider": "data.go.kr"`,
		`"registry": "` + jsonEscaped(registryPath) + `"`,
		`"host": "openapi.q-net.or.kr"`,
		`"adapter_status": "adapter"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in report file: %s", want, data)
		}
	}
}

func TestCatalogProvidersFiltersAdapterBacklog(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/123/service/list","source":{"system":"data.go.kr","raw":{"guide_url":"https://external.example.test/docs"}}}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "providers", "--registry", tmp, "--status", "adapter", "--kind", "external_endpoint", "--provider", "q-net", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"filtered_count": 1`,
		`"filters":`,
		`"status": "adapter"`,
		`"kind": "external_endpoint"`,
		`"provider": "q-net"`,
		`"host": "openapi.q-net.or.kr"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"host": "apis.data.go.kr"`) || strings.Contains(stdout, `"host": "external.example.test"`) {
		t.Fatalf("filtered provider output included non-matching host: %s", stdout)
	}
}

func TestCatalogDependenciesJSONReportsOperationInventory(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	outputPath := dir + "/dependencies.json"
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/123/service/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list","source":{"system":"data.go.kr","raw":{"is_confirmed_for_prod_nm":"심의승인","guide_url":"https://www.q-net.or.kr/docs"}}}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","source":{"system":"data.go.kr","raw":{"end_point_url":"http://openapi.tour.go.kr/openapi/service","api_type":"SOAP","data_format":"WMS"}}}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "dependencies", "--registry", registryPath, "--status", "adapter", "--kind", "external_endpoint", "--provider", "q-net", "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"filtered_count": 1`,
		`"operations_total": 3`,
		`"external_endpoint_operations": 1`,
		`"service_root_operations": 1`,
		`"approval_required_operations": 1`,
		`"registered_adapter_operations": 1`,
		`"missing_adapter_operations": 1`,
		`"dataset_id": "200"`,
		`"dependency_class": "external_endpoint"`,
		`"adapter_status": "adapter"`,
		`"provider_family": "q-net"`,
		`"approval_required": true`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "100"`) || strings.Contains(stdout, `"dataset_id": "300"`) {
		t.Fatalf("filtered dependency output included non-matching operation: %s", stdout)
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.dependencies.v1.schema.json", data); err != nil {
		t.Fatalf("expected dependency report to match schema: %v\n%s", err, data)
	}
}

func TestCatalogAdapterTargetsJSONReportsPrioritizedHosts(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	outputPath := dir + "/adapter-targets.json"
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://missing.example.test/api/list","request_params":[{"name":"q"}]}]},
		{"id":"101","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"상세","endpoint":"https://missing.example.test/api/detail","source":{"system":"data.go.kr","raw":{"is_confirmed_for_dev_nm":"심의승인"}}}]},
		{"id":"200","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","source":{"system":"data.go.kr","raw":{"end_point_url":"http://root.example.test/openapi/service","api_type":"SOAP"}}}]},
		{"id":"300","title":"기관_D","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "adapter-targets", "--registry", registryPath, "--kind", "external_endpoint", "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"filtered_count": 1`,
		`"target_hosts": 2`,
		`"target_operations": 3`,
		`"external_endpoint_operations": 2`,
		`"service_root_operations": 1`,
		`"approval_required_operations": 1`,
		`"missing_param_operations": 1`,
		`"rank": 1`,
		`"host": "missing.example.test"`,
		`"kinds":`,
		`"external_endpoint"`,
		`"sample_operations":`,
		`"dataset_id": "100"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"host": "openapi.q-net.or.kr"`) || strings.Contains(stdout, `"host": "root.example.test"`) {
		t.Fatalf("adapter target output included non-matching host: %s", stdout)
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.adapter-targets.v1.schema.json", data); err != nil {
		t.Fatalf("expected adapter target report to match schema: %v\n%s", err, data)
	}
}

func TestCatalogVerifyJSONSkipsUnsafeCandidates(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"base_date"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://external.example.test/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "--registry", tmp, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"total": 2`,
		`"skipped": 2`,
		`"reason": "missing_required_params"`,
		`"missing_params":`,
		`"base_date"`,
		`"reason": "external_provider_adapter_missing"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogVerifyUsesQNetAdapter(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"15025329",
			"title":"한국산업인력공단_산업인력 국가기술자격 통계 정보",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"연도별 등급별 실기 합격률 조회","endpoint":"http://openapi.q-net.or.kr/api/service/rest/InquiryStatSVC/getGradSiPassList","request_params":[{"name":"serviceKey"},{"name":"baseYY"}]}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("baseYY"); got != "2023" {
			t.Fatalf("baseYY=%q", got)
		}
		if got := req.URL.Query().Get("serviceKey"); got != "secret-value" {
			t.Fatalf("serviceKey=%q", got)
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/xml")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><gradename>기술사</gradename></item></items></body></response>`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", tmp, "--ref", "15025329", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"provider": "q-net"`,
		`"verified": 1`,
		`"status": "verified"`,
		`"semantic_status": "provider_ok"`,
		`"body_shape": "xml_items"`,
		`serviceKey=REDACTED`,
		`"baseYY": "2023"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("secret leaked in output: %s", stdout)
	}
}

func TestCatalogVerifyFiltersRegisteredProviderBeforeLimit(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"100",
			"title":"게이트웨이",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"base_date"}]}]
		},
		{
			"id":"15025329",
			"title":"한국산업인력공단_산업인력 국가기술자격 통계 정보",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"연도별 등급별 실기 합격률 조회","endpoint":"http://openapi.q-net.or.kr/api/service/rest/InquiryStatSVC/getGradSiPassList","request_params":[{"name":"serviceKey"},{"name":"baseYY"}]}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "openapi.q-net.or.kr" {
			t.Fatalf("expected q-net host after provider filter: %s", req.URL.String())
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/xml")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><name>ok</name></item></items></body></response>`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", tmp, "--provider", "q-net", "--kind", "external_endpoint", "--limit", "1", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"provider": "q-net"`,
		`"kind": "external_endpoint"`,
		`"filtered_count": 1`,
		`"verified": 1`,
		`"dataset_id": "15025329"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "100"`) {
		t.Fatalf("provider filter included gateway candidate: %s", stdout)
	}
}

func TestCatalogVerifyCallsEligibleSmokeCandidate(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"100",
			"title":"기관_A",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"base_date"},{"name":"numOfRows"}]}],
			"smoke":{"operation":"목록","params":{"base_date":"20260624"}}
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("base_date"); got != "20260624" {
			t.Fatalf("base_date=%q", got)
		}
		if got := req.URL.Query().Get("numOfRows"); got != "1" {
			t.Fatalf("numOfRows=%q", got)
		}
		if got := req.URL.Query().Get("serviceKey"); got != "secret-value" {
			t.Fatalf("serviceKey=%q", got)
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00","resultMsg":"OK"},"body":{"items":{"item":[{"name":"alpha"}]}}}}`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", tmp, "--ref", "100", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"verified": 1`,
		`"status": "verified"`,
		`"semantic_status": "provider_ok"`,
		`"body_shape": "rows:1"`,
		`serviceKey=REDACTED`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("secret leaked in output: %s", stdout)
	}
}

func TestCatalogVerifyMissingAuthKeepsExitAuth(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"100",
			"title":"기관_A",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","default_params":{"base_date":"20260624"}}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "--registry", tmp, "--json"}, nil, nil)
	if code != exitAuth {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"skipped": 1`,
		`"reason": "missing_auth"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogUpdateDryRunDoesNotReplaceRegistry(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	oldData := []byte(`[{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[]}]`)
	if err := osWriteFile(tmp, oldData); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`{
				"currentCount": 1,
				"data": [
					{"list_id": "200", "list_title": "기관_B", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/api"}
				],
				"page": 1,
				"perPage": 100,
				"totalCount": 1
			}`)),
			Header: make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "update", "data-go-kr", "--registry", tmp, "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"dry_run": true`, `"applied": false`, `"added": 1`, `"removed": 1`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(oldData) {
		t.Fatalf("dry-run replaced registry: %s", data)
	}
}

func TestCatalogUpdateApplyReplacesRegistryAndCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	tmp := dir + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[]}]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`{
				"currentCount": 1,
				"data": [
					{"list_id": "200", "list_title": "기관_B", "org_nm": "기관", "operation_nm": "목록", "end_point_url": "https://example.test/api"}
				],
				"page": 1,
				"perPage": 100,
				"totalCount": 1
			}`)),
			Header: make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "update", "data-go-kr", "--registry", tmp, "--apply", "--backup", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"applied": true`) || !strings.Contains(stdout, `"backup":`) {
		t.Fatalf("expected applied update with backup: %s", stdout)
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id": "200"`) || strings.Contains(string(data), `"id":"100"`) {
		t.Fatalf("registry was not replaced with imported spec: %s", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	foundBackup := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "registry.json.bak.") {
			foundBackup = true
			break
		}
	}
	if !foundBackup {
		t.Fatalf("expected backup file in %s", dir)
	}
}

func TestCatalogVerifyInputFiltersReport(t *testing.T) {
	tmp := t.TempDir() + "/verification.json"
	if err := osWriteFile(tmp, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"limit": 0,
		"truncated": false,
		"summary": {"total": 3, "verified": 1, "failed": 1, "skipped": 1, "unknown": 0},
		"results": [
			{"dataset_id": "100", "title": "성공", "operation": "목록", "provider": "data.go.kr", "dependency_class": "data_go_kr_gateway", "status": "verified"},
			{"dataset_id": "200", "title": "실패", "operation": "목록", "provider": "data.go.kr", "dependency_class": "data_go_kr_gateway", "status": "failed", "reason": "provider_error"},
			{"dataset_id": "300", "title": "스킵", "operation": "목록", "provider": "data.go.kr", "dependency_class": "external_endpoint", "status": "skipped", "reason": "external_provider_adapter_missing"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "--input", tmp, "--status", "failed", "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"status": "failed"`,
		`"filtered_count": 1`,
		`"filters":`,
		`"total": 1`,
		`"failed": 1`,
		`"dataset_id": "200"`,
		`"reason": "provider_error"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "100"`) || strings.Contains(stdout, `"dataset_id": "300"`) {
		t.Fatalf("filtered report included non-failed results: %s", stdout)
	}
}

func TestCatalogVerifyInputWritesFilteredReport(t *testing.T) {
	dir := t.TempDir()
	input := dir + "/verification.json"
	output := dir + "/failed.json"
	if err := osWriteFile(input, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"limit": 0,
		"truncated": false,
		"filtered_count": 2,
		"summary": {"total": 2, "verified": 1, "failed": 1, "skipped": 0, "unknown": 0},
		"results": [
			{"dataset_id": "100", "title": "성공", "operation": "목록", "provider": "data.go.kr", "dependency_class": "data_go_kr_gateway", "status": "verified"},
			{"dataset_id": "200", "title": "실패", "operation": "목록", "provider": "data.go.kr", "dependency_class": "data_go_kr_gateway", "status": "failed", "reason": "provider_error"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "--input", input, "--status", "failed", "--output", output, "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"filters":`,
		`"status": "failed"`,
		`"filtered_count": 1`,
		`"dataset_id": "200"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in filtered report: %s", want, data)
		}
	}
	if strings.Contains(string(data), `"dataset_id": "100"`) {
		t.Fatalf("filtered report included non-failed result: %s", data)
	}
}

func TestCatalogVerifySummaryRollsUpReport(t *testing.T) {
	dir := t.TempDir()
	input := dir + "/verification.json"
	output := dir + "/summary.json"
	if err := osWriteFile(input, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"registry": "registry.json",
		"limit": 0,
		"truncated": false,
		"filtered_count": 4,
		"summary": {"total": 4, "verified": 1, "failed": 2, "skipped": 1, "unknown": 0},
		"results": [
			{"dataset_id": "100", "title": "성공", "operation": "목록", "provider": "q-net", "endpoint_host": "openapi.q-net.or.kr", "dependency_class": "external_endpoint", "status": "verified"},
			{"dataset_id": "101", "title": "실패", "operation": "목록", "provider": "q-net", "endpoint_host": "openapi.q-net.or.kr", "dependency_class": "external_endpoint", "status": "failed", "reason": "qnet_connection_validation_failed", "provider_status": {"ok": false, "source": "resultCode/resultMsg", "code": "99", "reason_code": "qnet_connection_validation_failed"}},
			{"dataset_id": "102", "title": "실패2", "operation": "목록", "provider": "q-net", "endpoint_host": "openapi.q-net.or.kr", "dependency_class": "external_endpoint", "status": "failed", "reason": "qnet_connection_validation_failed"},
			{"dataset_id": "103", "title": "스킵", "operation": "목록", "provider": "q-net", "endpoint_host": "openapi.q-net.or.kr", "dependency_class": "external_endpoint", "status": "skipped", "reason": "qnet_wadl_metadata_only"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "summary", "--input", input, "--output", output, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "` + jsonEscaped(input) + `"`,
		`"by_reason":`,
		`"key": "qnet_connection_validation_failed"`,
		`"count": 2`,
		`"by_endpoint_host":`,
		`"openapi.q-net.or.kr"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"by_status"`) || !strings.Contains(string(data), `"qnet_wadl_metadata_only"`) {
		t.Fatalf("unexpected summary output file: %s", data)
	}
}

func TestCatalogVerifyInputRejectsRegistryArgs(t *testing.T) {
	code, _, stderr := runTest([]string{"catalog", "verify", "--input", "report.json", "--registry", "registry.json"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--input cannot be combined") {
		t.Fatalf("expected input conflict message: %s", stderr)
	}
}

func TestCatalogReleaseDraftWritesLayout(t *testing.T) {
	dir := t.TempDir()
	previousRegistryPath := dir + "/previous-registry.json"
	registryPath := dir + "/registry.json"
	verificationPath := dir + "/verification.json"
	outputDir := dir + "/release"
	paths := releaseDraftPaths(outputDir)
	if err := osWriteFile(previousRegistryPath, []byte(`[
		{"id":"090","title":"이전기관","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(verificationPath, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"limit": 1,
		"truncated": false,
		"filtered_count": 1,
		"summary": {"total": 1, "verified": 0, "failed": 0, "skipped": 1, "unknown": 0},
		"results": [
			{"dataset_id": "100", "title": "기관_A", "operation": "목록", "provider": "data.go.kr", "dependency_class": "external_endpoint", "status": "skipped", "reason": "external_provider_adapter_missing"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "release", "draft", "--registry", registryPath, "--previous-registry", previousRegistryPath, "--verification", verificationPath, "--output-dir", outputDir, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"verification_copied": true`,
		`"verification_summary_written": true`,
		`"specs": 1`,
		`"providers": 1`,
		`"previous_registry": "` + jsonEscaped(previousRegistryPath) + `"`,
		`"catalog_diff":`,
		`"dependencies":`,
		`"adapter_targets":`,
		`"provider_backlog":`,
		`"verification_summary":`,
		`"manifest":`,
		`"artifacts": 25`,
		`"provenance":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, path := range []string{
		outputDir + "/schemas/datapan.specs.v1.schema.json",
		outputDir + "/schemas/datapan.dependencies.v1.schema.json",
		outputDir + "/schemas/datapan.adapter-targets.v1.schema.json",
		outputDir + "/schemas/datapan.providers.v1.schema.json",
		outputDir + "/schemas/datapan.verification.v1.schema.json",
		outputDir + "/schemas/datapan.verification-summary.v1.schema.json",
		outputDir + "/schemas/datapan.release-manifest.v1.schema.json",
		outputDir + "/schemas/datapan.release-verification.v1.schema.json",
		outputDir + "/schemas/datapan.schema-index.v1.schema.json",
		outputDir + "/schemas/datapan.catalog-diff.v1.schema.json",
		outputDir + "/schemas/datapan.error-catalog.v1.schema.json",
		outputDir + "/schemas/datapan.catalog-audit.v1.schema.json",
		outputDir + "/schemas/datapan.provider-index.v1.schema.json",
		outputDir + "/schemas/index.json",
		outputDir + "/data/data-go-kr.registry.json",
		outputDir + "/data/provider-index.json",
		outputDir + "/reports/catalog-diff.json",
		outputDir + "/reports/catalog-audit.json",
		outputDir + "/reports/error-catalog.json",
		outputDir + "/reports/dependencies.json",
		outputDir + "/reports/adapter-targets.json",
		outputDir + "/reports/provider-backlog.json",
		outputDir + "/reports/latest-verification.json",
		outputDir + "/reports/latest-verification-summary.json",
		outputDir + "/provenance/data-go-kr.md",
		outputDir + "/manifest.json",
	} {
		if _, err := osReadFile(path); err != nil {
			t.Fatalf("expected release artifact %s: %v", path, err)
		}
	}
	provenance, err := osReadFile(outputDir + "/provenance/data-go-kr.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(provenance), "datapan catalog release draft") || !strings.Contains(string(provenance), "--previous-registry") || !strings.Contains(string(provenance), "datapan catalog diff") || !strings.Contains(string(provenance), "datapan catalog audit") || !strings.Contains(string(provenance), "datapan catalog dependencies") || !strings.Contains(string(provenance), "datapan catalog adapter-targets") {
		t.Fatalf("unexpected provenance: %s", provenance)
	}
	diffReport, err := osReadFile(outputDir + "/reports/catalog-diff.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"provider": "data.go.kr"`,
		`"old": "` + jsonEscaped(previousRegistryPath) + `"`,
		`"new": "` + jsonEscaped(paths.RegistryPath) + `"`,
		`"limit": 0`,
		`"truncated": false`,
		`"added": 1`,
		`"removed": 1`,
		`"id": "100"`,
		`"id": "090"`,
	} {
		if !strings.Contains(string(diffReport), want) {
			t.Fatalf("expected %q in catalog diff report: %s", want, diffReport)
		}
	}
	summary, err := osReadFile(outputDir + "/reports/latest-verification-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), `"source": "`+jsonEscaped(paths.VerificationPath)+`"`) || !strings.Contains(string(summary), `"external_provider_adapter_missing"`) {
		t.Fatalf("unexpected verification summary: %s", summary)
	}
	dependencies, err := osReadFile(outputDir + "/reports/dependencies.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"provider": "data.go.kr"`,
		`"registry": "` + jsonEscaped(paths.RegistryPath) + `"`,
		`"limit": 0`,
		`"operations_total": 1`,
		`"dataset_id": "100"`,
		`"dependency_class": "external_endpoint"`,
		`"adapter_status": "adapter"`,
		`"provider_family": "q-net"`,
	} {
		if !strings.Contains(string(dependencies), want) {
			t.Fatalf("expected %q in dependencies report: %s", want, dependencies)
		}
	}
	adapterTargets, err := osReadFile(outputDir + "/reports/adapter-targets.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"provider": "data.go.kr"`,
		`"registry": "` + jsonEscaped(paths.RegistryPath) + `"`,
		`"limit": 0`,
		`"targets": []`,
		`"target_hosts": 0`,
	} {
		if !strings.Contains(string(adapterTargets), want) {
			t.Fatalf("expected %q in adapter targets report: %s", want, adapterTargets)
		}
	}
	schemaIndex, err := osReadFile(outputDir + "/schemas/index.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"schema_version": "datapan.schema-index.v1"`,
		`"count": 13`,
		`"path": "schemas/datapan.dependencies.v1.schema.json"`,
		`"path": "schemas/datapan.adapter-targets.v1.schema.json"`,
		`"path": "schemas/datapan.schema-index.v1.schema.json"`,
		`"path": "schemas/datapan.catalog-diff.v1.schema.json"`,
		`"path": "schemas/datapan.error-catalog.v1.schema.json"`,
		`"path": "schemas/datapan.catalog-audit.v1.schema.json"`,
		`"path": "schemas/datapan.provider-index.v1.schema.json"`,
		`"contract": "dependencies"`,
		`"contract": "adapter-targets"`,
		`"contract": "schema-index"`,
		`"contract": "catalog-diff"`,
		`"contract": "error-catalog"`,
		`"contract": "catalog-audit"`,
		`"contract": "provider-index"`,
		`"version": "v1"`,
	} {
		if !strings.Contains(string(schemaIndex), want) {
			t.Fatalf("expected %q in schema index: %s", want, schemaIndex)
		}
	}
	manifest, err := osReadFile(outputDir + "/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"schema_version": "datapan.release-manifest.v1"`,
		`"artifact_count": 25`,
		`"path": "schemas/index.json"`,
		`"kind": "schema_index"`,
		`"path": "data/provider-index.json"`,
		`"kind": "provider_index"`,
		`"path": "reports/catalog-diff.json"`,
		`"kind": "catalog_diff"`,
		`"path": "reports/catalog-audit.json"`,
		`"kind": "catalog_audit"`,
		`"path": "reports/error-catalog.json"`,
		`"kind": "error_catalog"`,
		`"path": "reports/dependencies.json"`,
		`"kind": "dependencies"`,
		`"path": "reports/adapter-targets.json"`,
		`"kind": "adapter_targets"`,
		`"path": "reports/latest-verification-summary.json"`,
		`"kind": "verification_summary"`,
		`"sha256":`,
	} {
		if !strings.Contains(string(manifest), want) {
			t.Fatalf("expected %q in manifest: %s", want, manifest)
		}
	}
	verifyOutput := outputDir + "/reports/latest-release-verification.json"
	code, stdout, stderr = runTest([]string{"catalog", "release", "verify", "--manifest", outputDir + "/manifest.json", "--output", verifyOutput, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"schema_version": "datapan.release-verification.v1"`,
		`"manifest_schema_version": "datapan.release-manifest.v1"`,
		`"output": "` + jsonEscaped(verifyOutput) + `"`,
		`"checked": 25`,
		`"failed": 0`,
		`"status": "verified"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in verification output: %s", want, stdout)
		}
	}
	verifyReport, err := osReadFile(verifyOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(verifyReport), `"schema_version": "datapan.release-verification.v1"`) || strings.Contains(string(verifyReport), `"report":`) {
		t.Fatalf("unexpected release verification report file: %s", verifyReport)
	}
	if err := osWriteFile(outputDir+"/reports/provider-backlog.json", []byte(`{"tampered":true}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest([]string{"catalog", "release", "verify", "--manifest", outputDir + "/manifest.json", "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"size_mismatch"`) {
		t.Fatalf("expected size mismatch in output: %s", stdout)
	}
}

func TestCatalogReleaseVerifyRejectsInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	manifestPath := dir + "/manifest.json"
	if err := osWriteFile(dir+"/a.txt", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(manifestPath, []byte(`{
		"schema_version": "datapan.release-manifest.v0",
		"generated_at": "2026-06-24T00:00:00Z",
		"datapan_version": "test",
		"provider": "data.go.kr",
		"source_registry": "registry.json",
		"output_dir": "release",
		"artifact_count": 4,
		"artifacts": [
			{"path": "a.txt", "kind": "registry", "bytes": 1, "sha256": "not-a-sha"},
			{"path": "a.txt", "kind": "registry", "bytes": 1, "sha256": "not-a-sha"},
			{"path": "manifest.json", "kind": "provenance", "bytes": 1, "sha256": "not-a-sha"},
			{"path": "../outside.txt", "kind": "provenance", "bytes": 1, "sha256": "not-a-sha"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "release", "verify", "--manifest", manifestPath, "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"unsupported_schema_version"`,
		`"invalid_checksum"`,
		`"duplicate_artifact_path"`,
		`"manifest_self_reference"`,
		`"invalid_path"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestCatalogReleaseVerifyRejectsSchemaInvalidArtifact(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	outputDir := dir + "/release"
	if err := osWriteFile(registryPath, []byte(`[{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[]}]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "release", "draft", "--registry", registryPath, "--output-dir", outputDir, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	badRegistry := []byte(`{"not":"a datapan registry array"}`)
	if err := osWriteFile(outputDir+"/data/data-go-kr.registry.json", badRegistry); err != nil {
		t.Fatal(err)
	}
	manifestData, err := osReadFile(outputDir + "/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(badRegistry)
	for idx := range manifest.Artifacts {
		if manifest.Artifacts[idx].Path == "data/data-go-kr.registry.json" {
			manifest.Artifacts[idx].Bytes = int64(len(badRegistry))
			manifest.Artifacts[idx].SHA256 = fmt.Sprintf("%x", sum)
		}
	}
	if err := writeJSONFile(outputDir+"/manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runTest([]string{"catalog", "release", "verify", "--manifest", outputDir + "/manifest.json", "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"path": "data/data-go-kr.registry.json"`,
		`"reason": "schema_validation_failed"`,
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
		`"provider_status":`,
		`"source": "resultCode/resultMsg"`,
		`"code": "30"`,
		`"message": "SERVICE KEY IS NOT REGISTERED ERROR."`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
}

func TestGetOpenAPIServiceResponseErrorJSONPreservesProviderFields(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "application/xml")
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`
				<OpenAPI_ServiceResponse>
					<cmmMsgHeader>
						<errMsg>SERVICE ERROR</errMsg>
						<returnAuthMsg>SERVICE_KEY_IS_NOT_REGISTERED_ERROR</returnAuthMsg>
						<returnReasonCode>30</returnReasonCode>
					</cmmMsgHeader>
				</OpenAPI_ServiceResponse>
			`)),
			Header: header,
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
		`"semantic_status": "provider_error"`,
		`"source": "OpenAPI_ServiceResponse/cmmMsgHeader"`,
		`"reason_code": "30"`,
		`"auth_message": "SERVICE_KEY_IS_NOT_REGISTERED_ERROR"`,
		`"error_message": "SERVICE ERROR"`,
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

func jsonEscaped(value string) string {
	data, _ := json.Marshal(value)
	return string(data[1 : len(data)-1])
}
