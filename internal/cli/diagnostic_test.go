package cli

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

func TestNormalizeLocalDiagnosisEvidenceBoundaries(t *testing.T) {
	observedAt := time.Date(2026, 7, 16, 3, 4, 5, 0, time.UTC)
	approvedAt := observedAt.Add(-2 * time.Hour)
	tests := []struct {
		name        string
		evidence    localDiagnosticEvidence
		code        string
		state       string
		responsible string
		prohibited  []string
	}{
		{
			name: "approval required without authoritative approval receipt",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "approval", Reason: "provider_access_not_approved"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusForbidden}},
			code: "approval_required", state: "inferred", responsible: "user",
		},
		{
			name: "approval propagation requires confirmed state and timestamp",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "approval", Reason: "provider_access_not_approved"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusForbidden},
				Approval: &localApprovalEvidence{State: "approved", ConfirmedAt: approvedAt, ObservedAt: observedAt}},
			code: "approval_propagating", state: "inferred", responsible: "provider", prohibited: []string{"avoid_key_reissue"},
		},
		{
			name: "credential rejection is not propagation",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "authentication", Reason: "provider_rejected_credential"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusUnauthorized}},
			code: "credential_invalid", state: "inferred", responsible: "unknown",
		},
		{
			name: "registry credential classification remains inferred",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "authentication", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "credential", RuleID: "credential-rule",
			}}},
			code: "credential_invalid", state: "inferred", responsible: "unknown",
		},
		{
			name: "invalid input",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "input", Reason: "provider_rejected_input"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusBadRequest}},
			code: "invalid_input", state: "inferred", responsible: "user",
		},
		{
			name: "rate limit",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusTooManyRequests}},
			code: "rate_limited", state: "observed", responsible: "provider",
		},
		{
			name: "single provider 5xx is provisional outage evidence",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusServiceUnavailable}},
			code: "provider_outage", state: "inferred", responsible: "provider",
		},
		{
			name: "registry upstream outage classification still needs corroboration",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "upstream_outage", RuleID: "outage-rule",
			}}},
			code: "provider_outage", state: "inferred", responsible: "provider",
		},
		{
			name: "unexpected response contract",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "adapter", Reason: "unexpected_provider_response_shape"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusOK, SemanticStatus: "html_response"}},
			code: "contract_drift", state: "inferred", responsible: "datapan",
		},
		{
			name:     "transport failure alone stays unknown",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_transport_temporarily_failed"}},
			code:     "unknown", state: "unknown", responsible: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLocalDiagnosis(tt.evidence)
			if got.Code != tt.code || got.EvidenceState != tt.state || got.ResponsibleParty != tt.responsible {
				t.Fatalf("diagnosis=%+v", got)
			}
			if len(got.ProhibitedActions) != len(tt.prohibited) || (len(tt.prohibited) > 0 && !reflect.DeepEqual(got.ProhibitedActions, tt.prohibited)) {
				t.Fatalf("prohibited=%v want=%v", got.ProhibitedActions, tt.prohibited)
			}
		})
	}
}

func TestApprovalPropagationRejectsNonAuthoritativeStates(t *testing.T) {
	observedAt := time.Date(2026, 7, 16, 3, 4, 5, 0, time.UTC)
	base := localDiagnosticEvidence{
		Failure:  executionFailure{Category: "approval", Reason: "provider_access_not_approved"},
		Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusForbidden},
	}
	tests := []localApprovalEvidence{
		{State: "access_requested_not_confirmed", ConfirmedAt: observedAt.Add(-time.Hour), ObservedAt: observedAt},
		{State: "approved", ObservedAt: observedAt},
		{State: "approved", ConfirmedAt: observedAt.Add(time.Hour), ObservedAt: observedAt},
	}
	for _, approval := range tests {
		base.Approval = &approval
		if got := normalizeLocalDiagnosis(base); got.Code == "approval_propagating" {
			t.Fatalf("unsupported evidence inferred propagation: %+v => %+v", approval, got)
		}
	}
}

