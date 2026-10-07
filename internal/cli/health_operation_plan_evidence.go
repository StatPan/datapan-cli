package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// A selected operation may bind a few independently parsed documents. Each
	// sidecar is capped before allocation; source population size is unrelated.
	healthOperationDocumentEvidenceMaxBytes         = 1 << 20
	healthOperationPlanMaxSelectedEvidenceArtifacts = 4
	healthOperationPlanMaxEvidenceRefsPerRecord     = 2048
)

var (
	healthOperationDocumentSchemaOnce   sync.Once
	healthOperationDocumentSchema       *jsonschema.Schema
	healthOperationDocumentSchemaErr    error
	healthOperationDocumentV2SchemaOnce sync.Once
	healthOperationDocumentV2Schema     *jsonschema.Schema
	healthOperationDocumentV2SchemaErr  error
)

func validateSelectedHealthOperationDocumentEvidence(root string, plan healthOperationPlanRecord, index healthOperationPlanIndex, manifest releaseManifest) error {
	_, err := loadSelectedHealthOperationDocumentEvidence(root, plan, index, manifest)
	return err
}

func loadSelectedHealthOperationDocumentEvidence(root string, plan healthOperationPlanRecord, index healthOperationPlanIndex, manifest releaseManifest) (map[string]map[string]any, error) {
	refs, err := healthOperationPlanEvidenceRefs(plan)
	if err != nil {
		return nil, err
	}

	indexArtifacts := make(map[string]healthOperationPlanArtifactRef, len(index.GenerationInputs.DocumentEvidence))
	for _, artifact := range index.GenerationInputs.DocumentEvidence {
		if _, duplicate := indexArtifacts[artifact.Path]; duplicate {
			return nil, errors.New("document evidence artifact is duplicated in the index")
		}
		indexArtifacts[artifact.Path] = artifact
	}

	selectedArtifacts := make(map[string]healthOperationPlanArtifactRef)
	for _, ref := range refs {
		if ref.EvidenceKind != "operation_document" {
			continue
		}
		artifact, ok := indexArtifacts[ref.ArtifactPath]
		if !ok || !strings.EqualFold(artifact.SHA256, ref.SHA256) || artifact.Bytes < 1 || artifact.Bytes > healthOperationDocumentEvidenceMaxBytes {
			return nil, errors.New("operation-document evidence is not bounded by the selected index")
		}
		if !validHealthOperationDocumentPointer(ref.JSONPointer) {
			return nil, errors.New("operation-document evidence pointer is invalid")
		}
		selectedArtifacts[artifact.Path] = artifact
	}
	if len(selectedArtifacts) > healthOperationPlanMaxSelectedEvidenceArtifacts {
		return nil, errors.New("selected operation-document evidence count exceeds its resource ceiling")
	}

	documents := make(map[string]map[string]any, len(selectedArtifacts))
	for artifactPath, artifact := range selectedArtifacts {
		manifestArtifact, ok := manifestArtifact(manifest, artifact.Path)
		if !ok || manifestArtifact.Bytes != artifact.Bytes || !strings.EqualFold(manifestArtifact.SHA256, artifact.SHA256) {
			return nil, errors.New("operation-document evidence is not manifest-bound")
		}
		resolved, ok := releaseArtifactPath(root, artifact.Path)
		if !ok {
			return nil, errors.New("operation-document evidence path is invalid")
		}
		data, err := readBoundedFile(resolved, healthOperationDocumentEvidenceMaxBytes)
		if err != nil || int64(len(data)) != artifact.Bytes || !healthOperationPlanDigestMatches(artifact.SHA256, data) {
			return nil, errors.New("operation-document evidence bytes are unavailable or altered")
		}
		document, err := decodeHealthOperationDocumentEvidence(data)
		if err != nil {
			return nil, errors.New("operation-document evidence contract is invalid")
		}
		if err := validateHealthOperationDocumentEvidenceIdentity(document, plan); err != nil {
			return nil, err
		}
		documents[artifactPath] = document
	}

	for _, ref := range refs {
		if ref.EvidenceKind != "operation_document" {
			continue
		}
		document := documents[ref.ArtifactPath]
		if document == nil {
			return nil, errors.New("operation-document evidence was not loaded")
		}
		if _, ok := healthJSONPointer(document, ref.JSONPointer); !ok {
			return nil, errors.New("operation-document evidence pointer does not resolve")
		}
	}

	if plan.RequestPlan.RequestContract != nil && plan.RequestPlan.RequestContract.Transport.Authority == "operation_document" {
		if err := validateHealthOperationDocumentTransportFacts(plan, refs, documents); err != nil {
			return nil, err
		}
	}
	return documents, nil
}

