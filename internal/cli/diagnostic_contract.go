package cli

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const (
	diagnosticSchemaSHA256  = "da254b40947462347fcda90fdd7686b6632c76943b438f2046a28f079f33e403"
	diagnosticMappingSHA256 = "da55d52d2ee1f197969ac63a1d5ab5b98e3b88fd65f90d6a48800d2e3c522d33"
)

type diagnosticConsumerHandoff struct {
	Contract     diagnosticContractRef     `json:"contract"`
	Subject      diagnosticSubject         `json:"subject"`
	Result       diagnosticMappingResult   `json:"result"`
	Capabilities diagnosticCapabilities    `json:"capabilities"`
	Metrics      *diagnosticJourneyMetrics `json:"metrics,omitempty"`
}

type diagnosticJourneyMetrics struct {
	TimeToDiagnosisMS    *int64 `json:"time_to_diagnosis_ms,omitempty"`
	TimeToFirstSuccessMS *int64 `json:"time_to_first_success_ms,omitempty"`
}

type diagnosticJourneyClock struct {
	StartedAt   time.Time
	DiagnosedAt time.Time
}

func consumeDiagnosticJourneyClock(args []string) (diagnosticJourneyClock, []string, error) {
	started, args, err := consumeString(args, "--journey-started-at", "")
	if err != nil {
		return diagnosticJourneyClock{}, args, err
	}
	diagnosed, args, err := consumeString(args, "--journey-diagnosed-at", "")
	if err != nil {
		return diagnosticJourneyClock{}, args, err
	}
	clock := diagnosticJourneyClock{}
	if strings.TrimSpace(started) != "" {
		clock.StartedAt, err = time.Parse(time.RFC3339Nano, started)
		if err != nil {
			return diagnosticJourneyClock{}, args, fmt.Errorf("--journey-started-at must be RFC3339: %w", err)
		}
	}
	if strings.TrimSpace(diagnosed) != "" {
		if clock.StartedAt.IsZero() {
			return diagnosticJourneyClock{}, args, fmt.Errorf("--journey-diagnosed-at requires --journey-started-at")
		}
		clock.DiagnosedAt, err = time.Parse(time.RFC3339Nano, diagnosed)
		if err != nil {
			return diagnosticJourneyClock{}, args, fmt.Errorf("--journey-diagnosed-at must be RFC3339: %w", err)
		}
		if clock.DiagnosedAt.Before(clock.StartedAt) {
			return diagnosticJourneyClock{}, args, fmt.Errorf("--journey-diagnosed-at must not precede --journey-started-at")
		}
	}
	return clock, args, nil
}

func attachFailureJourneyMetrics(failure executionFailure, clock diagnosticJourneyClock) executionFailure {
	if failure.Diagnostic == nil || failure.Diagnostic.ConsumerHandoff == nil || clock.StartedAt.IsZero() {
		return failure
	}
	// A failed command owns its diagnosis boundary. Never use the optional
	// caller-carried prior diagnosis timestamp to compute this attempt's metric.
	var diagnosedAt time.Time
	if failure.Diagnostic.Timing != nil {
		diagnosedAt, _ = time.Parse(time.RFC3339Nano, failure.Diagnostic.Timing.DiagnosisComputedAt)
	}
	failure.Diagnostic.ConsumerHandoff.Metrics = diagnosticJourneyMetricsFrom(clock.StartedAt, diagnosedAt, time.Time{})
	return failure
}

func attachSuccessJourneyMetrics(diagnosis *localDiagnosticOutcome, clock diagnosticJourneyClock, firstSuccessAt time.Time) {
	if diagnosis == nil || diagnosis.ConsumerHandoff == nil || clock.StartedAt.IsZero() || clock.DiagnosedAt.IsZero() {
		return
	}
	diagnosis.ConsumerHandoff.Metrics = diagnosticJourneyMetricsFrom(clock.StartedAt, clock.DiagnosedAt, firstSuccessAt)
}

type diagnosticContractRef struct {
	Status           string `json:"status"`
	SchemaSHA256     string `json:"schema_sha256"`
	MappingSHA256    string `json:"mapping_sha256"`
	RuntimeAuthority bool   `json:"runtime_authority"`
}

type diagnosticSubject struct {
	SourceID    string `json:"source_id"`
	ProviderID  string `json:"provider_id"`
	DatasetID   string `json:"dataset_id"`
	OperationID string `json:"operation_id"`
}

type diagnosticAction struct {
	ActionID    string `json:"action_id"`
	Actor       string `json:"actor"`
	RationaleID string `json:"rationale_id"`
}

type diagnosticMappingResult struct {
	Code             string             `json:"code"`
	Determination    string             `json:"determination"`
	Layer            string             `json:"layer"`
	AccountableParty string             `json:"accountable_party"`
	Recommended      []diagnosticAction `json:"recommended"`
	Avoid            []diagnosticAction `json:"avoid"`
}

