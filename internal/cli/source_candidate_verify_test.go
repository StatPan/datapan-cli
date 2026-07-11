package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceCandidateVerifyUsesEnvAndRedactsOutput(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "source.json")
	candidates := filepath.Join(dir, "candidates.json")
	output := filepath.Join(dir, "verification.json")
	if err := os.WriteFile(profile, []byte(`{"schema_version":"datapan.source-profile.v1","source_id":"sample","provider":"Sample"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidates, []byte(`{
      "schema_version":"datapan.source-runtime-candidates.v1","source_id":"sample","provider":"Sample",
      "candidates":[{"candidate_id":"sample-query","method":"GET","endpoint_template":"https://example.test/items",
      "sample_parameters":{"apiKey":"${SAMPLE_KEY}","limit":"1"},
      "credential_policy":{"required":true,"key_names":["apiKey"],"injection_location":"query","placeholder":"${SAMPLE_KEY}"}}]
    }`), 0o600); err != nil {
		t.Fatal(err)
	}
	const secret = "fixture-secret-value"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("apiKey"); got != secret {
			t.Fatalf("credential query=%q", got)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"rows":[1]}`)), ContentLength: 12}, nil
	})
	code, stdout, stderr := runTest([]string{"verify", "--source-profile", profile, "--candidates", candidates, "--credential-env", "SAMPLE_TOKEN", "--output", output, "--json"}, fakeEnv{"SAMPLE_TOKEN": secret}, client)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{stdout, string(data)} {
		if strings.Contains(rendered, secret) || strings.Contains(rendered, "example.test") {
			t.Fatalf("verification output leaked request material: %s", rendered)
		}
	}
	if !strings.Contains(string(data), `"verified": 1`) || !strings.Contains(string(data), `"secret_values_present": false`) {
		t.Fatalf("unexpected verification report: %s", data)
	}
}

func TestSourceCandidateVerifySkipsMissingCredentialWithoutHTTP(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "source.json")
	candidates := filepath.Join(dir, "candidates.json")
	os.WriteFile(profile, []byte(`{"source_id":"sample","provider":"Sample"}`), 0o600)
	os.WriteFile(candidates, []byte(`{"source_id":"sample","provider":"Sample","candidates":[{"candidate_id":"sample-query","method":"GET","endpoint_template":"https://example.test/items","sample_parameters":{"key":"${KEY}"},"credential_policy":{"required":true,"key_names":["key"],"injection_location":"query","placeholder":"${KEY}"}}]}`), 0o600)
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("HTTP must not run without credential")
		return nil, nil
	})
	code, stdout, stderr := runTest([]string{"verify", "--source-profile", profile, "--candidates", candidates, "--credential-env", "MISSING_TOKEN", "--json"}, fakeEnv{}, client)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, `"skipped": 1`) || !strings.Contains(stdout, `"error_class": "credential"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}
