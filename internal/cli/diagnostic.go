package cli

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

// localDiagnosticOutcome is the CLI-owned, additive projection of evidence the
// CLI already has. It is deliberately not a copy of the Registry diagnostic
// envelope. The reviewed Registry contract remains the authority for the
// cross-product schema and is bound separately.
type localDiagnosticOutcome struct {
	Code               string                 `json:"code"`
	ResponsibleParty   string                 `json:"responsible_party"`
	EvidenceState      string                 `json:"evidence_state"`
	EvidenceAuthority  string                 `json:"evidence_authority"`
	ObservedAt         string                 `json:"observed_at,omitempty"`
	Scope              []string               `json:"scope"`
	Evidence           []string               `json:"evidence"`
	RecommendedActions []string               `json:"recommended_actions"`
	ProhibitedActions  []string               `json:"prohibited_actions,omitempty"`
	Timing             *localDiagnosticTiming `json:"timing,omitempty"`
}

type localDiagnosticTiming struct {
	CallAttemptStartedAt string `json:"call_attempt_started_at,omitempty"`
	DiagnosisComputedAt  string `json:"diagnosis_computed_at,omitempty"`
	CallAttemptElapsedMS *int64 `json:"call_attempt_elapsed_ms,omitempty"`
}

// localApprovalEvidence must come from an authoritative approval view. A
// submitted or requested state is intentionally insufficient to infer
// approval propagation.
type localApprovalEvidence struct {
	State       string
	ConfirmedAt time.Time
	ObservedAt  time.Time
}

type localDiagnosticEvidence struct {
	SubjectKey string
	Failure    executionFailure
	Envelope   *datago.ResponseEnvelope
	Health     *healthProbeReceipt
	Approval   *localApprovalEvidence
	Incident   *localIncidentEvidence
	Quality    *localQualityEvidence
	StartedAt  time.Time
	EndedAt    time.Time
}

type localIncidentEvidence struct {
	State      string
	Authority  string
	SubjectKey string
	ObservedAt time.Time
}

type localQualityEvidence struct {
	Kind       string
	PolicyID   string
	ResultID   string
	Authority  string
	SubjectKey string
	ObservedAt time.Time
}

func attachLocalDiagnosis(evidence localDiagnosticEvidence) executionFailure {
	failure := evidence.Failure
	diagnosis := normalizeLocalDiagnosis(evidence)
	failure.Diagnostic = &diagnosis
	return failure
}

