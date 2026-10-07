package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This normalized evaluator is deliberately independent of the Registry wire
// artifact. A future decoder may construct this value only after validating
// the manifest-bound assertion artifact, its source identity, and all evidence
// references. No response bytes or extracted values leave this package.
type healthNormalizedResponseAssertion struct {
	ObservationOnly         bool
	PayloadKind             string
	AcceptedHTTPStatusCodes []int
	SOAPEnvelopeNamespace   string
	RequiredFields          []healthNormalizedResponseField
	ProviderResultCodeMode  string
	ProviderResultCodePath  healthNormalizedResponsePath
	ProviderResultCodeType  string
	ProviderSuccessCodes    []healthAssertionScalar
	ProviderErrorCodes      []healthAssertionScalar
	ResultCollection        *healthNormalizedResponseCollection
	Branches                []healthNormalizedResponseBranch
}

// Branches are used by the Registry-owned response-assertion union. Wire
// decoding must bind every branch selector and predicate to the immutable
// assertion artifact and its source/review evidence before constructing these
// values.
type healthNormalizedResponseBranch struct {
	ID                       string
	Classification           string
	EmptyResultSemantics     string
	PayloadKind              string
	AcceptedHTTPStatusCodes  []int
	RootKind                 string
	RootQName                xml.Name
	Discriminators           []healthNormalizedResponseDiscriminator
	RequiredFields           []healthNormalizedResponseField
	ProviderResultCodeMode   string
	ProviderResultCodePath   healthNormalizedResponsePath
	ProviderResultCodeType   string
	ProviderResultCodeValues []healthAssertionScalar
	ErrorClasses             []healthNormalizedProviderErrorClass
	ResultCollection         *healthNormalizedResponseCollection
}

type healthNormalizedResponseDiscriminator struct {
	Path      healthNormalizedResponsePath
	Predicate string
	ValueType string
	Values    []healthAssertionScalar
}

type healthNormalizedProviderErrorClass struct {
	Category string
	Values   []healthAssertionScalar
}

type healthNormalizedResponsePath struct {
	JSONPointer string
	XMLPath     []xml.Name
}

type healthNormalizedResponseField struct {
	Path         healthNormalizedResponsePath
	ValueType    string
	MinimumCount int
	MaximumCount int
}

type healthNormalizedResponseCollection struct {
	JSONPointer    string
	XMLContainer   []xml.Name
	XMLItemPath    []xml.Name
	EmptySemantics string
}

// Scalar values are represented as JSON-native typed values in the Registry
// artifact and normalized to this canonical form by the eventual wire decoder.
// Numeric Value strings are canonical decimal encodings, not source lexemes.
type healthAssertionScalar struct {
	ValueType string
	Value     string
}

type healthResponseAssertionOutcome string

const (
	healthResponseHealthy       healthResponseAssertionOutcome = "healthy"
	healthResponseUnhealthy     healthResponseAssertionOutcome = "unhealthy"
	healthResponseIndeterminate healthResponseAssertionOutcome = "indeterminate"
)

type healthResponseAssertionResult struct {
	Outcome    healthResponseAssertionOutcome
	ReasonCode string
	// ProviderErrorClass is a fixed, evidence-backed category. Generic
	// provider_failure means the exact provider cause is unknown.
	ProviderErrorClass string
}

const (
	healthResponseAssertionMaxPredicates    = 128
	healthResponseAssertionMaxSelectedNodes = healthOperationPlanMaxJSONTokens / 2
	healthResponseXMLMaxNodes               = healthResponseAssertionMaxSelectedNodes
)

var healthXMLIntegerValuePattern = regexp.MustCompile(`^[+-]?[0-9]+$`)
var healthXMLNumberValuePattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// evaluateHealthNormalizedResponseAssertion applies only explicit predicates.
// A nonaccepted status or a documented provider error is unhealthy. A payload
// that cannot be parsed or does not prove the reviewed success shape is
// indeterminate. It never includes response values in an error or result.
func evaluateHealthNormalizedResponseAssertion(assertion healthNormalizedResponseAssertion, response healthHTTPResponse) healthResponseAssertionResult {
	if assertion.ObservationOnly {
		if err := validateHealthNormalizedResponseAssertion(assertion); err != nil {
			return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: "response_assertion_invalid"}
		}
		if response.StatusCode < 100 || response.StatusCode > 599 {
			return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: "response_status_invalid"}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_http_failure"}
		}
		if int64(len(response.Body)) > healthTransportMaxBytes {
			return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: "response_body_limit_exceeded"}
		}
		return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: "response_semantics_unestablished"}
	}
	if len(assertion.Branches) > 0 {
		if !healthNormalizedResponseLegacyFieldsEmpty(assertion) {
			return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: "response_assertion_invalid"}
		}
		return evaluateHealthNormalizedResponseBranches(assertion.Branches, response)
	}
	indeterminate := func(reason string) healthResponseAssertionResult {
		return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: reason}
	}
	unhealthy := func(reason string) healthResponseAssertionResult {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: reason}
	}
	if err := validateHealthNormalizedResponseAssertion(assertion); err != nil {
		return indeterminate("response_assertion_invalid")
	}
	if response.StatusCode < 100 || response.StatusCode > 599 {
		return indeterminate("response_status_invalid")
	}
	if int64(len(response.Body)) > healthTransportMaxBytes {
		return indeterminate("response_body_limit_exceeded")
	}
	if !healthResponseStatusAccepted(assertion.AcceptedHTTPStatusCodes, response.StatusCode) {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_status_not_accepted", ProviderErrorClass: "provider_failure"}
	}

	var document any
	var xmlDocument *healthResponseXMLDocument
	switch assertion.PayloadKind {
	case "json":
		parsed, err := decodeHealthBoundedResponseJSON(response.Body)
		if err != nil {
			return indeterminate("response_payload_invalid")
		}
		document = parsed
	case "xml", "soap_xml":
		parsed, err := decodeHealthBoundedResponseXML(response.Body)
		if err != nil {
			return indeterminate("response_payload_invalid")
		}
		xmlDocument = parsed
		if assertion.PayloadKind == "soap_xml" {
			fault, valid := healthSOAPResponseFault(xmlDocument, assertion.SOAPEnvelopeNamespace)
			if !valid {
				return indeterminate("response_payload_invalid")
			}
			if fault {
				return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_provider_error", ProviderErrorClass: "provider_failure"}
			}
		}
	default:
		return indeterminate("response_assertion_invalid")
	}

	// Evaluate a known provider failure before success-shape requirements. Error
	// envelopes often omit the fields present in successful responses.
	switch assertion.ProviderResultCodeMode {
	case "none", "not_applicable":
		// The eventual Registry decoder must prove these modes from reviewed
		// policy/evidence; their absence is never inferred by this evaluator.
	case "documented", "expected_success_example":
		actual, ok := healthResponseAssertionScalarAt(xmlDocument, document, assertion.PayloadKind, assertion.ProviderResultCodePath, assertion.ProviderResultCodeType)
		if !ok {
			return indeterminate("response_provider_code_unknown")
		}
		if healthAssertionScalarIn(assertion.ProviderErrorCodes, actual) {
			return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_provider_error", ProviderErrorClass: "provider_failure"}
		}
		if !healthAssertionScalarIn(assertion.ProviderSuccessCodes, actual) {
			return indeterminate("response_provider_code_unknown")
		}
	default:
		return indeterminate("response_assertion_invalid")
	}

	for _, field := range assertion.RequiredFields {
		if !healthResponseFieldMatches(xmlDocument, document, assertion.PayloadKind, field) {
			return indeterminate("response_shape_mismatch")
		}
	}
	if collection := assertion.ResultCollection; collection != nil {
		count, exists := healthResponseCollectionCount(xmlDocument, document, assertion.PayloadKind, *collection)
		if !exists {
			return indeterminate("response_shape_mismatch")
		}
		if count == 0 && collection.EmptySemantics == "invalid" {
			return unhealthy("response_collection_empty")
		}
	}
	return healthResponseAssertionResult{Outcome: healthResponseHealthy, ReasonCode: "response_assertion_passed"}
}

