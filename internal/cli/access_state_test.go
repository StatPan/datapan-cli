package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAccessStatusMissingRecordIsExplicitlyUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	statePath := filepath.Join(t.TempDir(), "access-state.json")
	code, stdout, stderr := runTest([]string{"access", "status", "--provider", "OpenDART", "--service", "disclosure", "--input", statePath, "--json"}, nil, nil)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["record_present"] != false || payload["application"].(map[string]any)["state"] != "unknown" {
		t.Fatalf("missing record did not fail closed: %s", stdout)
	}
}

func TestAccessRecordTransitionsAndDoesNotLeakSensitiveMaterial(t *testing.T) {
	t.Chdir(t.TempDir())
	statePath := filepath.Join(t.TempDir(), "access-state.json")
	args := []string{"access", "record", "--provider", "OpenDART", "--service", "disclosure", "--application-state", "requested", "--quota-state", "available", "--observed-at", "2026-07-24T09:00:00Z", "--output", statePath, "--json"}
	code, stdout, stderr := runTest(args, fakeEnv{"OPENDART_API_KEY": "credential-secret"}, nil)
	if code != exitOK || stderr != "" {
		t.Fatalf("first record code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "secret") {
		t.Fatalf("unexpected sensitive output: %s", stdout)
	}

	code, stdout, stderr = runTest([]string{"access", "record", "--provider", "OpenDART", "--service", "disclosure", "--application-state", "approved", "--rate-limit-state", "observed", "--observed-at", "2026-07-24T10:00:00Z", "--output", statePath, "--json"}, fakeEnv{"OPENDART_API_KEY": "credential-secret"}, nil)
	if code != exitOK || stderr != "" {
		t.Fatalf("transition code=%d stderr=%q", code, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["application"].(map[string]any)["state"] != "approved" || payload["rate_limit"].(map[string]any)["state"] != "observed" {
		t.Fatalf("transition missing: %s", stdout)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential-secret", "https://", "?", "response body", "user@example"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("access state contains forbidden %q: %s", forbidden, data)
		}
	}
	var state localAccessState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Redaction.CredentialValuesPresent || state.Redaction.CredentialHashesPresent || state.Redaction.RequestURLsPresent || state.Redaction.ParameterValuesPresent || state.Redaction.ResponseBodiesPresent || state.Redaction.UserIdentityPresent {
		t.Fatalf("redaction boundary was not explicit and clear: %#v", state.Redaction)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("state file permissions = %o, want 600", info.Mode().Perm())
	}

	code, _, stderr = runTest([]string{"access", "record", "--provider", "OpenDART", "--service", "disclosure", "--application-state", "rejected", "--observed-at", "2026-07-24T08:00:00Z", "--output", statePath, "--json"}, nil, nil)
	if code != exitUsage || !strings.Contains(stderr, "predates") {
		t.Fatalf("older observation was accepted: code=%d stderr=%q", code, stderr)
	}
}

func TestAccessRecordRejectsProviderURLAsServiceEvidence(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, stderr := runTest([]string{"access", "record", "--provider", "OpenDART", "--service", "https://example.test/?serviceKey=secret", "--application-state", "requested", "--observed-at", "2026-07-24T09:00:00Z", "--json"}, nil, nil)
	if code != exitUsage || !strings.Contains(stderr, "not URLs") {
		t.Fatalf("unsafe provider service was accepted: code=%d stderr=%q", code, stderr)
	}
}

func TestAccessRecordRegistryOperationAndNeverCallsProvider(t *testing.T) {
	t.Chdir(t.TempDir())
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(registryPath, []byte(`[{"id":"100","title":"test","provider":"data.go.kr","operations":[{"name":"lookup","endpoint":"https://apis.data.go.kr/test"}]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := roundTripFunc(func(_ *http.Request) (*http.Response, error) { requests++; return nil, nil })
	code, stdout, stderr := runTest([]string{"access", "record", "--ref", "100", "--operation", "lookup", "--application-state", "requested", "--observed-at", "2026-07-24T09:00:00Z", "--json"}, fakeEnv{"DATAPAN_REGISTRY_PATH": registryPath}, client)
	if code != exitOK || stderr != "" || requests != 0 {
		t.Fatalf("code=%d stderr=%q requests=%d", code, stderr, requests)
	}
	if !strings.Contains(stdout, `"scope": "registry_operation"`) || !strings.Contains(stdout, `"service": "apis.data.go.kr"`) {
		t.Fatalf("unexpected subject: %s", stdout)
	}
}
