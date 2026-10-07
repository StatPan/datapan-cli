package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var healthXMLLocalNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]*$`)

func healthOperationPlanRequestShape(plan healthOperationPlanRecord, credentialValue string, now time.Time) (healthHTTPRequestShape, error) {
	if err := validateHealthOperationPlanRecord(plan); err != nil {
		return healthHTTPRequestShape{}, errors.New("operation plan is unsupported")
	}
	contract := plan.RequestPlan.RequestContract
	if contract.Authentication.Requirement == "required" && strings.TrimSpace(credentialValue) == "" {
		return healthHTTPRequestShape{}, errors.New("operation plan credential is unavailable")
	}
	endpoint := contract.Transport.Scheme + "://" + contract.Transport.Host + contract.Transport.Path
	shape := healthHTTPRequestShape{
		Protocol: healthHTTPREST, PublicTargetOnly: true, ReadOnly: true, Method: contract.Transport.HTTPMethod, Endpoint: endpoint,
		Query: make(url.Values), Headers: make(http.Header), RequestBudget: contract.Limits.RequestBudget,
		Timeout:         time.Duration(contract.Limits.TimeoutMS) * time.Millisecond,
		MaxRequestBytes: contract.Limits.MaxRequestBytes, MaxResponseBytes: contract.Limits.MaxResponseBytes,
	}
	if contract.Transport.Protocol == "SOAP" {
		shape.Protocol = healthHTTPSOAP
		shape.SOAPAction = contract.Transport.SOAPAction
		shape.SOAPVersion = contract.Transport.SOAPVersion
		shape.Headers.Set("Content-Type", "text/xml; charset=utf-8")
		if shape.SOAPVersion == "1.2" {
			contentType := mime.FormatMediaType("application/soap+xml", map[string]string{"charset": "utf-8", "action": shape.SOAPAction})
			if contentType == "" {
				return healthHTTPRequestShape{}, errors.New("operation plan SOAP action is unsupported")
			}
			shape.Headers.Set("Content-Type", contentType)
		}
	}
	if !operationPlanEndpointMatches(plan.OperationIdentity.RegisteredEndpoint, contract.Transport.Host, contract.Transport.Path, contract.Transport.Scheme) {
		return healthHTTPRequestShape{}, errors.New("operation plan endpoint is unsupported")
	}

	var bodyParameters, headerParameters []struct {
		parameter healthOperationPlanParameter
		value     string
	}
	for _, parameter := range contract.Parameters {
		value := ""
		if parameter.ValueStrategy.Kind == "credential_reference" {
			value = credentialValue
		} else {
			resolved, err := healthOperationPlanParameterValue(parameter.ValueStrategy, now)
			if err != nil {
				return healthHTTPRequestShape{}, errors.New("operation plan parameter value is unsupported")
			}
			value = resolved
		}
		switch parameter.Location {
		case "query":
			shape.Query.Add(parameter.Name, value)
		case "header":
			shape.Headers.Add(parameter.Name, value)
		case "body":
			bodyParameters = append(bodyParameters, struct {
				parameter healthOperationPlanParameter
				value     string
			}{parameter, value})
		case "soap_header":
			headerParameters = append(headerParameters, struct {
				parameter healthOperationPlanParameter
				value     string
			}{parameter, value})
		default:
			return healthHTTPRequestShape{}, errors.New("operation plan parameter location is unsupported")
		}
	}
	if contract.Transport.Protocol == "SOAP" {
		body, err := healthSOAPDocumentLiteralBody(contract.Transport, bodyParameters, headerParameters)
		if err != nil {
			return healthHTTPRequestShape{}, errors.New("operation plan SOAP document is unsupported")
		}
		shape.Body = body
	}
	return shape, nil
}

func healthOperationPlanParameterValue(strategy struct {
	Kind          string                          `json:"kind"`
	Authority     string                          `json:"authority"`
	Minimum       *int                            `json:"minimum,omitempty"`
	Maximum       *int                            `json:"maximum,omitempty"`
	Selection     string                          `json:"selection,omitempty"`
	SelectedValue json.RawMessage                 `json:"selected_value,omitempty"`
	OffsetYears   *int                            `json:"offset_years,omitempty"`
	MinimumYear   *int                            `json:"minimum_year,omitempty"`
	MaximumYear   *int                            `json:"maximum_year,omitempty"`
	Anchor        string                          `json:"anchor,omitempty"`
	BindingField  string                          `json:"binding_field,omitempty"`
	ValueSHA256   string                          `json:"value_sha256,omitempty"`
	ValueRef      *healthOperationPlanEvidenceRef `json:"value_ref,omitempty"`
}, now time.Time) (string, error) {
	switch strategy.Kind {
	case "bounded_integer":
		if strategy.Minimum == nil || strategy.Maximum == nil || *strategy.Minimum > *strategy.Maximum {
			return "", errors.New("bounded integer parameters require ordered limits")
		}
		var selected int
		switch strategy.Selection {
		case "minimum":
			selected = *strategy.Minimum
		case "maximum":
			selected = *strategy.Maximum
		case "fixed":
			if len(strategy.SelectedValue) == 0 || json.Unmarshal(strategy.SelectedValue, &selected) != nil {
				return "", errors.New("fixed integer selection is invalid")
			}
		default:
			return "", errors.New("integer selection strategy is unsupported")
		}
		if selected < *strategy.Minimum || selected > *strategy.Maximum {
			return "", errors.New("selected integer is outside declared bounds")
		}
		return strconv.Itoa(selected), nil
	case "relative_year":
		if strategy.OffsetYears == nil || strategy.MinimumYear == nil || strategy.MaximumYear == nil || *strategy.MinimumYear > *strategy.MaximumYear || strategy.Anchor != "observation_calendar_year" {
			return "", errors.New("relative year strategy is incomplete")
		}
		selected := now.UTC().Year() + *strategy.OffsetYears
		if selected < *strategy.MinimumYear || selected > *strategy.MaximumYear {
			return "", errors.New("relative year is outside declared bounds")
		}
		return strconv.Itoa(selected), nil
	case "reviewed_enum", "reviewed_literal":
		if strategy.Authority != "reviewed_policy" && strategy.Authority != "synthetic_fixture" || len(strategy.SelectedValue) == 0 || strategy.ValueRef != nil || strategy.ValueSHA256 != "" {
			return "", errors.New("reviewed value must be selected directly in the immutable plan")
		}
		return healthOperationPlanScalarValue(strategy.SelectedValue)
	case "opaque_reviewed_value":
		return "", errors.New("opaque reviewed values are unsupported by this CLI build")
	case "credential_reference":
		return "", errors.New("credential references require a local binding resolver")
	default:
		return "", errors.New("operation plan value strategy is unsupported")
	}
}

func healthOperationPlanScalarValue(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err == nil {
		return "", errors.New("selected value has trailing JSON")
	}
	switch current := value.(type) {
	case string:
		if current == "" {
			return "", errors.New("selected value is empty")
		}
		return current, nil
	case json.Number:
		if _, err := current.Float64(); err != nil {
			return "", errors.New("selected numeric value is invalid")
		}
		return current.String(), nil
	case bool:
		return strconv.FormatBool(current), nil
	default:
		return "", errors.New("selected value is not scalar")
	}
}

func healthSOAPDocumentLiteralBody(
	transport struct {
		Protocol          string                           `json:"protocol"`
		Scheme            string                           `json:"scheme"`
		Host              string                           `json:"host"`
		Path              string                           `json:"path"`
		HTTPMethod        string                           `json:"http_method"`
		SOAPAction        string                           `json:"soap_action,omitempty"`
		SOAPVersion       string                           `json:"soap_version,omitempty"`
		EnvelopeNamespace string                           `json:"envelope_namespace,omitempty"`
		OperationQName    healthOperationPlanQName         `json:"operation_qname,omitempty"`
		BodyEncoding      string                           `json:"body_encoding,omitempty"`
		Authority         string                           `json:"authority"`
		EvidenceRefs      []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	},
	bodyParameters []struct {
		parameter healthOperationPlanParameter
		value     string
	},
	headerParameters []struct {
		parameter healthOperationPlanParameter
		value     string
	},
) ([]byte, error) {
	if transport.Protocol != "SOAP" || transport.BodyEncoding != "document_literal" || !healthOperationPlanSOAPEnvelopeMatches(transport.SOAPVersion, transport.EnvelopeNamespace) || !healthXMLLocalNamePattern.MatchString(transport.OperationQName.LocalName) || strings.TrimSpace(transport.OperationQName.Namespace) == "" {
		return nil, errors.New("SOAP document contract is unsupported")
	}
	var output strings.Builder
	output.WriteString(`<soap:Envelope xmlns:soap="`)
	if err := xml.EscapeText(&output, []byte(transport.EnvelopeNamespace)); err != nil {
		return nil, err
	}
	output.WriteString(`"><soap:Header>`)
	for index, parameter := range headerParameters {
		qualified := parameter.parameter.QualifiedName
		if qualified == nil || !healthXMLLocalNamePattern.MatchString(qualified.LocalName) || strings.TrimSpace(qualified.Namespace) == "" {
			return nil, errors.New("SOAP header parameter has no qualified name")
		}
		if err := healthSOAPWriteElement(&output, fmt.Sprintf("h%d", index), *qualified, parameter.value); err != nil {
			return nil, err
		}
	}
	output.WriteString(`</soap:Header><soap:Body><op:`)
	output.WriteString(transport.OperationQName.LocalName)
	output.WriteString(` xmlns:op="`)
	if err := xml.EscapeText(&output, []byte(transport.OperationQName.Namespace)); err != nil {
		return nil, err
	}
	output.WriteString(`">`)
	for index, parameter := range bodyParameters {
		name := healthOperationPlanQName{Namespace: transport.OperationQName.Namespace, LocalName: parameter.parameter.Name}
		if parameter.parameter.QualifiedName != nil {
			name = *parameter.parameter.QualifiedName
		}
		if err := healthSOAPWriteElement(&output, fmt.Sprintf("p%d", index), name, parameter.value); err != nil {
			return nil, err
		}
	}
	output.WriteString(`</op:`)
	output.WriteString(transport.OperationQName.LocalName)
	output.WriteString(`></soap:Body></soap:Envelope>`)
	return []byte(output.String()), nil
}

