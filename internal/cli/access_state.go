package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const accessStateSchemaVersion = "datapan.local-access-state.v1"

// accessSubject deliberately contains stable Registry identity or a named
// provider service, never an endpoint URL, request data, or user identity.
type accessSubject struct {
	Scope     string `json:"scope"`
	DatasetID string `json:"dataset_id,omitempty"`
	Operation string `json:"operation,omitempty"`
	Provider  string `json:"provider"`
	Service   string `json:"service"`
}

type accessObservation struct {
	State          string `json:"state"`
	ObservedAt     string `json:"observed_at,omitempty"`
	SourceContract string `json:"source_contract,omitempty"`
}

type localAccessRecord struct {
	Subject     accessSubject     `json:"subject"`
	Application accessObservation `json:"application"`
	Quota       accessObservation `json:"quota"`
	RateLimit   accessObservation `json:"rate_limit"`
	UpdatedAt   string            `json:"updated_at"`
}

type localAccessState struct {
	SchemaVersion string               `json:"schema_version"`
	Records       []localAccessRecord  `json:"records"`
	Redaction     accessStateRedaction `json:"redaction"`
}

type accessStateRedaction struct {
	CredentialValuesPresent bool `json:"credential_values_present"`
	CredentialHashesPresent bool `json:"credential_hashes_present"`
	RequestURLsPresent      bool `json:"request_urls_present"`
	ParameterValuesPresent  bool `json:"parameter_values_present"`
	ResponseBodiesPresent   bool `json:"response_bodies_present"`
	UserIdentityPresent     bool `json:"user_identity_present"`
}

func emptyLocalAccessState() localAccessState {
	return localAccessState{SchemaVersion: accessStateSchemaVersion, Records: []localAccessRecord{}}
}

func (a app) accessRecord(args []string, jsonOut bool) int {
	subject, args, err := a.accessSubjectFromArgs(args, jsonOut)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	application, args, err := consumeOptionalAccessState(args, "--application-state", validApplicationAccessState)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	quota, args, err := consumeOptionalAccessState(args, "--quota-state", validQuotaAccessState)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	rateLimit, args, err := consumeOptionalAccessState(args, "--rate-limit-state", validRateLimitAccessState)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	observedAt, args, err := consumeString(args, "--observed-at", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	sourceContract, args, err := consumeString(args, "--source-contract", "manual_operator_observation")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", defaultAccessStatePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 || (application == "" && quota == "" && rateLimit == "") {
		return a.fail(exitUsage, "usage: datapan access record (--ref REF [--operation NAME] | --provider NAME --service NAME) --application-state unknown|requested|approved|rejected [--quota-state unknown|available|exhausted] [--rate-limit-state unknown|not_observed|observed] --observed-at RFC3339 [--source-contract manual_operator_observation] [--output PATH] [--json]")
	}
	if sourceContract != "manual_operator_observation" {
		return a.fail(exitUsage, "--source-contract must be manual_operator_observation")
	}
	observed, err := parseAccessObservationTime(observedAt)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	state, err := readLocalAccessState(output)
	if err != nil {
		return a.fail(exitUsage, "read local access state: %v", err)
	}
	record, index := findLocalAccessRecord(state.Records, subject)
	if index < 0 {
		record = localAccessRecord{Subject: subject, Application: unknownAccessObservation(), Quota: unknownAccessObservation(), RateLimit: unknownAccessObservation()}
	}
	if err := updateAccessObservation(&record.Application, application, observed, sourceContract); err != nil {
		return a.fail(exitUsage, "application evidence: %v", err)
	}
	if err := updateAccessObservation(&record.Quota, quota, observed, sourceContract); err != nil {
		return a.fail(exitUsage, "quota evidence: %v", err)
	}
	if err := updateAccessObservation(&record.RateLimit, rateLimit, observed, sourceContract); err != nil {
		return a.fail(exitUsage, "rate-limit evidence: %v", err)
	}
	record.UpdatedAt = observed.Format(time.RFC3339)
	if index < 0 {
		state.Records = append(state.Records, record)
	} else {
		state.Records[index] = record
	}
	if err := writeAtomicJSON(output, state); err != nil {
		return a.fail(exitRequest, "write local access state: %v", err)
	}
	payload := accessStatusPayload(subject, record, output, true)
	payload["ok"], payload["recorded"] = true, true
	if jsonOut {
		return a.writeJSON(payload)
	}
	fmt.Fprintf(a.stdout, "Recorded local access evidence for %s. No provider request was made.\n", accessSubjectLabel(subject))
	return exitOK
}