type diagnosticCapabilities struct {
	DatasetApplication *datasetApplicationEntry `json:"dataset_application,omitempty"`
	LocalReproduction  localReproduction        `json:"local_reproduction"`
	ReusableExport     reusableExportHandoff    `json:"reusable_export"`
	PublicHealth       unavailableCapability    `json:"public_health"`
}

type datasetApplicationEntry struct {
	RouteKind           string `json:"route_kind"`
	URL                 string `json:"url"`
	DirectSubmissionURL bool   `json:"direct_submission_url"`
}

type localReproduction struct {
	Status             string `json:"status"`
	Mode               string `json:"mode"`
	CredentialHandling string `json:"credential_handling"`
	RequiresCredential bool   `json:"requires_credential"`
}

type reusableExportHandoff struct {
	Status             string   `json:"status"`
	Formats            []string `json:"formats,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	EvidenceLevel      string   `json:"evidence_level,omitempty"`
	SemanticValidation string   `json:"semantic_validation,omitempty"`
}

type unavailableCapability struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

var exactDatasetID = regexp.MustCompile(`^[0-9]{8}$`)

func reviewedDiagnosticHandoff(evidence localDiagnosticEvidence, local localDiagnosticOutcome) *diagnosticConsumerHandoff {
	if evidence.Plan == nil {
		return nil
	}
	subject, ok := diagnosticSubjectForPlan(*evidence.Plan)
	if !ok {
		return nil
	}
	result := exactDiagnosticResult(local.Code, false)
	// A single provider response is not qualifying Health or provider-notice
	// evidence, so it must never prohibit credential reissue for an outage.
	if local.Code == "provider_outage" {
		result = exactDiagnosticResult(local.Code, false)
	}
	return &diagnosticConsumerHandoff{
		Contract: diagnosticContractRef{Status: "reviewed_draft_dependency_gated", SchemaSHA256: diagnosticSchemaSHA256, MappingSHA256: diagnosticMappingSHA256, RuntimeAuthority: false},
		Subject:  subject,
		Result:   result,
		Capabilities: diagnosticCapabilities{
			DatasetApplication: datasetApplicationForSubject(subject),
			LocalReproduction:  localReproduction{Status: "available", Mode: "datapan_cli_local", CredentialHandling: "local_only", RequiresCredential: true},
			ReusableExport:     reusableExportHandoff{Status: "unavailable", Reason: "successful_local_result_required"},
			PublicHealth:       unavailableCapability{Status: "unavailable", Reason: "health_identity_dependency_unavailable"},
		},
	}
}

func reviewedSuccessfulHandoff(plan requestPlan, validationPassed, reusableResult bool) *diagnosticConsumerHandoff {
	subject, ok := diagnosticSubjectForPlan(plan)
	if !ok {
		return nil
	}
	code := "unknown"
	if validationPassed {
		code = "ready"
	}
	export := reusableExportHandoff{Status: "unavailable", Reason: "successful_local_result_required"}
	if reusableResult {
		validation := "not_proven"
		if validationPassed {
			validation = "passed"
		}
		export = reusableExportHandoff{Status: "available", Formats: []string{"json", "csv"}, EvidenceLevel: "parseable_transport_result", SemanticValidation: validation}
	}
	return &diagnosticConsumerHandoff{
		Contract: diagnosticContractRef{Status: "reviewed_draft_dependency_gated", SchemaSHA256: diagnosticSchemaSHA256, MappingSHA256: diagnosticMappingSHA256, RuntimeAuthority: false},
		Subject:  subject,
		Result:   exactDiagnosticResult(code, false),
		Capabilities: diagnosticCapabilities{
			DatasetApplication: datasetApplicationForSubject(subject),
			LocalReproduction:  localReproduction{Status: "available", Mode: "datapan_cli_local", CredentialHandling: "local_only", RequiresCredential: true},
			ReusableExport:     export,
			PublicHealth:       unavailableCapability{Status: "unavailable", Reason: "health_identity_dependency_unavailable"},
		},
	}
}

func diagnosticJourneyMetricsFrom(journeyStartedAt, diagnosisAt, firstSuccessAt time.Time) *diagnosticJourneyMetrics {
	if journeyStartedAt.IsZero() || diagnosisAt.IsZero() || diagnosisAt.Before(journeyStartedAt) {
		return nil
	}
	timeToDiagnosis := diagnosisAt.Sub(journeyStartedAt).Milliseconds()
	metrics := &diagnosticJourneyMetrics{TimeToDiagnosisMS: &timeToDiagnosis}
	if !firstSuccessAt.IsZero() {
		if firstSuccessAt.Before(diagnosisAt) {
			return nil
		}
		timeToFirstSuccess := firstSuccessAt.Sub(journeyStartedAt).Milliseconds()
		metrics.TimeToFirstSuccessMS = &timeToFirstSuccess
	}
	return metrics
}

func diagnosticSubjectForPlan(plan requestPlan) (diagnosticSubject, bool) {
	provider := strings.ToLower(strings.TrimSpace(plan.Spec.Provider))
	if provider != "data.go.kr" || !exactDatasetID.MatchString(plan.Spec.ID) || strings.TrimSpace(plan.Operation.Name) == "" {
		return diagnosticSubject{}, false
	}
	host, path := healthEndpoint(plan.Operation.Endpoint)
	op := healthProbeOperation{Provider: plan.Spec.Provider, DatasetID: plan.Spec.ID, OperationName: plan.Operation.Name, EndpointHost: host, EndpointPath: path, DependencyClass: datago.OperationDependencyClass(plan.Spec, plan.Operation)}
	return diagnosticSubject{SourceID: "data_go_kr", ProviderID: "data_go_kr", DatasetID: plan.Spec.ID, OperationID: healthOperationKey(op)}, true
}

func datasetApplicationForSubject(subject diagnosticSubject) *datasetApplicationEntry {
	if subject.SourceID != "data_go_kr" || subject.ProviderID != "data_go_kr" || !exactDatasetID.MatchString(subject.DatasetID) {
		return nil
	}
	return &datasetApplicationEntry{RouteKind: "dataset_application_entry", URL: fmt.Sprintf("https://www.data.go.kr/data/%s/openapi.do", subject.DatasetID), DirectSubmissionURL: false}
}

func exactDiagnosticResult(code string, outageCorroborated bool) diagnosticMappingResult {
	result := map[string]diagnosticMappingResult{
		"approval_required":    resultOf("approval_required", "observed", "access", "user", action("apply_for_operation", "user", "action.apply_for_operation"), action("reissue_credential", "user", "avoid.reissue_does_not_grant_operation")),
		"approval_propagating": resultOf("approval_propagating", "inferred", "access", "data_go_kr", action("wait_for_approval_sync", "user", "action.wait_for_approval_sync"), action("reissue_credential", "user", "avoid.reissue_restarts_sync")),
		"credential_invalid":   resultOf("credential_invalid", "observed", "access", "user", action("verify_credential_configuration", "user", "action.verify_credential_configuration"), action("assume_provider_outage", "user", "avoid.credential_rejection_is_not_outage")),
		"invalid_input":        resultOf("invalid_input", "observed", "request", "user", action("verify_request_parameters", "user", "action.verify_request_parameters"), action("assume_provider_outage", "user", "avoid.input_error_is_not_outage")),
		"rate_limited":         resultOf("rate_limited", "observed", "provider", "shared", action("retry_with_backoff", "datapan_cli", "action.retry_with_backoff"), action("retry_immediately", "datapan_cli", "avoid.immediate_retry_increases_pressure")),
		"contract_drift":       resultOf("contract_drift", "inferred", "response_contract", "shared", action("refresh_contract", "datapan_cli", "action.refresh_response_contract"), action("reissue_credential", "user", "avoid.contract_drift_not_credential")),
		"semantic_quality":     resultOf("semantic_quality", "inferred", "data_quality", "provider", action("inspect_data_quality", "user", "action.inspect_zero_or_missing_data"), action("treat_transport_success_as_data_success", "user", "avoid.http-200_does_not_prove_semantic_quality")),
		"stale_data":           resultOf("stale_data", "observed", "data_quality", "provider", action("inspect_data_quality", "user", "action.inspect_reference_date"), action("treat_transport_success_as_data_success", "user", "avoid.http-200_does_not_prove_freshness")),
		"ready":                resultOf("ready", "observed", "validation", "shared", action("continue_to_reuse", "user", "action.continue_to_reuse"), diagnosticAction{}),
	}
	if code == "provider_outage" {
		avoid := diagnosticAction{}
		if outageCorroborated {
			avoid = action("reissue_credential", "user", "avoid.outage_not_fixed_by_key")
		}
		return resultOf("provider_outage", "inferred", "provider", "provider", action("check_provider_status", "user", "action.check_provider_status"), avoid)
	}
	if value, ok := result[code]; ok {
		return value
	}
	return resultOf("unknown", "unknown", "unknown", "unknown", action("gather_more_evidence", "datapan_cli", "action.gather_more_evidence"), action("assume_provider_outage", "user", "avoid.ambiguous_symptom_is_not_outage"))
}

func action(id, actor, rationale string) diagnosticAction {
	return diagnosticAction{ActionID: id, Actor: actor, RationaleID: rationale}
}

func resultOf(code, determination, layer, owner string, recommended, avoid diagnosticAction) diagnosticMappingResult {
	r := diagnosticMappingResult{Code: code, Determination: determination, Layer: layer, AccountableParty: owner, Recommended: []diagnosticAction{recommended}, Avoid: []diagnosticAction{}}
	if avoid.ActionID != "" {
		r.Avoid = append(r.Avoid, avoid)
	}
	return r
}