func normalizeLocalDiagnosis(evidence localDiagnosticEvidence) localDiagnosticOutcome {
	failure := evidence.Failure
	out := localDiagnosticOutcome{
		Code:               "unknown",
		ResponsibleParty:   "unknown",
		EvidenceState:      "unknown",
		EvidenceAuthority:  "cli",
		ObservedAt:         diagnosticObservedAt(evidence.EndedAt),
		Scope:              []string{"provider_call"},
		Evidence:           stableFailureReasonEvidence(failure.Reason),
		RecommendedActions: []string{"inspect_diagnostic_evidence"},
		Timing:             diagnosisTiming(evidence.StartedAt, evidence.EndedAt),
	}

	if approvalPropagationSupported(evidence) {
		out.Code = "approval_propagating"
		out.ResponsibleParty = "provider"
		out.EvidenceState = "inferred"
		out.EvidenceAuthority = "data.go.kr_portal"
		out.Scope = []string{"usage_approval", "credential_activation"}
		out.Evidence = append(out.Evidence, "portal_approval:confirmed", "approval_timestamp:present")
		out.RecommendedActions = []string{"wait_for_provider_credential_sync", "retry_after_provider_propagation_window"}
		out.ProhibitedActions = []string{"avoid_key_reissue"}
		return normalizeDiagnosticSlices(out)
	}
	if incidentEvidenceSupported(evidence.SubjectKey, evidence.Incident) {
		out.Code = "provider_outage"
		out.ResponsibleParty = "provider"
		out.EvidenceState = "observed"
		out.EvidenceAuthority = evidence.Incident.Authority
		out.ObservedAt = diagnosticObservedAt(evidence.Incident.ObservedAt)
		out.Scope = []string{"provider_incident"}
		out.Evidence = append(out.Evidence, "provider_incident:confirmed")
		out.RecommendedActions = []string{"check_provider_status", "retry_after_incident_recovery"}
		out.ProhibitedActions = []string{"avoid_key_reissue"}
		return normalizeDiagnosticSlices(out)
	}
	if qualityEvidenceSupported(evidence.SubjectKey, evidence.Quality) {
		out.ResponsibleParty = "provider"
		out.EvidenceState = "observed"
		out.EvidenceAuthority = evidence.Quality.Authority
		out.ObservedAt = diagnosticObservedAt(evidence.Quality.ObservedAt)
		out.Scope = []string{"registry_quality_policy"}
		out.Evidence = append(out.Evidence, "quality_policy:present", "quality_result:present")
		switch evidence.Quality.Kind {
		case "stale":
			out.Code = "stale_data"
			out.RecommendedActions = []string{"inspect_reference_date", "inspect_update_schedule"}
		case "empty", "missing_expected_entity":
			out.Code = "semantic_quality"
			out.RecommendedActions = []string{"inspect_expected_data_presence", "inspect_reference_date"}
		}
		return normalizeDiagnosticSlices(out)
	}

	if routed := failure.RegistryRouting; routed != nil {
		out.EvidenceAuthority = "registry_and_provider"
		out.Evidence = append(out.Evidence, "registry_rule:matched")
		switch routed.Classification {
		case "credential":
			setLocalDiagnosis(&out, "credential_invalid", "unknown", "inferred", "check_local_credential", "check_dataset_approval", "check_provider_credential_registration")
		case "approval":
			setLocalDiagnosis(&out, "approval_required", "user", "observed", "check_dataset_approval", "start_or_inspect_usage_application")
		case "bad_request":
			setLocalDiagnosis(&out, "invalid_input", "user", "observed", "inspect_required_parameters", "retry_with_valid_input")
		case "rate_limit":
			setLocalDiagnosis(&out, "rate_limited", "provider", "observed", "wait_for_rate_limit_reset", "retry_with_backoff")
		case "upstream_outage", "maintenance":
			setLocalDiagnosis(&out, "provider_outage", "provider", "inferred", "check_provider_status", "retry_with_backoff")
		case "parser", "adapter", "provider_contract":
			setLocalDiagnosis(&out, "contract_drift", "datapan", "observed", "inspect_provider_contract", "report_redacted_contract_evidence")
		}
		if out.Code != "unknown" {
			return normalizeDiagnosticSlices(out)
		}
	}

	if receipt := evidence.Health; receipt != nil {
		out.EvidenceAuthority = "datapan_health"
		out.Scope = []string{"health_probe"}
		out.ObservedAt = receipt.ObservedAt
		out.Evidence = append(out.Evidence, "health_receipt:present")
		switch {
		case receipt.Observation.HTTPStatus >= 500:
			setLocalDiagnosis(&out, "provider_outage", "provider", "inferred", "check_provider_status", "retry_with_backoff")
		case receipt.Observation.HTTPStatus == http.StatusTooManyRequests || receipt.Assessment.Category == "rate_limited":
			setLocalDiagnosis(&out, "rate_limited", "provider", "observed", "wait_for_rate_limit_reset", "retry_with_backoff")
		case receipt.Assessment.Category == "schema_drift" || receipt.Assessment.Category == "semantic_failure":
			setLocalDiagnosis(&out, "contract_drift", "datapan", "inferred", "inspect_provider_contract", "report_redacted_contract_evidence")
		}
		if out.Code != "unknown" {
			return normalizeDiagnosticSlices(out)
		}
	}

	if envelope := evidence.Envelope; envelope != nil {
		out.EvidenceAuthority = "provider_response"
		if envelope.StatusCode != 0 {
			out.Evidence = append(out.Evidence, fmt.Sprintf("http_status:%d", envelope.StatusCode))
		}
		switch {
		case envelope.StatusCode == http.StatusTooManyRequests:
			setLocalDiagnosis(&out, "rate_limited", "provider", "observed", "wait_for_rate_limit_reset", "retry_with_backoff")
		case envelope.StatusCode >= 500:
			setLocalDiagnosis(&out, "provider_outage", "provider", "inferred", "check_provider_status", "retry_with_backoff")
		}
		if out.Code != "unknown" {
			return normalizeDiagnosticSlices(out)
		}
	}

	switch failure.Category {
	case "authentication":
		setLocalDiagnosis(&out, "credential_invalid", "unknown", "inferred", "check_local_credential", "check_dataset_approval", "check_provider_credential_registration")
	case "approval":
		setLocalDiagnosis(&out, "approval_required", "user", "inferred", "check_dataset_approval", "start_or_inspect_usage_application")
	case "input":
		setLocalDiagnosis(&out, "invalid_input", "user", "inferred", "inspect_required_parameters", "retry_with_valid_input")
	case "adapter":
		setLocalDiagnosis(&out, "contract_drift", "datapan", "inferred", "inspect_provider_contract", "report_redacted_contract_evidence")
	}
	return normalizeDiagnosticSlices(out)
}

