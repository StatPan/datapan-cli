package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const healthOperationResponseAssertionArtifactPathPrefix = "reports/operation-response-assertions/"

type healthOperationResponseAssertionV2Artifact struct {
	SchemaVersion string `json:"schema_version"`
	ArtifactKind  string `json:"artifact_kind"`
	SourceBinding struct {
		SourceID string `json:"source_id"`
		Provider string `json:"provider"`
		Protocol string `json:"protocol"`
	} `json:"source_binding"`
	OperationIdentity struct {
		OperationID          string `json:"operation_id"`
		DatasetID            string `json:"dataset_id"`
		OperationName        string `json:"operation_name"`
		UpstreamOperationKey string `json:"upstream_operation_key"`
	} `json:"operation_identity"`
	DocumentEvidence healthOperationPlanArtifactRef         `json:"document_evidence"`
	Review           map[string]any                         `json:"review"`
	Assertion        healthOperationResponseAssertionV2Body `json:"assertion"`
}

type healthOperationResponseAssertionV2Body struct {
	Mode        string                               `json:"mode,omitempty"`
	PayloadKind string                               `json:"payload_kind"`
	Branches    []healthOperationResponseAssertionV2 `json:"branches"`
}

type healthOperationResponseAssertionV2 struct {
	ID                             string                                        `json:"branch_id"`
	Classification                 string                                        `json:"classification"`
	EmptyResultSemantics           string                                        `json:"empty_result_semantics"`
	Selector                       healthOperationResponseAssertionV2Selector    `json:"selector"`
	HTTPStatusSourceRefs           []healthOperationPlanEvidenceRef              `json:"http_status_source_refs"`
	RequiredFields                 []healthOperationResponseAssertionV2Field     `json:"required_fields"`
	ProviderResultCodeStatus       string                                        `json:"provider_result_code_status"`
	ProviderResultCodeEvidenceRefs []healthOperationPlanEvidenceRef              `json:"provider_result_code_evidence_refs"`
	ProviderResultCodes            *healthOperationResponseAssertionV2Codes      `json:"provider_result_codes,omitempty"`
	ResultCollection               *healthOperationResponseAssertionV2Collection `json:"result_collection,omitempty"`
	SourceRefs                     []healthOperationPlanEvidenceRef              `json:"source_refs"`
	ReviewRefs                     []healthOperationPlanEvidenceRef              `json:"review_refs"`
}

type healthOperationResponseAssertionV2Selector struct {
	AcceptedHTTPStatusCodes []int                                             `json:"accepted_http_status_codes"`
	RootKind                string                                            `json:"root_kind"`
	RootQName               *healthOperationPlanQName                         `json:"root_qname,omitempty"`
	Discriminators          []healthOperationResponseAssertionV2Discriminator `json:"discriminators"`
}

type healthOperationResponseAssertionV2Path struct {
	Kind     string `json:"kind"`
	Value    string `json:"value,omitempty"`
	Segments []struct {
		Namespace string `json:"namespace"`
		LocalName string `json:"local_name"`
	} `json:"segments,omitempty"`
}

type healthOperationResponseAssertionV2Discriminator struct {
	Path       healthOperationResponseAssertionV2Path `json:"path"`
	Predicate  string                                 `json:"predicate"`
	ValueType  string                                 `json:"value_type,omitempty"`
	Values     []json.RawMessage                      `json:"values,omitempty"`
	SourceRefs []healthOperationPlanEvidenceRef       `json:"source_refs"`
}

type healthOperationResponseAssertionV2Cardinality struct {
	Minimum int             `json:"minimum"`
	Maximum json.RawMessage `json:"maximum"`
}

type healthOperationResponseAssertionV2Field struct {
	Path        healthOperationResponseAssertionV2Path        `json:"path"`
	ValueType   string                                        `json:"value_type"`
	Cardinality healthOperationResponseAssertionV2Cardinality `json:"cardinality"`
	SourceRefs  []healthOperationPlanEvidenceRef              `json:"source_refs"`
}

