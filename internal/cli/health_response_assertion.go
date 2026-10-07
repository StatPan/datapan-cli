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
}

const (
	healthResponseAssertionMaxPredicates = 128
	healthResponseXMLMaxNodes            = healthOperationPlanMaxJSONTokens / 2
)

var healthXMLIntegerValuePattern = regexp.MustCompile(`^[+-]?[0-9]+$`)
var healthXMLNumberValuePattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// evaluateHealthNormalizedResponseAssertion applies only explicit predicates.
// A nonaccepted status or a documented provider error is unhealthy. A payload
// that cannot be parsed or does not prove the reviewed success shape is
// indeterminate. It never includes response values in an error or result.
func evaluateHealthNormalizedResponseAssertion(assertion healthNormalizedResponseAssertion, response healthHTTPResponse) healthResponseAssertionResult {
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
		return unhealthy("response_status_not_accepted")
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
				return unhealthy("response_provider_fault")
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
			return unhealthy("response_provider_error")
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
		if field.MinimumCount < 0 || field.MaximumCount < field.MinimumCount || field.MaximumCount > healthResponseAssertionMaxPredicates || !healthResponsePathValid(assertion.PayloadKind, field.Path) || !healthResponseValueTypeSupported(assertion.PayloadKind, field.ValueType) {
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
	if err := preflightHealthOperationPlanJSON(body); err != nil {
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
