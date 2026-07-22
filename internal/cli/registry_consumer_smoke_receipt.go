package cli

import (
	"encoding/json"
	"fmt"
	"time"
)

// validateObservedRegistryConsumerSmokeReceiptAdmission checks the two
// properties that JSON Schema cannot compare: an admission accepts only an
// observed, completed receipt, and its observed_at must be within the
// caller-owned reference time and maximum age. The caller must provide both
// values; receipt producers cannot choose their own freshness window.
func validateObservedRegistryConsumerSmokeReceiptAdmission(data []byte, referenceAt time.Time, maxAge time.Duration) error {
	if referenceAt.IsZero() {
		return fmt.Errorf("registry consumer smoke admission requires reference time")
	}
	if maxAge <= 0 {
		return fmt.Errorf("registry consumer smoke admission requires positive maximum age")
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
