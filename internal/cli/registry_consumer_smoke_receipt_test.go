package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRegistryConsumerSmokeReceiptFixturesAreContractOnly(t *testing.T) {
	schema := compileRepositorySchemaForTest(t, "datapan.registry-consumer-smoke-receipt.v1.schema.json")
	root := filepath.Join("testdata", "registry-consumer-smoke-receipt")
	for _, name := range []string{
		"compatible-fixture.json",
		"incompatible-with-target-fixture.json",
		"incompatible-no-safe-target-fixture.json",
	} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		validateSchemaBytesForTest(t, schema, data)
		if err := validateObservedRegistryConsumerSmokeReceiptAdmission(schema, data, time.Date(2026, 7, 22, 0, 10, 0, 0, time.UTC), 10*time.Minute); err == nil || !strings.Contains(err.Error(), "rejects \"fixture\" evidence") {
			t.Fatalf("fixture %s must not be accepted as published smoke evidence: %v", name, err)
		}
	}
}

func TestRegistryConsumerSmokeReceiptSchemaRejectsAmbiguousOrUnredactedPayloads(t *testing.T) {
	schema := compileRepositorySchemaForTest(t, "datapan.registry-consumer-smoke-receipt.v1.schema.json")
	data, err := os.ReadFile(filepath.Join("testdata", "registry-consumer-smoke-receipt", "compatible-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"fixture claims completion":           func(payload map[string]any) { payload["completion_evidence"] = true },
		"fixture claims install":              func(payload map[string]any) { payload["install"].(map[string]any)["result"] = "succeeded" },
		"unredacted unknown field":            func(payload map[string]any) { payload["authorization"] = "secret-value" },
		"missing immutable registry revision": func(payload map[string]any) { delete(payload["registry"].(map[string]any), "revision") },
		"mutable registry revision":           func(payload map[string]any) { payload["registry"].(map[string]any)["revision"] = "main" },
	} {
		t.Run(name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			mutate(payload)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(instance); err == nil {
				t.Fatalf("invalid payload was accepted: %s", encoded)
			}
		})
	}
}

func TestObservedRegistryConsumerSmokeReceiptSchemaBindsRollbackToOutcome(t *testing.T) {
	schema := compileRepositorySchemaForTest(t, "datapan.registry-consumer-smoke-receipt.v1.schema.json")
	base, err := os.ReadFile(filepath.Join("testdata", "registry-consumer-smoke-receipt", "compatible-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "compatible requires rollback not required",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "succeeded", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "completed", "result": "compatible", "reason_code": "compatible"}
				payload["outcome"] = map[string]any{"status": "compatible", "reason_code": "compatible"}
				payload["rollback"] = map[string]any{"state": "not_required"}
			},
		},
		{
			name: "failure requires immutable rollback target",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "succeeded", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "completed", "result": "incompatible", "reason_code": "incompatible"}
				payload["outcome"] = map[string]any{"status": "failed", "reason_code": "incompatible"}
				payload["rollback"] = map[string]any{"state": "target_available", "target_registry_revision": "3333333333333333333333333333333333333333"}
			},
		},
		{
			name: "no safe target holds release",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "failed", "network_access": "published_registry_only", "reason_code": "install_failed"}
				payload["doctor"] = map[string]any{"execution": "not_run", "result": "not_observed", "reason_code": "install_failed"}
				payload["outcome"] = map[string]any{"status": "manual_hold", "reason_code": "no_safe_target"}
				payload["rollback"] = map[string]any{"state": "no_safe_target"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(base, &payload); err != nil {
				t.Fatal(err)
			}
			tc.mutate(payload)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			validateSchemaBytesForTest(t, schema, encoded)
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "failure target state without immutable target is rejected",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "succeeded", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "completed", "result": "incompatible"}
				payload["outcome"] = map[string]any{"status": "failed", "reason_code": "incompatible"}
				payload["rollback"] = map[string]any{"state": "target_available"}
			},
		},
		{
			name: "no safe target cannot name a target or claim failure complete",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "failed", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "not_run", "result": "not_observed"}
				payload["outcome"] = map[string]any{"status": "failed", "reason_code": "no_safe_target"}
				payload["rollback"] = map[string]any{"state": "no_safe_target", "target_registry_revision": "3333333333333333333333333333333333333333"}
			},
		},
		{
			name: "observed cannot be an offline contract result",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "not_run", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "not_run", "result": "not_observed"}
				payload["outcome"] = map[string]any{"status": "contract_validated", "reason_code": "fixture_contract_only"}
				payload["rollback"] = map[string]any{"state": "not_required"}
			},
		},
		{
			name: "failed observed receipt cannot omit rollback target",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "succeeded", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "completed", "result": "incompatible"}
				payload["outcome"] = map[string]any{"status": "failed", "reason_code": "incompatible"}
				payload["rollback"] = map[string]any{"state": "not_required"}
			},
		},
		{
			name: "manual hold cannot claim rollback not required",
			mutate: func(payload map[string]any) {
				payload["evidence_class"] = "observed"
				payload["completion_evidence"] = true
				payload["install"] = map[string]any{"execution": "completed", "result": "failed", "network_access": "published_registry_only"}
				payload["doctor"] = map[string]any{"execution": "not_run", "result": "not_observed"}
				payload["outcome"] = map[string]any{"status": "manual_hold", "reason_code": "no_safe_target"}
				payload["rollback"] = map[string]any{"state": "not_required"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(base, &payload); err != nil {
				t.Fatal(err)
			}
			tc.mutate(payload)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(instance); err == nil {
				t.Fatalf("invalid rollback payload was accepted: %s", encoded)
			}
		})
	}
}