type healthOperationResponseAssertionV2Codes struct {
	Path          healthOperationResponseAssertionV2Path         `json:"path"`
	ValueType     string                                         `json:"value_type"`
	Basis         string                                         `json:"basis"`
	SuccessValues []json.RawMessage                              `json:"success_values,omitempty"`
	ErrorValues   []json.RawMessage                              `json:"error_values,omitempty"`
	SourceRefs    []healthOperationPlanEvidenceRef               `json:"source_refs"`
	ErrorClasses  []healthOperationResponseAssertionV2ErrorClass `json:"error_classes,omitempty"`
}

type healthOperationResponseAssertionV2ErrorClass struct {
	Value      json.RawMessage                  `json:"value"`
	Category   string                           `json:"category"`
	SourceRefs []healthOperationPlanEvidenceRef `json:"source_refs"`
}

type healthOperationResponseAssertionV2Collection struct {
	Path                 *healthOperationResponseAssertionV2Path        `json:"path"`
	ContainerPath        healthOperationResponseAssertionV2Path         `json:"container_path"`
	ItemPath             *healthOperationResponseAssertionV2Path        `json:"item_path"`
	ContainerCardinality *healthOperationResponseAssertionV2Cardinality `json:"container_cardinality,omitempty"`
	ValueType            string                                         `json:"value_type"`
	Semantics            string                                         `json:"semantics"`
	SourceRefs           []healthOperationPlanEvidenceRef               `json:"source_refs"`
}

