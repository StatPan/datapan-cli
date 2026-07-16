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
	Code               string                     `json:"code"`
	ResponsibleParty   string                     `json:"responsible_party"`
	EvidenceState      string                     `json:"evidence_state"`
	EvidenceAuthority  string                     `json:"evidence_authority"`
	ObservedAt         string                     `json:"observed_at,omitempty"`
	Scope              []string                   `json:"scope"`
	Evidence           []string                   `json:"evidence"`
	RecommendedActions []string                   `json:"recommended_actions"`
	ProhibitedActions  []string                   `json:"prohibited_actions,omitempty"`
	Timing             *localDiagnosticTiming     `json:"timing,omitempty"`
	ConsumerHandoff    *diagnosticConsumerHandoff `json:"consumer_handoff,omitempty"`
}

type localDiagnosticTiming struct {
	CallAttemptStartedAt string `json:"call_attempt_started_at,omitempty"`
	CallAttemptEndedAt   string `json:"call_attempt_ended_at,omitempty"`
	DiagnosisComputedAt  string `json:"diagnosis_computed_at,omitempty"`
	CallAttemptElapsedMS *int64 `json:"call_attempt_elapsed_ms,omitempty"`
}

type localDiagnosticEvidence struct {
	Failure   executionFailure
	Envelope  *datago.ResponseEnvelope
	Plan      *requestPlan
	StartedAt time.Time
	EndedAt   time.Time
}

func attachLocalDiagnosis(evidence localDiagnosticEvidence) executionFailure {
	return attachLocalDiagnosisWithClock(evidence, time.Now)
}

func attachLocalDiagnosisWithClock(evidence localDiagnosticEvidence, now func() time.Time) executionFailure {
	failure := evidence.Failure
	diagnosis := normalizeLocalDiagnosis(evidence)
	// Capture computation after classification. EndedAt is the provider call
	// boundary and must not be relabeled as diagnosis computation time.
	diagnosis.Timing = localDiagnosticAttemptTiming(evidence.StartedAt, evidence.EndedAt, now().UTC())
	diagnosis.ConsumerHandoff = reviewedDiagnosticHandoff(evidence, diagnosis)
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
	}

	if routed := failure.RegistryRouting; routed != nil {
		out.EvidenceAuthority = "registry_and_provider"
		out.Evidence = append(out.Evidence, "registry_rule:matched")
		switch routed.Classification {
		case "credential":
			if registryRoutingHasBoundedProviderEvidence(routed) {
				setLocalDiagnosis(&out, "credential_invalid", "unknown", "inferred", "check_local_credential", "check_dataset_approval", "check_provider_credential_registration")
			}
		case "approval":
			if registryRoutingHasBoundedProviderEvidence(routed) {
				setLocalDiagnosis(&out, "approval_required", "user", "observed", "check_dataset_approval", "start_or_inspect_usage_application")
			}
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
		case envelope.StatusCode == http.StatusUnauthorized || envelope.StatusCode == http.StatusForbidden:
			out.Code = "unknown"
			out.ResponsibleParty = "unknown"
			out.EvidenceState = "unknown"
			out.RecommendedActions = []string{"check_local_credential", "check_dataset_approval", "check_provider_status"}
			return normalizeDiagnosticSlices(out)
		}
		if out.Code != "unknown" {
			return normalizeDiagnosticSlices(out)
		}
	}

	switch failure.Category {
	case "authentication":
		out.RecommendedActions = []string{"check_local_credential", "check_dataset_approval", "check_provider_status"}
	case "approval":
		out.RecommendedActions = []string{"check_local_credential", "check_dataset_approval", "check_provider_status"}
	case "input":
		setLocalDiagnosis(&out, "invalid_input", "user", "inferred", "inspect_required_parameters", "retry_with_valid_input")
	case "adapter":
		setLocalDiagnosis(&out, "contract_drift", "datapan", "inferred", "inspect_provider_contract", "report_redacted_contract_evidence")
	}
	return normalizeDiagnosticSlices(out)
}

func registryRoutingHasBoundedProviderEvidence(routing *registryFailureRouting) bool {
	if routing == nil {
		return false
	}
	switch routing.MatchKind {
	case "field_equals", "field_contains", "message_contains":
		return true
	default:
		return false
	}
}

func setLocalDiagnosis(out *localDiagnosticOutcome, code, responsible, evidenceState string, actions ...string) {
	out.Code = code
	out.ResponsibleParty = responsible
	out.EvidenceState = evidenceState
	out.RecommendedActions = append([]string(nil), actions...)
}

func localDiagnosticAttemptTiming(startedAt, endedAt, computedAt time.Time) *localDiagnosticTiming {
	if startedAt.IsZero() || endedAt.IsZero() || endedAt.Before(startedAt) || computedAt.IsZero() || computedAt.Before(endedAt) {
		return nil
	}
	elapsed := endedAt.Sub(startedAt).Milliseconds()
	return &localDiagnosticTiming{
		CallAttemptStartedAt: startedAt.UTC().Format(time.RFC3339Nano),
		CallAttemptEndedAt:   endedAt.UTC().Format(time.RFC3339Nano),
		DiagnosisComputedAt:  computedAt.UTC().Format(time.RFC3339Nano),
		CallAttemptElapsedMS: &elapsed,
	}
}

func localCallSucceededDiagnosis(envelope datago.ResponseEnvelope, startedAt, at time.Time) localDiagnosticOutcome {
	return localCallSucceededDiagnosisWithClock(envelope, startedAt, at, time.Now)
}

func localCallSucceededDiagnosisWithClock(envelope datago.ResponseEnvelope, startedAt, at time.Time, now func() time.Time) localDiagnosticOutcome {
	out := normalizeDiagnosticSlices(localDiagnosticOutcome{
		Code:               "call_succeeded",
		ResponsibleParty:   "none",
		EvidenceState:      "observed",
		EvidenceAuthority:  "provider_response",
		ObservedAt:         diagnosticObservedAt(at),
		Scope:              []string{"transport", "provider_response", "declared_semantic_status"},
		Evidence:           []string{fmt.Sprintf("http_status:%d", envelope.StatusCode), "semantic_status:" + safeDiagnosticToken(envelope.SemanticStatus)},
		RecommendedActions: []string{"export_reusable_result"},
	})
	out.Timing = localDiagnosticAttemptTiming(startedAt, at, now().UTC())
	return out
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