func setLocalDiagnosis(out *localDiagnosticOutcome, code, responsible, evidenceState string, actions ...string) {
	out.Code = code
	out.ResponsibleParty = responsible
	out.EvidenceState = evidenceState
	out.RecommendedActions = append([]string(nil), actions...)
}

func approvalPropagationSupported(evidence localDiagnosticEvidence) bool {
	failure, approval := evidence.Failure, evidence.Approval
	if approval == nil || !strings.EqualFold(strings.TrimSpace(approval.State), "approved") || approval.ConfirmedAt.IsZero() || approval.ObservedAt.IsZero() {
		return false
	}
	if approval.ConfirmedAt.After(approval.ObservedAt) {
		return false
	}
	if evidence.Envelope == nil || (evidence.Envelope.StatusCode != http.StatusUnauthorized && evidence.Envelope.StatusCode != http.StatusForbidden) {
		return false
	}
	return failure.Category == "authentication" || failure.Category == "approval"
}

func incidentEvidenceSupported(subjectKey string, incident *localIncidentEvidence) bool {
	return incident != nil && strings.EqualFold(strings.TrimSpace(incident.State), "confirmed") &&
		strings.TrimSpace(subjectKey) != "" && subjectKey == incident.SubjectKey &&
		strings.TrimSpace(incident.Authority) != "" && !incident.ObservedAt.IsZero()
}

func qualityEvidenceSupported(subjectKey string, quality *localQualityEvidence) bool {
	if quality == nil || strings.TrimSpace(quality.PolicyID) == "" || strings.TrimSpace(quality.ResultID) == "" ||
		strings.TrimSpace(subjectKey) == "" || subjectKey != quality.SubjectKey ||
		strings.TrimSpace(quality.Authority) == "" || quality.ObservedAt.IsZero() {
		return false
	}
	switch quality.Kind {
	case "stale", "empty", "missing_expected_entity":
		return true
	default:
		return false
	}
}

func diagnosisTiming(startedAt, endedAt time.Time) *localDiagnosticTiming {
	if startedAt.IsZero() || endedAt.IsZero() || endedAt.Before(startedAt) {
		return nil
	}
	elapsed := endedAt.Sub(startedAt).Milliseconds()
	return &localDiagnosticTiming{
		CallAttemptStartedAt: startedAt.UTC().Format(time.RFC3339Nano),
		DiagnosisComputedAt:  endedAt.UTC().Format(time.RFC3339Nano),
		CallAttemptElapsedMS: &elapsed,
	}
}

func localReadyDiagnosis(envelope datago.ResponseEnvelope, startedAt, at time.Time) localDiagnosticOutcome {
	out := localDiagnosticOutcome{
		Code:               "ready",
		ResponsibleParty:   "none",
		EvidenceState:      "observed",
		EvidenceAuthority:  "provider_response",
		ObservedAt:         diagnosticObservedAt(at),
		Scope:              []string{"transport", "provider_response", "declared_semantic_status"},
		Evidence:           []string{fmt.Sprintf("http_status:%d", envelope.StatusCode), "semantic_status:" + safeDiagnosticToken(envelope.SemanticStatus)},
		RecommendedActions: []string{"export_reusable_result"},
		Timing:             diagnosisTiming(startedAt, at),
	}
	return normalizeDiagnosticSlices(out)
}

func diagnosticObservedAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func normalizeDiagnosticSlices(out localDiagnosticOutcome) localDiagnosticOutcome {
	out.Evidence = uniqueStrings(out.Evidence)
	out.Scope = uniqueStrings(out.Scope)
	out.RecommendedActions = uniqueStrings(out.RecommendedActions)
	out.ProhibitedActions = uniqueStrings(out.ProhibitedActions)
	return out
}

func safeDiagnosticToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func stableFailureReasonEvidence(reason string) []string {
	stable := map[string]bool{
		"provider_rejected_credential":          true,
		"provider_access_not_approved":          true,
		"provider_rejected_input":               true,
		"unexpected_provider_response_shape":    true,
		"provider_temporarily_unavailable":      true,
		"provider_rejected_request":             true,
		"provider_transport_failed":             true,
		"provider_transport_temporarily_failed": true,
		"registry_compatibility_blocked":        true,
		"verification_evidence_stale":           true,
		"verification_evidence_expired":         true,
	}
	if stable[reason] {
		return []string{"failure_reason:" + reason}
	}
	return nil
}