func TestNormalizeLocalDiagnosisHealthDoesNotInventQualityPolicy(t *testing.T) {
	observedAt := "2026-07-16T03:04:05Z"
	tests := []struct {
		name    string
		receipt healthProbeReceipt
		code    string
	}{
		{name: "empty success stays unknown without presence policy", receipt: healthProbeReceipt{ObservedAt: observedAt, Observation: healthProbeObservation{HTTPStatus: 200, DataPresence: "empty", FreshnessStatus: "not_observed"}}, code: "unknown"},
		{name: "stale success stays unknown without freshness policy result", receipt: healthProbeReceipt{ObservedAt: observedAt, Observation: healthProbeObservation{HTTPStatus: 200, DataPresence: "present", FreshnessStatus: "stale"}}, code: "unknown"},
		{name: "health provider failure", receipt: healthProbeReceipt{ObservedAt: observedAt, Observation: healthProbeObservation{HTTPStatus: 503}, Assessment: healthProbeAssessment{Category: "provider_failure"}}, code: "provider_outage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLocalDiagnosis(localDiagnosticEvidence{Failure: executionFailure{Reason: "health_observation"}, Health: &tt.receipt})
			if got.Code != tt.code || got.EvidenceAuthority != "datapan_health" || got.ObservedAt != observedAt {
				t.Fatalf("diagnosis=%+v", got)
			}
		})
	}
}

func TestNormalizeLocalDiagnosisRequiresQualityPolicyAndResultIdentity(t *testing.T) {
	observedAt := time.Date(2026, 7, 16, 3, 4, 5, 0, time.UTC)
	base := localDiagnosticEvidence{SubjectKey: "operation-1", Failure: executionFailure{Reason: "health_observation"}}
	for _, quality := range []localQualityEvidence{
		{Kind: "empty", SubjectKey: "operation-1", Authority: "registry", ObservedAt: observedAt},
		{Kind: "empty", SubjectKey: "operation-1", PolicyID: "presence.v1", Authority: "registry", ObservedAt: observedAt},
		{Kind: "empty", SubjectKey: "operation-1", PolicyID: "presence.v1", ResultID: "result-1", ObservedAt: observedAt},
		{Kind: "empty", SubjectKey: "operation-2", PolicyID: "presence.v1", ResultID: "result-1", Authority: "registry", ObservedAt: observedAt},
	} {
		base.Quality = &quality
		if got := normalizeLocalDiagnosis(base); got.Code != "unknown" {
			t.Fatalf("incomplete policy evidence produced %s: %+v", got.Code, quality)
		}
	}
	base.Quality = &localQualityEvidence{Kind: "empty", SubjectKey: "operation-1", PolicyID: "presence.v1", ResultID: "result-1", Authority: "registry", ObservedAt: observedAt}
	if got := normalizeLocalDiagnosis(base); got.Code != "semantic_quality" || got.EvidenceState != "observed" {
		t.Fatalf("quality diagnosis=%+v", got)
	}
	base.Quality.Kind = "stale"
	if got := normalizeLocalDiagnosis(base); got.Code != "stale_data" || got.EvidenceState != "observed" {
		t.Fatalf("stale diagnosis=%+v", got)
	}
}

func TestAvoidKeyReissueRequiresCorroboratedIncident(t *testing.T) {
	observedAt := time.Date(2026, 7, 16, 3, 4, 5, 0, time.UTC)
	base := localDiagnosticEvidence{
		SubjectKey: "operation-1",
		Failure:    executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
		Envelope:   &datago.ResponseEnvelope{StatusCode: http.StatusServiceUnavailable},
	}
	if got := normalizeLocalDiagnosis(base); len(got.ProhibitedActions) != 0 {
		t.Fatalf("single 5xx prohibited actions=%v", got.ProhibitedActions)
	}
	base.Incident = &localIncidentEvidence{State: "confirmed", SubjectKey: "operation-2", Authority: "datapan_health", ObservedAt: observedAt}
	if got := normalizeLocalDiagnosis(base); len(got.ProhibitedActions) != 0 {
		t.Fatalf("mismatched incident prohibited actions=%v", got.ProhibitedActions)
	}
	base.Incident.SubjectKey = "operation-1"
	got := normalizeLocalDiagnosis(base)
	if got.Code != "provider_outage" || got.EvidenceState != "observed" || !reflect.DeepEqual(got.ProhibitedActions, []string{"avoid_key_reissue"}) {
		t.Fatalf("corroborated incident=%+v", got)
	}
}