func loadSelectedHealthResponseAssertion(root string, plan healthOperationPlanRecord, index healthOperationPlanIndex, manifest releaseManifest, documents map[string]map[string]any, selectedPolicy healthSelectedOperationPolicy) (healthNormalizedResponseAssertion, error) {
	contract := plan.RequestPlan.RequestContract
	if contract == nil {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion has no request contract")
	}
	response := contract.ResponseAssertion
	if response.Kind != "json_contract" && response.Kind != "xml_contract" && response.Kind != "soap_fault_free" && response.Kind != "observation_only" {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion kind is unsupported")
	}
	assertionPath := healthOperationResponseAssertionArtifactPathPrefix + plan.OperationIdentity.OperationID + ".json"
	path, pointer, ok := strings.Cut(response.AssertionRef, "#")
	if !ok || path != assertionPath || pointer != "/assertion" {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion reference is not operation-bound")
	}
	refs := response.EvidenceRefs
	var assertionRef *healthOperationPlanEvidenceRef
	for index := range refs {
		ref := &refs[index]
		if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath == assertionPath && ref.JSONPointer == "#/assertion" {
			if assertionRef != nil {
				return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact binding is duplicated")
			}
			assertionRef = ref
		}
	}
	if assertionRef == nil || !validSHA256Digest(assertionRef.SHA256) {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact is not bound by its plan")
	}
	artifact, ok := manifestArtifact(manifest, assertionPath)
	if !ok || artifact.Bytes < 1 || artifact.Bytes > healthOperationResponseAssertionMaxBytes || !strings.EqualFold(artifact.SHA256, assertionRef.SHA256) {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact is not bounded by its release manifest")
	}
	resolved, ok := releaseArtifactPath(root, assertionPath)
	if !ok {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact path is invalid")
	}
	data, err := readBoundedFile(resolved, healthOperationResponseAssertionMaxBytes)
	if err != nil || int64(len(data)) != artifact.Bytes || !healthOperationPlanDigestMatches(artifact.SHA256, data) {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact is unavailable or altered")
	}
	if err := preflightHealthOperationPlanJSON(data); err != nil {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact JSON is invalid")
	}
	schema, err := healthOperationResponseAssertionJSONSchema()
	if err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil || schema.Validate(instance) != nil {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact does not match its pinned schema")
	}
	var artifactValue healthOperationResponseAssertionV2Artifact
	if err := json.Unmarshal(data, &artifactValue); err != nil {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact cannot be decoded")
	}
	var artifactDocument map[string]any
	if err := json.Unmarshal(data, &artifactDocument); err != nil {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact pointers cannot be decoded")
	}
	assertionPointerCount, reviewPointerCount := 0, 0
	for _, ref := range refs {
		if ref.ArtifactPath != assertionPath {
			continue
		}
		if ref.EvidenceKind != "reviewed_policy" || !strings.EqualFold(ref.SHA256, artifact.SHA256) {
			return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact evidence is not manifest-bound")
		}
		switch ref.JSONPointer {
		case "#/assertion":
			assertionPointerCount++
		case "#/review":
			reviewPointerCount++
		default:
			return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact pointer is unsupported")
		}
		if _, ok := healthJSONPointer(artifactDocument, ref.JSONPointer); !ok {
			return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact pointer does not resolve")
		}
	}
	if assertionPointerCount != 1 || reviewPointerCount > 1 {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion artifact pointers are duplicated or incomplete")
	}
	if artifactValue.SchemaVersion != "datapan.operation-response-assertion.v2" || artifactValue.ArtifactKind != "operation_response_assertion" ||
		artifactValue.SourceBinding.SourceID != plan.SourceBinding.SourceID || artifactValue.SourceBinding.Provider != plan.SourceBinding.Provider || artifactValue.SourceBinding.Protocol != plan.OperationIdentity.Protocol ||
		artifactValue.OperationIdentity.OperationID != plan.OperationIdentity.OperationID || artifactValue.OperationIdentity.DatasetID != plan.OperationIdentity.DatasetID ||
		artifactValue.OperationIdentity.OperationName != plan.OperationIdentity.OperationName || artifactValue.OperationIdentity.UpstreamOperationKey != plan.OperationIdentity.UpstreamOperationKey {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion identity does not match its selected operation")
	}
	if err := validateHealthResponseAssertionDocumentEvidence(artifactValue.DocumentEvidence, plan, index, manifest, documents); err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	if response.Kind == "observation_only" {
		if artifactValue.Assertion.Mode != "observation_only" || artifactValue.Assertion.PayloadKind != "" || len(artifactValue.Assertion.Branches) != 0 {
			return healthNormalizedResponseAssertion{}, errors.New("observation-only assertion artifact contains response predicates")
		}
	} else if artifactValue.Assertion.Mode != "" {
		return healthNormalizedResponseAssertion{}, errors.New("typed assertion artifact has an observation-only mode")
	}
	if err := validateHealthResponseAssertionReferences(plan.OperationIdentity.OperationID, artifactValue.Assertion.Branches, refs, documents, manifest, selectedPolicy); err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	if err := validateHealthResponseAssertionPolicyBindings(plan, index, manifest, artifactValue, selectedPolicy); err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	assertion, err := normalizeHealthOperationResponseAssertionV2(artifactValue.Assertion)
	if err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	if err := validateHealthOperationResponseAssertionPlanProjection(plan, assertion); err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	if err := validateHealthNormalizedResponseAssertion(assertion); err != nil {
		return healthNormalizedResponseAssertion{}, err
	}
	return assertion, nil
}

func validateHealthResponseAssertionDocumentEvidence(ref healthOperationPlanArtifactRef, plan healthOperationPlanRecord, index healthOperationPlanIndex, manifest releaseManifest, documents map[string]map[string]any) error {
	if ref.Path == "" || ref.Bytes < 1 || ref.Bytes > healthOperationDocumentEvidenceMaxBytes || !validSHA256Digest(ref.SHA256) || documents[ref.Path] == nil {
		return errors.New("response assertion document evidence is not a selected bounded document")
	}
	indexFound := false
	for _, indexed := range index.GenerationInputs.DocumentEvidence {
		if indexed.Path == ref.Path && indexed.Bytes == ref.Bytes && strings.EqualFold(indexed.SHA256, ref.SHA256) {
			indexFound = true
			break
		}
	}
	manifestRef, manifestFound := manifestArtifact(manifest, ref.Path)
	if !indexFound || !manifestFound || manifestRef.Bytes != ref.Bytes || !strings.EqualFold(manifestRef.SHA256, ref.SHA256) {
		return errors.New("response assertion document evidence is not manifest-bound")
	}
	planRefFound := false
	for _, planRef := range plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs {
		if planRef.EvidenceKind == "operation_document" && planRef.ArtifactPath == ref.Path && strings.EqualFold(planRef.SHA256, ref.SHA256) {
			planRefFound = true
			break
		}
	}
	if !planRefFound {
		return errors.New("response assertion document evidence is not included in its plan")
	}
	return nil
}