func validateHealthNormalizedResponseAssertion(assertion healthNormalizedResponseAssertion) error {
	if assertion.ObservationOnly {
		if !healthNormalizedResponseAssertionOnlyObservation(assertion) {
			return errors.New("observation-only assertion contains response predicates")
		}
		return nil
	}
	if len(assertion.Branches) > 0 {
		if !healthNormalizedResponseLegacyFieldsEmpty(assertion) {
			return errors.New("branch assertion mixes legacy predicates")
		}
		return validateHealthNormalizedResponseBranches(assertion.Branches)
	}
	if assertion.PayloadKind != "json" && assertion.PayloadKind != "xml" && assertion.PayloadKind != "soap_xml" {
		return errors.New("unsupported response payload kind")
	}
	if len(assertion.AcceptedHTTPStatusCodes) == 0 || len(assertion.AcceptedHTTPStatusCodes) > 32 || len(assertion.RequiredFields) > healthResponseAssertionMaxPredicates || len(assertion.ProviderSuccessCodes) > healthResponseAssertionMaxPredicates || len(assertion.ProviderErrorCodes) > healthResponseAssertionMaxPredicates {
		return errors.New("response assertion predicate bounds are invalid")
	}
	statuses := make(map[int]struct{}, len(assertion.AcceptedHTTPStatusCodes))
	for _, status := range assertion.AcceptedHTTPStatusCodes {
		if status < 200 || status >= 300 {
			return errors.New("response assertion status is not a 2xx code")
		}
		if _, duplicate := statuses[status]; duplicate {
			return errors.New("response assertion status is duplicated")
		}
		statuses[status] = struct{}{}
	}
	if assertion.PayloadKind == "soap_xml" {
		if strings.TrimSpace(assertion.SOAPEnvelopeNamespace) == "" {
			return errors.New("SOAP assertion has no envelope namespace")
		}
	} else if assertion.SOAPEnvelopeNamespace != "" {
		return errors.New("non-SOAP assertion has a SOAP namespace")
	}
	if len(assertion.RequiredFields)+len(assertion.ProviderSuccessCodes)+len(assertion.ProviderErrorCodes) > healthResponseAssertionMaxPredicates {
		return errors.New("response assertion predicate count exceeds its ceiling")
	}
	for _, field := range assertion.RequiredFields {
		if field.MinimumCount < 0 || field.MaximumCount < field.MinimumCount || field.MaximumCount > healthResponseAssertionMaxSelectedNodes || !healthResponsePathValid(assertion.PayloadKind, field.Path) || !healthResponseValueTypeSupported(assertion.PayloadKind, field.ValueType) {
			return errors.New("response field predicate is invalid")
		}
	}
	switch assertion.ProviderResultCodeMode {
	case "none", "not_applicable":
		if assertion.ProviderResultCodeType != "" || len(assertion.ProviderSuccessCodes) != 0 || len(assertion.ProviderErrorCodes) != 0 || assertion.ProviderResultCodePath.JSONPointer != "" || len(assertion.ProviderResultCodePath.XMLPath) != 0 {
			return errors.New("code-free response assertion contains code predicates")
		}
	case "documented":
		if !healthResponsePathValid(assertion.PayloadKind, assertion.ProviderResultCodePath) || !healthResponseCodeTypeSupported(assertion.PayloadKind, assertion.ProviderResultCodeType) || len(assertion.ProviderSuccessCodes) == 0 || len(assertion.ProviderErrorCodes) == 0 {
			return errors.New("documented provider-code contract is incomplete")
		}
		if !healthResponseScalarsMatchType(assertion.ProviderSuccessCodes, assertion.ProviderResultCodeType) || !healthResponseScalarsMatchType(assertion.ProviderErrorCodes, assertion.ProviderResultCodeType) || healthResponseScalarSetsOverlap(assertion.ProviderSuccessCodes, assertion.ProviderErrorCodes) {
			return errors.New("documented provider-code sets are invalid")
		}
	case "expected_success_example":
		if !healthResponsePathValid(assertion.PayloadKind, assertion.ProviderResultCodePath) || !healthResponseCodeTypeSupported(assertion.PayloadKind, assertion.ProviderResultCodeType) || len(assertion.ProviderSuccessCodes) != 1 || len(assertion.ProviderErrorCodes) != 0 || !healthResponseScalarsMatchType(assertion.ProviderSuccessCodes, assertion.ProviderResultCodeType) {
			return errors.New("expected-success example is incomplete")
		}
	default:
		return errors.New("provider-code mode is unsupported")
	}
	if assertion.ResultCollection != nil {
		collection := assertion.ResultCollection
		if collection.EmptySemantics != "valid" && collection.EmptySemantics != "invalid" {
			return errors.New("result collection empty semantics are invalid")
		}
		if assertion.PayloadKind == "json" {
			if !validHealthOperationDocumentPointer(collection.JSONPointer) || len(collection.XMLContainer) != 0 || len(collection.XMLItemPath) != 0 {
				return errors.New("JSON result collection path is invalid")
			}
		} else if collection.JSONPointer != "" || !healthXMLPathValid(collection.XMLContainer) || !healthXMLPathValid(collection.XMLItemPath) {
			return errors.New("XML result collection paths are invalid")
		}
	}
	return nil
}

