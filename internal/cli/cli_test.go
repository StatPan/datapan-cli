package cli

import (
	"bytes"
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
					"name": "목록조회",
					"endpoint": "https://example.test/api",
					"request_params": [
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
		`"name": "PAGE"`,
		`"label": "페이지"`,
		`"response_params_count": 1`,
		`"example": "datapan get 999 --operation 목록조회 PAGE=VALUE ROWS=VALUE --json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
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
				}
			],
			"page": 1,
			"perPage": 2,
			"totalCount": 2
		}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "2", "--pages", "1", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"specs_written": 1`) || !strings.Contains(stdout, `"operations": 2`) {
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
