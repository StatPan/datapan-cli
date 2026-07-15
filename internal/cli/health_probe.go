package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type healthProbeReceipt struct {
	SchemaVersion string                 `json:"schema_version"`
	ProbeID       string                 `json:"probe_id"`
	ObservedAt    string                 `json:"observed_at"`
	Operation     healthProbeOperation   `json:"operation"`
	Registry      healthProbeRegistry    `json:"registry"`
	Policy        *healthProbePolicy     `json:"policy,omitempty"`
	Execution     healthProbeExecution   `json:"execution"`
	Observation   healthProbeObservation `json:"observation"`
	Assessment    healthProbeAssessment  `json:"assessment"`
	Redaction     healthProbeRedaction   `json:"redaction"`
}

type healthProbePolicy struct {
	Key       string `json:"key"`
	Version   int    `json:"version"`
	Authority string `json:"authority"`
	MaxLevel  string `json:"max_level"`
}

type healthProbeOperation struct {
	OperationKey    string `json:"operation_key"`
	DatasetID       string `json:"dataset_id"`
	OperationName   string `json:"operation_name"`
	Provider        string `json:"provider"`
	EndpointHost    string `json:"endpoint_host,omitempty"`
	EndpointPath    string `json:"endpoint_path,omitempty"`
	DependencyClass string `json:"dependency_class"`
}

type healthProbeRegistry struct {
	DatasetID       string `json:"dataset_id"`
	DatasetRevision string `json:"dataset_revision"`
	RegistrySHA256  string `json:"registry_sha256"`
	ManifestSHA256  string `json:"manifest_sha256,omitempty"`
}

type healthProbeExecution struct {
	CLIVersion         string   `json:"cli_version"`
	Attempted          bool     `json:"attempted"`
	TimeoutMS          int64    `json:"timeout_ms"`
	RequestBudget      int      `json:"request_budget"`
	SafeParameterNames []string `json:"safe_parameter_names,omitempty"`
}

type healthProbeObservation struct {
	MaxLevel             string `json:"max_level"`
	LatencyMS            int64  `json:"latency_ms"`
	HTTPStatus           int    `json:"http_status,omitempty"`
	ProviderCode         string `json:"provider_code,omitempty"`
	ProviderMessageClass string `json:"provider_message_class,omitempty"`
	SemanticStatus       string `json:"semantic_status,omitempty"`
	BodyShape            string `json:"body_shape,omitempty"`
	DataPresence         string `json:"data_presence"`
	SchemaStatus         string `json:"schema_status"`
	FreshnessStatus      string `json:"freshness_status"`
}

type healthProbeAssessment struct {
	Outcome     string   `json:"outcome"`
	Category    string   `json:"category"`
	Retryable   bool     `json:"retryable"`
	ReasonCode  string   `json:"reason_code"`
	NextActions []string `json:"next_actions,omitempty"`
}

type healthProbeRedaction struct {
	CredentialsRemoved  bool `json:"credentials_removed"`
	QueryValuesRemoved  bool `json:"query_values_removed"`
	ResponseRowsRemoved bool `json:"response_rows_removed"`
}

func (a app) healthProbeReceipt(candidate datago.VerificationCandidate, result datago.VerificationResult, trust registryTrustContext, timeout time.Duration) (healthProbeReceipt, error) {
	if !trust.ProvenancePresent || trust.DatasetID == "" || !validImmutableRevision(trust.DatasetRevision) || len(trust.RegistrySHA256) != 64 || trust.RegistryDigestMatches == nil || !*trust.RegistryDigestMatches || trust.ManifestBinding != "verified" {
		return healthProbeReceipt{}, fmt.Errorf("immutable installed Registry provenance is required; run datapan init --json")
	}
	provenance, err := readRegistryInstallProvenance(defaultRegistryInstallProvenancePath)
	if err != nil {
		return healthProbeReceipt{}, fmt.Errorf("read Registry provenance: %w", err)
	}
	if len(provenance.ReleaseManifestSHA256) != 64 {
		return healthProbeReceipt{}, fmt.Errorf("manifest-bound installed Registry provenance is required; run datapan init --json")
	}
	host, path := healthEndpoint(candidate.Operation.Endpoint)
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(candidate.EndpointHost))
	}
	op := healthProbeOperation{
		DatasetID: candidate.Spec.ID, OperationName: candidate.Operation.Name,
		Provider: candidate.Spec.Provider, EndpointHost: host, EndpointPath: path,
		DependencyClass: candidate.DependencyClass,
	}
	op.OperationKey = healthOperationKey(op)
	policy := trust.HealthPolicies[op.OperationKey]
	assessment := classifyHealthProbe(result)
	parameterNames := make([]string, 0, len(result.Params))
	for name := range result.Params {
		if !isAuthParam(name) {
			parameterNames = append(parameterNames, name)
		}
	}
	sort.Strings(parameterNames)
	observation := healthProbeObservation{
		MaxLevel: healthMaxLevel(result), LatencyMS: result.DurationMS, HTTPStatus: result.HTTPStatus,
		SemanticStatus: result.SemanticStatus, BodyShape: result.BodyShape,
		DataPresence: healthDataPresence(result), SchemaStatus: "not_observed", FreshnessStatus: "not_observed",
	}
	if result.ProviderStatus != nil {
		observation.ProviderCode = firstNonEmpty(result.ProviderStatus.Code, result.ProviderStatus.ReasonCode)
		observation.ProviderMessageClass = result.ProviderStatus.Source
	}
	return healthProbeReceipt{
		SchemaVersion: "datapan.health-probe.v1", ProbeID: newProbeUUID(),
		ObservedAt: firstNonEmpty(result.VerifiedAt, time.Now().UTC().Format(time.RFC3339)), Operation: op,
		Registry:    healthProbeRegistry{DatasetID: trust.DatasetID, DatasetRevision: trust.DatasetRevision, RegistrySHA256: strings.ToLower(trust.RegistrySHA256), ManifestSHA256: strings.ToLower(provenance.ReleaseManifestSHA256)},
		Policy:      policy,
		Execution:   healthProbeExecution{CLIVersion: version, Attempted: result.Status != "skipped", TimeoutMS: timeout.Milliseconds(), RequestBudget: boolInt(result.Status != "skipped"), SafeParameterNames: parameterNames},
		Observation: observation, Assessment: assessment,
		Redaction: healthProbeRedaction{CredentialsRemoved: true, QueryValuesRemoved: true, ResponseRowsRemoved: true},
	}, nil
}