func healthNormalizedResponseLegacyFieldsEmpty(assertion healthNormalizedResponseAssertion) bool {
	return !assertion.ObservationOnly && assertion.PayloadKind == "" && len(assertion.AcceptedHTTPStatusCodes) == 0 && assertion.SOAPEnvelopeNamespace == "" && len(assertion.RequiredFields) == 0 && assertion.ProviderResultCodeMode == "" && assertion.ProviderResultCodePath.JSONPointer == "" && len(assertion.ProviderResultCodePath.XMLPath) == 0 && assertion.ProviderResultCodeType == "" && len(assertion.ProviderSuccessCodes) == 0 && len(assertion.ProviderErrorCodes) == 0 && assertion.ResultCollection == nil
}

func healthNormalizedResponseAssertionOnlyObservation(assertion healthNormalizedResponseAssertion) bool {
	return assertion.ObservationOnly && assertion.PayloadKind == "" && len(assertion.AcceptedHTTPStatusCodes) == 0 && assertion.SOAPEnvelopeNamespace == "" && len(assertion.RequiredFields) == 0 && assertion.ProviderResultCodeMode == "" && assertion.ProviderResultCodePath.JSONPointer == "" && len(assertion.ProviderResultCodePath.XMLPath) == 0 && assertion.ProviderResultCodeType == "" && len(assertion.ProviderSuccessCodes) == 0 && len(assertion.ProviderErrorCodes) == 0 && assertion.ResultCollection == nil && len(assertion.Branches) == 0
}

func evaluateHealthNormalizedResponseBranches(branches []healthNormalizedResponseBranch, response healthHTTPResponse) healthResponseAssertionResult {
	// Reject unlisted HTTP statuses before decoding provider data. For accepted
	// statuses, select exactly one documented branch from its selectors and
	// validate only that branch; shape failure never falls through.
	indeterminate := func(reason string) healthResponseAssertionResult {
		return healthResponseAssertionResult{Outcome: healthResponseIndeterminate, ReasonCode: reason}
	}
	if err := validateHealthNormalizedResponseBranches(branches); err != nil {
		return indeterminate("response_assertion_invalid")
	}
	if response.StatusCode < 100 || response.StatusCode > 599 {
		return indeterminate("response_status_invalid")
	}
	statusCandidate := false
	for _, branch := range branches {
		if healthResponseStatusAccepted(branch.AcceptedHTTPStatusCodes, response.StatusCode) {
			statusCandidate = true
			break
		}
	}
	if !statusCandidate {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_status_not_accepted", ProviderErrorClass: "provider_failure"}
	}
	if int64(len(response.Body)) > healthTransportMaxBytes {
		return indeterminate("response_body_limit_exceeded")
	}

	var jsonDocument any
	jsonAttempted, jsonValid := false, false
	var xmlDocument *healthResponseXMLDocument
	xmlAttempted, xmlValid := false, false
	var anyPayloadParsed bool
	type branchMatch struct {
		branch    healthNormalizedResponseBranch
		soapFault bool
	}
	matches := make([]branchMatch, 0, 2)
	for _, branch := range branches {
		if !healthResponseStatusAccepted(branch.AcceptedHTTPStatusCodes, response.StatusCode) {
			continue
		}
		var parseValid bool
		if branch.PayloadKind == "json" {
			if !jsonAttempted {
				jsonAttempted = true
				var err error
				jsonDocument, err = decodeHealthBoundedResponseJSON(response.Body)
				jsonValid = err == nil
			}
			parseValid = jsonValid
		} else {
			if !xmlAttempted {
				xmlAttempted = true
				xmlDocument, _ = decodeHealthBoundedResponseXML(response.Body)
				xmlValid = xmlDocument != nil
			}
			parseValid = xmlValid
		}
		if parseValid {
			anyPayloadParsed = true
		}
		if !parseValid {
			continue
		}
		if !healthResponseRootMatches(branch, xmlDocument, jsonDocument) {
			continue
		}
		soapFault := false
		if branch.PayloadKind == "soap_xml" {
			fault, valid := healthSOAPResponseFault(xmlDocument, branch.RootQName.Space)
			if !valid {
				continue
			}
			soapFault = fault
		}
		if !healthResponseDiscriminatorsMatch(branch, xmlDocument, jsonDocument) {
			continue
		}
		matches = append(matches, branchMatch{branch: branch, soapFault: soapFault})
	}
	if len(matches) == 0 {
		if !anyPayloadParsed {
			return indeterminate("response_payload_invalid")
		}
		return indeterminate("response_branch_unmatched")
	}
	if len(matches) > 1 {
		return indeterminate("response_branch_ambiguous")
	}
	selectedMatch := matches[0]
	selected := selectedMatch.branch
	// Selection is complete before checking any branch-specific response
	// shape. A malformed selected branch is indeterminate and never falls
	// through to another branch.
	for _, field := range selected.RequiredFields {
		if !healthResponseFieldMatches(xmlDocument, jsonDocument, selected.PayloadKind, field) {
			return indeterminate("response_shape_mismatch")
		}
	}
	collectionEmptyFailure := false
	if collection := selected.ResultCollection; collection != nil {
		count, exists := healthResponseCollectionCount(xmlDocument, jsonDocument, selected.PayloadKind, *collection)
		if !exists {
			return indeterminate("response_shape_mismatch")
		}
		collectionEmptyFailure = count == 0 && collection.EmptySemantics == "invalid"
	}

	matchedProviderCode := false
	var providerCode healthAssertionScalar
	switch selected.ProviderResultCodeMode {
	case "none", "not_applicable":
	case "documented", "expected_success_example":
		actual, ok := healthResponseAssertionScalarAt(xmlDocument, jsonDocument, selected.PayloadKind, selected.ProviderResultCodePath, selected.ProviderResultCodeType)
		if !ok || !healthAssertionScalarIn(selected.ProviderResultCodeValues, actual) {
			return indeterminate("response_provider_code_unknown")
		}
		matchedProviderCode = true
		providerCode = actual
	default:
		return indeterminate("response_assertion_invalid")
	}
	if selected.Classification == "provider_error" && matchedProviderCode {
		return healthResponseAssertionResult{
			Outcome: healthResponseUnhealthy, ReasonCode: "response_provider_error",
			ProviderErrorClass: healthResponseBranchErrorClass(selected.ErrorClasses, providerCode),
		}
	}
	if selectedMatch.soapFault {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_provider_error", ProviderErrorClass: "provider_failure"}
	}
	if selected.Classification == "provider_error" {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_provider_error", ProviderErrorClass: "provider_failure"}
	}
	if collectionEmptyFailure {
		return healthResponseAssertionResult{Outcome: healthResponseUnhealthy, ReasonCode: "response_collection_empty"}
	}
	return healthResponseAssertionResult{Outcome: healthResponseHealthy, ReasonCode: "response_assertion_passed"}
}

