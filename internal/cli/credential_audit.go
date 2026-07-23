package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	providers "github.com/StatPan/datapan-cli/internal/provider"
)

// credentialAuditReceipt retains only the minimum evidence needed to explain
// readiness. It intentionally has no URL, parameter values, response body, or
// credential-derived field.
type credentialAuditReceipt struct {
	SchemaVersion string `json:"schema_version"`
	ObservedAt    string `json:"observed_at"`
	Operation     struct {
		DatasetID       string `json:"dataset_id"`
		Operation       string `json:"operation"`
		Provider        string `json:"provider"`
		CredentialGroup string `json:"credential_group"`
		EndpointHost    string `json:"endpoint_host"`
	} `json:"operation"`
	Credential struct {
		Present         bool     `json:"present"`
		SelectedEnvVar  string   `json:"selected_env_var,omitempty"`
		AcceptedEnvVars []string `json:"accepted_env_vars"`
		LiveVerified    bool     `json:"live_verified"`
	} `json:"credential"`
	Execution struct {
		OptIn         bool  `json:"opt_in"`
		RequestBudget int   `json:"request_budget"`
		Attempted     bool  `json:"attempted"`
		TimeoutMS     int64 `json:"timeout_ms"`
	} `json:"execution"`
	Assessment struct {
		Outcome    string `json:"outcome"`
		Category   string `json:"category"`
		ReasonCode string `json:"reason_code"`
		HTTPStatus int    `json:"http_status,omitempty"`
	} `json:"assessment"`
	Redaction struct {
		CredentialValuesPresent bool `json:"credential_values_present"`
		CredentialHashesPresent bool `json:"credential_hashes_present"`
		RequestURLsPresent      bool `json:"request_urls_present"`
		ResponseBodiesPresent   bool `json:"response_bodies_present"`
	} `json:"redaction"`
}

