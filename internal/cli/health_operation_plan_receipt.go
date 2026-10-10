package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const healthOperationPlanReceiptMaxBytes = 64 << 10

type healthOperationPlanProbeReceipt struct {
	SchemaVersion string                              `json:"schema_version"`
	AttemptID     string                              `json:"attempt_id"`
	CLI           healthOperationPlanProbeCLI         `json:"cli"`
	Registry      healthOperationPlanProbeRegistry    `json:"registry"`
	Operation     healthOperationPlanProbeOperation   `json:"operation"`
	Execution     healthOperationPlanProbeExecution   `json:"execution"`
	Observation   healthOperationPlanProbeObservation `json:"observation"`
	Redaction     healthOperationPlanProbeRedaction   `json:"redaction"`
}

type healthOperationPlanProbeCLI struct {
	Version      string `json:"version"`
	BinarySHA256 string `json:"binary_sha256"`
}

type healthOperationPlanProbeRegistry struct {
	DatasetID                   string `json:"dataset_id"`
	RegistryRevision            string `json:"registry_revision"`
	Distribution                string `json:"distribution"`
	DistributionDatasetRevision string `json:"distribution_dataset_revision,omitempty"`
	RegistrySHA256              string `json:"registry_sha256"`
	ManifestSHA256              string `json:"manifest_sha256"`
	OperationManifestSHA256     string `json:"operation_manifest_sha256"`
	ProviderIndexSHA256         string `json:"provider_index_sha256"`
	PlanSchemaSHA256            string `json:"plan_schema_sha256"`
	IndexSHA256                 string `json:"index_sha256"`
	ShardSHA256                 string `json:"shard_sha256"`
	SourceIdentitySetSHA256     string `json:"source_identity_set_sha256"`
}

type healthOperationPlanProbeOperation struct {
	OperationID string `json:"operation_id"`
	SourceID    string `json:"source_id"`
	Provider    string `json:"provider"`
	AdapterID   string `json:"adapter_id"`
	Protocol    string `json:"protocol"`
}

type healthOperationPlanProbeExecution struct {
	RequestStarted bool  `json:"request_started"`
	RequestBudget  int   `json:"request_budget"`
	TimeoutMS      int64 `json:"timeout_ms"`
	DurationMS     int64 `json:"duration_ms"`
}

type healthOperationPlanProbeObservation struct {
	ResponseObserved bool   `json:"response_observed"`
	ObservedAt       string `json:"observed_at,omitempty"`
	HTTPStatus       int    `json:"http_status,omitempty"`
	Outcome          string `json:"outcome"`
	ReasonCode       string `json:"reason_code"`
	AssertionKind    string `json:"assertion_kind"`
	AssertionStatus  string `json:"assertion_status"`
}

type healthOperationPlanProbeRedaction struct {
	CredentialValuesRemoved     bool `json:"credential_values_removed"`
	CredentialReferencesRemoved bool `json:"credential_references_removed"`
	CredentialEnvNamesRemoved   bool `json:"credential_env_names_removed"`
	QueryValuesRemoved          bool `json:"query_values_removed"`
	RequestBodyRemoved          bool `json:"request_body_removed"`
	ResponseBodyRemoved         bool `json:"response_body_removed"`
	ResponseRowsRemoved         bool `json:"response_rows_removed"`
	EndpointDetailsRemoved      bool `json:"endpoint_details_removed"`
	QuotaDetailsRemoved         bool `json:"quota_details_removed"`
}