func healthOperationPlanEvidenceRefs(plan healthOperationPlanRecord) ([]healthOperationPlanEvidenceRef, error) {
	contract := plan.RequestPlan.RequestContract
	capacity := len(plan.RequestPlan.EvidenceRefs) + len(plan.RuntimeBinding.EvidenceRefs) + len(plan.Admission.EvidenceRefs)
	if plan.LegacyPolicy != nil {
		capacity++
	}
	if contract != nil {
		capacity += len(contract.Transport.EvidenceRefs) + len(contract.OperationEffect.EvidenceRefs) + len(contract.ParameterInventoryEvidenceRefs)
		capacity += len(contract.Authentication.EvidenceRefs) + len(contract.Limits.EvidenceRefs) + len(contract.ResponseAssertion.EvidenceRefs)
		for _, parameter := range contract.Parameters {
			capacity += len(parameter.EvidenceRefs)
			if parameter.ValueStrategy.ValueRef != nil {
				capacity++
			}
		}
	}
	for _, quota := range plan.RuntimeBinding.QuotaPolicies {
		capacity += len(quota.EvidenceRefs)
	}
	if capacity > healthOperationPlanMaxEvidenceRefsPerRecord {
		return nil, errors.New("operation-plan evidence reference count exceeds its resource ceiling")
	}

	refs := make([]healthOperationPlanEvidenceRef, 0, capacity)
	refs = append(refs, plan.RequestPlan.EvidenceRefs...)
	refs = append(refs, plan.RuntimeBinding.EvidenceRefs...)
	refs = append(refs, plan.Admission.EvidenceRefs...)
	if plan.LegacyPolicy != nil {
		refs = append(refs, plan.LegacyPolicy.PolicyRef)
	}
	if contract != nil {
		refs = append(refs, contract.Transport.EvidenceRefs...)
		refs = append(refs, contract.OperationEffect.EvidenceRefs...)
		refs = append(refs, contract.ParameterInventoryEvidenceRefs...)
		refs = append(refs, contract.Authentication.EvidenceRefs...)
		refs = append(refs, contract.Limits.EvidenceRefs...)
		refs = append(refs, contract.ResponseAssertion.EvidenceRefs...)
		for _, parameter := range contract.Parameters {
			refs = append(refs, parameter.EvidenceRefs...)
			if parameter.ValueStrategy.ValueRef != nil {
				refs = append(refs, *parameter.ValueStrategy.ValueRef)
			}
		}
	}
	for _, quota := range plan.RuntimeBinding.QuotaPolicies {
		refs = append(refs, quota.EvidenceRefs...)
	}
	return refs, nil
}