func validateHealthNormalizedResponseBranches(branches []healthNormalizedResponseBranch) error {
	if len(branches) == 0 || len(branches) > 16 {
		return errors.New("response branch count is outside its ceiling")
	}
	ids := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		if branch.ID == "" || len(branch.ID) > 96 || !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(branch.ID) {
			return errors.New("response branch identity is invalid")
		}
		if _, duplicate := ids[branch.ID]; duplicate {
			return errors.New("response branch identity is duplicated")
		}
		ids[branch.ID] = struct{}{}
		if branch.Classification != "success" && branch.Classification != "provider_error" {
			return errors.New("response branch classification is unsupported")
		}
		if branch.EmptyResultSemantics != "" {
			if branch.ResultCollection == nil {
				if branch.EmptyResultSemantics != "not_applicable" {
					return errors.New("response branch without a collection has empty semantics")
				}
			} else if branch.EmptyResultSemantics != branch.ResultCollection.EmptySemantics {
				return errors.New("response branch and collection empty semantics differ")
			}
		}
		if branch.PayloadKind != "json" && branch.PayloadKind != "xml" && branch.PayloadKind != "soap_xml" {
			return errors.New("response branch payload kind is unsupported")
		}
		if len(branch.AcceptedHTTPStatusCodes) == 0 || len(branch.AcceptedHTTPStatusCodes) > 32 {
			return errors.New("response branch status set is incomplete")
		}
		seenStatuses := make(map[int]struct{}, len(branch.AcceptedHTTPStatusCodes))
		for _, status := range branch.AcceptedHTTPStatusCodes {
			if status < 100 || status > 599 || branch.Classification == "success" && (status < 200 || status >= 300) {
				return errors.New("response branch status is invalid for its classification")
			}
			if _, duplicate := seenStatuses[status]; duplicate {
				return errors.New("response branch status is duplicated")
			}
			seenStatuses[status] = struct{}{}
		}
		if branch.PayloadKind == "json" {
			if branch.RootQName != (xml.Name{}) || branch.RootKind != "object" && branch.RootKind != "array" && branch.RootKind != "scalar" {
				return errors.New("JSON response branch root selector is invalid")
			}
		} else {
			if branch.RootKind != "xml_element" || !healthXMLPathValid([]xml.Name{branch.RootQName}) {
				return errors.New("XML response branch root selector is invalid")
			}
			if branch.PayloadKind == "soap_xml" && (branch.RootQName.Space == "" || branch.RootQName.Local != "Envelope") {
				return errors.New("SOAP response branch must select an envelope root")
			}
		}
		if len(branch.Discriminators) > 8 {
			return errors.New("response branch discriminator count exceeds its ceiling")
		}
		seenDiscriminatorPaths := make([]healthNormalizedResponsePath, 0, len(branch.Discriminators))
		for _, discriminator := range branch.Discriminators {
			if !healthResponsePathValid(branch.PayloadKind, discriminator.Path) {
				return errors.New("response branch discriminator path is invalid")
			}
			for _, path := range seenDiscriminatorPaths {
				if healthNormalizedResponsePathsEqual(path, discriminator.Path) {
					return errors.New("response branch repeats a discriminator path")
				}
			}
			seenDiscriminatorPaths = append(seenDiscriminatorPaths, discriminator.Path)
			switch discriminator.Predicate {
			case "present", "absent":
				if discriminator.ValueType != "" || len(discriminator.Values) != 0 {
					return errors.New("presence discriminator contains typed values")
				}
			case "node_type":
				if !healthResponseValueTypeSupported(branch.PayloadKind, discriminator.ValueType) || len(discriminator.Values) != 0 {
					return errors.New("node-type discriminator is invalid")
				}
			case "equals_any":
				if !healthResponseCodeTypeSupported(branch.PayloadKind, discriminator.ValueType) || len(discriminator.Values) == 0 || len(discriminator.Values) > 32 || !healthResponseScalarsMatchType(discriminator.Values, discriminator.ValueType) {
					return errors.New("exact-value discriminator is invalid")
				}
			default:
				return errors.New("response branch discriminator predicate is unsupported")
			}
		}
		if len(branch.RequiredFields) > healthResponseAssertionMaxPredicates || len(branch.ProviderResultCodeValues) > healthResponseAssertionMaxPredicates || len(branch.ErrorClasses) > 6 {
			return errors.New("response branch predicate count exceeds its ceiling")
		}
		for _, field := range branch.RequiredFields {
			if field.MinimumCount < 0 || field.MaximumCount < field.MinimumCount || field.MaximumCount > healthResponseAssertionMaxSelectedNodes || !healthResponsePathValid(branch.PayloadKind, field.Path) || !healthResponseValueTypeSupported(branch.PayloadKind, field.ValueType) {
				return errors.New("response branch field predicate is invalid")
			}
		}
		switch branch.ProviderResultCodeMode {
		case "none", "not_applicable":
			if branch.ProviderResultCodeType != "" || len(branch.ProviderResultCodeValues) != 0 || branch.ProviderResultCodePath.JSONPointer != "" || len(branch.ProviderResultCodePath.XMLPath) != 0 || len(branch.ErrorClasses) != 0 {
				return errors.New("response branch without provider codes contains code predicates")
			}
		case "documented":
			if !healthResponsePathValid(branch.PayloadKind, branch.ProviderResultCodePath) || !healthResponseCodeTypeSupported(branch.PayloadKind, branch.ProviderResultCodeType) || len(branch.ProviderResultCodeValues) == 0 || !healthResponseScalarsMatchType(branch.ProviderResultCodeValues, branch.ProviderResultCodeType) {
				return errors.New("documented response branch code is incomplete")
			}
		case "expected_success_example":
			if branch.Classification != "success" || !healthResponsePathValid(branch.PayloadKind, branch.ProviderResultCodePath) || !healthResponseCodeTypeSupported(branch.PayloadKind, branch.ProviderResultCodeType) || len(branch.ProviderResultCodeValues) != 1 || !healthResponseScalarsMatchType(branch.ProviderResultCodeValues, branch.ProviderResultCodeType) || len(branch.ErrorClasses) != 0 {
				return errors.New("response branch success example is incomplete")
			}
		default:
			return errors.New("response branch provider-code mode is unsupported")
		}
		if branch.Classification != "provider_error" && len(branch.ErrorClasses) != 0 || branch.Classification == "provider_error" && len(branch.ErrorClasses) > 0 && branch.ProviderResultCodeMode != "documented" {
			return errors.New("response branch error class mapping is unsupported")
		}
		classValues := make(map[healthAssertionScalar]struct{})
		categories := make(map[string]struct{}, len(branch.ErrorClasses))
		for _, class := range branch.ErrorClasses {
			if !healthResponseErrorCategoryAllowed(class.Category) || len(class.Values) == 0 || !healthResponseScalarsMatchType(class.Values, branch.ProviderResultCodeType) {
				return errors.New("response branch error class mapping is invalid")
			}
			if _, duplicate := categories[class.Category]; duplicate {
				return errors.New("response branch error class is duplicated")
			}
			categories[class.Category] = struct{}{}
			for _, value := range class.Values {
				canonical, _ := healthCanonicalAssertionScalar(value)
				if !healthAssertionScalarIn(branch.ProviderResultCodeValues, canonical) {
					return errors.New("response branch error class value is not a documented provider error")
				}
				if _, duplicate := classValues[canonical]; duplicate {
					return errors.New("response branch error classes overlap")
				}
				classValues[canonical] = struct{}{}
			}
		}
		if collection := branch.ResultCollection; collection != nil {
			if collection.EmptySemantics != "valid" && collection.EmptySemantics != "invalid" {
				return errors.New("response branch collection semantics are invalid")
			}
			if branch.PayloadKind == "json" {
				if !validHealthOperationDocumentPointer(collection.JSONPointer) || len(collection.XMLContainer) != 0 || len(collection.XMLItemPath) != 0 {
					return errors.New("JSON response branch collection path is invalid")
				}
			} else if collection.JSONPointer != "" || !healthXMLPathValid(collection.XMLContainer) || !healthXMLPathValid(collection.XMLItemPath) {
				return errors.New("XML response branch collection paths are invalid")
			}
		}
	}
	for left := 0; left < len(branches); left++ {
		for right := left + 1; right < len(branches); right++ {
			exclusive, samePathOverlap := healthResponseBranchesDisjoint(branches[left], branches[right])
			if !exclusive && samePathOverlap {
				return errors.New("response branch selectors have overlapping predicates on the same path")
			}
		}
	}
	return nil
}

