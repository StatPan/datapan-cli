package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
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

func zipBytesForTest(t *testing.T, name string, data string) []byte {
	t.Helper()
	return zipFilesForTest(t, map[string]string{name: data})
}

func zipFilesForTest(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func TestSearchLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[{"id":"local-1","title":"지역 설치 Registry API","provider":"data.go.kr","priority":"P2","operations":[]}]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"search", "지역 설치", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "local-1"`) {
		t.Fatalf("expected default installed registry result: %s", stdout)
	}
	if !strings.Contains(stdout, `"callable": false`) {
		t.Fatalf("expected non-callable search metadata: %s", stdout)
	}
	for _, want := range []string{
		`"call_ready": false`,
		`"call_route": "not_callable"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in non-callable search metadata: %s", want, stdout)
		}
	}
}

func TestDoctorJSONWithDefaultRegistryAndAuth(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[{"id":"local-1","title":"지역 설치 Registry API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test"}]}]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"doctor", "--json"}, fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	registry := payload["registry"].(map[string]any)
	if registry["source"] != "default" || int(registry["specs"].(float64)) != 1 || int(registry["operations"].(float64)) != 1 {
		t.Fatalf("unexpected registry status: %#v", registry)
	}
	auth := payload["auth"].(map[string]any)
	if auth["credential_present"] != true || auth["selected_env_var"] != "DATA_PORTAL_API_KEY" {
		t.Fatalf("unexpected auth status: %#v", auth)
	}
	providers := payload["providers"].(map[string]any)
	if int(providers["adapter_count"].(float64)) < 2 || int(providers["host_count"].(float64)) < 2 {
		t.Fatalf("unexpected provider status: %#v", providers)
	}
	if payload["ready_for_search"] != true || payload["ready_for_calls"] != true {
		t.Fatalf("unexpected readiness: %#v", payload)
	}
	nextSteps := fmt.Sprint(payload["next_steps"])
	for _, want := range []string{
		"datapan ready --limit 10 --json",
		"datapan try \"단기예보\" base_date=20260622 --org 기상청 --json",
		"datapan coverage --json",
		"datapan studio --output-dir .datapan/studio --limit 200 --json",
	} {
		if !strings.Contains(nextSteps, want) {
			t.Fatalf("expected %q next step: %#v", want, payload["next_steps"])
		}
	}
}

func TestStatusJSONAliasesDoctor(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[{"id":"local-1","title":"지역 설치 Registry API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test"}]}]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"status", "--json"}, fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"ready_for_calls": true`,
		`"credential_present": true`,
		`datapan try \"단기예보\" base_date=20260622 --org 기상청 --json`,
		`datapan coverage --json`,
		`datapan studio --output-dir .datapan/studio --limit 200 --json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in status output: %s", want, stdout)
		}
	}
}

func TestStatusJSONReportsInstalledReleaseEvidence(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"local-1","title":"지역 설치 Registry API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test"}]},
		{"id":"local-2","title":"외부 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"조회","endpoint":"https://external.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultReleaseVerificationPath), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		defaultReleaseManifestPath: "{}",
		defaultReleaseVerificationPath: `{
			"generated_at": "2026-06-24T00:00:00Z",
			"provider": "data.go.kr",
			"registry": ".datapan/data-go-kr.registry.json",
			"limit": 2,
			"truncated": false,
			"filtered_count": 2,
			"summary": {"total": 2, "verified": 1, "failed": 0, "skipped": 1, "unknown": 0},
			"results": []
		}`,
		defaultReleaseRouteDispositionPath: `{
			"generated_at": "2026-06-24T00:00:00Z",
			"provider": "data.go.kr",
			"limit": 0,
			"truncated": false,
			"summary": {
				"routes_total": 1,
				"operations": 1,
				"hosts": 1,
				"with_probe_evidence": 1,
				"without_probe_evidence": 0,
				"dead_route_candidates": 1,
				"transient_failures": 0,
				"parameter_blocked_routes": 0,
				"adapter_candidates": 0,
				"by_disposition": []
			},
			"routes": []
		}`,
		defaultReleaseCoveragePath: `{
			"generated_at": "2026-06-24T00:00:00Z",
			"provider": "data.go.kr",
			"registry": ".datapan/data-go-kr.registry.json",
			"source": "release",
			"summary": {
				"specs": 2,
				"operations": 2,
				"callable_operations": 2,
				"callable_operation_percent": 100,
				"external_adapter_coverage_percent": 99.5
			},
			"route_evidence": {
				"present": true,
				"evidence_adjusted_adapter_candidates": 0
			}
		}`,
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := osWriteFile(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}

	code, stdout, stderr := runTest([]string{"status", "--json"}, fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"release_evidence":`,
		`"present": true`,
		`"release_dir": ".datapan/release"`,
		`"file_count": 4`,
		`"verification_total": 2`,
		`"verification_verified": 1`,
		`"verification_evidence_operation_percent": 100`,
		`"route_disposition_routes": 1`,
		`"route_disposition_adapter_candidates": 0`,
		`"callable_operation_percent": 100`,
		`"external_adapter_coverage_percent": 99.5`,
		`"evidence_adjusted_adapter_candidates": 0`,
		`"coverage_command": "datapan coverage --registry .datapan/data-go-kr.registry.json --verification .datapan/release/reports/latest-verification.json --route-disposition .datapan/release/reports/route-disposition.json --json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in status release evidence: %s", want, stdout)
		}
	}
}

func TestStatusHumanTitle(t *testing.T) {
	code, stdout, stderr := runTest([]string{"status"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "Datapan status") {
		t.Fatalf("expected status title: %s", stdout)
	}
	if !strings.Contains(stdout, "release evidence:") {
		t.Fatalf("expected release evidence status: %s", stdout)
	}
}

func TestCommandHelpDoesNotExecuteRefCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`not-json`)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "kit", args: []string{"kit", "--help"}, want: "datapan kit <ref>"},
		{name: "export", args: []string{"export", "--help"}, want: "datapan export --format postman"},
		{name: "codegen", args: []string{"codegen", "go", "--help"}, want: "datapan codegen go <ref>"},
		{name: "catalog release", args: []string{"catalog", "release", "verify", "--help"}, want: "datapan catalog release verify --manifest PATH"},
		{name: "help topic", args: []string{"help", "access", "login"}, want: "datapan access login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runTest(tt.args, nil, nil)
			if code != exitOK {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("expected no stderr, got %s", stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("expected %q in command help: %s", tt.want, stdout)
			}
		})
	}
}

func TestInitNextStepsUseTopLevelShortcuts(t *testing.T) {
	steps := fmt.Sprint(initNextSteps(defaultRegistryPath, true))
	for _, want := range []string{
		"datapan ready --limit 10 --json",
		"datapan try \"단기예보\" base_date=20260622 --org 기상청 --json",
		"datapan coverage --json",
		"datapan studio --output-dir .datapan/studio --limit 200 --json",
		"datapan status --json",
	} {
		if !strings.Contains(steps, want) {
			t.Fatalf("expected %q in init next steps: %s", want, steps)
		}
	}
	if strings.Contains(steps, "datapan doctor --json") {
		t.Fatalf("expected status shortcut instead of doctor in init next steps: %s", steps)
	}
}

func TestCatalogOverviewJSONLoadsDefaultRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","source_category":"교통물류","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","source_category":"교통물류","operations":[{"name":"외부","endpoint":"https://external.example.test/api"}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"다른기관","source_category":"환경기상","operations":[{"name":"EKAPE","endpoint":"http://data.ekape.or.kr/openapi-data/service/user/confirm/eggCoustomer"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "overview.json")
	code, stdout, stderr := runTest([]string{"catalog", "overview", "--limit", "2", "--output", output, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"specs": 3`,
		`"operations": 3`,
		`"organizations": 2`,
		`"categories": 2`,
		`"data_go_kr_gateway_operations": 1`,
		`"external_endpoint_operations": 2`,
		`"registered_adapter_operations": 1`,
		`"missing_adapter_operations": 1`,
		`"adapter_count": 51`,
		`"name": "기관"`,
		`"host": "external.example.test"`,
		`datapan coverage --registry .datapan/data-go-kr.registry.json --json`,
		`datapan providers --registry .datapan/data-go-kr.registry.json --gaps --limit 20 --json`,
		`datapan targets --registry .datapan/data-go-kr.registry.json --limit 20 --json`,
		`datapan verify --registry .datapan/data-go-kr.registry.json --provider forest --kind external_endpoint --limit 4 --json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in overview output: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"provider": "data.go.kr"`) || !strings.Contains(string(data), `"missing_adapter_hosts"`) {
		t.Fatalf("unexpected overview report file: %s", data)
	}
}

