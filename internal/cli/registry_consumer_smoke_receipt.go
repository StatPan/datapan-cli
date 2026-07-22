package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// validateObservedRegistryConsumerSmokeReceiptAdmission checks the two
// properties that JSON Schema cannot compare. It validates the complete
// receipt schema first, then accepts only an observed, completed receipt whose
// observed_at is within the caller-owned reference time and maximum age. The
// caller must provide both values; receipt producers cannot choose their own
// freshness window.
func validateObservedRegistryConsumerSmokeReceiptAdmission(schema *jsonschema.Schema, data []byte, referenceAt time.Time, maxAge time.Duration) error {
	if schema == nil {
		return fmt.Errorf("registry consumer smoke admission requires schema validator")
	}
	if referenceAt.IsZero() {
		return fmt.Errorf("registry consumer smoke admission requires reference time")
	}
	if maxAge <= 0 {
		return fmt.Errorf("registry consumer smoke admission requires positive maximum age")
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("registry consumer smoke receipt schema validation failed")
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("registry consumer smoke receipt schema validation failed")
	}
	var receipt struct {
		EvidenceClass      string `json:"evidence_class"`
		CompletionEvidence bool   `json:"completion_evidence"`
		ObservedAt         string `json:"observed_at"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return fmt.Errorf("decode registry consumer smoke receipt: %w", err)
	}
	if receipt.EvidenceClass != "observed" {
		return fmt.Errorf("registry consumer smoke admission rejects %q evidence", receipt.EvidenceClass)
	}
	if !receipt.CompletionEvidence {
		return fmt.Errorf("registry consumer smoke admission requires completion evidence")
	}
	observedAt, err := time.Parse(time.RFC3339, receipt.ObservedAt)
	if err != nil {
		return fmt.Errorf("registry consumer smoke receipt observed_at: %w", err)
	}
	referenceAt = referenceAt.UTC()
	if observedAt.After(referenceAt) {
		return fmt.Errorf("registry consumer smoke receipt observed_at is in the future")
	}
	if referenceAt.Sub(observedAt) > maxAge {
		return fmt.Errorf("registry consumer smoke receipt is stale")
	}
	return nil
}