func (a app) accessStatus(args []string, jsonOut bool) int {
	subject, args, err := a.accessSubjectFromArgs(args, jsonOut)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	input, args, err := consumeString(args, "--input", defaultAccessStatePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan access status (--ref REF [--operation NAME] | --provider NAME --service NAME) [--input PATH] [--json]")
	}
	state, err := readLocalAccessState(input)
	if err != nil {
		return a.fail(exitUsage, "read local access state: %v", err)
	}
	record, index := findLocalAccessRecord(state.Records, subject)
	if index < 0 {
		record = localAccessRecord{Subject: subject, Application: unknownAccessObservation(), Quota: unknownAccessObservation(), RateLimit: unknownAccessObservation()}
	}
	payload := accessStatusPayload(subject, record, input, index >= 0)
	payload["ok"] = true
	if jsonOut {
		return a.writeJSON(payload)
	}
	fmt.Fprintf(a.stdout, "%s: application=%s quota=%s rate_limit=%s\n", accessSubjectLabel(subject), record.Application.State, record.Quota.State, record.RateLimit.State)
	return exitOK
}

func (a app) accessSubjectFromArgs(args []string, jsonOut bool) (accessSubject, []string, error) {
	ref, args, err := consumeString(args, "--ref", "")
	if err != nil {
		return accessSubject{}, nil, err
	}
	operation, args, err := consumeString(args, "--operation", "")
	if err != nil {
		return accessSubject{}, nil, err
	}
	provider, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return accessSubject{}, nil, err
	}
	service, args, err := consumeString(args, "--service", "")
	if err != nil {
		return accessSubject{}, nil, err
	}
	ref, operation = strings.TrimSpace(ref), strings.TrimSpace(operation)
	provider, service = strings.TrimSpace(provider), strings.TrimSpace(service)
	if ref != "" {
		if provider != "" || service != "" {
			return accessSubject{}, nil, fmt.Errorf("use either --ref/--operation or --provider/--service")
		}
		spec, _, ok := a.resolveOne(ref, jsonOut)
		if !ok {
			return accessSubject{}, nil, fmt.Errorf("could not resolve --ref %q", ref)
		}
		op, ok := accessOperation(spec, operation)
		if !ok {
			if operation == "" && len(spec.Operations) > 1 {
				return accessSubject{}, nil, fmt.Errorf("--operation is required when the Registry dataset has multiple operations")
			}
			return accessSubject{}, nil, fmt.Errorf("unknown operation %q for %s", operation, spec.ID)
		}
		return accessSubject{Scope: "registry_operation", DatasetID: spec.ID, Operation: op.Name, Provider: strings.TrimSpace(spec.Provider), Service: accessServiceName(spec, op)}, args, nil
	}
	if provider == "" || service == "" {
		return accessSubject{}, nil, fmt.Errorf("provide --ref (and --operation when needed), or both --provider and --service")
	}
	if operation != "" {
		return accessSubject{}, nil, fmt.Errorf("--operation requires --ref")
	}
	if !safeAccessServiceIdentifier(provider) || !safeAccessServiceIdentifier(service) {
		return accessSubject{}, nil, fmt.Errorf("--provider and --service must be short identifiers, not URLs, query strings, or credential material")
	}
	return accessSubject{Scope: "provider_service", Provider: provider, Service: service}, args, nil
}

func safeAccessServiceIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && !strings.Contains(value, "://") && !strings.ContainsAny(value, "?&#=\r\n\t")
}

func accessOperation(spec datago.Spec, name string) (datago.Operation, bool) {
	if name == "" {
		if len(spec.Operations) != 1 {
			return datago.Operation{}, false
		}
		return spec.Operations[0], true
	}
	for _, operation := range spec.Operations {
		if operation.Name == name {
			return operation, true
		}
	}
	return datago.Operation{}, false
}

func accessServiceName(spec datago.Spec, operation datago.Operation) string {
	if host, err := endpointHostForCredential(operation.Endpoint); err == nil && host != "" {
		return host
	}
	return strings.ToLower(strings.TrimSpace(spec.Provider))
}

func consumeOptionalAccessState(args []string, name string, valid func(string) bool) (string, []string, error) {
	value, args, err := consumeString(args, name, "")
	if err != nil {
		return "", nil, err
	}
	value = strings.TrimSpace(value)
	if value != "" && !valid(value) {
		return "", nil, fmt.Errorf("%s has unsupported state %q", name, value)
	}
	return value, args, nil
}

func validApplicationAccessState(value string) bool {
	return value == "unknown" || value == "requested" || value == "approved" || value == "rejected"
}
func validQuotaAccessState(value string) bool {
	return value == "unknown" || value == "available" || value == "exhausted"
}
func validRateLimitAccessState(value string) bool {
	return value == "unknown" || value == "not_observed" || value == "observed"
}

func parseAccessObservationTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("--observed-at must be an RFC3339 timestamp")
	}
	return parsed.UTC(), nil
}

func unknownAccessObservation() accessObservation { return accessObservation{State: "unknown"} }