func TestCatalogCoverageJSONIncludesVerificationEvidence(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	verificationPath := filepath.Join(dir, "verification.json")
	routeDispositionPath := filepath.Join(dir, "route-disposition.json")
	output := filepath.Join(dir, "coverage.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list","default_params":{"pageNo":"1"}}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"외부","endpoint":"https://external.example.test/api"}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","operations":[{"name":"EKAPE","endpoint":"http://data.ekape.or.kr/openapi-data/service/user/confirm/eggCoustomer"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(verificationPath, []byte(`{
		"generated_at":"2026-06-24T00:00:00Z",
		"provider":"data.go.kr",
		"registry":"registry.json",
		"limit":3,
		"timeout":"10s",
		"truncated":false,
		"filtered_count":3,
		"summary":{"total":3,"verified":1,"failed":1,"skipped":1,"unknown":0},
		"results":[
			{"dataset_id":"100","title":"A","operation":"목록","provider":"data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"},
			{"dataset_id":"200","title":"B","operation":"외부","provider":"data.go.kr","endpoint_host":"external.example.test","dependency_class":"external_endpoint","status":"skipped","reason":"external_provider_adapter_missing"},
			{"dataset_id":"300","title":"C","operation":"EKAPE","provider":"ekape","endpoint_host":"data.ekape.or.kr","dependency_class":"external_endpoint","status":"failed","reason":"provider_error"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(routeDispositionPath, []byte(`{
		"generated_at":"2026-06-24T00:00:00Z",
		"provider":"data.go.kr",
		"registry":"registry.json",
		"probe":"probe.json",
		"limit":0,
		"truncated":false,
		"summary":{
			"routes_total":1,
			"operations":1,
			"hosts":1,
			"with_probe_evidence":1,
			"without_probe_evidence":0,
			"dead_route_candidates":1,
			"transient_failures":0,
			"parameter_blocked_routes":0,
			"adapter_candidates":0,
			"by_disposition":[{"key":"dead_route_candidate","count":1,"disposition":"dead_route_candidate"}]
		},
		"routes":[]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "coverage", "--registry", registryPath, "--verification", verificationPath, "--route-disposition", routeDispositionPath, "--limit", "1", "--output", output, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"operations": 3`,
		`"callable_operations": 3`,
		`"callable_operation_percent": 100`,
		`"registered_adapter_operations": 1`,
		`"missing_adapter_operations": 1`,
		`"external_adapter_coverage_percent": 50`,
		`"goals":`,
		`"callable_operation_percent_target": 99`,
		`"callable_operation_percent_met": true`,
		`"external_adapter_coverage_percent_target": 98`,
		`"external_adapter_coverage_percent_met": false`,
		`"evidence_operation_percent_target": 10`,
		`"evidence_operation_percent_met": true`,
		`"present": true`,
		`"timeout": "10s"`,
		`"verified": 1`,
		`"evidence_operation_percent": 100`,
		`"route_evidence":`,
		`"routes_total": 1`,
		`"dead_route_candidates": 1`,
		`"remaining_adapter_candidates": 0`,
		`"raw_missing_adapter_operations": 1`,
		`"evidence_adjusted_adapter_candidates": 0`,
		`"host": "external.example.test"`,
		`datapan coverage --registry`,
		`datapan providers --registry`,
		`datapan targets --registry`,
		`datapan verify --registry`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in coverage output: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"verification":`) || !strings.Contains(string(data), `"provider_split_ready": true`) || !strings.Contains(string(data), `"missing_adapter_operations_target": 10`) {
		t.Fatalf("unexpected coverage report file: %s", data)
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.coverage.v1.schema.json", data); err != nil {
		t.Fatalf("coverage report schema validation failed: %v", err)
	}
}

func TestCoverageTopLevelLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"외부","endpoint":"https://external.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"coverage", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"registry": ".datapan/data-go-kr.registry.json"`,
		`"specs": 2`,
		`"operations": 2`,
		`"missing_adapter_operations": 1`,
		`"label": "missing adapters"`,
		`"command": "datapan providers --registry .datapan/data-go-kr.registry.json --gaps --limit 20 --json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in coverage output: %s", want, stdout)
		}
	}
}

func TestCoverageAutoLoadsInstalledReleaseEvidence(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"외부","endpoint":"https://external.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultReleaseVerificationPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultReleaseVerificationPath, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"registry": ".datapan/data-go-kr.registry.json",
		"limit": 2,
		"timeout": "10s",
		"truncated": false,
		"filtered_count": 2,
		"summary": {"total": 2, "verified": 1, "failed": 0, "skipped": 1, "unknown": 0},
		"results": [
			{"dataset_id":"100","title":"기관_A","operation":"목록","provider":"data.go.kr","endpoint_host":"apis.data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"},
			{"dataset_id":"200","title":"기관_B","operation":"외부","provider":"data.go.kr","endpoint_host":"external.example.test","dependency_class":"external_endpoint","status":"skipped","reason":"external_provider_adapter_missing"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultReleaseRouteDispositionPath, []byte(`{
		"generated_at": "2026-06-24T00:00:00Z",
		"provider": "data.go.kr",
		"registry": ".datapan/data-go-kr.registry.json",
		"probe": ".datapan/release/reports/latest-verification.json",
		"limit": 0,
		"truncated": false,
		"summary": {
			"routes_total": 1,
			"operations": 1,
			"hosts": 1,
			"with_probe_evidence": 1,
			"without_probe_evidence": 0,
			"dead_route_candidates": 1,
			"transient_failures": 0,
			"parameter_blocked_routes": 0,
			"adapter_candidates": 0,
			"by_disposition": []
		},
		"routes": [
			{"dataset_id":"200","title":"기관_B","operation":"외부","endpoint_host":"external.example.test","dependency_class":"external_endpoint","disposition":"dead_route_candidate","recommended_action":"exclude"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"coverage", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"verification": ".datapan/release/reports/latest-verification.json"`,
		`"route_disposition": ".datapan/release/reports/route-disposition.json"`,
		`"evidence_operation_percent": 100`,
		`"route_evidence":`,
		`"remaining_adapter_candidates": 0`,
		`"evidence_adjusted_adapter_candidates": 0`,
		`datapan coverage --registry .datapan/data-go-kr.registry.json --verification .datapan/release/reports/latest-verification.json --route-disposition .datapan/release/reports/route-disposition.json --json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in auto evidence coverage output: %s", want, stdout)
		}
	}
}

func TestCatalogCoverageLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"외부","endpoint":"https://external.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"catalog", "coverage", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"registry": ".datapan/data-go-kr.registry.json"`,
		`"specs": 2`,
		`"missing_adapter_operations": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in catalog coverage output: %s", want, stdout)
		}
	}
}

func TestCatalogProvidersLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"500","title":"산림청_숲 이야기","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "providers", "--status", "adapter", "--provider", "forest", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"provider": "forest"`,
		`"host": "api.forest.go.kr"`,
		`"adapter_status": "adapter"`,
		`"next_commands":`,
		`"verify": "datapan verify --host api.forest.go.kr --limit 3 --json"`,
		`"filtered_count": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in provider output: %s", want, stdout)
		}
	}
}

func TestProvidersTopLevelShortcutsLoadDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"500","title":"산림청_숲 이야기","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}]},
		{"id":"600","title":"외부_미등록 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://missing.example.test/openapi/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"providers", "--adapters", "--provider", "forest", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"provider": "forest"`,
		`"host": "api.forest.go.kr"`,
		`"adapter_status": "adapter"`,
		`"filtered_count": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in provider adapter output: %s", want, stdout)
		}
	}

	code, stdout, stderr = runTest([]string{"providers", "--gaps", "--limit", "1", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"status": "missing"`,
		`"kind": "external_endpoint"`,
		`"host": "missing.example.test"`,
		`"adapter_status": "missing"`,
		`"adapter_targets": "datapan targets --host missing.example.test --limit 5 --json"`,
		`"dependencies": "datapan ops --host missing.example.test --limit 20 --json"`,
		`"filtered_count": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in provider gap output: %s", want, stdout)
		}
	}
}

func TestProvidersSplitJSONUsesDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"500","title":"산림청_숲 이야기","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}]},
		{"id":"600","title":"외부_미등록 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://missing.example.test/openapi/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"providers", "--split", "--limit", "1", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"registry": ".datapan/data-go-kr.registry.json"`,
		`"split_readiness":`,
		`"status": "ready"`,
		`"provider_split_ready": true`,
		`"registered_adapter_operations": 1`,
		`"missing_adapter_operations": 1`,
		`"external_adapter_coverage_percent": 50`,
		`"host": "missing.example.test"`,
		`"label": "provider adapters"`,
		`"command": "datapan providers --registry .datapan/data-go-kr.registry.json --adapters --json"`,
		`"label": "provider gaps"`,
		`"command": "datapan providers --registry .datapan/data-go-kr.registry.json --gaps --limit 20 --json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in provider split output: %s", want, stdout)
		}
	}
}

func TestProvidersSplitHumanOutput(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"500","title":"산림청_숲 이야기","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}]},
		{"id":"600","title":"외부_미등록 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://missing.example.test/openapi/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"providers", "--split", "--registry", registryPath, "--limit", "1"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"Provider split readiness",
		"status: ready",
		"ready: true",
		"external adapter coverage: 1/2 operations (50.0%)",
		"Top missing adapter hosts",
		"missing.example.test: 1 ops",
		"recommendation: adapter boundary has enough exercised surface to consider a datapan-providers split",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in human provider split output: %s", want, stdout)
		}
	}
}

func TestProvidersSplitRejectsConflictingShortcut(t *testing.T) {
	code, _, stderr := runTest([]string{"providers", "--split", "--gaps"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "use only one of --split, --adapters, --gaps, or --missing") {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
}

func TestOpsTopLevelLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://missing.example.test/api/list","request_params":[{"name":"q"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"ops", "--host", "missing.example.test", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"filtered_count": 1`,
		`"dataset_id": "100"`,
		`"operation": "목록"`,
		`"endpoint_host": "missing.example.test"`,
		`"adapter_status": "missing"`,
		`"skip_reason": "external_provider_adapter_missing"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in ops output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "200"`) {
		t.Fatalf("ops output included non-matching operation: %s", stdout)
	}
}

func TestVerifyTopLevelLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://external.example.test/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"verify", "--host", "external.example.test", "--limit", "1", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"dataset_id": "100"`,
		`"endpoint_host": "external.example.test"`,
		`"status": "skipped"`,
		`"reason": "external_provider_adapter_missing"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in verify output: %s", want, stdout)
		}
	}
}

func TestTargetsTopLevelLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://missing.example.test/api/list","request_params":[{"name":"q"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runTest([]string{"targets", "--host", "missing.example.test", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"filtered_count": 1`,
		`"host": "missing.example.test"`,
		`"rank": 1`,
		`"sample_operations":`,
		`"dataset_id": "100"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in target output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"host": "openapi.q-net.or.kr"`) {
		t.Fatalf("target output included registered adapter host: %s", stdout)
	}
}

func TestCatalogDependenciesLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"500","title":"산림청_숲 이야기","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "dependencies", "--status", "adapter", "--provider", "forest", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"provider_family": "forest"`,
		`"endpoint_host": "api.forest.go.kr"`,
		`"adapter_status": "adapter"`,
		`"filtered_count": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in dependencies output: %s", want, stdout)
		}
	}
}

func TestCatalogStudioWritesConsumerBundle(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	outputDir := filepath.Join(dir, "studio")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A API","provider":"data.go.kr","priority":"P2","organization":"기관_A","source_category":"환경기상","description":"테스트 설명","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list","request_params":[{"name":"pageNo","label":"페이지"}],"response_params":[{"name":"resultCode","label":"결과코드"}]}]},
		{"id":"200","title":"기관_B API","provider":"data.go.kr","priority":"P2","organization":"기관_B","operations":[]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "studio", "--registry", registryPath, "--output-dir", outputDir, "--query", "기관_A", "--limit", "5", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"output_dir": "` + jsonEscaped(outputDir) + `"`,
		`"count": 1`,
		`"kind": "overview"`,
		`"kind": "datasets"`,
		`"kind": "bundle"`,
		`"kind": "viewer"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in studio output: %s", want, stdout)
		}
	}
	for _, name := range []string{"overview.json", "datasets.json", "studio.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	data, err := osReadFile(filepath.Join(outputDir, "datasets.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"schema_version": "datapan.studio-datasets.v1"`,
		`"id": "100"`,
		`"callable": true`,
		`"kit": "datapan kit 100 --operation \"목록\" pageNo=1 --json"`,
		`"request_params":`,
		`"response_params_count": 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in studio datasets: %s", want, text)
		}
	}
	bundle, err := osReadFile(filepath.Join(outputDir, "studio.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bundle), `"schema_version": "datapan.studio-bundle.v1"`) || !strings.Contains(string(bundle), `"split_readiness"`) {
		t.Fatalf("unexpected studio bundle: %s", bundle)
	}
	viewer, err := osReadFile(filepath.Join(outputDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<title>Datapan Studio Bundle</title>`,
		`id="datapan-data"`,
		`Search datasets, organizations, commands`,
		`datapan.studio-bundle.v1`,
	} {
		if !strings.Contains(string(viewer), want) {
			t.Fatalf("expected %q in studio viewer: %s", want, viewer)
		}
	}
}

func TestStudioTopLevelLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"100","title":"기관_A API","provider":"data.go.kr","priority":"P2","organization":"기관_A","source_category":"환경기상","description":"테스트 설명","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list","request_params":[{"name":"pageNo","label":"페이지"}],"response_params":[{"name":"resultCode","label":"결과코드"}]}]},
		{"id":"200","title":"기관_B API","provider":"data.go.kr","priority":"P2","organization":"기관_B","operations":[]}
	]`)); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "studio")

	code, stdout, stderr := runTest([]string{"studio", "--output-dir", outputDir, "--query", "기관_A", "--limit", "5", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"source": "default"`,
		`"registry": ".datapan/data-go-kr.registry.json"`,
		`"count": 1`,
		`"kind": "viewer"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in studio output: %s", want, stdout)
		}
	}
	viewer, err := osReadFile(filepath.Join(outputDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(viewer), `Datapan Studio Bundle`) || !strings.Contains(string(viewer), `기관_A API`) {
		t.Fatalf("unexpected studio viewer: %s", viewer)
	}
}

func TestDoctorJSONSuggestsInstallAndAuth(t *testing.T) {
	t.Chdir(t.TempDir())
	code, stdout, stderr := runTest([]string{"doctor", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"source": "embedded"`) {
		t.Fatalf("expected embedded registry status: %s", stdout)
	}
	if !strings.Contains(stdout, `datapan catalog install datapan-registry --json`) {
		t.Fatalf("expected install next step: %s", stdout)
	}
	if !strings.Contains(stdout, `DATAPAN_DATA_GO_KR_KEY`) {
		t.Fatalf("expected auth next step: %s", stdout)
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
	for _, want := range []string{
		`"callable": true`,
		`"call_ready": true`,
		`"call_route": "data_go_kr_gateway"`,
		`"call_provider": "data.go.kr"`,
		`"default_operation":`,
		`"examples":`,
		`"show": "datapan show 15126469"`,
		`"kit": "datapan kit 15126469`,
		`--json`,
		`"params": "datapan params 15126469`,
		`"curl": "datapan curl 15126469`,
		`"openapi": "datapan export --format openapi 15126469`,
		`"codegen_go": "datapan codegen go 15126469`,
		`"codegen_node": "datapan codegen node 15126469`,
		`"codegen_python": "datapan codegen python 15126469`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in search output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `serviceKey=VALUE`) {
		t.Fatalf("search examples should not ask for serviceKey: %s", stdout)
	}
}

