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

func TestSearchRejectsInventedSectorFilter(t *testing.T) {
	code, _, stderr := runTest([]string{"search", "실거래", "--sector", "realestate"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--sector is not a source metadata field") {
		t.Fatalf("expected sector rejection: %s", stderr)
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

func TestAccessJSONIncludesGuidedNextSteps(t *testing.T) {
	code, stdout, stderr := runTest([]string{"access", "15126469", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		`"application_url": "https://www.data.go.kr/data/15126469/openapi.do"`,
		`"purpose_text":`,
		`"smoke_command": "datapan call 15126469 --operation getRTMSDataSvcAptTrade --param DEAL_YMD=202501 --param LAWD_CD=11110 --json"`,
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
	if !strings.Contains(stderr, `unknown data.go.kr list id "missing"`) {
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
	if !strings.Contains(stderr, `unknown data.go.kr list id "missing"`) {
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