func updateAccessObservation(current *accessObservation, state string, observed time.Time, source string) error {
	if state == "" {
		return nil
	}
	if current.ObservedAt != "" {
		previous, err := time.Parse(time.RFC3339, current.ObservedAt)
		if err != nil {
			return fmt.Errorf("stored observation time is invalid")
		}
		if observed.Before(previous) {
			return fmt.Errorf("new observation predates the stored observation")
		}
	}
	*current = accessObservation{State: state, ObservedAt: observed.Format(time.RFC3339), SourceContract: source}
	return nil
}

func readLocalAccessState(path string) (localAccessState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyLocalAccessState(), nil
	}
	if err != nil {
		return localAccessState{}, err
	}
	var state localAccessState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return localAccessState{}, err
	}
	if state.SchemaVersion != accessStateSchemaVersion {
		return localAccessState{}, fmt.Errorf("unsupported access-state schema")
	}
	if state.Redaction.CredentialValuesPresent || state.Redaction.CredentialHashesPresent || state.Redaction.RequestURLsPresent || state.Redaction.ParameterValuesPresent || state.Redaction.ResponseBodiesPresent || state.Redaction.UserIdentityPresent {
		return localAccessState{}, fmt.Errorf("access-state redaction boundary is violated")
	}
	for _, record := range state.Records {
		if err := validateLocalAccessRecord(record); err != nil {
			return localAccessState{}, err
		}
	}
	return state, nil
}

func validateLocalAccessRecord(record localAccessRecord) error {
	if record.Subject.Scope != "registry_operation" && record.Subject.Scope != "provider_service" {
		return fmt.Errorf("unsupported access subject scope")
	}
	if strings.TrimSpace(record.Subject.Provider) == "" || strings.TrimSpace(record.Subject.Service) == "" {
		return fmt.Errorf("access record subject is incomplete")
	}
	if record.Subject.Scope == "registry_operation" && (record.Subject.DatasetID == "" || record.Subject.Operation == "") {
		return fmt.Errorf("Registry operation access record is incomplete")
	}
	for _, observation := range []accessObservation{record.Application, record.Quota, record.RateLimit} {
		if observation.State == "" {
			return fmt.Errorf("access record state is missing")
		}
		if observation.ObservedAt != "" {
			if _, err := time.Parse(time.RFC3339, observation.ObservedAt); err != nil {
				return fmt.Errorf("access record observation time is invalid")
			}
			if observation.SourceContract != "manual_operator_observation" {
				return fmt.Errorf("access record source contract is unsupported")
			}
		}
	}
	if !validApplicationAccessState(record.Application.State) || !validQuotaAccessState(record.Quota.State) || !validRateLimitAccessState(record.RateLimit.State) {
		return fmt.Errorf("access record state is unsupported")
	}
	return nil
}

func findLocalAccessRecord(records []localAccessRecord, subject accessSubject) (localAccessRecord, int) {
	for index, record := range records {
		if record.Subject == subject {
			return record, index
		}
	}
	return localAccessRecord{}, -1
}

func accessStatusPayload(subject accessSubject, record localAccessRecord, path string, found bool) map[string]any {
	return map[string]any{
		"subject": subject, "record_present": found, "state_path": path,
		"application": record.Application, "quota": record.Quota, "rate_limit": record.RateLimit,
		"next_steps": accessNextSteps(subject, record),
		"redaction":  map[string]bool{"credential_values_present": false, "credential_hashes_present": false, "request_urls_present": false, "parameter_values_present": false, "response_bodies_present": false, "user_identity_present": false},
	}
}

func accessNextSteps(subject accessSubject, record localAccessRecord) []string {
	steps := []string{}
	switch record.Application.State {
	case "unknown":
		steps = append(steps, "record manual provider application evidence when it is available; do not infer approval from a configured credential")
	case "requested":
		steps = append(steps, "check the provider application status manually; do not reissue or rotate a credential")
	case "rejected":
		steps = append(steps, "review the provider's manual rejection reason before changing local configuration")
	case "approved":
		if subject.Scope == "registry_operation" {
			steps = append(steps, "run a local dry-run before any opt-in provider call: datapan get "+subject.DatasetID+" --operation "+quoteShellArg(subject.Operation)+" --dry-run --json")
		}
	}
	if record.Quota.State == "exhausted" || record.RateLimit.State == "observed" {
		steps = append(steps, "wait for the documented provider quota window or contact the provider; do not create a replacement key")
	}
	if len(steps) == 0 {
		steps = append(steps, "keep local access evidence current; this record is not provider verification")
	}
	return steps
}

func accessSubjectLabel(subject accessSubject) string {
	if subject.Scope == "registry_operation" {
		return subject.DatasetID + "/" + subject.Operation
	}
	return subject.Provider + "/" + subject.Service
}