func TestSearchHumanOutputShowsNextCommands(t *testing.T) {
	code, stdout, stderr := runTest([]string{"search", "실거래", "--org", "국토교통부", "--limit", "1"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{
		`callable: yes`,
		`call ready: yes (data.go.kr gateway)`,
		`next: datapan show 15126469`,
		`try: datapan get 15126469`,
		`kit: datapan kit 15126469`,
		`--json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in search output: %s", want, stdout)
		}
	}
}

func TestSearchJSONShowsCallRouteMetadata(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"epost-route","title":"EPost Route API","provider":"data.go.kr","priority":"P2","organization":"우정사업본부","operations":[{"name":"요금제","endpoint":"http://openapi.epost.go.kr/api"}]},
		{"id":"qnet-route","title":"QNet Route API","provider":"data.go.kr","priority":"P2","organization":"한국산업인력공단","operations":[{"name":"자격","endpoint":"http://openapi.q-net.or.kr/api"}]},
		{"id":"external-route","title":"External Route API","provider":"data.go.kr","priority":"P2","organization":"외부기관","operations":[{"name":"목록","endpoint":"https://partner.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"search", "Route", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"id": "epost-route"`,
		`"call_ready": true`,
		`"call_route": "provider_adapter"`,
		`"call_provider": "epost"`,
		`"endpoint_host": "openapi.epost.go.kr"`,
		`"id": "qnet-route"`,
		`"call_route": "provider_adapter_verification_only"`,
		`"call_provider": "q-net"`,
		`"endpoint_host": "openapi.q-net.or.kr"`,
		`"id": "external-route"`,
		`"call_route": "generic_external"`,
		`"endpoint_host": "partner.example.test"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in call route metadata: %s", want, stdout)
		}
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

func TestListAllowsEmptyDatasetListing(t *testing.T) {
	code, stdout, stderr := runTest([]string{"list", "--limit", "2", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"query": ""`,
		`"count": 2`,
		`"results":`,
		`"examples":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in list output: %s", want, stdout)
		}
	}
}

func TestLsFiltersLikeSearch(t *testing.T) {
	code, stdout, stderr := runTest([]string{"ls", "--org", "기상청", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id": "15084084"`) || !strings.Contains(stdout, `"organization": "기상청"`) {
		t.Fatalf("expected ls to reuse search filters: %s", stdout)
	}
}

func TestListCallableFiltersBeforeLimit(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Not Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]},
		{"id":"200","title":"Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"300","title":"Also Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"조회","endpoint":"https://apis.data.go.kr/test/get"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"list", "--callable", "--limit", "1", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"callable_only": true`,
		`"count": 1`,
		`"id": "200"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in callable list output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"id": "100"`) {
		t.Fatalf("expected not-callable spec to be filtered out: %s", stdout)
	}
}

func TestSearchCallableCountsAsFilter(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Not Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]},
		{"id":"200","title":"Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"search", "--callable", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id": "200"`) || strings.Contains(stdout, `"id": "100"`) {
		t.Fatalf("expected search --callable to filter callable specs without a query: %s", stdout)
	}
}

func TestSearchCallReadyFiltersBeforeLimit(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Not Callable API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]},
		{"id":"200","title":"QNet Verification Only API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"자격","endpoint":"http://openapi.q-net.or.kr/api"}]},
		{"id":"300","title":"Ready Gateway API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"400","title":"Ready Provider API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"요금","endpoint":"http://openapi.epost.go.kr/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"search", "--call-ready", "--limit", "1", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"call_ready_only": true`,
		`"count": 1`,
		`"id": "300"`,
		`"call_ready": true`,
		`"call_route": "data_go_kr_gateway"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in call-ready list output: %s", want, stdout)
		}
	}
	for _, notWant := range []string{`"id": "100"`, `"id": "200"`} {
		if strings.Contains(stdout, notWant) {
			t.Fatalf("expected not-ready spec to be filtered out: %s", stdout)
		}
	}
}

func TestSearchReadyAlias(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Generic External API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://partner.example.test/api"}]},
		{"id":"200","title":"Ready Provider API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"요금","endpoint":"http://openapi.epost.go.kr/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"ls", "--ready", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id": "200"`) || strings.Contains(stdout, `"id": "100"`) {
		t.Fatalf("expected --ready alias to filter call-ready specs: %s", stdout)
	}
}

func TestReadyCommandFiltersCallReadySpecs(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Generic External API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://partner.example.test/api"}]},
		{"id":"150","title":"Ready 취소 API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"신청취소처리","endpoint":"https://apis.data.go.kr/test/cancel"}]},
		{"id":"200","title":"Ready Gateway API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]},
		{"id":"300","title":"Ready Provider API","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"요금","endpoint":"http://openapi.epost.go.kr/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"ready", "--limit", "1", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"call_ready_only": true`,
		`"count": 1`,
		`"id": "200"`,
		`"call_ready": true`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in ready output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"id": "100"`) {
		t.Fatalf("ready should filter generic external specs: %s", stdout)
	}
	if strings.Contains(stdout, `"id": "150"`) {
		t.Fatalf("ready should rank read-style APIs before action-style APIs: %s", stdout)
	}
}

func TestTrySelectsCallReadySpecAndBuildsCommands(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"Weather Generic External API","provider":"data.go.kr","priority":"P2","organization":"기상청","operations":[{"name":"목록","endpoint":"https://partner.example.test/api","request_params":[{"name":"q"}]}]},
		{"id":"200","title":"Weather Gateway API","provider":"data.go.kr","priority":"P1","organization":"기상청","operations":[{"name":"단기예보","endpoint":"https://apis.data.go.kr/test/weather","request_params":[{"name":"serviceKey"},{"name":"pageNo","label":"페이지"},{"name":"numOfRows","label":"행수"},{"name":"base_date","label":"발표일자"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"try", "weather", "base_date=20260622", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"call_ready_only": true`,
		`"dataset": "200"`,
		`"operation": "단기예보"`,
		`"base_date": "20260622"`,
		`"pageNo": "1"`,
		`"numOfRows": "10"`,
		`datapan get 200 --operation`,
		`--params-file 200_params.json --json`,
		`datapan export --format postman 200`,
		`datapan codegen python 200`,
		`"next_steps":`,
		`"label": "write params"`,
		`"command": "datapan params 200 --operation`,
		`"label": "dry run"`,
		`"label": "call api"`,
		`"label": "starter kit"`,
		`datapan kit 200 --operation`,
		`--params-file 200_params.json --output-dir 200-kit --json`,
		`"label": "status"`,
		`"command": "datapan status --json"`,
		`"label": "coverage"`,
		`"command": "datapan coverage --registry`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in try output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `serviceKey=VALUE`) || strings.Contains(stdout, `"dataset": "100"`) {
		t.Fatalf("try should not leak auth params or select not-ready route: %s", stdout)
	}
}

func TestTryHumanOutputShowsOrderedNextSteps(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"200","title":"Weather Gateway API","provider":"data.go.kr","priority":"P1","organization":"기상청","operations":[{"name":"단기예보","endpoint":"https://apis.data.go.kr/test/weather","request_params":[{"name":"serviceKey"},{"name":"base_date"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"try", "weather", "base_date=20260622"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`Datapan try`,
		`commands:`,
		`next:`,
		`write params: datapan params 200`,
		`dry run: datapan get 200`,
		`call api: datapan get 200`,
		`starter kit: datapan kit 200`,
		`status: datapan status --json`,
		`coverage: datapan coverage --registry`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in try human output: %s", want, stdout)
		}
	}
}

func TestTryLoadsDefaultInstalledRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`[
		{"id":"local-try","title":"Local Try API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/local/list","request_params":[{"name":"pageNo"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"try", "Local", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"dataset": "local-try"`,
		`"registry_source": "default"`,
		`"pageNo": "1"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in try output: %s", want, stdout)
		}
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
						{"name": "authApiKey", "label": "인증키2"},
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
		`"call_ready": false`,
		`"call_route": "generic_external"`,
		`"endpoint_host": "example.test"`,
		`"request_count": 421`,
		`"request_params":`,
		`"auth_params":`,
		`"name": "serviceKey"`,
		`"name": "authApiKey"`,
		`"name": "PAGE"`,
		`"label": "페이지"`,
		`"response_params_count": 1`,
		`"example": "datapan get 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --json"`,
		`"kit": "datapan kit 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --json"`,
		`"params": "datapan params 999 --operation \"목록 조회\" --output 999_params.json"`,
		`"curl": "datapan curl 999 --operation \"목록 조회\" PAGE=1 ROWS=10"`,
		`"postman": "datapan export --format postman 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --output 999.postman_collection.json"`,
		`"openapi": "datapan export --format openapi 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --output 999.openapi.json"`,
		`"codegen_go": "datapan codegen go 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --output 999_client.go"`,
		`"codegen_node": "datapan codegen node 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --output 999_client.js"`,
		`"codegen_python": "datapan codegen python 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --output 999_client.py"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `serviceKey=VALUE`) || strings.Contains(stdout, `authApiKey=VALUE`) {
		t.Fatalf("show examples should not ask users to pass auth params: %s", stdout)
	}
}

func TestParamsWritesReusableParamsFile(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	paramsPath := dir + "/params.json"
	if err := osWriteFile(registryPath, []byte(`[
		{
			"id": "999",
			"title": "테스트기관_테스트 API",
			"provider": "data.go.kr",
			"operations": [
				{
					"name": "목록 조회",
					"endpoint": "https://apis.data.go.kr/test/list",
					"default_params": {"pageNo": "1"},
					"request_params": [
						{"name": "serviceKey", "label": "인증키"},
						{"name": "pageNo", "label": "페이지"},
						{"name": "numOfRows", "label": "행수"},
						{"name": "keyword", "label": "검색어"}
					]
				}
			],
			"smoke": {
				"operation": "목록 조회",
				"params": {"keyword": "소나무", "numOfRows": "1"}
			}
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"params", "999", "keyword=해운대", "serviceKey=should-not-write", "--operation", "목록 조회", "--param", "numOfRows=5", "--output", paramsPath, "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"dataset": "999"`,
		`"operation": "목록 조회"`,
		`"output": "` + jsonEscaped(paramsPath) + `"`,
		`"next_get": "datapan get 999 --operation \"목록 조회\" --params-file`,
		`--json"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in params summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(paramsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"keyword": "해운대"`,
		`"numOfRows": "5"`,
		`"pageNo": "1"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in params file: %s", want, text)
		}
	}
	if strings.Contains(text, "serviceKey") {
		t.Fatalf("params file should not include auth params: %s", text)
	}

	code, stdout, stderr = runTest(
		[]string{"get", "999", "--operation", "목록 조회", "--params-file", paramsPath, "--timeout", "5s", "--dry-run", "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"dry_run": true`,
		`"keyword": "해운대"`,
		`"numOfRows": "5"`,
		`"pageNo": "1"`,
		`"timeout": "5s"`,
		`serviceKey=REDACTED`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in get dry-run: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("dry-run should not leak key: %s", stdout)
	}
}

func TestGetUsesCallCapableForestAdapter(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{
			"id": "forest-1",
			"title": "산림청_숲 이야기",
			"provider": "data.go.kr",
			"priority": "P2",
			"operations": [
				{
					"name": "숲 이야기",
					"endpoint": "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI",
					"request_params": [
						{"name": "serviceKey"},
						{"name": "searchWrd"},
						{"name": "pageNo"}
					]
				}
			]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.forest.go.kr" {
			t.Fatalf("expected forest host, got %s", req.URL.Host)
		}
		if req.URL.Query().Get("serviceKey") != "secret-value" {
			t.Fatalf("expected service key in forest request: %s", req.URL.RawQuery)
		}
		if req.URL.Query().Get("searchWrd") != "소나무" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected forest request params: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE.</resultMsg></header><body><items><item><fsname>소나무</fsname></item></items></body></response>`)),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "forest-1", "--operation", "숲 이야기", "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"provider": "forest"`,
		`"dataset": "forest-1"`,
		`"semantic_status": "provider_ok"`,
		`"url": "http://api.forest.go.kr/openapi/service/cultureInfoService/fStoryOpenAPI?`,
		`serviceKey=REDACTED`,
		`<fsname>소나무</fsname>`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in forest get output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("forest get output should not leak key: %s", stdout)
	}
}

func TestGetTimeoutCancelsRequest(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"slow-1","title":"느린 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/slow/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	code, stdout, stderr := runTest(
		[]string{"get", "slow-1", "--operation", "목록", "--timeout", "1ms", "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"error": "request_failed"`,
		`"dataset": "slow-1"`,
		`"timeout": "1ms"`,
		`context deadline exceeded`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in timeout output: %s", want, stdout)
		}
	}
}

func TestGetRejectsInvalidTimeout(t *testing.T) {
	code, _, stderr := runTest([]string{"get", "15084084", "--timeout", "0", "--json"}, fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"}, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--timeout requires a positive duration") {
		t.Fatalf("expected timeout usage error: %s", stderr)
	}
}

func TestUsePlansDatasetCommands(t *testing.T) {
	paramsPath := t.TempDir() + "/params.json"
	if err := osWriteFile(paramsPath, []byte(`{"base_date":"20240101","base_time":"0100","nx":"55"}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"use", "15084084", "--params-file", paramsPath, "base_date=20260622", "--param", "base_time=0500", "--param", "nx=60", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"dataset": "15084084"`,
		`"operation": "getVilageFcst"`,
		`"base_date": "20260622"`,
		`"base_time": "0500"`,
		`"nx": "60"`,
		`"uses_params_file": "15084084_params.json"`,
		`"params": "datapan params 15084084`,
		`base_date=20260622`,
		`base_time=0500`,
		`nx=60`,
		`"dry_run": "datapan get 15084084`,
		`--params-file 15084084_params.json --dry-run --json`,
		`"save_csv": "datapan save 15084084`,
		`"postman": "datapan export --format postman 15084084`,
		`"openapi": "datapan export --format openapi 15084084`,
		`"codegen_go": "datapan codegen go 15084084`,
		`"codegen_node": "datapan codegen node 15084084`,
		`"codegen_python": "datapan codegen python 15084084`,
		`"next_steps":`,
		`"label": "write params"`,
		`"label": "starter kit"`,
		`datapan kit 15084084`,
		`--params-file 15084084_params.json --output-dir 15084084-kit --json`,
		`"label": "status"`,
		`"label": "coverage"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in use plan: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") || strings.Contains(stdout, "serviceKey") || strings.Contains(stdout, "20240101") || strings.Contains(stdout, "0100") || strings.Contains(stdout, "nx=55") {
		t.Fatalf("use plan should not leak credential material or auth params: %s", stdout)
	}
}

func TestUseWritesStarterKit(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runTest(
		[]string{"use", "15084084", "--output-dir", dir, "base_date=20260622", "--param", "base_time=0500", "--param", "nx=60", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"output_dir": "` + jsonEscaped(dir) + `"`,
		`"kind": "params"`,
		`"kind": "curl"`,
		`"kind": "postman"`,
		`"kind": "openapi"`,
		`"kind": "codegen_go"`,
		`"kind": "codegen_node"`,
		`"kind": "codegen_python"`,
		`"kind": "readme"`,
		`--params-file`,
		jsonEscaped(filepath.Join(dir, "15084084_params.json")),
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in use kit summary: %s", want, stdout)
		}
	}
	for _, name := range []string{
		"15084084_params.json",
		"15084084.curl.sh",
		"15084084.postman_collection.json",
		"15084084.openapi.json",
		"15084084_client.go",
		"15084084_client.js",
		"15084084_client.py",
		"README.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected generated %s: %v", name, err)
		}
	}
	paramsData, err := osReadFile(filepath.Join(dir, "15084084_params.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(paramsData), `"base_date": "20260622"`) || strings.Contains(string(paramsData), "serviceKey") {
		t.Fatalf("unexpected params file: %s", paramsData)
	}
	for _, name := range []string{
		"15084084.curl.sh",
		"15084084.postman_collection.json",
		"15084084.openapi.json",
		"15084084_client.go",
		"15084084_client.js",
		"15084084_client.py",
		"README.md",
	} {
		data, err := osReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret-value") {
			t.Fatalf("%s leaked secret: %s", name, data)
		}
	}
	if err := osWriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/generated\n\ngo 1.26\n")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated starter Go client should compile: %v\n%s", err, out)
	}
	if python, ok := findPythonForTest(); ok {
		cmd = exec.Command(python, "-m", "py_compile", filepath.Join(dir, "15084084_client.py"))
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("generated starter Python client should compile: %v\n%s", err, out)
		}
	}
	if node, ok := findNodeForTest(); ok {
		cmd = exec.Command(node, "--check", filepath.Join(dir, "15084084_client.js"))
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("generated starter Node client should parse: %v\n%s", err, out)
		}
	}
}