func validateHealthResponseAssertionReferences(operationID string, branches []healthOperationResponseAssertionV2, planRefs []healthOperationPlanEvidenceRef, documents map[string]map[string]any, manifest releaseManifest, selectedPolicy healthSelectedOperationPolicy) error {
	planRefSet := make(map[healthOperationPlanEvidenceRef]struct{}, len(planRefs))
	for _, ref := range planRefs {
		planRefSet[ref] = struct{}{}
	}
	for _, branch := range branches {
		refs := make([]healthOperationPlanEvidenceRef, 0, len(branch.SourceRefs)+len(branch.HTTPStatusSourceRefs)+len(branch.ProviderResultCodeEvidenceRefs)+len(branch.ReviewRefs))
		refs = append(refs, branch.SourceRefs...)
		refs = append(refs, branch.HTTPStatusSourceRefs...)
		refs = append(refs, branch.ProviderResultCodeEvidenceRefs...)
		refs = append(refs, branch.ReviewRefs...)
		for _, field := range branch.RequiredFields {
			refs = append(refs, field.SourceRefs...)
		}
		for _, discriminator := range branch.Selector.Discriminators {
			refs = append(refs, discriminator.SourceRefs...)
		}
		if branch.ProviderResultCodes != nil {
			refs = append(refs, branch.ProviderResultCodes.SourceRefs...)
			for _, class := range branch.ProviderResultCodes.ErrorClasses {
				refs = append(refs, class.SourceRefs...)
			}
		}
		if branch.ResultCollection != nil {
			refs = append(refs, branch.ResultCollection.SourceRefs...)
		}
		for _, ref := range refs {
			if _, ok := planRefSet[ref]; !ok {
				return errors.New("response assertion evidence is omitted from the selected plan")
			}
			artifact, ok := manifestArtifact(manifest, ref.ArtifactPath)
			if !ok || !strings.EqualFold(artifact.SHA256, ref.SHA256) || artifact.Bytes < 1 {
				return errors.New("response assertion evidence is not manifest-bound")
			}
			switch ref.EvidenceKind {
			case "operation_document":
				document := documents[ref.ArtifactPath]
				if document == nil || !validHealthOperationDocumentPointer(ref.JSONPointer) {
					return errors.New("response assertion source evidence is not a selected operation document")
				}
				if _, ok := healthJSONPointer(document, ref.JSONPointer); !ok {
					return errors.New("response assertion source evidence pointer does not resolve")
				}
			case "reviewed_policy":
				if ref.ArtifactPath == healthOperationResponseAssertionArtifactPathPrefix+operationID+".json" {
					if ref.JSONPointer != "#/assertion" {
						return errors.New("response assertion self-reference pointer is invalid")
					}
					continue
				}
				if _, ok := selectedPolicy.resolve(ref.JSONPointer); !ok {
					return errors.New("response assertion review evidence pointer does not resolve in the selected policy")
				}
			default:
				return errors.New("response assertion evidence kind is unsupported")
			}
		}
	}
	return nil
}