func (a app) catalogVerifyHealthOperationPlan(output string, jsonOut bool) int {
	loaded := a.healthOperationPlan
	if loaded == nil || !jsonOut || !healthOperationPlanReceiptPathSafe(output, *loaded) {
		return a.fail(exitUsage, "health operation-plan execution requires JSON output to a new path outside the installed Registry")
	}
	binaryDigest, err := healthExecutableSHA256()
	if err != nil {
		if errors.Is(err, errHealthRunningImageAttestationUnavailable) {
			return a.fail(exitRequest, "health operation-plan probing requires Linux running-image attestation")
		}
		return a.fail(exitRequest, "could not identify the running CLI binary")
	}

	started := time.Now()
	receipt := newHealthOperationPlanProbeReceipt(*loaded, binaryDigest)
	finish := func() int {
		elapsed := time.Since(started).Milliseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		receipt.Execution.DurationMS = elapsed
		return a.writeHealthOperationPlanReceipt(output, receipt)
	}

	plan := loaded.Plan
	contract := plan.RequestPlan.RequestContract
	if contract.Authentication.Requirement == "required" {
		credential, resolveErr := a.resolveHealthPlanCredential(plan, loaded.Options.CredentialBindingsPath)
		if resolveErr != nil {
			switch {
			case errors.Is(resolveErr, errHealthCredentialBindingsUnavailable):
				receipt.Observation.ReasonCode = "credential_binding_unavailable"
			case errors.Is(resolveErr, errHealthCredentialBindingMismatch):
				receipt.Observation.ReasonCode = "credential_binding_mismatch"
			case errors.Is(resolveErr, errHealthCredentialSourceUnavailable):
				receipt.Observation.ReasonCode = "credential_source_unavailable"
			default:
				receipt.Observation.ReasonCode = "credential_binding_unavailable"
			}
			return finish()
		}
		// Keep the secret in memory only for request construction and dispatch.
		shape, shapeErr := healthOperationPlanRequestShape(plan, credential.Value, time.Now().UTC())
		if shapeErr != nil {
			receipt.Observation.ReasonCode = "operation_plan_unsupported"
			return finish()
		}
		return a.executeHealthOperationPlanRequest(output, receipt, shape, started)
	}

	shape, shapeErr := healthOperationPlanRequestShape(plan, "", time.Now().UTC())
	if shapeErr != nil {
		receipt.Observation.ReasonCode = "operation_plan_unsupported"
		return finish()
	}
	return a.executeHealthOperationPlanRequest(output, receipt, shape, started)
}

func newHealthOperationPlanProbeReceipt(loaded healthOperationPlanLoadResult, binaryDigest string) healthOperationPlanProbeReceipt {
	contract := loaded.Plan.RequestPlan.RequestContract
	assertionKind := contract.ResponseAssertion.Kind
	return healthOperationPlanProbeReceipt{
		SchemaVersion: "datapan.health-operation-plan-probe.v1",
		AttemptID:     loaded.Options.AttemptID,
		CLI:           healthOperationPlanProbeCLI{Version: version, BinarySHA256: binaryDigest},
		Registry: healthOperationPlanProbeRegistry{
			DatasetID:                   loaded.RegistryTrust.DatasetID,
			RegistryRevision:            loaded.Options.RegistryRevision,
			Distribution:                loaded.RegistryTrust.Distribution,
			DistributionDatasetRevision: loaded.RegistryTrust.DatasetRevision,
			RegistrySHA256:              strings.ToLower(loaded.RegistryTrust.RegistrySHA256),
			ManifestSHA256:              strings.ToLower(loaded.ManifestSHA256),
			OperationManifestSHA256:     strings.ToLower(loaded.Index.GenerationInputs.OperationManifest.SHA256),
			ProviderIndexSHA256:         strings.ToLower(loaded.Index.GenerationInputs.ProviderIndex.SHA256),
			PlanSchemaSHA256:            healthOperationPlanSchemaSHA256,
			IndexSHA256:                 strings.ToLower(loaded.IndexSHA256),
			ShardSHA256:                 strings.ToLower(loaded.Shard.SHA256),
			SourceIdentitySetSHA256:     strings.ToLower(loaded.SourceScope.IdentitySetSHA256),
		},
		Operation: healthOperationPlanProbeOperation{
			OperationID: loaded.Plan.OperationIdentity.OperationID,
			SourceID:    loaded.Plan.SourceBinding.SourceID,
			Provider:    loaded.Plan.SourceBinding.Provider,
			AdapterID:   loaded.Plan.SourceBinding.AdapterID,
			Protocol:    loaded.Plan.OperationIdentity.Protocol,
		},
		Execution: healthOperationPlanProbeExecution{
			RequestBudget: 0,
			TimeoutMS:     int64(contract.Limits.TimeoutMS),
		},
		Observation: healthOperationPlanProbeObservation{
			Outcome:         "blocked",
			ReasonCode:      "operation_plan_unsupported",
			AssertionKind:   assertionKind,
			AssertionStatus: "not_run",
		},
		Redaction: healthOperationPlanProbeRedaction{
			CredentialValuesRemoved:     true,
			CredentialReferencesRemoved: true,
			CredentialEnvNamesRemoved:   true,
			QueryValuesRemoved:          true,
			RequestBodyRemoved:          true,
			ResponseBodyRemoved:         true,
			ResponseRowsRemoved:         true,
			EndpointDetailsRemoved:      true,
			QuotaDetailsRemoved:         true,
		},
	}
}

