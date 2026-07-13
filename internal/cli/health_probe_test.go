package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthProbeGatewayReceiptsClassifyAndRedact(t *testing.T) {
	cases := []struct {
		name, body, contentType, category, presence string
		status                                      int
		err                                         error
		wantCode                                    int
	}{
		{"success", `{"response":{"header":{"resultCode":"00","resultMsg":"OK"},"body":{"items":{"item":[{"secret_row":"do-not-emit"}]}}}}`, "application/json", "healthy", "present", 200, nil, exitOK},
		{"http failure", `failure`, "text/plain", "provider_failure", "indeterminate", 500, nil, exitRequest},
		{"provider error in 200", `{"response":{"header":{"resultCode":"30","resultMsg":"SERVICE KEY IS NOT REGISTERED"}}}`, "application/json", "credential_rejected", "indeterminate", 200, nil, exitRequest},
		{"rate limit", `slow down`, "text/plain", "rate_limited", "indeterminate", 429, nil, exitRequest},
		{"rejected credential", `forbidden`, "text/plain", "credential_rejected", "indeterminate", 403, nil, exitRequest},
		{"malformed body", `{broken`, "application/json", "schema_drift", "indeterminate", 200, nil, exitRequest},
		{"empty data", ``, "application/json", "healthy", "empty", 200, nil, exitOK},
		{"timeout", ``, "", "timeout", "not_observed", 0, context.DeadlineExceeded, exitRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := setupHealthProbeRegistry(t, gatewayRegistryJSON())
			output := filepath.Join(root, "receipt.json")
			client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.URL.Query().Get("serviceKey"); got != "credential-secret" {
					t.Fatalf("executor did not inject credential: %q", got)
				}
				if tc.err != nil {
					return nil, tc.err
				}
				header := make(http.Header)
				header.Set("Content-Type", tc.contentType)
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			code, stdout, stderr := runTest([]string{"verify", "--ref", "100", "--operation", "list", "--health", "--timeout", "10ms", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
			if code != tc.wantCode || stderr != "" {
				t.Fatalf("code=%d want=%d stdout=%s stderr=%s", code, tc.wantCode, stdout, stderr)
			}
			assertHealthReceipt(t, output, tc.category, tc.presence)
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"credential-secret", "serviceKey=", "secret_row", "do-not-emit", "?"} {
				if strings.Contains(string(data), forbidden) {
					t.Fatalf("receipt leaked %q: %s", forbidden, data)
				}
			}
		})
	}
}

func TestHealthProbeMissingCredentialIsOneSkippedReceipt(t *testing.T) {
	root := setupHealthProbeRegistry(t, gatewayRegistryJSON())
	output := filepath.Join(root, "receipt.json")
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing credential must stop before HTTP")
		return nil, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "100", "--operation", "list", "--health", "--output", output, "--json"}, fakeEnv{}, client)
	if code != exitAuth || stderr != "" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	assertHealthReceipt(t, output, "credential_missing", "not_observed")
}

func TestHealthProbeRegisteredExternalAdapterUsesSameReceipt(t *testing.T) {
	registry := `[ {"id":"200","title":"QNet","provider":"data.go.kr","priority":"P2","operations":[{"name":"stats","endpoint":"http://openapi.q-net.or.kr/api/service/rest/InquiryStatSVC/getGradSiPassList","default_params":{"baseYY":"2023"},"request_params":[{"name":"serviceKey"},{"name":"baseYY"}]}]} ]`
	root := setupHealthProbeRegistry(t, registry)
	output := filepath.Join(root, "adapter-receipt.json")
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<response><header><resultCode>00</resultCode><resultMsg>NORMAL SERVICE</resultMsg></header><body><items><item><name>row-hidden</name></item></items></body></response>`))}, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "200", "--operation", "stats", "--health", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	assertHealthReceipt(t, output, "healthy", "present")
	data, _ := os.ReadFile(output)
	if strings.Contains(string(data), "row-hidden") || !strings.Contains(string(data), `"dependency_class": "external_endpoint"`) {
		t.Fatalf("bad adapter receipt: %s", data)
	}
}

func TestHealthOperationKeyIsRegistryRevisionIndependent(t *testing.T) {
	op := healthProbeOperation{Provider: "data.go.kr", DatasetID: "100", OperationName: "list", DependencyClass: "data_go_kr_gateway", EndpointHost: "apis.data.go.kr", EndpointPath: "/100/list"}
	first := healthOperationKey(op)
	second := healthOperationKey(op)
	if first != second || len(first) != 64 {
		t.Fatalf("unstable operation key: %q %q", first, second)
	}
	op.OperationName = "renamed"
	if healthOperationKey(op) == first {
		t.Fatal("operation rename must create a new identity")
	}
}

func setupHealthProbeRegistry(t *testing.T, registry string) string {
	t.Helper()
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.MkdirAll(".datapan", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultRegistryPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(registry))
	manifest := sha256.Sum256([]byte("manifest"))
	verified := true
	revision := strings.Repeat("a", 40)
	provenance := registryInstallProvenance{SchemaVersion: "datapan.registry-install.v1", InstalledAt: "2026-07-13T00:00:00Z", Provider: "datapan-registry", RegistryPath: defaultRegistryPath, RegistrySHA256: fmt.Sprintf("%x", sum), ReleaseTag: revision, AssetURL: "https://example.test/registry.zip", PinMode: "pinned", SourceMode: "default_installed", ManifestRegistryVerified: &verified, Distribution: "huggingface_dataset", DatasetID: "StatPan/datapan-registry", DatasetRevision: revision, DatasetManifestURL: "https://example.test/manifest", DatasetManifestSHA256: strings.Repeat("b", 64), ReleaseManifestSHA256: fmt.Sprintf("%x", manifest)}
	if err := writeJSONFile(defaultRegistryInstallProvenancePath, provenance); err != nil {
		t.Fatal(err)
	}
	return root
}

func gatewayRegistryJSON() string {
	return `[ {"id":"100","title":"Gateway","provider":"data.go.kr","priority":"P2","operations":[{"name":"list","endpoint":"https://apis.data.go.kr/100/list","request_params":[{"name":"serviceKey"},{"name":"numOfRows"}]}]} ]`
}

func assertHealthReceipt(t *testing.T, path, category, presence string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaBytesForTest(t, compileRepositorySchemaForTest(t, "datapan.health-probe.v1.schema.json"), data)
	var receipt healthProbeReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Assessment.Category != category || receipt.Observation.DataPresence != presence {
		t.Fatalf("category=%s presence=%s receipt=%s", receipt.Assessment.Category, receipt.Observation.DataPresence, data)
	}
	if receipt.Registry.DatasetRevision != strings.Repeat("a", 40) || len(receipt.Registry.RegistrySHA256) != 64 || len(receipt.Registry.ManifestSHA256) != 64 {
		t.Fatalf("mutable/missing provenance: %s", data)
	}
	if !receipt.Redaction.CredentialsRemoved || !receipt.Redaction.QueryValuesRemoved || !receipt.Redaction.ResponseRowsRemoved {
		t.Fatalf("redaction assertions missing: %s", data)
	}
}