func (a app) authAudit(args []string, jsonOut bool) int {
	ref, args, err := consumeString(args, "--ref", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	operation, args, err := consumeString(args, "--operation", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	timeout, args, err := consumeDuration(args, "--timeout", defaultCallTimeout)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	live, args := consumeBool(args, "--live")
	if !live || ref == "" || operation == "" || output == "" || output == "-" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan auth audit --live --ref REF --operation NAME --output PATH [--timeout DURATION] [--json]")
	}
	result := a.reg.Resolve(ref, 10)
	if result.Status != datago.ResolveFound {
		if result.Status == datago.ResolveAmbiguous {
			return a.mapError(errAmbiguous{ref: ref, candidates: result.Candidates}, jsonOut)
		}
		return a.mapError(errNotFound{ref}, jsonOut)
	}
	op, ok := result.Spec.Operation(operation)
	if !ok {
		return a.fail(exitNotFound, "unknown operation %q for %s", operation, result.Spec.ID)
	}
	host, err := endpointHostForCredential(op.Endpoint)
	if err != nil {
		return a.fail(exitUsage, "audit endpoint: %v", err)
	}
	group, credential, present, err := a.credentialForOperation(op)
	if err != nil {
		return a.fail(exitRequest, "credential inventory: %v", err)
	}
	receipt := newCredentialAuditReceipt(result.Spec, op, host, group, credential, present, timeout)
	if !present {
		receipt.Assessment.Outcome = "not_observed"
		receipt.Assessment.Category = "missing_auth"
		receipt.Assessment.ReasonCode = "credential_not_configured"
		return a.writeCredentialAudit(receipt, output, jsonOut, exitAuth)
	}
	trust := a.localRegistryTrust()
	if !trust.ExecutionAllowed {
		receipt.Assessment.Outcome = "not_observed"
		receipt.Assessment.Category = "unknown"
		receipt.Assessment.ReasonCode = "registry_execution_blocked"
		return a.writeCredentialAudit(receipt, output, jsonOut, exitRequest)
	}

	candidate := datago.VerificationCandidate{Spec: result.Spec, Operation: op, EndpointHost: host, DependencyClass: datago.OperationDependencyClass(result.Spec, op)}
	candidate.Params, candidate.MissingParams = datago.VerificationParams(result.Spec, op)
	verification := a.auditVerification(candidate, credential, timeout)
	receipt.Execution.Attempted = verification.Status != "skipped"
	if receipt.Execution.Attempted {
		receipt.Execution.RequestBudget = 1
	}
	receipt.Credential.LiveVerified = verification.Status == "verified"
	receipt.Assessment.Outcome, receipt.Assessment.Category, receipt.Assessment.ReasonCode = classifyCredentialAudit(verification)
	receipt.Assessment.HTTPStatus = verification.HTTPStatus
	code := exitRequest
	if receipt.Assessment.Category == "verified" {
		code = exitOK
	} else if receipt.Assessment.Category == "missing_auth" {
		code = exitAuth
	}
	return a.writeCredentialAudit(receipt, output, jsonOut, code)
}

func newCredentialAuditReceipt(spec datago.Spec, op datago.Operation, host string, group credentialGroup, credential providers.Credential, present bool, timeout time.Duration) credentialAuditReceipt {
	var receipt credentialAuditReceipt
	receipt.SchemaVersion = "datapan.credential-audit.v1"
	receipt.ObservedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	receipt.Operation.DatasetID = spec.ID
	receipt.Operation.Operation = op.Name
	receipt.Operation.Provider = spec.Provider
	receipt.Operation.CredentialGroup = group.ID
	receipt.Operation.EndpointHost = host
	receipt.Credential.Present = present
	receipt.Credential.SelectedEnvVar = credential.Name
	receipt.Credential.AcceptedEnvVars = append([]string(nil), group.EnvNames...)
	receipt.Execution.OptIn = true
	receipt.Execution.TimeoutMS = timeout.Milliseconds()
	receipt.Redaction = struct {
		CredentialValuesPresent bool `json:"credential_values_present"`
		CredentialHashesPresent bool `json:"credential_hashes_present"`
		RequestURLsPresent      bool `json:"request_urls_present"`
		ResponseBodiesPresent   bool `json:"response_bodies_present"`
	}{}
	return receipt
}

func (a app) auditVerification(candidate datago.VerificationCandidate, credential providers.Credential, timeout time.Duration) datago.VerificationResult {
	registry, err := providers.DefaultRegistry()
	if err != nil {
		return datago.VerificationResult{Status: "failed", Reason: "adapter_registry_unavailable"}
	}
	if adapter, ok := registry.MatchHost(candidate.EndpointHost); ok {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		result := adapter.Verify(ctx, providers.VerificationRequest{Spec: candidate.Spec, Operation: candidate.Operation, Params: candidate.Params, MissingParams: candidate.MissingParams, Credential: credential, HTTP: a.http, VerifiedAt: time.Now().UTC().Format(time.RFC3339)})
		result.Reason = redactCredentialText(result.Reason, credential.Value)
		return result
	}
	if len(candidate.MissingParams) > 0 {
		return datago.VerificationResult{Status: "skipped", Reason: "input_required"}
	}
	plan, _, err := a.requestPlanForOperation(candidate.Spec, candidate.Operation, candidate.Params)
	if err != nil {
		return datago.VerificationResult{Status: "failed", Reason: safeExecutionError(err, requestPlan{Credential: credential})}
	}
	plan.Timeout = timeout
	envelope, err := a.execute(plan)
	if err != nil {
		return datago.VerificationResult{Status: "failed", Reason: safeExecutionError(err, plan), URL: plan.RedactedURL}
	}
	status := "verified"
	if !envelope.OK {
		status = "failed"
	}
	return datago.VerificationResult{Status: status, Reason: redactCredentialText(envelope.Message, credential.Value), HTTPStatus: envelope.StatusCode, SemanticStatus: envelope.SemanticStatus, ProviderStatus: envelope.ProviderStatus, URL: envelope.URL}
}

func classifyCredentialAudit(result datago.VerificationResult) (outcome, category, reason string) {
	reason = strings.TrimSpace(result.Reason)
	lower := strings.ToLower(strings.Join([]string{reason, result.SemanticStatus}, " "))
	switch {
	case result.Status == "verified":
		return "verified", "verified", "provider_response_verified"
	case result.Status == "skipped" && (strings.Contains(lower, "missing_auth") || strings.Contains(lower, "credential")):
		return "not_observed", "missing_auth", firstNonEmpty(reason, "credential_not_configured")
	case result.Status == "skipped" || strings.Contains(lower, "missing_required") || strings.Contains(lower, "input_required") || strings.Contains(lower, "missing params"):
		return "not_observed", "input_required", firstNonEmpty(reason, "input_required")
	case strings.Contains(lower, "approval") || strings.Contains(lower, "not registered") || strings.Contains(lower, "not_registered"):
		return "observed", "approval_required", firstNonEmpty(reason, "provider_approval_required")
	case result.HTTPStatus == 401 || result.HTTPStatus == 403 || strings.Contains(lower, "credential") || strings.Contains(lower, "unauthorized"):
		return "observed", "credential_invalid", firstNonEmpty(reason, "provider_rejected_credential")
	case result.HTTPStatus == 429 || strings.Contains(lower, "rate") || strings.Contains(lower, "quota"):
		return "observed", "rate_limited", firstNonEmpty(reason, "provider_rate_limited")
	case result.HTTPStatus >= 500 || strings.Contains(lower, "timeout") || strings.Contains(lower, "network") || strings.Contains(lower, "transport"):
		return "observed", "provider_unavailable", firstNonEmpty(reason, "provider_unavailable")
	default:
		return "observed", "unknown", firstNonEmpty(reason, "unclassified_provider_response")
	}
}

func (a app) writeCredentialAudit(receipt credentialAuditReceipt, output string, jsonOut bool, exitCode int) int {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	data = append(data, '\n')
	if err := writeOutputAtomic(output, data, a.stdout); err != nil {
		return a.fail(exitRequest, "write audit receipt: %v", err)
	}
	if jsonOut {
		if _, err := a.stdout.Write(data); err != nil {
			return exitRequest
		}
	} else {
		fmt.Fprintf(a.stdout, "Credential audit: %s (%s) receipt=%s\n", receipt.Assessment.Category, receipt.Assessment.ReasonCode, output)
	}
	return exitCode
}