func TestKitWritesStarterKitWithDefaultOutputDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	code, stdout, stderr := runTest(
		[]string{"kit", "15084084", "base_date=20260622", "--param", "base_time=0500", "--param", "nx=60", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"output_dir": "15084084-kit"`,
		`"kind": "params"`,
		`"kind": "postman"`,
		`"kind": "openapi"`,
		`"kind": "codegen_go"`,
		jsonEscaped(filepath.Join("15084084-kit", "15084084_params.json")),
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in kit summary: %s", want, stdout)
		}
	}
	for _, name := range []string{
		"15084084_params.json",
		"15084084.curl.sh",
		"15084084.postman_collection.json",
		"15084084.openapi.json",
		"15084084_client.go",
		"15084084_client.js",
		"15084084_client.py",
		"README.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, "15084084-kit", name)); err != nil {
			t.Fatalf("expected generated %s: %v", name, err)
		}
	}
	if strings.Contains(stdout, "secret-value") || strings.Contains(stdout, "serviceKey") {
		t.Fatalf("kit summary should not leak credential material or auth params: %s", stdout)
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

func TestCatalogImportEnrichesLinkDetailOperations(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host + req.URL.Path {
		case "api.odcloud.kr/api/15077093/v1/open-data-list":
			body := `{
				"currentCount": 1,
				"data": [
					{"api_type": "LINK", "list_id": "15005231", "list_title": "경기도 정기간행물 현황", "title": "정기간행물 현황", "org_nm": "경기도", "data_format": "XML"}
				],
				"page": 1,
				"perPage": 1,
				"totalCount": 1
			}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		case "www.data.go.kr/data/15005231/openapi.do":
			body := `<a href="https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&amp;infId=ABC&amp;infSeq=3" onclick="fn_LinkApiRequest('uddi:test')">link</a>`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected request %s", req.URL)
			return nil, nil
		}
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "import", "data-go-kr", "--output", tmp, "--per-page", "1", "--pages", "1", "--enrich-link-details", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"operations": 1`, `"link_detail_enrichment"`, `"operations_added": 1`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := osReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"endpoint": "https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&infId=ABC&infSeq=3"`) {
		t.Fatalf("expected enriched endpoint in registry: %s", data)
	}
}

func TestCatalogImportTimeoutExpandsForLinkDetailEnrichment(t *testing.T) {
	if got := catalogImportTimeout(false, 0); got != 2*time.Minute {
		t.Fatalf("default timeout=%v", got)
	}
	if got := catalogImportTimeout(true, 25); got != 5*time.Minute {
		t.Fatalf("bounded enrichment timeout=%v", got)
	}
	if got := catalogImportTimeout(true, 0); got != 30*time.Minute {
		t.Fatalf("full enrichment timeout=%v", got)
	}
}

func TestCatalogEnrichLinkDetailsUpdatesExistingRegistry(t *testing.T) {
	dir := t.TempDir()
	input := dir + "/registry.json"
	output := dir + "/enriched.json"
	registry := `[{
		"id": "15005231",
		"title": "경기도 정기간행물 현황",
		"provider": "data.go.kr",
		"organization": "경기도",
		"priority": "P2",
		"operations": [],
		"source": {
			"system": "data.go.kr",
			"raw": {
				"api_type": "LINK",
				"list_id": "15005231",
				"list_title": "경기도 정기간행물 현황",
				"title": "정기간행물 현황",
				"operation_nm": "",
				"operation_url": "",
				"end_point_url": ""
			}
		}
	}]`
	if err := osWriteFile(input, []byte(registry)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://www.data.go.kr/data/15005231/openapi.do" {
			t.Fatalf("unexpected request %s", req.URL)
		}
		body := `<a href="https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&amp;infId=ABC&amp;infSeq=3" onclick="fn_LinkApiRequest('uddi:test')">link</a>`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "enrich", "link-details", "--registry", input, "--output", output, "--json"},
		nil,
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"operations_added": 1`) {
		t.Fatalf("expected enrichment summary: %s", stdout)
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"endpoint": "https://data.gg.go.kr/portal/data/service/selectServicePage.do?page=1&infId=ABC&infSeq=3"`) {
		t.Fatalf("expected enriched endpoint: %s", data)
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

func TestCatalogInstallDatapanRegistryDownloadsReleaseAsset(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	output := filepath.Join(dir, "registry.json")
	registry := `[{"id":"100","title":"테스트 API","provider":"data.go.kr","priority":"P2","operations":[]}]`
	zipData := zipFilesForTest(t, map[string]string{
		datapanRegistryZipRegistryPath: registry,
		"manifest.json": `{
			"schema_version": "datapan.release-manifest.v1",
			"generated_at": "2026-06-24T00:00:00Z",
			"datapan_version": "test",
			"provider": "data.go.kr",
			"source_registry": "registry.json",
			"output_dir": ".",
			"artifact_count": 29,
			"artifacts": []
		}`,
		"RELEASE_NOTES.md": "# Datapan Registry Release\n",
		"reports/latest-release-verification.json": `{
			"manifest": "manifest.json",
			"root": ".",
			"schema_version": "datapan.release-verification.v1",
			"manifest_schema_version": "datapan.release-manifest.v1",
			"artifact_count": 29,
			"checked": 29,
			"failed": 0,
			"ok": true,
			"results": []
		}`,
		"reports/latest-verification.json": `{
			"generated_at": "2026-06-24T00:00:00Z",
			"provider": "data.go.kr",
			"registry": "data/data-go-kr.registry.json",
			"limit": 1,
			"truncated": false,
			"filtered_count": 1,
			"summary": {"total": 1, "verified": 1, "failed": 0, "skipped": 0, "unknown": 0},
			"results": [
				{"dataset_id":"100","title":"테스트 API","operation":"목록","provider":"data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"}
			]
		}`,
		"reports/latest-release-readiness.json": `{
			"manifest": "manifest.json",
			"root": ".",
			"schema_version": "datapan.release-readiness.v1",
			"generated_at": "2026-06-24T00:00:00Z",
			"datapan_version": "test",
			"provider": "data.go.kr",
			"ready": true,
			"summary": {
				"gates_total": 16,
				"passed": 16,
				"warned": 0,
				"failed": 0,
				"required_artifacts": 10,
				"missing_required_artifacts": 0,
				"recommended_artifacts": 3,
				"missing_recommended_artifacts": 0,
				"schema_artifacts": 16,
				"registry_specs": 12060
			},
			"gates": []
		}`,
		"reports/route-disposition.json": `{
			"generated_at": "2026-06-24T00:00:00Z",
			"provider": "data.go.kr",
			"limit": 0,
			"truncated": false,
			"summary": {
				"routes_total": 28,
				"operations": 28,
				"hosts": 10,
				"with_probe_evidence": 28,
				"without_probe_evidence": 0,
				"dead_route_candidates": 14,
				"transient_failures": 14,
				"parameter_blocked_routes": 0,
				"adapter_candidates": 0,
				"by_disposition": []
			},
			"routes": []
		}`,
	})
	var urls []string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		switch req.URL.String() {
		case defaultDatapanRegistryReleaseAPI:
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name": "vtest",
					"assets": [
						{"name": "datapan-registry-vtest.zip", "browser_download_url": "https://example.test/registry.zip"}
					]
				}`)),
				Header: make(http.Header),
			}, nil
		case "https://example.test/registry.zip":
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(zipData)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL.String())
			return nil, nil
		}
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", output, "--json"}, nil, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %s", stderr)
	}
	if len(urls) != 2 || urls[0] != defaultDatapanRegistryReleaseAPI || urls[1] != "https://example.test/registry.zip" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout)
	}
	if payload["ok"] != true || int(payload["specs"].(float64)) != 1 || payload["registry"] != output {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload["release_dir"] != ".datapan/release" {
		t.Fatalf("unexpected release dir: %#v", payload)
	}
	release := payload["release"].(map[string]any)
	for _, want := range []string{
		`"manifest_present": true`,
		`"release_notes_present": true`,
		`"verification_ok": true`,
		`"readiness_ready": true`,
		`"manifest_artifacts": 29`,
		`"readiness_registry_specs": 12060`,
		`"route_disposition_present": true`,
		`"route_disposition_routes": 28`,
		`"route_disposition_dead_route_candidates": 14`,
		`"route_disposition_transient_failures": 14`,
		`"route_disposition_adapter_candidates": 0`,
		`"release_files":`,
		`.datapan/release/reports/latest-verification.json`,
		`.datapan/release/reports/route-disposition.json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in install release evidence: %s", want, stdout)
		}
	}
	if release["manifest_generated_at"] != "2026-06-24T00:00:00Z" {
		t.Fatalf("unexpected release evidence: %#v", release)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id":"100"`) {
		t.Fatalf("expected installed registry data: %s", string(data))
	}
	for _, path := range []string{
		filepath.Join(dir, ".datapan", "release", "manifest.json"),
		filepath.Join(dir, ".datapan", "release", "reports", "latest-verification.json"),
		filepath.Join(dir, ".datapan", "release", "reports", "route-disposition.json"),
	} {
		if _, err := os.ReadFile(path); err != nil {
			t.Fatalf("expected release evidence file %s: %v", path, err)
		}
	}
}