func healthSOAPWriteElement(output *strings.Builder, prefix string, name healthOperationPlanQName, value string) error {
	if !healthXMLLocalNamePattern.MatchString(name.LocalName) || strings.TrimSpace(name.Namespace) == "" {
		return errors.New("SOAP element name is invalid")
	}
	output.WriteByte('<')
	output.WriteString(prefix)
	output.WriteByte(':')
	output.WriteString(name.LocalName)
	output.WriteString(` xmlns:`)
	output.WriteString(prefix)
	output.WriteString(`="`)
	if err := xml.EscapeText(output, []byte(name.Namespace)); err != nil {
		return err
	}
	output.WriteString(`">`)
	if err := xml.EscapeText(output, []byte(value)); err != nil {
		return err
	}
	output.WriteString(`</`)
	output.WriteString(prefix)
	output.WriteByte(':')
	output.WriteString(name.LocalName)
	output.WriteByte('>')
	return nil
}

func healthOperationPlanResponseAssertion(plan healthOperationPlanRecord, response healthHTTPResponse) (bool, error) {
	contract := plan.RequestPlan.RequestContract
	if contract == nil || response.StatusCode < 100 || response.StatusCode > 599 || contract.ResponseAssertion.EmptyResultSemantics != "valid" {
		return false, errors.New("response assertion is unsupported")
	}
	switch contract.ResponseAssertion.Kind {
	case "http_status":
		for _, expected := range contract.ResponseAssertion.ExpectedStatusCodes {
			if response.StatusCode == expected {
				return true, nil
			}
		}
		return false, nil
	case "soap_fault_free":
		if contract.Transport.Protocol != "SOAP" || response.StatusCode < 200 || response.StatusCode >= 300 {
			return false, nil
		}
		return healthSOAPResponseFaultFree(response.Body, contract.Transport.EnvelopeNamespace)
	default:
		return false, errors.New("response assertion kind is unsupported")
	}
}

func healthSOAPResponseFaultFree(body []byte, envelopeNamespace string) (bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	rootSeen, bodySeen, faultSeen := false, false, false
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, err
		}
		switch current := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				rootSeen = current.Name.Local == "Envelope" && current.Name.Space == envelopeNamespace
			}
			if current.Name.Local == "Body" && current.Name.Space == envelopeNamespace {
				bodySeen = true
			}
			if bodySeen && current.Name.Local == "Fault" && current.Name.Space == envelopeNamespace {
				faultSeen = true
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				return false, errors.New("SOAP response nesting is invalid")
			}
		}
		if depth > healthOperationPlanMaxJSONDepth {
			return false, errors.New("SOAP response nesting exceeds the parser ceiling")
		}
	}
	if !rootSeen || !bodySeen || depth != 0 {
		return false, errors.New("SOAP response envelope is invalid")
	}
	return !faultSeen, nil
}
