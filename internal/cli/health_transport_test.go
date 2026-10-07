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

func rewriteBoundCatalog(t *testing.T, path string, change func(*manifestHealthCatalog)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog manifestHealthCatalog
	if err = json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	change(&catalog)
	data, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	writeBoundCatalogAndUpdatePins(t, path, data)
}

func writeBoundCatalogAndUpdatePins(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	manifestData, err := os.ReadFile(defaultReleaseManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err = json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].Path == healthCatalogArtifactPath {
			manifest.Artifacts[i].SHA256 = fmt.Sprintf("%x", sum)
			manifest.Artifacts[i].Bytes = int64(len(data))
		}
	}
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(defaultReleaseManifestPath, manifestData, 0600); err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	provenance, err := readRegistryInstallProvenance(defaultRegistryInstallProvenancePath)
	if err != nil {
		t.Fatal(err)
	}
	provenance.ReleaseManifestSHA256 = fmt.Sprintf("%x", manifestSum)
	if err = writeJSONFile(defaultRegistryInstallProvenancePath, provenance); err != nil {
		t.Fatal(err)
	}
}

func TestHealthCatalogPreservesDeclaredTransportAndLegacyHTTPS(t *testing.T) {
	for _, scheme := range []string{"http", "https", ""} {
		t.Run("scheme-"+scheme, func(t *testing.T) {
			root, path := setupManifestBoundHealthCatalog(t)
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) { c.Entries[0].Endpoint.Scheme = scheme })
			want := scheme
			if want == "" {
				want = "https"
			}
			requests := 0
			client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Scheme != want || req.URL.Host != "apis.data.go.kr" {
					t.Fatal("request changed declared transport or authority")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00"},"body":{"items":[]}}}`))}, nil
			})
			code, _, _ := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", path, "--health-registry-revision", strings.Repeat("a", 40), "--output", filepath.Join(root, "receipt.json"), "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
			if code != exitOK || requests != 1 {
				t.Fatalf("code=%d requests=%d", code, requests)
			}
		})
	}
}

func TestHealthCatalogRejectsInjectedTransportBeforeRequest(t *testing.T) {
	for _, tc := range []struct{ name, scheme, host, path string }{
		{"scheme", "ftp", "apis.data.go.kr", "/service"},
		{"scheme-whitespace", " http", "apis.data.go.kr", "/service"},
		{"userinfo", "https", "user@apis.data.go.kr", "/service"},
		{"port", "https", "apis.data.go.kr:443", "/service"},
		{"authority-query", "https", "apis.data.go.kr?secret=x", "/service"},
		{"path-query", "https", "apis.data.go.kr", "/service?secret=x"},
		{"path-fragment", "https", "apis.data.go.kr", "/service#x"},
		{"path-network", "https", "apis.data.go.kr", "//example.test/service"},
		{"path-newline", "https", "apis.data.go.kr", "/service\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, path := setupManifestBoundHealthCatalog(t)
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) {
				c.Entries[0].Endpoint.Scheme = tc.scheme
				c.Entries[0].Endpoint.Host = tc.host
				c.Entries[0].Endpoint.Path = tc.path
			})
			client := roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid endpoint reached provider")
				return nil, nil
			})
			code, _, stderr := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", path, "--health-registry-revision", strings.Repeat("a", 40), "--output", filepath.Join(root, "receipt.json"), "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
			if code != exitUsage || !strings.Contains(stderr, "health catalog is not ready") {
				t.Fatalf("invalid endpoint code=%d", code)
			}
		})
	}
}

func TestHealthDaejeonNormalEnvelopeProducesRedactedHealthyReceipt(t *testing.T) {
	root, path := setupManifestBoundHealthCatalog(t)
	rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) {
		e := &c.Entries[0]
		e.Endpoint.Path = "/6300000/openapi2022/restrnt/getrestrnt"
		e.Aliases.CLIOperationKey = healthOperationKey(healthProbeOperation{DatasetID: e.Aliases.DatasetID, OperationName: e.Aliases.OperationName, Provider: e.Provider, EndpointHost: e.Endpoint.Host, EndpointPath: e.Endpoint.Path, DependencyClass: e.Endpoint.DependencyClass})
	})
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"},"body":{"items":[{"private":"row-hidden"}]}}}`))}, nil
	})
	output := filepath.Join(root, "receipt.json")
	code, _, _ := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", path, "--health-registry-revision", strings.Repeat("a", 40), "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var receipt healthProbeReceipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Assessment.Outcome != "healthy" || receipt.Observation.ProviderCode != "C00" || receipt.Observation.MaxLevel != "L4" {
		t.Fatal("normal provider envelope did not produce healthy L4 evidence")
	}
	for _, private := range []string{"credential-secret", "row-hidden", "NORMAL SERVICE"} {
		if strings.Contains(string(data), private) {
			t.Fatal("receipt leaked private request/response material")
		}
	}
}