func validateHealthOperationResponseAssertionPlanProjection(plan healthOperationPlanRecord, assertion healthNormalizedResponseAssertion) error {
	if plan.RequestPlan.RequestContract == nil {
		return errors.New("plan request contract is unavailable")
	}
	response := plan.RequestPlan.RequestContract.ResponseAssertion
	if response.Kind == "observation_only" {
		if !assertion.ObservationOnly || response.EmptyResultSemantics != "not_applicable" || len(response.ExpectedStatusCodes) != 0 {
			return errors.New("observation-only plan projection differs from its reviewed artifact")
		}
		return nil
	}
	if assertion.ObservationOnly {
		return errors.New("typed response plan projects an observation-only artifact")
	}
	statuses := make(map[int]struct{})
	semantics := ""
	for _, branch := range assertion.Branches {
		for _, status := range branch.AcceptedHTTPStatusCodes {
			statuses[status] = struct{}{}
		}
		if branch.Classification == "success" {
			if semantics != "" && semantics != branch.EmptyResultSemantics {
				return errors.New("successful response branches disagree on empty-result semantics")
			}
			semantics = branch.EmptyResultSemantics
		}
	}
	if semantics == "" || response.EmptyResultSemantics != semantics || len(statuses) != len(response.ExpectedStatusCodes) {
		return errors.New("plan response assertion projection differs from reviewed branches")
	}
	for _, status := range response.ExpectedStatusCodes {
		if _, ok := statuses[status]; !ok {
			return errors.New("plan response status projection differs from reviewed branches")
		}
	}
	payloadKind := ""
	for _, branch := range assertion.Branches {
		if payloadKind != "" && payloadKind != branch.PayloadKind {
			return errors.New("response assertion branches mix payload kinds")
		}
		payloadKind = branch.PayloadKind
	}
	kind := map[string]string{"json": "json_contract", "xml": "xml_contract", "soap_xml": "soap_fault_free"}[payloadKind]
	if kind == "" || response.Kind != kind {
		return errors.New("plan response assertion kind differs from reviewed artifact")
	}
	return nil
}