func decodeHealthOperationDocumentEvidence(data []byte) (map[string]any, error) {
	if len(data) == 0 || int64(len(data)) > healthOperationDocumentEvidenceMaxBytes {
		return nil, errors.New("operation-document evidence size is invalid")
	}
	if err := preflightHealthOperationPlanJSON(data); err != nil {
		return nil, err
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	var schema *jsonschema.Schema
	var err error
	switch envelope.SchemaVersion {
	case "datapan.operation-document-evidence.v1":
		schema, err = healthOperationDocumentEvidenceJSONSchema()
	case "datapan.operation-document-evidence.v2":
		schema, err = healthOperationDocumentEvidenceV2JSONSchema()
	default:
		return nil, errors.New("operation-document evidence schema version is unsupported")
	}
	if err != nil {
		return nil, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(instance); err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func healthOperationDocumentEvidenceJSONSchema() (*jsonschema.Schema, error) {
	healthOperationDocumentSchemaOnce.Do(func() {
		if !healthOperationPlanDigestMatches(healthOperationDocumentEvidenceSHA256, embeddedHealthOperationDocumentEvidenceSchema) {
			healthOperationDocumentSchemaErr = errors.New("embedded operation-document evidence schema digest mismatch")
			return
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(embeddedHealthOperationDocumentEvidenceSchema))
		if err != nil {
			healthOperationDocumentSchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(healthOperationDocumentEvidenceSchemaID, document); err != nil {
			healthOperationDocumentSchemaErr = err
			return
		}
		healthOperationDocumentSchema, healthOperationDocumentSchemaErr = compiler.Compile(healthOperationDocumentEvidenceSchemaID)
	})
	return healthOperationDocumentSchema, healthOperationDocumentSchemaErr
}

func healthOperationDocumentEvidenceV2JSONSchema() (*jsonschema.Schema, error) {
	healthOperationDocumentV2SchemaOnce.Do(func() {
		healthOperationDocumentV2Schema, healthOperationDocumentV2SchemaErr = compileHealthPinnedJSONSchema(
			healthOperationDocumentEvidenceV2SchemaID,
			healthOperationDocumentEvidenceV2SHA256,
			embeddedHealthOperationDocumentEvidenceV2Schema,
		)
	})
	return healthOperationDocumentV2Schema, healthOperationDocumentV2SchemaErr
}

func validateHealthOperationDocumentEvidenceIdentity(document map[string]any, plan healthOperationPlanRecord) error {
	identity, ok := document["identity"].(map[string]any)
	if !ok || stringValueFromJSON(identity["operation_id"]) != plan.OperationIdentity.OperationID ||
		stringValueFromJSON(identity["provider"]) != plan.SourceBinding.Provider ||
		stringValueFromJSON(identity["protocol"]) != plan.OperationIdentity.Protocol {
		return errors.New("operation-document evidence identity does not match the selected plan")
	}
	if stringValueFromJSON(document["schema_version"]) == "datapan.operation-document-evidence.v2" && stringValueFromJSON(identity["source_id"]) != plan.SourceBinding.SourceID {
		return errors.New("operation-document evidence source identity does not match the selected plan")
	}
	for _, pair := range [][2]string{
		{"dataset_id", plan.OperationIdentity.DatasetID},
		{"operation_name", plan.OperationIdentity.OperationName},
		{"upstream_operation_key", plan.OperationIdentity.UpstreamOperationKey},
	} {
		if pair[1] != "" && stringValueFromJSON(identity[pair[0]]) != pair[1] {
			return errors.New("operation-document evidence identity does not match the selected plan")
		}
	}
	return nil
}

func validateHealthOperationDocumentTransportFacts(plan healthOperationPlanRecord, refs []healthOperationPlanEvidenceRef, documents map[string]map[string]any) error {
	transport := plan.RequestPlan.RequestContract.Transport
	fields := []struct {
		name     string
		expected string
		caseFold bool
		method   bool
	}{
		{name: "protocol", expected: transport.Protocol, caseFold: true},
		{name: "scheme", expected: transport.Scheme, caseFold: true},
		{name: "host", expected: transport.Host, caseFold: true},
		{name: "path", expected: transport.Path},
		{name: "http_method", expected: transport.HTTPMethod, caseFold: true, method: true},
	}
	if transport.Protocol == "SOAP" {
		fields = append(fields,
			struct {
				name, expected   string
				caseFold, method bool
			}{name: "soap_action", expected: transport.SOAPAction},
			struct {
				name, expected   string
				caseFold, method bool
			}{name: "soap_version", expected: transport.SOAPVersion},
			struct {
				name, expected   string
				caseFold, method bool
			}{name: "envelope_namespace", expected: transport.EnvelopeNamespace},
			struct {
				name, expected   string
				caseFold, method bool
			}{name: "body_encoding", expected: transport.BodyEncoding},
		)
	}

	for _, field := range fields {
		pointer := "#/transport/" + field.name
		var target any
		matches := 0
		for _, ref := range transport.EvidenceRefs {
			if ref.EvidenceKind != "operation_document" || ref.JSONPointer != pointer {
				continue
			}
			if document := documents[ref.ArtifactPath]; document != nil {
				if value, ok := healthJSONPointer(document, ref.JSONPointer); ok {
					target = value
					matches++
				}
			}
		}
		if matches != 1 {
			return fmt.Errorf("operation-document transport fact %s is not uniquely bound", field.name)
		}
		fact, ok := target.(map[string]any)
		factStatus := ""
		if ok {
			factStatus = stringValueFromJSON(fact["status"])
		}
		statusAccepted := factStatus == "documented"
		// Protocol is part of the signed Registry operation identity. The source
		// manifest may be its authority while the document sidecar independently
		// binds the concrete request transport.
		if field.name == "protocol" && factStatus == "registered_manifest" {
			statusAccepted = true
		}
		if !ok || !statusAccepted {
			return fmt.Errorf("operation-document transport fact %s is not documented", field.name)
		}
		actual := stringValueFromJSON(fact["value"])
		if field.expected == "" || actual == "" {
			return fmt.Errorf("operation-document transport fact %s is incomplete", field.name)
		}
		if field.caseFold {
			if !strings.EqualFold(actual, field.expected) {
				return fmt.Errorf("operation-document transport fact %s differs from the selected plan", field.name)
			}
		} else if actual != field.expected {
			return fmt.Errorf("operation-document transport fact %s differs from the selected plan", field.name)
		}
		if field.method && stringValueFromJSON(fact["authority_scope"]) != "operation_specific" {
			return errors.New("operation-document HTTP method is not operation-specific")
		}
	}
	if transport.Protocol == "SOAP" {
		pointer := "#/transport/operation_qname"
		var target any
		matches := 0
		for _, ref := range transport.EvidenceRefs {
			if ref.EvidenceKind == "operation_document" && ref.JSONPointer == pointer {
				if document := documents[ref.ArtifactPath]; document != nil {
					if value, ok := healthJSONPointer(document, ref.JSONPointer); ok {
						target = value
						matches++
					}
				}
			}
		}
		fact, ok := target.(map[string]any)
		if matches != 1 || !ok || stringValueFromJSON(fact["status"]) != "documented" {
			return errors.New("operation-document SOAP operation QName is not uniquely documented")
		}
		actual := stringValueFromJSON(fact["value"])
		if actual == "" || actual != "{"+transport.OperationQName.Namespace+"}"+transport.OperationQName.LocalName {
			return errors.New("operation-document SOAP operation QName differs from the selected plan")
		}
	}
	return nil
}

func validHealthOperationDocumentPointer(pointer string) bool {
	if pointer == "#" {
		return true
	}
	return strings.HasPrefix(pointer, "#/") && !strings.ContainsAny(pointer, "\r\n\x00")
}

func healthJSONPointer(document any, pointer string) (any, bool) {
	if !validHealthOperationDocumentPointer(pointer) {
		return nil, false
	}
	if pointer == "#" {
		return document, true
	}
	pathText := strings.TrimPrefix(pointer, "#")
	for _, encoded := range strings.Split(strings.TrimPrefix(pathText, "/"), "/") {
		segment, ok := decodeHealthJSONPointerSegment(encoded)
		if !ok {
			return nil, false
		}
		switch current := document.(type) {
		case map[string]any:
			value, exists := current[segment]
			if !exists {
				return nil, false
			}
			document = value
		case []any:
			if segment == "" || (len(segment) > 1 && segment[0] == '0') {
				return nil, false
			}
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(current) {
				return nil, false
			}
			document = current[index]
		default:
			return nil, false
		}
	}
	return document, true
}

func decodeHealthJSONPointerSegment(value string) (string, bool) {
	var out strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '~' {
			out.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) {
			return "", false
		}
		index++
		switch value[index] {
		case '0':
			out.WriteByte('~')
		case '1':
			out.WriteByte('/')
		default:
			return "", false
		}
	}
	return out.String(), true
}

func stringValueFromJSON(value any) string {
	text, _ := value.(string)
	return text
}