func healthResponseBranchesDisjoint(left, right healthNormalizedResponseBranch) (exclusive, samePathOverlap bool) {
	if (left.PayloadKind == "json") != (right.PayloadKind == "json") {
		return true, false
	}
	sharedStatus := false
	for _, status := range left.AcceptedHTTPStatusCodes {
		if healthResponseStatusAccepted(right.AcceptedHTTPStatusCodes, status) {
			sharedStatus = true
			break
		}
	}
	if !sharedStatus {
		return true, false
	}
	if left.PayloadKind == "json" {
		if left.RootKind != right.RootKind {
			return true, false
		}
	} else if left.RootQName != right.RootQName {
		return true, false
	}
	if len(left.Discriminators) == 0 || len(right.Discriminators) == 0 {
		return false, true
	}
	for _, leftDiscriminator := range left.Discriminators {
		for _, rightDiscriminator := range right.Discriminators {
			if !healthNormalizedResponsePathsEqual(leftDiscriminator.Path, rightDiscriminator.Path) {
				continue
			}
			disjoint, overlaps := healthResponseDiscriminatorsDisjoint(leftDiscriminator, rightDiscriminator)
			if disjoint {
				return true, false
			}
			if overlaps {
				samePathOverlap = true
			}
		}
	}
	// Different positive/negative member paths may co-occur. The runtime
	// selector must still require exactly one match and fail indeterminate on
	// zero or multiple matching branches.
	return false, samePathOverlap
}

func healthResponseDiscriminatorsDisjoint(left, right healthNormalizedResponseDiscriminator) (disjoint, overlaps bool) {
	leftNegative, rightNegative := left.Predicate == "absent", right.Predicate == "absent"
	leftPositive, rightPositive := left.Predicate == "present" || left.Predicate == "node_type" || left.Predicate == "equals_any", right.Predicate == "present" || right.Predicate == "node_type" || right.Predicate == "equals_any"
	if leftNegative && rightPositive || rightNegative && leftPositive {
		return true, false
	}
	if left.Predicate == "node_type" && right.Predicate == "node_type" && left.ValueType != right.ValueType {
		return true, false
	}
	if left.Predicate == "node_type" && right.Predicate == "equals_any" && left.ValueType != right.ValueType || right.Predicate == "node_type" && left.Predicate == "equals_any" && right.ValueType != left.ValueType {
		return true, false
	}
	if left.Predicate == "equals_any" && right.Predicate == "equals_any" {
		if left.ValueType != right.ValueType {
			return true, false
		}
		for _, value := range left.Values {
			if healthAssertionScalarIn(right.Values, value) {
				return false, true
			}
		}
		return true, false
	}
	return false, true
}

func healthNormalizedResponsePathsEqual(left, right healthNormalizedResponsePath) bool {
	if left.JSONPointer != right.JSONPointer || len(left.XMLPath) != len(right.XMLPath) {
		return false
	}
	for index := range left.XMLPath {
		if left.XMLPath[index] != right.XMLPath[index] {
			return false
		}
	}
	return true
}

func healthResponseRootMatches(branch healthNormalizedResponseBranch, xmlDocument *healthResponseXMLDocument, jsonDocument any) bool {
	if branch.PayloadKind == "json" {
		switch branch.RootKind {
		case "object":
			_, ok := jsonDocument.(map[string]any)
			return ok
		case "array":
			_, ok := jsonDocument.([]any)
			return ok
		case "scalar":
			switch jsonDocument.(type) {
			case nil, bool, string, json.Number:
				return true
			default:
				return false
			}
		}
		return false
	}
	return xmlDocument != nil && xmlDocument.Root >= 0 && xmlDocument.Nodes[xmlDocument.Root].Name == branch.RootQName
}