func (a app) executeHealthOperationPlanRequest(output string, receipt healthOperationPlanProbeReceipt, shape healthHTTPRequestShape, started time.Time) int {
	writeReceipt := func() int {
		elapsed := time.Since(started).Milliseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		receipt.Execution.DurationMS = elapsed
		return a.writeHealthOperationPlanReceipt(output, receipt)
	}
	if !a.healthOperationPlan.Options.Deadline.After(time.Now()) {
		receipt.Observation.ReasonCode = "deadline_expired_before_request"
		return writeReceipt()
	}
	if _, err := buildHealthHTTPRequest(shape); err != nil {
		var transportErr healthTransportError
		if errors.As(err, &transportErr) && transportErr.code == healthTransportRequestLimit {
			receipt.Observation.ReasonCode = "request_limit_exceeded"
		} else {
			receipt.Observation.ReasonCode = "operation_plan_unsupported"
		}
		return writeReceipt()
	}

	parent, cancel := context.WithDeadline(context.Background(), a.healthOperationPlan.Options.Deadline)
	defer cancel()
	receipt.Execution.RequestStarted = true
	receipt.Execution.RequestBudget = 1
	response, requestErr := executeHealthHTTPRequest(parent, a.http, shape)
	if response.StatusCode >= 100 && response.StatusCode <= 599 {
		receipt.Observation.ResponseObserved = true
		receipt.Observation.HTTPStatus = response.StatusCode
		receipt.Observation.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if requestErr != nil {
		// Observation-only contracts need no response body to classify HTTP
		// status. A bounded body read error on a non-2xx response cannot erase
		// the already observed HTTP failure.
		if a.healthOperationPlan.ResponseAssertion.ObservationOnly && response.StatusCode >= 100 && response.StatusCode <= 599 && (response.StatusCode < 200 || response.StatusCode >= 300) {
			receipt.Observation.Outcome = "unhealthy"
			receipt.Observation.ReasonCode = "response_http_failure"
			receipt.Observation.AssertionStatus = "failed"
			return writeReceipt()
		}
		receipt.Observation.Outcome = "indeterminate"
		var transportErr healthTransportError
		if errors.As(requestErr, &transportErr) {
			switch transportErr.code {
			case healthTransportDeadline:
				receipt.Observation.ReasonCode = "request_deadline_exceeded"
			case healthTransportRequestLimit:
				receipt.Observation.ReasonCode = "request_limit_exceeded"
			case healthTransportResponseLimit:
				receipt.Observation.ReasonCode = "response_limit_exceeded"
				receipt.Observation.Outcome = "indeterminate"
			case healthTransportResponseRead:
				receipt.Observation.ReasonCode = "response_read_failed"
			case healthTransportInvalidResponse:
				receipt.Observation.ReasonCode = "response_invalid"
			default:
				receipt.Observation.ReasonCode = "request_transport_failed"
			}
		} else {
			receipt.Observation.ReasonCode = "request_transport_failed"
		}
		return writeReceipt()
	}

	asserted := evaluateHealthNormalizedResponseAssertion(a.healthOperationPlan.ResponseAssertion, response)
	if a.healthOperationPlan.ResponseAssertion.ObservationOnly {
		switch asserted.ReasonCode {
		case "response_semantics_unestablished":
			receipt.Observation.Outcome = "indeterminate"
			receipt.Observation.ReasonCode = "response_semantics_unestablished"
			receipt.Observation.AssertionStatus = "not_run"
		case "response_http_failure":
			receipt.Observation.Outcome = "unhealthy"
			receipt.Observation.ReasonCode = "response_http_failure"
			receipt.Observation.AssertionStatus = "failed"
		default:
			receipt.Observation.Outcome = "indeterminate"
			receipt.Observation.ReasonCode = asserted.ReasonCode
			receipt.Observation.AssertionStatus = "not_run"
		}
		return writeReceipt()
	}
	switch asserted.Outcome {
	case healthResponseHealthy:
		receipt.Observation.Outcome = "healthy"
		receipt.Observation.ReasonCode = "response_assertion_passed"
		receipt.Observation.AssertionStatus = "passed"
	case healthResponseUnhealthy:
		receipt.Observation.Outcome = "unhealthy"
		receipt.Observation.ReasonCode = "response_assertion_failed"
		receipt.Observation.AssertionStatus = "failed"
	default:
		receipt.Observation.Outcome = "indeterminate"
		receipt.Observation.ReasonCode = "response_assertion_invalid"
		receipt.Observation.AssertionStatus = "not_run"
	}
	return writeReceipt()
}

func (a app) writeHealthOperationPlanReceipt(output string, receipt healthOperationPlanProbeReceipt) int {
	data, err := json.Marshal(receipt)
	if err != nil {
		return a.fail(exitRequest, "could not encode health operation-plan receipt")
	}
	data = append(data, '\n')
	if len(data) > healthOperationPlanReceiptMaxBytes {
		return a.fail(exitRequest, "health operation-plan receipt exceeds its output ceiling")
	}
	if err := writeOutputAtomic(output, data, io.Discard); err != nil {
		return a.fail(exitRequest, "could not persist health operation-plan receipt")
	}
	n, err := a.stdout.Write(data)
	if err != nil || n != len(data) {
		return a.fail(exitRequest, "could not write health operation-plan receipt")
	}
	if receipt.Observation.Outcome == "healthy" {
		return exitOK
	}
	if receipt.Observation.Outcome == "blocked" {
		return exitAuth
	}
	return exitRequest
}

func healthOperationPlanReceiptPathSafe(output string, loaded healthOperationPlanLoadResult) bool {
	if output == "" || output == "-" || strings.ContainsAny(output, "\r\n\x00") {
		return false
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return false
	}
	if _, err := os.Lstat(absOutput); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	parentInfo, err := os.Stat(filepath.Dir(absOutput))
	if err != nil || !parentInfo.IsDir() {
		return false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absOutput))
	if err != nil {
		return false
	}
	resolvedOutput := filepath.Join(parent, filepath.Base(absOutput))
	for _, excluded := range []string{loaded.ArtifactRoot, filepath.Dir(defaultRegistryPath), filepath.Dir(defaultReleaseManifestPath), filepath.Dir(defaultRegistryInstallProvenancePath)} {
		absRoot, err := filepath.Abs(excluded)
		if err != nil {
			return false
		}
		root, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			return false
		}
		if healthPathIsWithin(root, resolvedOutput) {
			return false
		}
	}
	bindings, err := filepath.Abs(loaded.Options.CredentialBindingsPath)
	if err == nil {
		resolvedBindings, resolveErr := filepath.EvalSymlinks(bindings)
		if resolveErr == nil && sameFilePath(resolvedOutput, resolvedBindings) {
			return false
		}
		if resolveErr != nil && sameFilePath(resolvedOutput, bindings) {
			return false
		}
	}
	return true
}

func healthPathIsWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return true
	}
	if relative == "." {
		return true
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