func healthEndpoint(raw string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ""
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.ToLower(parsed.Hostname()), path
}

func healthOperationKey(op healthProbeOperation) string {
	fields := []string{op.Provider, op.DatasetID, op.OperationName, op.DependencyClass, strings.ToLower(op.EndpointHost), op.EndpointPath}
	h := sha256.New()
	for _, field := range fields {
		_, _ = fmt.Fprintf(h, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func newProbeUUID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		sum := sha256.Sum256([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
		copy(id[:], sum[:16])
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func classifyHealthProbe(result datago.VerificationResult) healthProbeAssessment {
	reason := strings.ToLower(strings.TrimSpace(result.Reason))
	assessment := healthProbeAssessment{Outcome: "indeterminate", Category: "indeterminate", ReasonCode: firstNonEmpty(result.Reason, result.SemanticStatus, result.Status)}
	if assessment.ReasonCode == "" {
		assessment.ReasonCode = "indeterminate"
	}
	switch {
	case result.Status == "skipped" && reason == "missing_auth":
		assessment.Outcome, assessment.Category, assessment.ReasonCode = "skipped", "credential_missing", "missing_auth"
	case result.Status == "skipped" && (reason == "missing_required_params" || reason == "approval_required"):
		assessment.Outcome, assessment.Category = "skipped", "parameter_blocked"
	case result.Status == "skipped":
		assessment.Outcome, assessment.Category = "skipped", "unsupported"
	case result.HTTPStatus == 429:
		assessment.Outcome, assessment.Category, assessment.Retryable = "unhealthy", "rate_limited", true
	case result.HTTPStatus == 401 || result.HTTPStatus == 403 || strings.Contains(reason, "not registered") || strings.Contains(reason, "credential"):
		assessment.Outcome, assessment.Category = "unhealthy", "credential_rejected"
	case strings.Contains(reason, "deadline") || strings.Contains(reason, "timeout"):
		assessment.Outcome, assessment.Category, assessment.Retryable = "unhealthy", "timeout", true
	case result.HTTPStatus >= 500:
		assessment.Outcome, assessment.Category, assessment.Retryable = "unhealthy", "provider_failure", true
	case result.HTTPStatus >= 400:
		assessment.Outcome, assessment.Category = "unhealthy", "provider_failure"
	case result.ProviderStatus != nil && !result.ProviderStatus.OK:
		assessment.Outcome, assessment.Category = "unhealthy", "provider_failure"
	case result.SemanticStatus == "html_response" || result.SemanticStatus == "provider_error":
		assessment.Outcome, assessment.Category = "unhealthy", "semantic_failure"
	case result.SemanticStatus == "unclassified_response":
		assessment.Outcome, assessment.Category, assessment.ReasonCode = "unhealthy", "schema_drift", "unclassified_response"
	case result.Status == "failed" && result.HTTPStatus == 0:
		assessment.Outcome, assessment.Category, assessment.Retryable = "unhealthy", "transport_failure", true
	case result.Status == "verified":
		assessment.Outcome, assessment.Category, assessment.ReasonCode = "healthy", "healthy", firstNonEmpty(result.SemanticStatus, "verification_succeeded")
	case result.Status == "failed":
		assessment.Outcome, assessment.Category = "unhealthy", "semantic_failure"
	}
	if assessment.Category != "healthy" && assessment.Category != "skipped" && assessment.Category != "unsupported" && assessment.Category != "parameter_blocked" && assessment.Category != "credential_missing" {
		assessment.ReasonCode = assessment.Category
	}
	return assessment
}

func healthMaxLevel(result datago.VerificationResult) string {
	if result.ProviderStatus != nil && result.ProviderStatus.OK {
		return "L4"
	}
	if result.BodyShape != "" && result.BodyShape != "html" && result.BodyShape != "html_portal" {
		return "L3"
	}
	if result.HTTPStatus != 0 {
		return "L2"
	}
	if result.Status == "failed" && result.Reason != "" {
		return "L0"
	}
	return "L0"
}

func healthDataPresence(result datago.VerificationResult) string {
	shape := strings.ToLower(result.BodyShape)
	if result.SemanticStatus == "empty_response" || strings.Contains(shape, "rows:0") || strings.Contains(shape, "items:0") {
		return "empty"
	}
	if strings.Contains(shape, "rows:") || strings.Contains(shape, "items") {
		return "present"
	}
	if result.Status == "skipped" || result.HTTPStatus == 0 {
		return "not_observed"
	}
	return "indeterminate"
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func healthProbeExitCode(receipt healthProbeReceipt) int {
	switch receipt.Assessment.Outcome {
	case "healthy":
		return exitOK
	case "skipped", "indeterminate":
		return exitAuth
	default:
		return exitRequest
	}
}

func writeOutputAtomic(path string, data []byte, stdout interface{ Write([]byte) (int, error) }) error {
	if path == "" || path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".datapan-health-probe-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(tmpPath, path)
	}
	return nil
}