func TestObservedRegistryConsumerSmokeReceiptAdmissionFailsClosed(t *testing.T) {
	schema := compileRepositorySchemaForTest(t, "datapan.registry-consumer-smoke-receipt.v1.schema.json")
	reference := time.Date(2026, 7, 22, 0, 10, 0, 0, time.UTC)
	base, err := os.ReadFile(filepath.Join("testdata", "registry-consumer-smoke-receipt", "compatible-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	observed := func(at string) []byte {
		var payload map[string]any
		if err := json.Unmarshal(base, &payload); err != nil {
			t.Fatal(err)
		}
		payload["evidence_class"] = "observed"
		payload["completion_evidence"] = true
		payload["observed_at"] = at
		payload["install"] = map[string]any{"execution": "completed", "result": "succeeded", "network_access": "published_registry_only"}
		payload["doctor"] = map[string]any{"execution": "completed", "result": "compatible", "reason_code": "compatible"}
		payload["outcome"] = map[string]any{"status": "compatible", "reason_code": "compatible"}
		payload["rollback"] = map[string]any{"state": "not_required"}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	withUnknownField := observed("2026-07-22T00:00:00Z")
	withUnknownField = append(withUnknownField[:len(withUnknownField)-1], []byte(`,"authorization":"secret-value"}`)...)
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "fresh observed", data: observed("2026-07-22T00:00:00Z")},
		{name: "future", data: observed("2026-07-22T00:10:01Z"), want: "future"},
		{name: "stale", data: observed("2026-07-21T23:59:59Z"), want: "stale"},
		{name: "malformed", data: observed("not-a-time"), want: "observed_at"},
		{name: "fixture", data: base, want: "rejects \"fixture\" evidence"},
		{name: "minimal observed payload", data: []byte(`{"evidence_class":"observed","completion_evidence":true,"observed_at":"2026-07-22T00:00:00Z"}`), want: "schema validation failed"},
		{name: "unknown secret field", data: withUnknownField, want: "schema validation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateObservedRegistryConsumerSmokeReceiptAdmission(schema, tc.data, reference, 10*time.Minute)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("fresh observed receipt rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("admission diagnostic leaked a secret: %v", err)
			}
		})
	}
}