func normalizeHealthOperationResponseAssertionV2(wire healthOperationResponseAssertionV2Body) (healthNormalizedResponseAssertion, error) {
	if wire.Mode == "observation_only" {
		if wire.PayloadKind != "" || len(wire.Branches) != 0 {
			return healthNormalizedResponseAssertion{}, errors.New("observation-only assertion has typed predicates")
		}
		return healthNormalizedResponseAssertion{ObservationOnly: true}, nil
	}
	if wire.Mode != "" {
		return healthNormalizedResponseAssertion{}, errors.New("response assertion mode is unsupported")
	}
	assertion := healthNormalizedResponseAssertion{}
	for _, branch := range wire.Branches {
		normalized := healthNormalizedResponseBranch{
			ID: branch.ID, Classification: branch.Classification, EmptyResultSemantics: branch.EmptyResultSemantics,
			PayloadKind: wire.PayloadKind, AcceptedHTTPStatusCodes: append([]int(nil), branch.Selector.AcceptedHTTPStatusCodes...),
			RootKind: branch.Selector.RootKind,
		}
		if branch.Selector.RootQName != nil {
			normalized.RootQName = xml.Name{Space: branch.Selector.RootQName.Namespace, Local: branch.Selector.RootQName.LocalName}
		}
		for _, discriminator := range branch.Selector.Discriminators {
			path, err := normalizeHealthOperationResponsePath(discriminator.Path)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			values, err := normalizeHealthOperationResponseScalars(discriminator.Values, discriminator.ValueType)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			normalized.Discriminators = append(normalized.Discriminators, healthNormalizedResponseDiscriminator{
				Path: path, Predicate: discriminator.Predicate, ValueType: discriminator.ValueType, Values: values,
			})
		}
		for _, field := range branch.RequiredFields {
			path, err := normalizeHealthOperationResponsePath(field.Path)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			minimum, maximum, err := normalizeHealthOperationResponseCardinality(field.Cardinality)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			normalized.RequiredFields = append(normalized.RequiredFields, healthNormalizedResponseField{Path: path, ValueType: field.ValueType, MinimumCount: minimum, MaximumCount: maximum})
		}
		switch branch.ProviderResultCodeStatus {
		case "documented", "expected_success_example":
			if branch.ProviderResultCodes == nil {
				return healthNormalizedResponseAssertion{}, errors.New("response provider-code contract is missing")
			}
			normalized.ProviderResultCodeMode = branch.ProviderResultCodeStatus
			var err error
			normalized.ProviderResultCodePath, err = normalizeHealthOperationResponsePath(branch.ProviderResultCodes.Path)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			normalized.ProviderResultCodeType = branch.ProviderResultCodes.ValueType
			codeValues := branch.ProviderResultCodes.SuccessValues
			if branch.Classification == "provider_error" {
				codeValues = branch.ProviderResultCodes.ErrorValues
			}
			normalized.ProviderResultCodeValues, err = normalizeHealthOperationResponseScalars(codeValues, branch.ProviderResultCodes.ValueType)
			if err != nil {
				return healthNormalizedResponseAssertion{}, err
			}
			for _, class := range branch.ProviderResultCodes.ErrorClasses {
				value, err := normalizeHealthOperationResponseScalarAs(class.Value, branch.ProviderResultCodes.ValueType)
				if err != nil {
					return healthNormalizedResponseAssertion{}, errors.New("response provider error class value has an invalid type")
				}
				normalized.ErrorClasses = append(normalized.ErrorClasses, healthNormalizedProviderErrorClass{Category: class.Category, Values: []healthAssertionScalar{value}})
			}
		case "not_applicable", "none_by_policy":
			if branch.ProviderResultCodes != nil {
				return healthNormalizedResponseAssertion{}, errors.New("code-free response branch contains provider-code values")
			}
			normalized.ProviderResultCodeMode = "none"
		default:
			return healthNormalizedResponseAssertion{}, errors.New("response provider-code status is unsupported")
		}
		if branch.ResultCollection != nil {
			if branch.ResultCollection.ValueType != "array" {
				return healthNormalizedResponseAssertion{}, errors.New("response collection node is not typed as an array")
			}
			collection := &healthNormalizedResponseCollection{EmptySemantics: branch.ResultCollection.Semantics}
			switch wire.PayloadKind {
			case "json":
				if branch.ResultCollection.Path == nil || branch.ResultCollection.Path.Kind != "json_pointer" || branch.ResultCollection.ItemPath != nil || branch.ResultCollection.ContainerCardinality != nil {
					return healthNormalizedResponseAssertion{}, errors.New("JSON response collection uses unsupported selectors")
				}
				collection.JSONPointer = branch.ResultCollection.Path.Value
			case "xml", "soap_xml":
				if branch.ResultCollection.Path != nil || branch.ResultCollection.ItemPath == nil || branch.ResultCollection.ContainerCardinality == nil {
					return healthNormalizedResponseAssertion{}, errors.New("XML response collection selectors are incomplete")
				}
				containerMinimum, containerMaximum, err := normalizeHealthOperationResponseCardinality(*branch.ResultCollection.ContainerCardinality)
				if err != nil || containerMinimum != 1 || containerMaximum != 1 {
					return healthNormalizedResponseAssertion{}, errors.New("XML response collection container is not exactly one node")
				}
				collection.XMLContainer, err = normalizeHealthOperationXMLPath(branch.ResultCollection.ContainerPath)
				if err != nil {
					return healthNormalizedResponseAssertion{}, err
				}
				collection.XMLItemPath, err = normalizeHealthOperationXMLPath(*branch.ResultCollection.ItemPath)
				if err != nil {
					return healthNormalizedResponseAssertion{}, err
				}
			default:
				return healthNormalizedResponseAssertion{}, errors.New("response collection payload kind is unsupported")
			}
			normalized.ResultCollection = collection
		}
		assertion.Branches = append(assertion.Branches, normalized)
	}
	return assertion, nil
}

func normalizeHealthOperationResponsePath(path healthOperationResponseAssertionV2Path) (healthNormalizedResponsePath, error) {
	switch path.Kind {
	case "json_pointer":
		if len(path.Segments) != 0 || !validHealthOperationDocumentPointer(path.Value) {
			return healthNormalizedResponsePath{}, errors.New("JSON response selector is invalid")
		}
		return healthNormalizedResponsePath{JSONPointer: path.Value}, nil
	case "xml_qname_path":
		if path.Value != "" {
			return healthNormalizedResponsePath{}, errors.New("XML response selector contains a JSON value")
		}
		segments, err := normalizeHealthOperationXMLPath(path)
		if err != nil {
			return healthNormalizedResponsePath{}, err
		}
		return healthNormalizedResponsePath{XMLPath: segments}, nil
	default:
		return healthNormalizedResponsePath{}, errors.New("response selector kind is unsupported")
	}
}