func healthResponseDiscriminatorsMatch(branch healthNormalizedResponseBranch, xmlDocument *healthResponseXMLDocument, jsonDocument any) bool {
	for _, discriminator := range branch.Discriminators {
		switch discriminator.Predicate {
		case "present":
			if !healthResponsePathPresent(xmlDocument, jsonDocument, branch.PayloadKind, discriminator.Path) {
				return false
			}
		case "absent":
			if healthResponsePathPresent(xmlDocument, jsonDocument, branch.PayloadKind, discriminator.Path) {
				return false
			}
		case "node_type":
			if !healthResponseFieldMatches(xmlDocument, jsonDocument, branch.PayloadKind, healthNormalizedResponseField{
				Path: discriminator.Path, ValueType: discriminator.ValueType, MinimumCount: 1, MaximumCount: 1,
			}) {
				return false
			}
		case "equals_any":
			actual, ok := healthResponseAssertionScalarAt(xmlDocument, jsonDocument, branch.PayloadKind, discriminator.Path, discriminator.ValueType)
			if !ok || !healthAssertionScalarIn(discriminator.Values, actual) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func healthResponsePathPresent(xmlDocument *healthResponseXMLDocument, jsonDocument any, payloadKind string, path healthNormalizedResponsePath) bool {
	if payloadKind == "json" {
		_, exists := healthJSONPointer(jsonDocument, path.JSONPointer)
		return exists
	}
	return len(healthXMLSelect(xmlDocument, path.XMLPath, nil)) > 0
}

func healthResponseErrorCategoryAllowed(category string) bool {
	switch category {
	case "credential_rejected", "rate_limited", "parameter_blocked", "provider_failure", "semantic_failure", "unsupported":
		return true
	default:
		return false
	}
}

func healthResponseBranchErrorClass(classes []healthNormalizedProviderErrorClass, code healthAssertionScalar) string {
	for _, class := range classes {
		if healthAssertionScalarIn(class.Values, code) {
			return class.Category
		}
	}
	return "provider_failure"
}

func healthResponsePathValid(payloadKind string, path healthNormalizedResponsePath) bool {
	if payloadKind == "json" {
		return path.JSONPointer != "" && len(path.XMLPath) == 0 && validHealthOperationDocumentPointer(path.JSONPointer)
	}
	return path.JSONPointer == "" && healthXMLPathValid(path.XMLPath)
}

func healthXMLPathValid(path []xml.Name) bool {
	if len(path) == 0 || len(path) > healthOperationPlanMaxJSONDepth {
		return false
	}
	for _, name := range path {
		if !healthXMLLocalNamePattern.MatchString(name.Local) {
			return false
		}
	}
	return true
}

func healthResponseValueTypeSupported(payloadKind, valueType string) bool {
	switch valueType {
	case "object":
		return true
	case "array":
		return payloadKind == "json"
	case "string", "boolean", "integer", "number":
		return true
	default:
		return false
	}
}

func healthResponseCodeTypeSupported(payloadKind, valueType string) bool {
	return valueType == "string" || valueType == "boolean" || valueType == "integer" || valueType == "number"
}

func healthResponseScalarsMatchType(values []healthAssertionScalar, valueType string) bool {
	seen := make(map[healthAssertionScalar]struct{}, len(values))
	for _, value := range values {
		if value.ValueType != valueType {
			return false
		}
		canonical, ok := healthCanonicalAssertionScalar(value)
		if !ok {
			return false
		}
		if _, duplicate := seen[canonical]; duplicate {
			return false
		}
		seen[canonical] = struct{}{}
	}
	return true
}

func healthResponseScalarSetsOverlap(left, right []healthAssertionScalar) bool {
	values := make(map[healthAssertionScalar]struct{}, len(left))
	for _, value := range left {
		canonical, ok := healthCanonicalAssertionScalar(value)
		if ok {
			values[canonical] = struct{}{}
		}
	}
	for _, value := range right {
		canonical, ok := healthCanonicalAssertionScalar(value)
		if ok {
			if _, found := values[canonical]; found {
				return true
			}
		}
	}
	return false
}

func healthCanonicalAssertionScalar(value healthAssertionScalar) (healthAssertionScalar, bool) {
	switch value.ValueType {
	case "string":
		return value, true
	case "boolean":
		if value.Value == "true" || value.Value == "false" {
			return value, true
		}
	case "integer", "number":
		decimal, ok := healthParseDecimal(value.Value, true, value.ValueType == "integer")
		if ok {
			return healthAssertionScalar{ValueType: value.ValueType, Value: decimal.canonical()}, true
		}
	}
	return healthAssertionScalar{}, false
}

func healthAssertionScalarIn(values []healthAssertionScalar, actual healthAssertionScalar) bool {
	canonicalActual, ok := healthCanonicalAssertionScalar(actual)
	if !ok {
		return false
	}
	for _, value := range values {
		canonicalValue, valid := healthCanonicalAssertionScalar(value)
		if valid && canonicalValue == canonicalActual {
			return true
		}
	}
	return false
}

func healthResponseStatusAccepted(statuses []int, actual int) bool {
	for _, status := range statuses {
		if status == actual {
			return true
		}
	}
	return false
}

func decodeHealthBoundedResponseJSON(body []byte) (any, error) {
	if len(body) == 0 || int64(len(body)) > healthTransportMaxBytes {
		return nil, errors.New("JSON response is empty or exceeds the byte ceiling")
	}
	if !utf8.Valid(body) || !healthJSONUnicodeEscapesValid(body) {
		return nil, errors.New("JSON response contains invalid Unicode")
	}
	if err := preflightHealthResponseJSON(body); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	return document, nil
}

// encoding/json intentionally replaces invalid UTF-8 and unpaired UTF-16
// surrogate escapes with U+FFFD. For an assertion contract, that replacement
// could turn corrupted provider data into a matching string, so response
// strings must use valid Unicode before decoding.
func healthJSONUnicodeEscapesValid(data []byte) bool {
	for index := 0; index < len(data); {
		if data[index] != '"' {
			index++
			continue
		}
		index++
		closed := false
		for index < len(data) {
			switch data[index] {
			case '"':
				index++
				closed = true
			case '\\':
				index++
				if index >= len(data) {
					return false
				}
				if data[index] != 'u' {
					index++
					continue
				}
				unit, ok := healthJSONUnicodeEscapeUnit(data, index+1)
				if !ok {
					return false
				}
				index += 5 // the `u` and its four hexadecimal digits
				if unit >= 0xDC00 && unit <= 0xDFFF {
					return false
				}
				if unit >= 0xD800 && unit <= 0xDBFF {
					if index+6 > len(data) || data[index] != '\\' || data[index+1] != 'u' {
						return false
					}
					low, valid := healthJSONUnicodeEscapeUnit(data, index+2)
					if !valid || low < 0xDC00 || low > 0xDFFF {
						return false
					}
					index += 6
				}
			default:
				index++
			}
			if closed {
				break
			}
		}
		if !closed {
			return false
		}
	}
	return true
}

func healthJSONUnicodeEscapeUnit(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value uint16
	for _, digit := range data[start : start+4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

type healthResponseXMLNode struct {
	Name     xml.Name
	Children []int
	Text     []byte
}

type healthResponseXMLDocument struct {
	Nodes []healthResponseXMLNode
	Root  int
}

func decodeHealthBoundedResponseXML(body []byte) (*healthResponseXMLDocument, error) {
	if len(body) == 0 || int64(len(body)) > healthTransportMaxBytes {
		return nil, errors.New("XML response is empty or exceeds the byte ceiling")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("XML response contains invalid UTF-8")
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	document := &healthResponseXMLDocument{Root: -1}
	stack := make([]int, 0, 16)
	tokenCount := 0
	xmlDeclarationSeen := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		tokenCount++
		if tokenCount > healthOperationPlanMaxJSONTokens {
			return nil, errors.New("XML response token ceiling exceeded")
		}
		switch current := token.(type) {
		case xml.StartElement:
			if len(stack)+1 > healthOperationPlanMaxJSONDepth || len(document.Nodes) >= healthResponseXMLMaxNodes {
				return nil, errors.New("XML response structure exceeds its ceiling")
			}
			parent := -1
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			} else if document.Root != -1 {
				return nil, errors.New("XML response has multiple root elements")
			}
			index := len(document.Nodes)
			document.Nodes = append(document.Nodes, healthResponseXMLNode{Name: current.Name})
			if parent == -1 {
				document.Root = index
			} else {
				document.Nodes[parent].Children = append(document.Nodes[parent].Children, index)
			}
			stack = append(stack, index)
		case xml.EndElement:
			if len(stack) == 0 || document.Nodes[stack[len(stack)-1]].Name != current.Name {
				return nil, errors.New("XML response element nesting is invalid")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(current)) != "" {
					return nil, errors.New("XML response has text outside its root")
				}
				continue
			}
			index := stack[len(stack)-1]
			document.Nodes[index].Text = append(document.Nodes[index].Text, current...)
		case xml.Directive:
			return nil, errors.New("XML directives are unsupported")
		case xml.ProcInst:
			if current.Target != "xml" || xmlDeclarationSeen || document.Root != -1 || len(stack) != 0 {
				return nil, errors.New("XML processing instructions are unsupported")
			}
			xmlDeclarationSeen = true
		case xml.Comment:
			// Comments have no meaning to response predicates.
		}
	}
	if document.Root < 0 || len(stack) != 0 {
		return nil, errors.New("XML response document is incomplete")
	}
	return document, nil
}

func healthXMLSelect(document *healthResponseXMLDocument, path []xml.Name, start []int) []int {
	if document == nil || !healthXMLPathValid(path) {
		return nil
	}
	current := start
	if len(current) == 0 {
		if document.Root < 0 || document.Nodes[document.Root].Name != path[0] {
			return nil
		}
		current = []int{document.Root}
		path = path[1:]
	}
	for _, name := range path {
		next := make([]int, 0)
		for _, parent := range current {
			for _, child := range document.Nodes[parent].Children {
				if document.Nodes[child].Name == name {
					next = append(next, child)
				}
			}
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	return current
}

func healthSOAPResponseFault(document *healthResponseXMLDocument, envelopeNamespace string) (bool, bool) {
	if document == nil || document.Root < 0 || document.Nodes[document.Root].Name != (xml.Name{Space: envelopeNamespace, Local: "Envelope"}) {
		return false, false
	}
	envelope := document.Root
	bodyNodes := make([]int, 0, 1)
	for _, child := range document.Nodes[envelope].Children {
		if document.Nodes[child].Name == (xml.Name{Space: envelopeNamespace, Local: "Body"}) {
			bodyNodes = append(bodyNodes, child)
		}
	}
	if len(bodyNodes) != 1 {
		return false, false
	}
	fault := false
	for _, child := range document.Nodes[bodyNodes[0]].Children {
		if document.Nodes[child].Name == (xml.Name{Space: envelopeNamespace, Local: "Fault"}) {
			fault = true
		}
	}
	return fault, true
}

func healthResponseFieldMatches(xmlDocument *healthResponseXMLDocument, jsonDocument any, payloadKind string, field healthNormalizedResponseField) bool {
	if payloadKind == "json" {
		value, exists := healthJSONPointer(jsonDocument, field.Path.JSONPointer)
		if !exists {
			return field.MinimumCount == 0
		}
		return field.MinimumCount <= 1 && field.MaximumCount >= 1 && healthJSONValueHasType(value, field.ValueType)
	}
	nodes := healthXMLSelect(xmlDocument, field.Path.XMLPath, nil)
	if len(nodes) < field.MinimumCount || len(nodes) > field.MaximumCount {
		return false
	}
	for _, index := range nodes {
		if !healthXMLNodeHasType(xmlDocument, index, field.ValueType) {
			return false
		}
	}
	return true
}

func healthJSONValueHasType(value any, valueType string) bool {
	switch valueType {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		decimal, valid := healthParseDecimal(number.String(), false, false)
		return valid && decimal.isInteger()
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, valid := healthParseDecimal(number.String(), false, false)
		return valid
	default:
		return false
	}
}

func healthXMLNodeHasType(document *healthResponseXMLDocument, index int, valueType string) bool {
	if document == nil || index < 0 || index >= len(document.Nodes) {
		return false
	}
	node := &document.Nodes[index]
	text := string(node.Text)
	switch valueType {
	case "object":
		return len(node.Children) > 0
	case "string":
		return len(node.Children) == 0
	case "boolean":
		return len(node.Children) == 0 && (text == "true" || text == "false" || text == "1" || text == "0")
	case "integer":
		if len(node.Children) != 0 || !healthXMLIntegerValuePattern.MatchString(text) {
			return false
		}
		_, valid := healthParseDecimal(text, true, true)
		return valid
	case "number":
		if len(node.Children) != 0 || !healthXMLNumberValuePattern.MatchString(text) {
			return false
		}
		_, valid := healthParseDecimal(text, true, false)
		return valid
	default:
		return false
	}
}

func healthResponseAssertionScalarAt(xmlDocument *healthResponseXMLDocument, jsonDocument any, payloadKind string, path healthNormalizedResponsePath, valueType string) (healthAssertionScalar, bool) {
	if payloadKind == "json" {
		value, exists := healthJSONPointer(jsonDocument, path.JSONPointer)
		if !exists {
			return healthAssertionScalar{}, false
		}
		return healthJSONScalar(value, valueType)
	}
	nodes := healthXMLSelect(xmlDocument, path.XMLPath, nil)
	if len(nodes) != 1 {
		return healthAssertionScalar{}, false
	}
	node := &xmlDocument.Nodes[nodes[0]]
	if len(node.Children) != 0 {
		return healthAssertionScalar{}, false
	}
	text := string(node.Text)
	switch valueType {
	case "string":
		return healthAssertionScalar{ValueType: valueType, Value: text}, true
	case "boolean":
		switch text {
		case "true", "1":
			return healthAssertionScalar{ValueType: valueType, Value: "true"}, true
		case "false", "0":
			return healthAssertionScalar{ValueType: valueType, Value: "false"}, true
		}
	case "integer":
		if healthXMLIntegerValuePattern.MatchString(text) {
			if decimal, ok := healthParseDecimal(text, true, true); ok {
				return healthAssertionScalar{ValueType: valueType, Value: decimal.canonical()}, true
			}
		}
	case "number":
		if healthXMLNumberValuePattern.MatchString(text) {
			if decimal, ok := healthParseDecimal(text, true, false); ok {
				return healthAssertionScalar{ValueType: valueType, Value: decimal.canonical()}, true
			}
		}
	}
	return healthAssertionScalar{}, false
}

func healthJSONScalar(value any, valueType string) (healthAssertionScalar, bool) {
	switch valueType {
	case "string":
		text, ok := value.(string)
		return healthAssertionScalar{ValueType: valueType, Value: text}, ok
	case "boolean":
		boolean, ok := value.(bool)
		return healthAssertionScalar{ValueType: valueType, Value: strconv.FormatBool(boolean)}, ok
	case "integer", "number":
		number, ok := value.(json.Number)
		if !ok {
			return healthAssertionScalar{}, false
		}
		decimal, valid := healthParseDecimal(number.String(), false, false)
		if !valid || valueType == "integer" && !decimal.isInteger() {
			return healthAssertionScalar{}, false
		}
		return healthAssertionScalar{ValueType: valueType, Value: decimal.canonical()}, true
	default:
		return healthAssertionScalar{}, false
	}
}

func healthResponseCollectionCount(xmlDocument *healthResponseXMLDocument, jsonDocument any, payloadKind string, collection healthNormalizedResponseCollection) (int, bool) {
	if payloadKind == "json" {
		value, exists := healthJSONPointer(jsonDocument, collection.JSONPointer)
		if !exists {
			return 0, false
		}
		items, ok := value.([]any)
		return len(items), ok
	}
	containers := healthXMLSelect(xmlDocument, collection.XMLContainer, nil)
	if len(containers) != 1 {
		return 0, false
	}
	items := healthXMLSelect(xmlDocument, collection.XMLItemPath, containers)
	return len(items), true
}

type healthCanonicalDecimal struct {
	negative bool
	digits   string
	scale    int64
}

func (d healthCanonicalDecimal) canonical() string {
	if d.digits == "0" {
		return "0"
	}
	sign := ""
	if d.negative {
		sign = "-"
	}
	return sign + d.digits + "e" + strconv.FormatInt(d.scale, 10)
}

func (d healthCanonicalDecimal) isInteger() bool {
	return d.digits == "0" || d.scale >= 0
}

// healthParseDecimal accepts JSON number grammar or the stricter XML lexical
// forms above, then canonicalizes without converting through float64. Exponent
// magnitude is bounded by the response byte ceiling so a huge exponent cannot
// overflow the canonical scale or alias another numeric value.
func healthParseDecimal(value string, allowLeadingPlus, integerOnly bool) (healthCanonicalDecimal, bool) {
	if value == "" {
		return healthCanonicalDecimal{}, false
	}
	negative := false
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '+' && !allowLeadingPlus {
			return healthCanonicalDecimal{}, false
		}
		negative = value[0] == '-'
		value = value[1:]
		if value == "" {
			return healthCanonicalDecimal{}, false
		}
	}
	mantissa, exponentText, hasExponent := strings.Cut(value, "e")
	if !hasExponent {
		mantissa, exponentText, hasExponent = strings.Cut(value, "E")
	}
	if strings.ContainsAny(exponentText, "eE") {
		return healthCanonicalDecimal{}, false
	}
	exponent := int64(0)
	if hasExponent {
		parsed, ok := healthParseBoundedExponent(exponentText)
		if !ok {
			return healthCanonicalDecimal{}, false
		}
		exponent = parsed
	}
	whole, fractional, hasPoint := strings.Cut(mantissa, ".")
	if strings.Contains(fractional, ".") || (hasPoint && fractional == "" && !allowLeadingPlus) || (whole == "" && (!hasPoint || fractional == "")) {
		return healthCanonicalDecimal{}, false
	}
	if whole == "" {
		whole = "0"
	}
	if !healthDigitsOnly(whole) || hasPoint && !healthDigitsOnly(fractional) {
		return healthCanonicalDecimal{}, false
	}
	if !allowLeadingPlus && len(whole) > 1 && whole[0] == '0' {
		return healthCanonicalDecimal{}, false
	}
	digits := whole + fractional
	first := 0
	for first < len(digits) && digits[first] == '0' {
		first++
	}
	if first == len(digits) {
		if integerOnly {
			return healthCanonicalDecimal{digits: "0"}, true
		}
		return healthCanonicalDecimal{digits: "0"}, true
	}
	digits = digits[first:]
	scale := exponent - int64(len(fractional))
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		scale++
	}
	decimal := healthCanonicalDecimal{negative: negative, digits: digits, scale: scale}
	if integerOnly && !decimal.isInteger() {
		return healthCanonicalDecimal{}, false
	}
	return decimal, true
}

func healthDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func healthParseBoundedExponent(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	negative := false
	if value[0] == '-' || value[0] == '+' {
		negative = value[0] == '-'
		value = value[1:]
	}
	if !healthDigitsOnly(value) {
		return 0, false
	}
	const exponentLimit = healthTransportMaxBytes
	var result int64
	for index := 0; index < len(value); index++ {
		digit := int64(value[index] - '0')
		if result > (exponentLimit-digit)/10 {
			return 0, false
		}
		result = result*10 + digit
	}
	if result > exponentLimit {
		return 0, false
	}
	if negative {
		return -result, true
	}
	return result, true
}