func TestInitInstallsRegistryAndReportsNextSteps(t *testing.T) {
	t.Chdir(t.TempDir())
	registry := `[{"id":"100","title":"테스트 API","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/test/list"}]}]`
	zipData := zipBytesForTest(t, datapanRegistryZipRegistryPath, registry)
	var urls []string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		switch req.URL.String() {
		case defaultDatapanRegistryReleaseAPI:
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name": "vtest",
					"assets": [
						{"name": "datapan-registry-vtest.zip", "browser_download_url": "https://example.test/init-registry.zip"}
					]
				}`)),
				Header: make(http.Header),
			}, nil
		case "https://example.test/init-registry.zip":
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(zipData)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL.String())
			return nil, nil
		}
	})
	code, stdout, stderr := runTest([]string{"init", "--json"}, fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"}, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(urls) != 2 || urls[0] != defaultDatapanRegistryReleaseAPI || urls[1] != "https://example.test/init-registry.zip" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
	data, err := os.ReadFile(defaultRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id":"100"`) {
		t.Fatalf("expected initialized registry data: %s", string(data))
	}
	for _, want := range []string{
		`"ok": true`,
		`"ready_for_search": true`,
		`"ready_for_calls": true`,
		`"registry": ".datapan/data-go-kr.registry.json"`,
		`"specs": 1`,
		`"operations": 1`,
		`"credential_present": true`,
		`"adapter_count": 51`,
		`datapan ready --limit 10 --json`,
		`datapan search \"실거래\" --org 국토교통부 --json`,
		`datapan use 15084084 base_date=20260622 base_time=0500 nx=60 ny=127 --json`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in init output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("init should not leak key material: %s", stdout)
	}
}

func TestCatalogInstallDatapanRegistryUsesGitHubTokenForAPIOnly(t *testing.T) {
	output := filepath.Join(t.TempDir(), "registry.json")
	registry := `[{"id":"100","title":"테스트 API","provider":"data.go.kr","priority":"P2","operations":[]}]`
	zipData := zipBytesForTest(t, datapanRegistryZipRegistryPath, registry)
	var apiAuth string
	var assetAuth string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case defaultDatapanRegistryReleaseAPI:
			apiAuth = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name": "vtest",
					"assets": [
						{"name": "datapan-registry-vtest.zip", "browser_download_url": "https://example.test/registry.zip"}
					]
				}`)),
				Header: make(http.Header),
			}, nil
		case "https://example.test/registry.zip":
			assetAuth = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(zipData)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL.String())
			return nil, nil
		}
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", output, "--json"}, fakeEnv{"GH_TOKEN": "secret-token"}, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if apiAuth != "Bearer secret-token" {
		t.Fatalf("expected GitHub API auth header, got %q", apiAuth)
	}
	if assetAuth != "" {
		t.Fatalf("did not expect auth header on non-GitHub API asset request, got %q", assetAuth)
	}
	if strings.Contains(stdout, "secret-token") {
		t.Fatalf("install should not leak GitHub token: %s", stdout)
	}
}

func TestCatalogInstallDatapanRegistryAcceptsGitHubReleasePageURL(t *testing.T) {
	output := filepath.Join(t.TempDir(), "registry.json")
	registry := `[{"id":"101","title":"릴리스 URL API","provider":"data.go.kr","priority":"P2","operations":[]}]`
	zipData := zipBytesForTest(t, datapanRegistryZipRegistryPath, registry)
	var urls []string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		switch req.URL.String() {
		case "https://api.github.com/repos/StatPan/datapan-registry/releases/tags/vtest":
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name": "vtest",
					"assets": [
						{"name": "datapan-registry-vtest.zip", "browser_download_url": "https://example.test/release-page.zip"}
					]
				}`)),
				Header: make(http.Header),
			}, nil
		case "https://example.test/release-page.zip":
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(zipData)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL.String())
			return nil, nil
		}
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", output, "--release-url", "https://github.com/StatPan/datapan-registry/releases/tag/vtest", "--json"}, nil, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(urls) != 2 || urls[0] != "https://api.github.com/repos/StatPan/datapan-registry/releases/tags/vtest" || urls[1] != "https://example.test/release-page.zip" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
	if !strings.Contains(stdout, `"specs": 1`) {
		t.Fatalf("expected install success output: %s", stdout)
	}
}

func TestCatalogInstallDatapanRegistryUsesDirectURL(t *testing.T) {
	output := filepath.Join(t.TempDir(), "registry.json")
	registry := `[{"id":"200","title":"직접 URL API","provider":"data.go.kr","priority":"P2","operations":[]}]`
	zipData := zipBytesForTest(t, datapanRegistryZipRegistryPath, registry)
	var urls []string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		if req.URL.String() != "https://example.test/direct.zip" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(zipData)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", output, "--url", "https://example.test/direct.zip", "--json"}, nil, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(urls) != 1 || urls[0] != "https://example.test/direct.zip" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
	if !strings.Contains(stdout, `"url": "https://example.test/direct.zip"`) {
		t.Fatalf("expected direct URL in output: %s", stdout)
	}
}

func TestCatalogInstallDatapanRegistryMissingRegistryInZipJSON(t *testing.T) {
	zipData := zipBytesForTest(t, "data/other.json", `[]`)
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(zipData)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", filepath.Join(t.TempDir(), "registry.json"), "--url", "https://example.test/direct.zip", "--json"}, nil, client)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr for JSON failure, got %s", stderr)
	}
	if !strings.Contains(stdout, `"error": "request_failed"`) || !strings.Contains(stdout, `zip does not contain data/data-go-kr.registry.json`) {
		t.Fatalf("expected missing registry JSON failure: %s", stdout)
	}
}

func TestCatalogInstallDatapanRegistryRejectsJSONStdoutConflict(t *testing.T) {
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--registry", "-", "--url", "https://example.test/direct.zip", "--json"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout != "" || !strings.Contains(stderr, "use --registry PATH with --json") {
		t.Fatalf("unexpected output stdout=%s stderr=%s", stdout, stderr)
	}
}