func TestLocalDiagnosticTimingAndReadyScope(t *testing.T) {
	startedAt := time.Date(2026, 7, 16, 3, 4, 5, 123000000, time.FixedZone("KST", 9*60*60))
	endedAt := startedAt.Add(1750 * time.Millisecond)
	timing := diagnosisTiming(startedAt, endedAt)
	if timing == nil || timing.CallAttemptElapsedMS == nil || *timing.CallAttemptElapsedMS != 1750 {
		t.Fatalf("timing=%+v", timing)
	}
	if timing.CallAttemptStartedAt != "2026-07-15T18:04:05.123Z" || timing.DiagnosisComputedAt != "2026-07-15T18:04:06.873Z" {
		t.Fatalf("UTC timing=%+v", timing)
	}
	ready := localReadyDiagnosis(datago.ResponseEnvelope{StatusCode: 200, SemanticStatus: "json_response"}, startedAt, endedAt)
	if ready.Code != "ready" || ready.EvidenceState != "observed" || ready.Timing == nil || ready.Timing.CallAttemptElapsedMS == nil {
		t.Fatalf("ready=%+v", ready)
	}
	wantScope := []string{"transport", "provider_response", "declared_semantic_status"}
	if !reflect.DeepEqual(ready.Scope, wantScope) {
		t.Fatalf("scope=%v want=%v", ready.Scope, wantScope)
	}
}

func TestLocalDiagnosisDoesNotCopyProviderMessagesOrCredentials(t *testing.T) {
	credential := "secret-value"
	encoded := "secret%2Dvalue"
	envelope := datago.ResponseEnvelope{
		StatusCode: http.StatusServiceUnavailable,
		Message:    "reflected " + credential + " " + encoded,
		Body:       `{"authorization":"Bearer secret-value"}`,
		ProviderStatus: &datago.ProviderStatus{
			Message: "provider reflected " + credential,
		},
	}
	got := normalizeLocalDiagnosis(localDiagnosticEvidence{
		Failure:  executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
		Envelope: &envelope,
	})
	encodedOutcome := strings.Join(append(append(got.Evidence, got.RecommendedActions...), got.ProhibitedActions...), " ")
	for _, forbidden := range []string{credential, encoded, "Bearer", "authorization"} {
		if strings.Contains(encodedOutcome, forbidden) {
			t.Fatalf("diagnostic copied sensitive provider content %q: %+v", forbidden, got)
		}
	}
	unknown := normalizeLocalDiagnosis(localDiagnosticEvidence{Failure: executionFailure{Reason: "secret-value"}})
	if strings.Contains(strings.Join(unknown.Evidence, " "), credential) {
		t.Fatalf("arbitrary failure reason leaked into evidence: %+v", unknown)
	}
}

func TestGetFailureAddsDiagnosticWithoutChangingExitContract(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
	)
	if code != exitRequest || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"category": "external_provider"`, `"code": "provider_outage"`, `"evidence_state": "inferred"`,
		`"scope": [`, `"provider_call"`, `"call_attempt_started_at":`, `"diagnosis_computed_at":`, `"call_attempt_elapsed_ms":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"secret-value", "avoid_key_reissue", "time_to_diagnosis_ms", "time_to_first_success_ms"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
		}
	}
}

func TestGetSuccessReportsBoundedReadyScopeNotJourneyMetric(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
	)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"code": "ready"`, `"transport"`, `"provider_response"`, `"declared_semantic_status"`, `"call_attempt_elapsed_ms":`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"secret-value", "time_to_diagnosis_ms", "time_to_first_success_ms", "first_success_at"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
		}
	}
}
