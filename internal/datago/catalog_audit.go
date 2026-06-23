package datago

import "strings"

type CatalogAudit struct {
	SpecsTotal                      int          `json:"specs_total"`
	OperationsTotal                 int          `json:"operations_total"`
	CallableOperations              int          `json:"callable_operations"`
	SpecsWithoutOperations          int          `json:"specs_without_operations"`
	SpecsWithoutCallableOperation   int          `json:"specs_without_callable_operation"`
	OperationsWithoutEndpoint       int          `json:"operations_without_endpoint"`
	OperationsWithoutRequestParams  int          `json:"operations_without_request_params"`
	OperationsWithoutResponseParams int          `json:"operations_without_response_params"`
	SpecsMissingOrganization        int          `json:"specs_missing_organization"`
	SpecsMissingSourceURL           int          `json:"specs_missing_source_url"`
	SpecsMissingUpdatedAt           int          `json:"specs_missing_updated_at"`
	Samples                         AuditSamples `json:"samples"`
}

type AuditSamples struct {
	WithoutOperations        []AuditSample `json:"without_operations,omitempty"`
	WithoutCallableOperation []AuditSample `json:"without_callable_operation,omitempty"`
	MissingOrganization      []AuditSample `json:"missing_organization,omitempty"`
	MissingUpdatedAt         []AuditSample `json:"missing_updated_at,omitempty"`
}

type AuditSample struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Organization       string `json:"organization,omitempty"`
	SourceCategory     string `json:"source_category,omitempty"`
	OperationsCount    int    `json:"operations_count"`
	CallableOperations int    `json:"callable_operations"`
}

func AuditRegistry(reg Registry, sampleLimit int) CatalogAudit {
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	audit := CatalogAudit{}
	for _, spec := range reg.Specs() {
		audit.SpecsTotal++
		if strings.TrimSpace(spec.Organization) == "" {
			audit.SpecsMissingOrganization++
			audit.Samples.MissingOrganization = appendSample(audit.Samples.MissingOrganization, spec, sampleLimit)
		}
		if spec.Source == nil || strings.TrimSpace(spec.Source.URL) == "" {
			audit.SpecsMissingSourceURL++
		}
		if !hasUpdatedAt(spec) {
			audit.SpecsMissingUpdatedAt++
			audit.Samples.MissingUpdatedAt = appendSample(audit.Samples.MissingUpdatedAt, spec, sampleLimit)
		}
		if len(spec.Operations) == 0 {
			audit.SpecsWithoutOperations++
			audit.SpecsWithoutCallableOperation++
			audit.Samples.WithoutOperations = appendSample(audit.Samples.WithoutOperations, spec, sampleLimit)
			audit.Samples.WithoutCallableOperation = appendSample(audit.Samples.WithoutCallableOperation, spec, sampleLimit)
			continue
		}
		callable := false
		for _, op := range spec.Operations {
			audit.OperationsTotal++
			if strings.TrimSpace(op.Endpoint) == "" {
				audit.OperationsWithoutEndpoint++
			} else {
				callable = true
				audit.CallableOperations++
			}
			if len(op.RequestParams) == 0 {
				audit.OperationsWithoutRequestParams++
			}
			if len(op.ResponseParams) == 0 {
				audit.OperationsWithoutResponseParams++
			}
		}
		if !callable {
			audit.SpecsWithoutCallableOperation++
			audit.Samples.WithoutCallableOperation = appendSample(audit.Samples.WithoutCallableOperation, spec, sampleLimit)
		}
	}
	return audit
}

func appendSample(samples []AuditSample, spec Spec, limit int) []AuditSample {
	if limit == 0 || len(samples) >= limit {
		return samples
	}
	return append(samples, auditSample(spec))
}

func auditSample(spec Spec) AuditSample {
	callable := 0
	for _, op := range spec.Operations {
		if strings.TrimSpace(op.Endpoint) != "" {
			callable++
		}
	}
	return AuditSample{
		ID:                 spec.ID,
		Title:              spec.Title,
		Organization:       spec.Organization,
		SourceCategory:     spec.SourceCategory,
		OperationsCount:    len(spec.Operations),
		CallableOperations: callable,
	}
}

func hasUpdatedAt(spec Spec) bool {
	if spec.Source != nil && rawString(spec.Source.Raw, "updated_at") != "" {
		return true
	}
	for _, op := range spec.Operations {
		if op.Source != nil && rawString(op.Source.Raw, "updated_at") != "" {
			return true
		}
	}
	return false
}

func rawString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	value, ok := raw[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(toString(value))
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
