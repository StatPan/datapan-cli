package cli

import (
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

func TestManifestBoundHealthCatalogSkipsMonolithAndResolvesTenOperations(t *testing.T) {
	root, catalogPath := setupManifestBoundHealthCatalog(t)
	output := filepath.Join(root, "receipt.json")
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "credential-secret" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected bounded request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00"},"body":{"items":[]}}}`))}, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", catalogPath, "--health-registry-revision", strings.Repeat("a", 40), "--timeout", "10s", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	assertHealthReceipt(t, output, "healthy", "empty")
	var receipt healthProbeReceipt
	data, err := os.ReadFile(output)
	if err != nil || json.Unmarshal(data, &receipt) != nil {
		t.Fatalf("read receipt policy: %v", err)
	}
	if receipt.Policy == nil || receipt.Policy.Key != "dpr-op-00000001" || receipt.Policy.Version != 1 || receipt.Policy.Authority != "datapan-registry" || receipt.Policy.MaxLevel != "L4" {
		t.Fatalf("manifest-bound policy missing from receipt: %#v", receipt.Policy)
	}
	if strings.Contains(string(data), "credential-secret") {
		t.Fatal("manifest-bound receipt is unavailable or unsafe")
	}
}

func TestManifestBoundHealthCatalogRejectsTamperBeforeProviderExecution(t *testing.T) {
	_, catalogPath := setupManifestBoundHealthCatalog(t)
	if err := os.WriteFile(catalogPath, []byte(`{"schema_version":"datapan.health-probe-catalog.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("tampered catalog must stop before provider execution")
		return nil, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", catalogPath, "--health-registry-revision", strings.Repeat("a", 40), "--output", filepath.Join(t.TempDir(), "receipt.json"), "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitUsage || !strings.Contains(stderr, "health catalog is not ready") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func setupManifestBoundHealthCatalog(t *testing.T) (string, string) {
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
	if err := os.MkdirAll(filepath.Dir(defaultReleaseManifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	registryData := []byte(`not-json-monolith-that-must-not-be-loaded`)
	if err := os.WriteFile(defaultRegistryPath, registryData, 0o600); err != nil {
		t.Fatal(err)
	}
	registrySum := sha256.Sum256(registryData)

	catalog := manifestHealthCatalog{SchemaVersion: healthCatalogSchema, Authority: "datapan-registry"}
	catalog.SourceRegistry.SHA256 = fmt.Sprintf("%x", registrySum)
	for i := 1; i <= 10; i++ {
		var entry manifestHealthCatalogEntry
		entry.OperationID = fmt.Sprintf("dpr-op-%08d", i)
		entry.Policy.Key, entry.Policy.Version, entry.Policy.Authority, entry.Policy.MaxLevel = entry.OperationID, 1, "datapan-registry", "L4"
		entry.Aliases.DatasetID = fmt.Sprintf("150000%02d", i)
		entry.Aliases.OperationName = fmt.Sprintf("operation-%d", i)
		entry.Provider = "data.go.kr"
		entry.Endpoint.Host = "apis.data.go.kr"
		entry.Endpoint.Path = fmt.Sprintf("/service/operation-%d", i)
		entry.Endpoint.DependencyClass = "data_go_kr_gateway"
		entry.Eligibility.Status = "credential_required"
		entry.Execution.TimeoutCeilingMS, entry.Execution.RequestBudget = 10000, 1
		entry.Execution.SafeParameters = []manifestHealthParameter{{Name: "pageNo", Strategy: "bounded_integer", Minimum: 1, Maximum: 1}}
		op := healthProbeOperation{DatasetID: entry.Aliases.DatasetID, OperationName: entry.Aliases.OperationName, Provider: entry.Provider, EndpointHost: entry.Endpoint.Host, EndpointPath: entry.Endpoint.Path, DependencyClass: entry.Endpoint.DependencyClass}
		entry.Aliases.CLIOperationKey = healthOperationKey(op)
		catalog.Entries = append(catalog.Entries, entry)
	}
	catalogData, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "health-probe-catalog.json")
	if err := os.WriteFile(catalogPath, catalogData, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogSum := sha256.Sum256(catalogData)
	manifest := releaseManifest{SchemaVersion: "datapan.release-manifest.v1", ArtifactCount: 2, Artifacts: []releaseManifestArtifact{
		{Path: "data/data-go-kr.registry.json", Kind: "registry", Bytes: int64(len(registryData)), SHA256: fmt.Sprintf("%x", registrySum)},
		{Path: healthCatalogArtifactPath, Kind: "health_probe_catalog", Bytes: int64(len(catalogData)), SHA256: fmt.Sprintf("%x", catalogSum)},
	}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReleaseManifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	verified := true
	revision := strings.Repeat("a", 40)
	provenance := registryInstallProvenance{SchemaVersion: "datapan.registry-install.v1", Provider: "datapan-registry", RegistryPath: defaultRegistryPath, RegistrySHA256: fmt.Sprintf("%x", registrySum), ReleaseTag: revision, AssetURL: "https://example.test/registry.zip", PinMode: "pinned", SourceMode: "default_installed", Distribution: "huggingface_dataset", DatasetID: "StatPan/datapan-registry", DatasetRevision: revision, DatasetManifestURL: "https://example.test/manifest", DatasetManifestSHA256: strings.Repeat("b", 64), ReleaseManifestSHA256: fmt.Sprintf("%x", manifestSum), ManifestRegistryVerified: &verified}
	if err := writeJSONFile(defaultRegistryInstallProvenancePath, provenance); err != nil {
		t.Fatal(err)
	}
	return root, catalogPath
}