func TestCatalogInstallDatapanRegistryCanRepairInvalidDefaultRegistry(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(defaultRegistryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(defaultRegistryPath, []byte(`not-json`)); err != nil {
		t.Fatal(err)
	}
	registry := `[{"id":"300","title":"복구 API","provider":"data.go.kr","priority":"P2","operations":[]}]`
	zipData := zipBytesForTest(t, datapanRegistryZipRegistryPath, registry)
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(zipData)),
			Header:     make(http.Header),
		}, nil
	})
	code, stdout, stderr := runTest([]string{"catalog", "install", "datapan-registry", "--url", "https://example.test/direct.zip", "--json"}, nil, client)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	data, err := os.ReadFile(defaultRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id":"300"`) {
		t.Fatalf("expected repaired default registry: %s", string(data))
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
		`"registered_adapter_hosts": 2`,
		`"missing_adapter_hosts": 0`,
		`"needs_adapter_operations": 0`,
		`"unsupported_protocol_operations": 1`,
		`"host": "openapi.q-net.or.kr"`,
		`"provider": "q-net"`,
		`"adapter_status": "adapter"`,
		`"next_commands":`,
		`"dependencies": "datapan ops --registry`,
		`--host openapi.q-net.or.kr --limit 20 --json"`,
		`"verify": "datapan verify --registry`,
		`--host openapi.q-net.or.kr --limit 3 --json"`,
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
		`"registered_adapter_operations": 2`,
		`"missing_adapter_operations": 0`,
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

func TestCatalogRouteDispositionCombinesBacklogAndProbeEvidence(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	probePath := dir + "/probe.json"
	outputPath := dir + "/route-disposition.json"
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://dead.example.test/api/list"}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"상세","endpoint":"https://slow.example.test/api/detail"}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"검색","endpoint":"https://candidate.example.test/api/search"}]},
		{"id":"400","title":"기관_D","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://openapi.q-net.or.kr/api/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(probePath, []byte(`{
		"generated_at": "2026-06-25T00:00:00Z",
		"provider": "data.go.kr",
		"limit": 0,
		"truncated": false,
		"filtered_count": 2,
		"summary": {"total": 2, "verified": 0, "failed": 2, "skipped": 0, "unknown": 0},
		"results": [
			{"dataset_id": "100", "title": "기관_A", "operation": "목록", "provider": "data.go.kr", "endpoint_host": "dead.example.test", "dependency_class": "external_endpoint", "status": "failed", "reason": "unadapted_probe_http_404", "http_status": 404},
			{"dataset_id": "200", "title": "기관_B", "operation": "상세", "provider": "data.go.kr", "endpoint_host": "slow.example.test", "dependency_class": "external_endpoint", "status": "failed", "reason": "unadapted_probe_timeout"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "route-disposition", "--registry", registryPath, "--probe", probePath, "--output", outputPath, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"routes_total": 3`,
		`"with_probe_evidence": 2`,
		`"without_probe_evidence": 1`,
		`"dead_route_candidates": 1`,
		`"transient_failures": 1`,
		`"adapter_candidates": 1`,
		`"disposition": "dead_route_candidate"`,
		`"disposition": "transient_failure"`,
		`"disposition": "adapter_candidate"`,
		`"recommended_action":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"openapi.q-net.or.kr"`) {
		t.Fatalf("registered adapter route leaked into disposition report: %s", stdout)
	}
	data, err := osReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	validator, available, err := loadReleaseSchemaValidator(filepath.Clean(filepath.Join("..", "..")))
	if err != nil || !available {
		t.Fatalf("expected schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.route-disposition.v1.schema.json", data); err != nil {
		t.Fatalf("expected route disposition report to match schema: %v\n%s", err, data)
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

func TestCatalogVerifyProbeUnadaptedExternalEndpoint(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"목록","endpoint":"https://external.example.test/api/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"},{"name":"numOfRows"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("pageNo"); got != "1" {
			t.Fatalf("pageNo=%q", got)
		}
		if got := req.URL.Query().Get("serviceKey"); got != "" {
			t.Fatalf("probe leaked serviceKey=%q", got)
		}
		header := make(http.Header)
		header.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`<html>not found</html>`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest([]string{"catalog", "verify", "--registry", tmp, "--probe-unadapted", "--json"}, nil, client)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"failed": 1`,
		`"status": "failed"`,
		`"reason": "unadapted_probe_http_404"`,
		`"http_status": 404`,
		`"body_shape": "html"`,
		`"url": "https://external.example.test/api/list?numOfRows=1&pageNo=1"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "serviceKey") {
		t.Fatalf("probe output leaked auth param: %s", stdout)
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

func TestCatalogVerifyAdapterUsesTimeout(t *testing.T) {
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
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Second):
			t.Fatal("request was not cancelled by catalog verify timeout")
			return nil, nil
		}
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", tmp, "--ref", "15025329", "--timeout", "1ms", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"timeout": "1ms"`,
		`"failed": 1`,
		`"status": "failed"`,
		`context deadline exceeded`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
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

func TestCatalogVerifyFiltersOrganizationBeforeLimit(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"100",
			"title":"행정안전부 API",
			"provider":"data.go.kr",
			"priority":"P2",
			"organization":"행정안전부",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]
		},
		{
			"id":"200",
			"title":"다른 기관 API",
			"provider":"data.go.kr",
			"priority":"P2",
			"organization":"다른기관",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/200/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/100/list") {
			t.Fatalf("expected 행정안전부 operation request, got %s", req.URL.String())
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
		[]string{"catalog", "verify", "--registry", tmp, "--org", "행정안전부", "--limit", "1", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"organization": "행정안전부"`,
		`"filtered_count": 1`,
		`"verified": 1`,
		`"dataset_id": "100"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "200"`) {
		t.Fatalf("organization filter included other institution candidate: %s", stdout)
	}
}

func TestCatalogVerifyExcludeInputSkipsSeenOperations(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	seenPath := filepath.Join(dir, "seen.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"첫번째","endpoint":"https://apis.data.go.kr/100/first","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"두번째","endpoint":"https://apis.data.go.kr/200/second","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(seenPath, []byte(`{
		"generated_at":"2026-06-24T00:00:00Z",
		"provider":"data.go.kr",
		"limit":1,
		"truncated":false,
		"filtered_count":1,
		"summary":{"total":1,"verified":1,"failed":0,"skipped":0,"unknown":0},
		"results":[{"dataset_id":"100","title":"기관_A","operation":"첫번째","provider":"data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"}]
	}`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/200/second") {
			t.Fatalf("expected unseen operation request, got %s", req.URL.String())
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00","resultMsg":"OK"},"body":{"items":{"item":[{"name":"beta"}]}}}}`)),
			Header:     header,
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", registryPath, "--kind", "data_go_kr_gateway", "--exclude-input", seenPath, "--limit", "1", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"exclude_input": "` + jsonEscaped(seenPath) + `"`,
		`"dataset_id": "200"`,
		`"verified": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"dataset_id": "100"`) {
		t.Fatalf("seen operation was not excluded: %s", stdout)
	}
}

func TestCatalogVerifyPlanBuildsNextBatches(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	verificationPath := filepath.Join(dir, "verification.json")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","operations":[{"name":"관문","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]},
		{"id":"200","title":"기관_B","provider":"data.go.kr","priority":"P2","operations":[{"name":"QNet","endpoint":"http://openapi.q-net.or.kr/api/service/rest/InquiryStatSVC/getGradSiPassList","request_params":[{"name":"serviceKey"},{"name":"baseYY"}]}]},
		{"id":"300","title":"기관_C","provider":"data.go.kr","priority":"P2","operations":[{"name":"Missing","endpoint":"https://external.example.test/api"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(verificationPath, []byte(`{
		"generated_at":"2026-06-24T00:00:00Z",
		"provider":"data.go.kr",
		"limit":1,
		"truncated":false,
		"filtered_count":1,
		"summary":{"total":1,"verified":1,"failed":0,"skipped":0,"unknown":0},
		"results":[{"dataset_id":"100","title":"기관_A","operation":"관문","provider":"data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"}]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "plan", "--registry", registryPath, "--verification", verificationPath, "--batch-size", "2", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"evidence_total": 1`,
		`"uncovered_gateway_candidates": 0`,
		`"uncovered_adapter_candidates": 1`,
		`"missing_adapter_hosts": 1`,
		`"label": "q-net"`,
		`--exclude-input`,
		`datapan catalog coverage --registry`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in plan output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"label": "gateway"`) {
		t.Fatalf("gateway batch should be covered by existing evidence: %s", stdout)
	}
}

func TestCatalogVerifyPlanFiltersOrganization(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	if err := osWriteFile(registryPath, []byte(`[
		{
			"id":"100",
			"title":"행정안전부 관문",
			"provider":"data.go.kr",
			"priority":"P2",
			"organization":"행정안전부",
			"operations":[{"name":"관문","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]
		},
		{
			"id":"200",
			"title":"다른 기관 관문",
			"provider":"data.go.kr",
			"priority":"P2",
			"organization":"다른기관",
			"operations":[{"name":"관문","endpoint":"https://apis.data.go.kr/200/list","request_params":[{"name":"serviceKey"},{"name":"pageNo"}]}]
		},
		{
			"id":"300",
			"title":"다른 기관 QNet",
			"provider":"data.go.kr",
			"priority":"P2",
			"organization":"다른기관",
			"operations":[{"name":"QNet","endpoint":"http://openapi.q-net.or.kr/api/service/rest/InquiryStatSVC/getGradSiPassList","request_params":[{"name":"serviceKey"},{"name":"baseYY"}]}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "plan", "--registry", registryPath, "--org", "행정안전부", "--batch-size", "5", "--json"},
		nil,
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"organization": "행정안전부"`,
		`"uncovered_gateway_candidates": 1`,
		`"uncovered_adapter_candidates": 0`,
		`"planned_operations": 1`,
		`--org 행정안전부`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in plan output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"label": "q-net"`) {
		t.Fatalf("organization filter included other institution adapter batch: %s", stdout)
	}
}

func TestCatalogVerifyGatewayUsesTimeout(t *testing.T) {
	tmp := t.TempDir() + "/registry.json"
	if err := osWriteFile(tmp, []byte(`[
		{
			"id":"100",
			"title":"기관_A",
			"provider":"data.go.kr",
			"priority":"P2",
			"operations":[{"name":"목록","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"base_date"}],"default_params":{"base_date":"20260624"}}]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Second):
			t.Fatal("request was not cancelled by catalog verify timeout")
			return nil, nil
		}
	})
	code, stdout, stderr := runTest(
		[]string{"catalog", "verify", "--registry", tmp, "--ref", "100", "--timeout", "1ms", "--json"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"},
		client,
	)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"timeout": "1ms"`,
		`"failed": 1`,
		`"status": "failed"`,
		`context deadline exceeded`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
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

func TestCatalogVerifyMergeReports(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	output := filepath.Join(dir, "merged.json")
	if err := osWriteFile(first, []byte(`{
		"generated_at":"2026-06-24T00:00:00Z",
		"provider":"data.go.kr",
		"registry":"registry.json",
		"limit":1,
		"filtered_count":1,
		"summary":{"total":1,"verified":1,"failed":0,"skipped":0,"unknown":0},
		"results":[{"dataset_id":"100","title":"A","operation":"목록","provider":"data.go.kr","dependency_class":"data_go_kr_gateway","status":"verified"}]
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(second, []byte(`{
		"generated_at":"2026-06-24T00:01:00Z",
		"provider":"data.go.kr",
		"registry":"registry.json",
		"limit":1,
		"filtered_count":1,
		"summary":{"total":1,"verified":0,"failed":0,"skipped":1,"unknown":0},
		"results":[{"dataset_id":"200","title":"B","operation":"상세","provider":"epost","endpoint_host":"openapi.epost.go.kr","dependency_class":"external_endpoint","status":"skipped","reason":"epost_missing_required_params"}]
	}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "verify", "merge", "--input", first, "--input", second, "--output", output, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"results": 2`, `"verified": 1`, `"skipped": 1`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"filtered_count": 2`, `"dataset_id": "100"`, `"dataset_id": "200"`, `"reason": "epost_missing_required_params"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %q in merged report: %s", want, string(data))
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
	code, _, stderr = runTest([]string{"catalog", "verify", "--input", "report.json", "--timeout", "1s"}, nil, nil)
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--timeout") {
		t.Fatalf("expected input timeout conflict message: %s", stderr)
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
	if err := os.MkdirAll(paths.ReportsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(paths.UnadaptedProbePath, []byte(`{
		"generated_at": "2026-06-24T00:01:00Z",
		"provider": "data.go.kr",
		"registry": "registry.json",
		"limit": 1,
		"timeout": "1s",
		"truncated": false,
		"filtered_count": 1,
		"summary": {"total": 1, "verified": 0, "failed": 1, "skipped": 0, "unknown": 0},
		"results": [
			{"dataset_id": "999", "title": "미적응 외부", "operation": "목록", "provider": "data.go.kr", "endpoint_host": "external.example", "dependency_class": "external_endpoint", "status": "failed", "reason": "unadapted_probe_http_404", "http_status": 404}
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
		`"coverage":`,
		`"verification_plan":`,
		`"verification_summary":`,
		`"unadapted_external_probe":`,
		`"unadapted_external_probe_summary":`,
		`"manifest":`,
		`"artifacts": 39`,
		`"runtime_evidence_growth":`,
		`"provenance":`,
		`"release_notes":`,
		`"unadapted_probe_included": true`,
		`"unadapted_probe_summary_written": true`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, path := range []string{
		outputDir + "/schemas/datapan.specs.v1.schema.json",
		outputDir + "/schemas/datapan.dependencies.v1.schema.json",
		outputDir + "/schemas/datapan.adapter-targets.v1.schema.json",
		outputDir + "/schemas/datapan.route-disposition.v1.schema.json",
		outputDir + "/schemas/datapan.providers.v1.schema.json",
		outputDir + "/schemas/datapan.coverage.v1.schema.json",
		outputDir + "/schemas/datapan.verification.v1.schema.json",
		outputDir + "/schemas/datapan.verification-plan.v1.schema.json",
		outputDir + "/schemas/datapan.verification-summary.v1.schema.json",
		outputDir + "/schemas/datapan.runtime-evidence-growth.v1.schema.json",
		outputDir + "/schemas/datapan.release-manifest.v1.schema.json",
		outputDir + "/schemas/datapan.release-verification.v1.schema.json",
		outputDir + "/schemas/datapan.release-readiness.v1.schema.json",
		outputDir + "/schemas/datapan.schema-index.v1.schema.json",
		outputDir + "/schemas/datapan.catalog-diff.v1.schema.json",
		outputDir + "/schemas/datapan.error-catalog.v1.schema.json",
		outputDir + "/schemas/datapan.catalog-audit.v1.schema.json",
		outputDir + "/schemas/datapan.provider-index.v1.schema.json",
		outputDir + "/schemas/datapan.studio-datasets.v1.schema.json",
		outputDir + "/schemas/datapan.studio-bundle.v1.schema.json",
		outputDir + "/schemas/index.json",
		outputDir + "/data/data-go-kr.registry.json",
		outputDir + "/data/provider-index.json",
		outputDir + "/reports/catalog-diff.json",
		outputDir + "/reports/catalog-audit.json",
		outputDir + "/reports/error-catalog.json",
		outputDir + "/reports/dependencies.json",
		outputDir + "/reports/adapter-targets.json",
		outputDir + "/reports/route-disposition.json",
		outputDir + "/reports/provider-backlog.json",
		outputDir + "/reports/coverage.json",
		outputDir + "/reports/verification-plan.json",
		outputDir + "/reports/data-go-kr/runtime-evidence-growth.json",
		outputDir + "/reports/latest-verification.json",
		outputDir + "/reports/latest-verification-summary.json",
		outputDir + "/reports/unadapted-external-probe.json",
		outputDir + "/reports/unadapted-external-probe-summary.json",
		outputDir + "/provenance/data-go-kr.md",
		outputDir + "/RELEASE_NOTES.md",
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
	if !strings.Contains(string(provenance), "datapan catalog release draft") || !strings.Contains(string(provenance), "--previous-registry") || !strings.Contains(string(provenance), "datapan catalog diff") || !strings.Contains(string(provenance), "datapan catalog audit") || !strings.Contains(string(provenance), "datapan catalog dependencies") || !strings.Contains(string(provenance), "datapan catalog adapter-targets") || !strings.Contains(string(provenance), "datapan catalog route-disposition") || !strings.Contains(string(provenance), "datapan catalog coverage") || !strings.Contains(string(provenance), "datapan catalog verify plan") {
		t.Fatalf("unexpected provenance: %s", provenance)
	}
	releaseNotes, err := osReadFile(outputDir + "/RELEASE_NOTES.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Datapan Registry Release",
		"- specs: `1`",
		"- catalog_diff: `1` added, `1` removed, `0` changed, `0` stable",
		"- provider_adapters:",
		"- coverage:",
		"- route_disposition:",
		"- route_disposition_artifact:",
		"- coverage_artifact:",
		"- coverage_goals:",
		"- verification_plan:",
		"- verification_plan_artifact:",
		"- runtime_evidence_growth: `100.0%` coverage, target `10.0%`, remaining `0`, status `at_target`",
		"- runtime_evidence_growth_artifact:",
		"- split_readiness:",
		"- verification: `1` total, `0` verified, `0` failed, `1` skipped, `0` unknown",
		"- unadapted_external_probe: `1` total, `0` verified, `1` failed, `0` skipped, `0` unknown",
		"- unadapted_external_probe_artifact:",
		"- unadapted_external_probe_summary_artifact:",
		"datapan catalog release verify --manifest manifest.json",
		"datapan catalog release readiness --manifest manifest.json",
	} {
		if !strings.Contains(string(releaseNotes), want) {
			t.Fatalf("expected %q in release notes: %s", want, releaseNotes)
		}
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
		`"count": 20`,
		`"path": "schemas/datapan.dependencies.v1.schema.json"`,
		`"path": "schemas/datapan.adapter-targets.v1.schema.json"`,
		`"path": "schemas/datapan.route-disposition.v1.schema.json"`,
		`"path": "schemas/datapan.coverage.v1.schema.json"`,
		`"path": "schemas/datapan.verification-plan.v1.schema.json"`,
		`"path": "schemas/datapan.runtime-evidence-growth.v1.schema.json"`,
		`"path": "schemas/datapan.release-readiness.v1.schema.json"`,
		`"path": "schemas/datapan.schema-index.v1.schema.json"`,
		`"path": "schemas/datapan.catalog-diff.v1.schema.json"`,
		`"path": "schemas/datapan.error-catalog.v1.schema.json"`,
		`"path": "schemas/datapan.catalog-audit.v1.schema.json"`,
		`"path": "schemas/datapan.provider-index.v1.schema.json"`,
		`"path": "schemas/datapan.studio-datasets.v1.schema.json"`,
		`"path": "schemas/datapan.studio-bundle.v1.schema.json"`,
		`"contract": "dependencies"`,
		`"contract": "adapter-targets"`,
		`"contract": "route-disposition"`,
		`"contract": "coverage"`,
		`"contract": "verification-plan"`,
		`"contract": "runtime-evidence-growth"`,
		`"contract": "release-readiness"`,
		`"contract": "schema-index"`,
		`"contract": "catalog-diff"`,
		`"contract": "error-catalog"`,
		`"contract": "catalog-audit"`,
		`"contract": "provider-index"`,
		`"contract": "studio-datasets"`,
		`"contract": "studio-bundle"`,
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
		`"artifact_count": 39`,
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
		`"path": "reports/route-disposition.json"`,
		`"kind": "route_disposition"`,
		`"path": "reports/coverage.json"`,
		`"kind": "coverage"`,
		`"path": "reports/verification-plan.json"`,
		`"kind": "verification_plan"`,
		`"path": "reports/data-go-kr/runtime-evidence-growth.json"`,
		`"kind": "runtime_evidence_growth"`,
		`"path": "reports/latest-verification-summary.json"`,
		`"kind": "verification_summary"`,
		`"path": "reports/unadapted-external-probe.json"`,
		`"kind": "unadapted_external_probe"`,
		`"path": "reports/unadapted-external-probe-summary.json"`,
		`"kind": "unadapted_external_probe_summary"`,
		`"path": "RELEASE_NOTES.md"`,
		`"kind": "release_notes"`,
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
		`"checked": 39`,
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
	readinessOutput := outputDir + "/reports/latest-release-readiness.json"
	code, stdout, stderr = runTest([]string{"catalog", "release", "readiness", "--manifest", outputDir + "/manifest.json", "--output", readinessOutput, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"schema_version": "datapan.release-readiness.v1"`,
		`"ready": true`,
		`"id": "manifest_verified"`,
		`"id": "required_artifact_dependencies"`,
		`"id": "required_artifact_adapter_targets"`,
		`"id": "required_artifact_release_notes"`,
		`"id": "required_artifact_runtime_evidence_growth"`,
		`"id": "runtime_evidence_growth_target"`,
		`"id": "recommended_artifact_catalog_diff"`,
		`"id": "recommended_artifact_unadapted_external_probe"`,
		`"id": "recommended_artifact_unadapted_external_probe_summary"`,
		`"registry_specs": 1`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in readiness output: %s", want, stdout)
		}
	}
	readinessReport, err := osReadFile(readinessOutput)
	if err != nil {
		t.Fatal(err)
	}
	validator, available, err := loadReleaseSchemaValidator(outputDir)
	if err != nil || !available {
		t.Fatalf("expected release schema validator: available=%v err=%v", available, err)
	}
	if err := validator.validate("https://schemas.datapan.dev/datapan.release-readiness.v1.schema.json", readinessReport); err != nil {
		t.Fatalf("expected readiness report to match schema: %v\n%s", err, readinessReport)
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

func TestCatalogReleaseDraftWarnsWhenRuntimeEvidenceBelowTarget(t *testing.T) {
	dir := t.TempDir()
	registryPath := dir + "/registry.json"
	outputDir := dir + "/release"
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[
			{"name":"목록1","endpoint":"https://api.data.go.kr/openapi/list1"},
			{"name":"목록2","endpoint":"https://api.data.go.kr/openapi/list2"},
			{"name":"목록3","endpoint":"https://api.data.go.kr/openapi/list3"},
			{"name":"목록4","endpoint":"https://api.data.go.kr/openapi/list4"},
			{"name":"목록5","endpoint":"https://api.data.go.kr/openapi/list5"},
			{"name":"목록6","endpoint":"https://api.data.go.kr/openapi/list6"},
			{"name":"목록7","endpoint":"https://api.data.go.kr/openapi/list7"},
			{"name":"목록8","endpoint":"https://api.data.go.kr/openapi/list8"},
			{"name":"목록9","endpoint":"https://api.data.go.kr/openapi/list9"},
			{"name":"목록10","endpoint":"https://api.data.go.kr/openapi/list10"},
			{"name":"목록11","endpoint":"https://api.data.go.kr/openapi/list11"},
			{"name":"목록12","endpoint":"https://api.data.go.kr/openapi/list12"}
		]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "release", "draft", "--registry", registryPath, "--output-dir", outputDir, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"runtime_evidence_growth":`,
		`"artifacts": 34`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	runtimeEvidence, err := osReadFile(outputDir + "/reports/data-go-kr/runtime-evidence-growth.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"schema_version": "datapan.runtime-evidence-growth.v1"`,
		`"operations": 12`,
		`"total": 0`,
		`"target_evidence_total": 2`,
		`"remaining_to_target": 2`,
		`"status": "below_target"`,
		`"kind": "runtime_evidence_below_target"`,
	} {
		if !strings.Contains(string(runtimeEvidence), want) {
			t.Fatalf("expected %q in runtime evidence growth report: %s", want, runtimeEvidence)
		}
	}
	verifyOutput := outputDir + "/reports/latest-release-verification.json"
	code, stdout, stderr = runTest([]string{"catalog", "release", "verify", "--manifest", outputDir + "/manifest.json", "--output", verifyOutput, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"kind": "runtime_evidence_growth"`) || !strings.Contains(stdout, `"status": "verified"`) {
		t.Fatalf("expected verified runtime evidence growth artifact: %s", stdout)
	}
	readinessOutput := outputDir + "/reports/latest-release-readiness.json"
	code, stdout, stderr = runTest([]string{"catalog", "release", "readiness", "--manifest", outputDir + "/manifest.json", "--output", readinessOutput, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"id": "runtime_evidence_growth_target"`,
		`"status": "warn"`,
		`"artifact_kind": "runtime_evidence_growth"`,
		`"expected": 2`,
		`"actual": 0`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in readiness output: %s", want, stdout)
		}
	}
}

func TestCatalogReleaseDraftRunsFromSchemaOnlyRoot(t *testing.T) {
	schemaSources, err := datapanSchemaSources()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	releaseRoot := filepath.Join(dir, "release-root")
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(releaseRoot, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, source := range schemaSources {
		data, err := osReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := osWriteFile(filepath.Join(releaseRoot, "schemas", schemaFileName(source)), data); err != nil {
			t.Fatal(err)
		}
	}
	previousRegistryPath := filepath.Join(sourceDir, "previous-registry.json")
	registryPath := filepath.Join(sourceDir, "registry.json")
	verificationPath := filepath.Join(sourceDir, "verification.json")
	if err := osWriteFile(previousRegistryPath, []byte(`[{"id":"090","title":"이전기관","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]}]`)); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(registryPath, []byte(`[{"id":"100","title":"기관_A","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[]}]`)); err != nil {
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
			{"dataset_id": "100", "title": "기관_A", "provider": "data.go.kr", "status": "skipped", "reason": "metadata_only"}
		]
	}`)); err != nil {
		t.Fatal(err)
	}
	t.Chdir(releaseRoot)
	code, stdout, stderr := runTest([]string{"catalog", "release", "draft", "--registry", registryPath, "--previous-registry", previousRegistryPath, "--verification", verificationPath, "--output-dir", ".", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"artifacts": 37`,
		`"runtime_evidence_growth":`,
		`"catalog_diff":`,
		`"verification_summary_written": true`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if _, err := osReadFile(filepath.Join(releaseRoot, "manifest.json")); err != nil {
		t.Fatalf("expected manifest from schema-only release root: %v", err)
	}
	if _, err := osReadFile(filepath.Join(releaseRoot, "reports", "catalog-diff.json")); err != nil {
		t.Fatalf("expected catalog diff from schema-only release root: %v", err)
	}
}

func TestCatalogReleaseReadinessRequiresUnadaptedProbeForMissingAdapters(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	outputDir := filepath.Join(dir, "release")
	if err := osWriteFile(registryPath, []byte(`[
		{"id":"100","title":"미적응 외부기관","provider":"data.go.kr","priority":"P2","organization":"기관","operations":[{"name":"목록","endpoint":"https://unadapted.example/openapi/list"}]}
	]`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"catalog", "release", "draft", "--registry", registryPath, "--output-dir", outputDir, "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	readinessOutput := filepath.Join(outputDir, "reports", "latest-release-readiness.json")
	code, stdout, stderr = runTest([]string{"catalog", "release", "readiness", "--manifest", filepath.Join(outputDir, "manifest.json"), "--output", readinessOutput, "--json"}, nil, nil)
	if code != exitRequest {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": false`,
		`"ready": false`,
		`"id": "required_artifact_unadapted_external_probe"`,
		`"id": "required_artifact_unadapted_external_probe_summary"`,
		`"message": "unadapted external endpoint evidence is required while missing adapter operations remain"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in readiness output: %s", want, stdout)
		}
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

func TestParamsFileHasLowerPrecedenceThanCLIParams(t *testing.T) {
	paramsPath := t.TempDir() + "/params.json"
	if err := osWriteFile(paramsPath, []byte(`{"base_date":"20240101","base_time":"0100","nx":"55"}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--params-file", paramsPath, "base_date=20260622", "--param", "base_time=0500", "--param", "nx=60", "--dry-run", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"base_date": "20260622"`,
		`"base_time": "0500"`,
		`"nx": "60"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in get dry-run: %s", want, stdout)
		}
	}
	for _, bad := range []string{"20240101", "0100", `"nx": "55"`, "secret-value"} {
		if strings.Contains(stdout, bad) {
			t.Fatalf("unexpected %q in get dry-run: %s", bad, stdout)
		}
	}

	code, stdout, stderr = runTest(
		[]string{"curl", "15084084", "--params-file", paramsPath, "base_date=20260622", "--param", "base_time=0500", "--param", "nx=60", "--json"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`base_date=20260622`, `base_time=0500`, `nx=60`, `serviceKey=${DATA_PORTAL_API_KEY}`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in curl plan: %s", want, stdout)
		}
	}
	for _, bad := range []string{"20240101", "0100", "nx=55", "secret-value"} {
		if strings.Contains(stdout, bad) {
			t.Fatalf("unexpected %q in curl plan: %s", bad, stdout)
		}
	}
}

func TestCurlExportsCommandWithoutCredential(t *testing.T) {
	code, stdout, stderr := runTest(
		[]string{"curl", "기상청_단기예보 조회서비스", "--json", "base_date=20260622", "base_time=0500", "nx=60", "ny=127"},
		nil,
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"dataset": "15084084"`,
		`"env_var": "DATAPAN_DATA_GO_KR_KEY"`,
		`"command": "curl -fsS`,
		`base_date=20260622`,
		`serviceKey=${DATAPAN_DATA_GO_KR_KEY}`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in curl export: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "REDACTED") || strings.Contains(stdout, "secret-value") {
		t.Fatalf("curl export should use env placeholder, not key material: %s", stdout)
	}
}

func TestCurlExportUsesExistingEnvVarName(t *testing.T) {
	code, stdout, stderr := runTest(
		[]string{"export", "--format", "curl", "15084084", "--param", "base_date=20260622", "--param", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "serviceKey=${DATA_PORTAL_API_KEY}") {
		t.Fatalf("expected selected env var placeholder: %s", stdout)
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("secret leaked in curl export: %s", stdout)
	}
}

func TestPostmanExportWritesCollection(t *testing.T) {
	dir := t.TempDir()
	output := dir + "/collection.json"
	code, stdout, stderr := runTest(
		[]string{"export", "--format", "postman", "15084084", "--output", output, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"format": "postman"`,
		`"output": "` + jsonEscaped(output) + `"`,
		`"dataset": "15084084"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in postman summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"`,
		`"key": "DATA_PORTAL_API_KEY"`,
		`"value": "{{DATA_PORTAL_API_KEY}}"`,
		`"key": "serviceKey"`,
		`"key": "base_date"`,
		`"value": "20260622"`,
		`"method": "GET"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in Postman collection: %s", want, data)
		}
	}
	if strings.Contains(text, "secret-value") || strings.Contains(text, "${DATA_PORTAL_API_KEY}") {
		t.Fatalf("Postman collection should use Postman variable placeholder only: %s", data)
	}
}

func TestOpenAPIExportWritesDocument(t *testing.T) {
	dir := t.TempDir()
	output := dir + "/openapi.json"
	code, stdout, stderr := runTest(
		[]string{"export", "--format", "openapi", "15084084", "--output", output, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"format": "openapi"`,
		`"output": "` + jsonEscaped(output) + `"`,
		`"dataset": "15084084"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in openapi summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"openapi": "3.1.0"`,
		`"url": "https://apis.data.go.kr"`,
		`"/1360000/VilageFcstInfoService_2.0/getVilageFcst"`,
		`"name": "serviceKey"`,
		`"type": "apiKey"`,
		`"default": "${DATA_PORTAL_API_KEY}"`,
		`"name": "base_date"`,
		`"default": "20260622"`,
		`"operationId": "datapan_15084084_getVilageFcst"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in OpenAPI document: %s", want, data)
		}
	}
	if strings.Contains(text, "secret-value") || strings.Contains(text, "{{DATA_PORTAL_API_KEY}}") {
		t.Fatalf("OpenAPI document should use env placeholder only: %s", data)
	}
}

func TestCodegenGoWritesCompilableClient(t *testing.T) {
	dir := t.TempDir()
	output := dir + "/client.go"
	code, stdout, stderr := runTest(
		[]string{"codegen", "go", "15084084", "--package", "forecastclient", "--output", output, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"target": "go"`,
		`"package": "forecastclient"`,
		`"function": "GetVilageFcst"`,
		`"dataset": "15084084"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in codegen summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`package forecastclient`,
		`const defaultServiceKeyEnv = "DATA_PORTAL_API_KEY"`,
		`func NewFromEnv() (*Client, error)`,
		`func (c *Client) GetVilageFcst(ctx context.Context, params map[string]string) ([]byte, error)`,
		`"base_date": "20260622"`,
		`q.Set("serviceKey", c.ServiceKey)`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in generated Go client: %s", want, data)
		}
	}
	if strings.Contains(text, "secret-value") || strings.Contains(text, "${DATA_PORTAL_API_KEY}") {
		t.Fatalf("generated Go client should not embed key material or shell placeholders: %s", data)
	}
	goMod := dir + "/go.mod"
	if err := osWriteFile(goMod, []byte("module example.test/generated\n\ngo 1.26\n")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated client should compile: %v\n%s", err, out)
	}
}

func TestGoCodegenFallsBackToEndpointNameForKoreanOperation(t *testing.T) {
	plan := curlExportPlan{
		Spec: datago.Spec{ID: "15084084"},
		Operation: datago.Operation{
			Name:     "단기예보조회",
			Endpoint: "https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0/getVilageFcst",
		},
	}
	if got := goFunctionName(plan); got != "GetVilageFcst" {
		t.Fatalf("goFunctionName()=%q", got)
	}
}

func TestCodegenPythonWritesImportableClient(t *testing.T) {
	dir := t.TempDir()
	output := dir + "/client.py"
	code, stdout, stderr := runTest(
		[]string{"codegen", "python", "15084084", "--output", output, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"target": "python"`,
		`"function": "get_vilage_fcst"`,
		`"dataset": "15084084"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in codegen summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`DEFAULT_SERVICE_KEY_ENV = "DATA_PORTAL_API_KEY"`,
		`DEFAULT_PARAMS = {`,
		`"base_date": "20260622"`,
		`class DatapanClient:`,
		`def from_env(cls, env_var: str = DEFAULT_SERVICE_KEY_ENV)`,
		`def build_url(self, params: Optional[Mapping[str, str]] = None) -> str:`,
		`def get_vilage_fcst(self, params: Optional[Mapping[str, str]] = None, timeout: float = 30.0) -> bytes:`,
		`query["serviceKey"] = self.service_key`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in generated Python client: %s", want, data)
		}
	}
	if strings.Contains(text, "secret-value") || strings.Contains(text, "${DATA_PORTAL_API_KEY}") {
		t.Fatalf("generated Python client should not embed key material or shell placeholders: %s", data)
	}
	python, ok := findPythonForTest()
	if !ok {
		t.Skip("python executable not found")
	}
	cmd := exec.Command(python, "-m", "py_compile", output)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Python client should compile: %v\n%s", err, out)
	}
}

func TestCodegenNodeWritesSyntaxCheckedClient(t *testing.T) {
	dir := t.TempDir()
	output := dir + "/client.js"
	code, stdout, stderr := runTest(
		[]string{"codegen", "node", "15084084", "--output", output, "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"target": "node"`,
		`"function": "getVilageFcst"`,
		`"dataset": "15084084"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in codegen summary: %s", want, stdout)
		}
	}
	data, err := osReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`const DEFAULT_SERVICE_KEY_ENV = "DATA_PORTAL_API_KEY";`,
		`const DEFAULT_PARAMS = {`,
		`"base_date": "20260622"`,
		`class DatapanClient {`,
		`static fromEnv(env = process.env, envVar = DEFAULT_SERVICE_KEY_ENV, options = {})`,
		`buildURL(params = {})`,
		`async getVilageFcst(params = {}, options = {})`,
		`url.searchParams.set("serviceKey", this.serviceKey);`,
		`module.exports = {`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in generated Node client: %s", want, data)
		}
	}
	if strings.Contains(text, "secret-value") || strings.Contains(text, "${DATA_PORTAL_API_KEY}") {
		t.Fatalf("generated Node client should not embed key material or shell placeholders: %s", data)
	}
	node, ok := findNodeForTest()
	if !ok {
		t.Skip("node executable not found")
	}
	cmd := exec.Command(node, "--check", output)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Node client should parse: %v\n%s", err, out)
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

func TestGetDryRunUsesProviderCallPlan(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	registry := `[{
		"id":"3055528",
		"title":"농림축산식품부 농림축산검역본부_식물검역정보",
		"provider":"data.go.kr",
		"operations":[{
			"name":"국가코드",
			"endpoint":"http://openapi.pqis.go.kr/openapi/service/plntQrantStats?_wadl&type=xml",
			"request_params":[
				{"name":"nationName"},
				{"name":"pageNo"},
				{"name":"numOfRows"}
			],
			"source":{"raw":{
				"request_param_nm_en":"nationName,pageNo,numOfRows",
				"request_param_nm":"국가명,페이지번호,한페이지결과수",
				"is_confirmed_for_dev_nm":"자동승인",
				"is_confirmed_for_prod_nm":"자동승인"
			}}
		}]
	}]`
	if err := os.WriteFile(registryPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"get", "3055528", "--operation", "국가코드", "--dry-run", "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"dry_run": true`,
		`"url": "http://openapi.pqis.go.kr/openapi/service/plntQrantStats/nationCode?`,
		`"nationName": "한국"`,
		`"serviceKey": "REDACTED"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "_wadl") || strings.Contains(stdout, "secret-value") {
		t.Fatalf("dry-run should use provider-normalized redacted URL: %s", stdout)
	}
}

func TestGetDryRunUsesProviderCallPlanForSchemeLessEndpoint(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	registry := `[{
		"id":"15056640",
		"title":"한국전력거래소_현재전력수급현황조회",
		"provider":"data.go.kr",
		"operations":[{
			"name":"현재전력수급현황조회",
			"endpoint":"openapi.kpx.or.kr/openapi/sukub5mMaxDatetime/getSukub5mMaxDatetime",
			"source":{"raw":{
				"is_confirmed_for_dev_nm":"자동승인",
				"is_confirmed_for_prod_nm":"자동승인"
			}}
		}]
	}]`
	if err := os.WriteFile(registryPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(
		[]string{"get", "15056640", "--operation", "현재전력수급현황조회", "--dry-run", "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		nil,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"dry_run": true`,
		`"url": "https://openapi.kpx.or.kr/openapi/sukub5mMaxDatetime/getSukub5mMaxDatetime?`,
		`"numOfRows": "1"`,
		`"pageNo": "1"`,
		`"serviceKey": "REDACTED"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "secret-value") {
		t.Fatalf("dry-run should redact credential: %s", stdout)
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
	want := `"smoke_command": "datapan get 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --json"`
	if !strings.Contains(stdout, want) {
		t.Fatalf("expected synthesized smoke command %q in output: %s", want, stdout)
	}
	if !strings.Contains(stdout, `After approval, run: datapan get 999 --operation \"목록 조회\" PAGE=1 ROWS=10 --json`) {
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

func TestSyncCachesResponseRowsParamsAndManifest(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	cacheDir := filepath.Join(dir, "cache")
	if err := osWriteFile(registryPath, []byte(`[
		{
			"id": "999",
			"title": "테스트기관_테스트 API",
			"provider": "data.go.kr",
			"operations": [
				{
					"name": "목록 조회",
					"endpoint": "https://apis.data.go.kr/test/list",
					"request_params": [
						{"name": "serviceKey"},
						{"name": "keyword"},
						{"name": "pageNo"}
					]
				}
			]
		}
	]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("serviceKey"); got != "secret-value" {
			t.Fatalf("serviceKey=%q", got)
		}
		if req.URL.Query().Get("keyword") != "소나무" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected query: %s", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"currentCount":2,"data":[{"name":"alpha","count":1},{"name":"beta","count":2}],"page":1,"perPage":10,"totalCount":2}`)),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"sync", "999", "keyword=소나무", "--operation", "목록 조회", "--param", "pageNo=1", "--output-dir", cacheDir, "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"cache_dir": "` + jsonEscaped(cacheDir) + `"`,
		`"dataset": "999"`,
		`"operation": "목록 조회"`,
		`"semantic_status": "json_response"`,
		`"rows": 2`,
		`"integrity": {`,
		`"ok": true`,
		`"current_count": 2`,
		`"total_count": 2`,
		`"kind": "response"`,
		`"kind": "rows_json"`,
		`"kind": "rows_csv"`,
		`"kind": "manifest"`,
		`"preview_command": "datapan preview --input`,
		`"label": "preview"`,
		`"label": "export csv"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in sync output: %s", want, stdout)
		}
	}
	for _, file := range []string{"params.json", "response.json", "rows.json", "rows.csv", "manifest.json"} {
		if _, err := osReadFile(filepath.Join(cacheDir, file)); err != nil {
			t.Fatalf("expected %s: %v", file, err)
		}
	}
	for _, file := range []string{"params.json", "response.json", "manifest.json"} {
		data, err := osReadFile(filepath.Join(cacheDir, file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret-value") {
			t.Fatalf("%s should not contain credential material: %s", file, data)
		}
	}
	params, err := osReadFile(filepath.Join(cacheDir, "params.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(params), `"keyword": "소나무"`) || strings.Contains(string(params), "serviceKey") {
		t.Fatalf("unexpected params cache: %s", params)
	}
	csvData, err := osReadFile(filepath.Join(cacheDir, "rows.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csvData), "count,name") || !strings.Contains(string(csvData), "2,beta") {
		t.Fatalf("unexpected rows csv: %s", csvData)
	}
}

func TestSyncReportsIntegrityWarnings(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	cacheDir := filepath.Join(dir, "cache")
	if err := osWriteFile(registryPath, []byte(`[{
		"id": "999",
		"title": "테스트기관_정합성 API",
		"provider": "data.go.kr",
		"operations": [{
			"name": "목록 조회",
			"endpoint": "https://apis.data.go.kr/test/list",
			"request_params": [{"name": "serviceKey"}]
		}]
	}]`)); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"currentCount":2,"data":[{"id":1},{"id":2}],"page":1,"perPage":1,"totalCount":1}`)),
		}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"sync", "999", "--operation", "목록 조회", "--output-dir", cacheDir, "--json"},
		fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath, "DATA_PORTAL_API_KEY": "secret-value"},
		client,
	)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"integrity": {`,
		`"ok": false`,
		`"row_count": 2`,
		`"current_count": 2`,
		`"total_count": 1`,
		`"warnings": [`,
		`"row_count_exceeds_total_count"`,
		`"row_count_exceeds_per_page"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in sync output: %s", want, stdout)
		}
	}
	manifest, err := osReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"row_count_exceeds_total_count"`) {
		t.Fatalf("expected integrity warning in manifest: %s", manifest)
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

func TestPreviewInputJSON(t *testing.T) {
	tmp := t.TempDir() + "/response.json"
	if err := osWriteFile(tmp, []byte(`{"response":{"body":{"items":{"item":[{"name":"alpha","count":1},{"name":"beta","count":2}]}}}}`)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"preview", "--input", tmp, "--limit", "1", "--json"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"ok": true`,
		`"format": "json"`,
		`"count": 2`,
		`"limit": 1`,
		`"truncated": true`,
		`"columns":`,
		`"count"`,
		`"name"`,
		`"name": "alpha"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in preview JSON: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, `"name": "beta"`) {
		t.Fatalf("preview should limit returned rows: %s", stdout)
	}
}

func TestPreviewInputCSVHumanTable(t *testing.T) {
	tmp := t.TempDir() + "/rows.csv"
	if err := osWriteFile(tmp, []byte("name,count\nalpha,1\nbeta,2\n")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest([]string{"head", "--input", tmp, "--format", "csv", "--limit", "2"}, nil, nil)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`Preview `,
		`format: csv`,
		`rows: 2`,
		`count`,
		`name`,
		`alpha`,
		`beta`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in preview table: %s", want, stdout)
		}
	}
}

func jsonEscaped(value string) string {
	data, _ := json.Marshal(value)
	return string(data[1 : len(data)-1])
}

func findPythonForTest() (string, bool) {
	for _, name := range []string{"python", "python3", "py"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--version").Run(); err == nil {
			return path, true
		}
	}
	return "", false
}

func findNodeForTest() (string, bool) {
	path, err := exec.LookPath("node")
	if err != nil {
		return "", false
	}
	if err := exec.Command(path, "--version").Run(); err != nil {
		return "", false
	}
	return path, true
}