func normalizeHealthOperationXMLPath(path healthOperationResponseAssertionV2Path) ([]xml.Name, error) {
	if path.Kind != "xml_qname_path" || len(path.Segments) == 0 || len(path.Segments) > healthOperationPlanMaxJSONDepth {
		return nil, errors.New("XML response selector path is invalid")
	}
	segments := make([]xml.Name, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, xml.Name{Space: segment.Namespace, Local: segment.LocalName})
	}
	if !healthXMLPathValid(segments) {
		return nil, errors.New("XML response selector QName is invalid")
	}
	return segments, nil
}

func normalizeHealthOperationResponseCardinality(cardinality healthOperationResponseAssertionV2Cardinality) (int, int, error) {
	if cardinality.Minimum < 1 || cardinality.Minimum > healthResponseAssertionMaxSelectedNodes || len(cardinality.Maximum) == 0 {
		return 0, 0, errors.New("response selected-node cardinality is outside its ceiling")
	}
	maximum := healthResponseAssertionMaxSelectedNodes
	if string(cardinality.Maximum) != "null" {
		if err := json.Unmarshal(cardinality.Maximum, &maximum); err != nil {
			return 0, 0, errors.New("response selected-node maximum is invalid")
		}
	}
	if maximum < cardinality.Minimum || maximum > healthResponseAssertionMaxSelectedNodes {
		return 0, 0, errors.New("response selected-node cardinality is outside its ceiling")
	}
	return cardinality.Minimum, maximum, nil
}

func normalizeHealthOperationResponseScalars(rawValues []json.RawMessage, valueType string) ([]healthAssertionScalar, error) {
	if len(rawValues) > healthResponseAssertionMaxPredicates {
		return nil, errors.New("response scalar value set exceeds its ceiling")
	}
	values := make([]healthAssertionScalar, 0, len(rawValues))
	for _, raw := range rawValues {
		value, err := normalizeHealthOperationResponseScalarAs(raw, valueType)
		if err != nil {
			return nil, errors.New("response scalar value does not match its declared type")
		}
		values = append(values, value)
	}
	return values, nil
}

func normalizeHealthOperationResponseScalarAs(raw json.RawMessage, expectedType string) (healthAssertionScalar, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return healthAssertionScalar{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return healthAssertionScalar{}, errors.New("response scalar has trailing JSON")
	}
	switch typed := value.(type) {
	case string:
		if expectedType != "string" || typed == "" || len(typed) > 512 {
			return healthAssertionScalar{}, errors.New("response scalar string is outside its ceiling")
		}
		return healthAssertionScalar{ValueType: "string", Value: typed}, nil
	case bool:
		if expectedType != "boolean" {
			return healthAssertionScalar{}, errors.New("response scalar boolean type differs from its declaration")
		}
		if typed {
			return healthAssertionScalar{ValueType: "boolean", Value: "true"}, nil
		}
		return healthAssertionScalar{ValueType: "boolean", Value: "false"}, nil
	case json.Number:
		if expectedType != "integer" && expectedType != "number" {
			return healthAssertionScalar{}, errors.New("response scalar numeric type differs from its declaration")
		}
		decimal, ok := healthParseDecimal(typed.String(), false, false)
		if !ok || expectedType == "integer" && !decimal.isInteger() {
			return healthAssertionScalar{}, errors.New("response scalar number is invalid")
		}
		return healthAssertionScalar{ValueType: expectedType, Value: typed.String()}, nil
	default:
		return healthAssertionScalar{}, fmt.Errorf("response scalar has unsupported JSON type %T", value)
	}
}

func validSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validateHealthOperationResponseAssertionArtifactSchema(data []byte) error {
	if err := preflightHealthOperationPlanJSON(data); err != nil {
		return err
	}
	schema, err := healthOperationResponseAssertionJSONSchema()
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return schema.Validate(instance)
}
