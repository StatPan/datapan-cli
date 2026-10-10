package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// The reviewed policy set is read once per child under an 8 MiB file bound,
	// then only referenced rows are decoded. These limits describe the selected
	// artifact's parser budget; they do not assert Registry population size.
	healthOperationPolicyArtifactPath    = "policy/operation-observation-policies.v1.json"
	healthOperationPolicyMaxBytes        = 8 << 20
	healthOperationPolicyMaxTokens       = 500_000
	healthOperationPolicyMaxArtifactRefs = 32_000
	healthOperationPolicyMaxRows         = 8
)

type healthOperationPolicyRowKey struct {
	Section string
	Index   int
}

type healthSelectedOperationPolicy struct {
	Artifact healthOperationPlanArtifactRef
	Rows     map[healthOperationPolicyRowKey]map[string]any
}

func loadSelectedHealthOperationPolicy(root string, plan healthOperationPlanRecord, manifest releaseManifest) (healthSelectedOperationPolicy, error) {
	refs, err := healthOperationPlanEvidenceRefs(plan)
	if err != nil {
		return healthSelectedOperationPolicy{}, err
	}
	selectedRows := make(map[healthOperationPolicyRowKey]struct{})
	var policyDigest string
	for _, ref := range refs {
		if ref.EvidenceKind != "reviewed_policy" {
			continue
		}
		if ref.ArtifactPath == healthOperationResponseAssertionArtifactPathPrefix+plan.OperationIdentity.OperationID+".json" {
			if ref.JSONPointer != "#/assertion" && ref.JSONPointer != "#/review" {
				return healthSelectedOperationPolicy{}, errors.New("response assertion artifact pointer is invalid")
			}
			continue
		}
		if ref.ArtifactPath != healthOperationPolicyArtifactPath || !validSHA256(ref.SHA256) {
			return healthSelectedOperationPolicy{}, errors.New("reviewed policy evidence references an unsupported artifact")
		}
		if policyDigest != "" && !strings.EqualFold(policyDigest, ref.SHA256) {
			return healthSelectedOperationPolicy{}, errors.New("selected reviewed policy references disagree on digest")
		}
		policyDigest = ref.SHA256
		key, _, ok := parseHealthOperationPolicyPointer(ref.JSONPointer)
		if !ok {
			return healthSelectedOperationPolicy{}, errors.New("reviewed policy pointer is outside a bounded row")
		}
		selectedRows[key] = struct{}{}
		if len(selectedRows) > healthOperationPolicyMaxRows {
			return healthSelectedOperationPolicy{}, errors.New("selected reviewed policy row count exceeds its resource ceiling")
		}
	}
	if len(selectedRows) == 0 || policyDigest == "" {
		return healthSelectedOperationPolicy{}, errors.New("selected operation has no manifest-bound reviewed policy row")
	}

	artifact, ok := manifestArtifact(manifest, healthOperationPolicyArtifactPath)
	if !ok || artifact.Bytes < 1 || artifact.Bytes > healthOperationPolicyMaxBytes || !strings.EqualFold(artifact.SHA256, policyDigest) {
		return healthSelectedOperationPolicy{}, errors.New("reviewed policy artifact exceeds its selected-resource bound or differs from the release manifest")
	}
	path, ok := releaseArtifactPath(root, healthOperationPolicyArtifactPath)
	if !ok {
		return healthSelectedOperationPolicy{}, errors.New("reviewed policy artifact path is invalid")
	}
	data, err := readBoundedFile(path, healthOperationPolicyMaxBytes)
	if err != nil || int64(len(data)) != artifact.Bytes || !healthOperationPlanDigestMatches(artifact.SHA256, data) {
		return healthSelectedOperationPolicy{}, errors.New("reviewed policy artifact bytes are unavailable or altered")
	}
	if err := preflightHealthJSONWithLimits(data, healthOperationPolicyMaxTokens, healthOperationPolicyMaxArtifactRefs, true); err != nil {
		return healthSelectedOperationPolicy{}, errors.New("reviewed policy artifact exceeds its JSON resource bounds")
	}

	rows, schemaVersion, artifactKind, err := extractSelectedHealthOperationPolicyRows(data, selectedRows)
	if err != nil || schemaVersion != "datapan.operation-observation-policy.v1" || artifactKind != "operation_observation_policy_set" || len(rows) != len(selectedRows) {
		return healthSelectedOperationPolicy{}, errors.New("selected reviewed policy rows are unavailable or have an invalid envelope")
	}
	schema, err := healthOperationPolicyJSONSchema()
	if err != nil {
		return healthSelectedOperationPolicy{}, err
	}
	decoded := make(map[healthOperationPolicyRowKey]map[string]any, len(rows))
	for key, raw := range rows {
		if len(raw) == 0 || len(raw) > healthOperationPolicyMaxBytes {
			return healthSelectedOperationPolicy{}, errors.New("selected reviewed policy row exceeds its resource bound")
		}
		instanceData, err := healthOperationPolicyValidationEnvelope(key.Section, raw)
		if err != nil {
			return healthSelectedOperationPolicy{}, err
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(instanceData))
		if err != nil {
			return healthSelectedOperationPolicy{}, fmt.Errorf("selected reviewed policy wrapper cannot be decoded: %w", err)
		}
		if err := schema.Validate(instance); err != nil {
			return healthSelectedOperationPolicy{}, fmt.Errorf("selected reviewed policy row does not match its pinned schema: %w", err)
		}
		row, err := decodeHealthPolicyRow(raw)
		if err != nil {
			return healthSelectedOperationPolicy{}, errors.New("selected reviewed policy row cannot be decoded")
		}
		decoded[key] = row
	}
	selected := healthSelectedOperationPolicy{Artifact: healthOperationPlanArtifactRef{Path: artifact.Path, SHA256: artifact.SHA256, Bytes: artifact.Bytes}, Rows: decoded}
	for _, ref := range refs {
		if ref.EvidenceKind != "reviewed_policy" || ref.ArtifactPath == healthOperationResponseAssertionArtifactPathPrefix+plan.OperationIdentity.OperationID+".json" {
			continue
		}
		if _, ok := selected.resolve(ref.JSONPointer); !ok {
			return healthSelectedOperationPolicy{}, errors.New("reviewed policy evidence pointer does not resolve within a selected row")
		}
	}
	return selected, nil
}

func (policy healthSelectedOperationPolicy) resolve(pointer string) (any, bool) {
	key, suffix, ok := parseHealthOperationPolicyPointer(pointer)
	if !ok {
		return nil, false
	}
	row := policy.Rows[key]
	if row == nil {
		return nil, false
	}
	return healthJSONPointer(row, suffix)
}

func parseHealthOperationPolicyPointer(pointer string) (healthOperationPolicyRowKey, string, bool) {
	if !strings.HasPrefix(pointer, "#/") || strings.ContainsAny(pointer, "\r\n\x00") {
		return healthOperationPolicyRowKey{}, "", false
	}
	segments := strings.Split(strings.TrimPrefix(pointer, "#/"), "/")
	if len(segments) < 2 {
		return healthOperationPolicyRowKey{}, "", false
	}
	section := segments[0]
	if section != "policies" && section != "profiles" && section != "effect_profiles" {
		return healthOperationPolicyRowKey{}, "", false
	}
	indexText := segments[1]
	if indexText == "" || len(indexText) > 1 && indexText[0] == '0' {
		return healthOperationPolicyRowKey{}, "", false
	}
	for _, c := range indexText {
		if c < '0' || c > '9' {
			return healthOperationPolicyRowKey{}, "", false
		}
	}
	index, err := strconv.Atoi(indexText)
	if err != nil || index < 0 {
		return healthOperationPolicyRowKey{}, "", false
	}
	key := healthOperationPolicyRowKey{Section: section, Index: index}
	if len(segments) == 2 {
		return key, "#", true
	}
	return key, "#/" + strings.Join(segments[2:], "/"), true
}

func extractSelectedHealthOperationPolicyRows(data []byte, selected map[healthOperationPolicyRowKey]struct{}) (map[healthOperationPolicyRowKey]json.RawMessage, string, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, "", "", errors.New("reviewed policy envelope is not an object")
	}
	rows := make(map[healthOperationPolicyRowKey]json.RawMessage, len(selected))
	schemaVersion, artifactKind := "", ""
	tokens := 1
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, "", "", err
		}
		tokens++
		key, ok := token.(string)
		if !ok {
			return nil, "", "", errors.New("reviewed policy envelope key is invalid")
		}
		switch key {
		case "schema_version":
			if err := decoder.Decode(&schemaVersion); err != nil {
				return nil, "", "", err
			}
			tokens++
		case "artifact_kind":
			if err := decoder.Decode(&artifactKind); err != nil {
				return nil, "", "", err
			}
			tokens++
		case "policies", "profiles", "effect_profiles":
			open, err := decoder.Token()
			if err != nil || open != json.Delim('[') {
				return nil, "", "", errors.New("reviewed policy row collection is not an array")
			}
			tokens++
			for index := 0; decoder.More(); index++ {
				rowKey := healthOperationPolicyRowKey{Section: key, Index: index}
				if _, wanted := selected[rowKey]; wanted {
					var raw json.RawMessage
					if err := decoder.Decode(&raw); err != nil {
						return nil, "", "", err
					}
					if int64(len(raw)) > healthOperationPolicyMaxBytes {
						return nil, "", "", errors.New("selected reviewed policy row exceeds its resource bound")
					}
					rows[rowKey] = raw
					// The whole artifact passed the token preflight before this selected-row decode.
				} else if err := skipHealthPolicyJSONValue(decoder, &tokens, 1); err != nil {
					return nil, "", "", err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return nil, "", "", errors.New("reviewed policy row collection is incomplete")
			}
			tokens++
		default:
			if err := skipHealthPolicyJSONValue(decoder, &tokens, 1); err != nil {
				return nil, "", "", err
			}
		}
		if tokens > healthOperationPolicyMaxTokens {
			return nil, "", "", errors.New("reviewed policy token budget exceeded")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, "", "", errors.New("reviewed policy envelope is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, "", "", errors.New("reviewed policy has trailing JSON")
	}
	return rows, schemaVersion, artifactKind, nil
}

func skipHealthPolicyJSONValue(decoder *json.Decoder, tokenCount *int, depth int) error {
	if depth > healthOperationPlanMaxJSONDepth {
		return errors.New("reviewed policy nesting budget exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	*tokenCount++
	if *tokenCount > healthOperationPolicyMaxTokens {
		return errors.New("reviewed policy token budget exceeded")
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			*tokenCount++
			if *tokenCount > healthOperationPolicyMaxTokens {
				return errors.New("reviewed policy token budget exceeded")
			}
			if err := skipHealthPolicyJSONValue(decoder, tokenCount, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("reviewed policy object is incomplete")
		}
		*tokenCount++
	case '[':
		for decoder.More() {
			if err := skipHealthPolicyJSONValue(decoder, tokenCount, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("reviewed policy array is incomplete")
		}
		*tokenCount++
	default:
		return errors.New("reviewed policy JSON container is invalid")
	}
	return nil
}

func healthOperationPolicyValidationEnvelope(section string, raw json.RawMessage) ([]byte, error) {
	if section != "policies" && section != "profiles" && section != "effect_profiles" {
		return nil, errors.New("reviewed policy section is unsupported")
	}
	sections := map[string]json.RawMessage{
		"policies": json.RawMessage("[]"),
		"profiles": json.RawMessage("[]"),
	}
	if section == "effect_profiles" {
		sections[section] = json.RawMessage("[]")
	}
	sections[section] = json.RawMessage("[" + string(raw) + "]")
	envelope := map[string]json.RawMessage{
		"schema_version": json.RawMessage(`"datapan.operation-observation-policy.v1"`),
		"artifact_kind":  json.RawMessage(`"operation_observation_policy_set"`),
	}
	for name, value := range sections {
		envelope[name] = value
	}
	return json.Marshal(envelope)
}

func decodeHealthPolicyRow(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil || row == nil {
		return nil, errors.New("reviewed policy row is not an object")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("reviewed policy row has trailing JSON")
	}
	return row, nil
}

func validateSelectedHealthOperationEffectPolicy(plan healthOperationPlanRecord, policy healthSelectedOperationPolicy, documents map[string]map[string]any) error {
	contract := plan.RequestPlan.RequestContract
	if contract == nil {
		return errors.New("operation effect has no request contract")
	}
	switch contract.OperationEffect.Authority {
	case "reviewed_policy":
		// Continue below with the Registry-owned reviewed policy row.
	case "operation_document":
		return validateSelectedHealthOperationDocumentEffect(plan, contract, documents)
	case "operation_specific_declaration":
		// No pinned Registry fact currently maps this authority to a typed
		// documented read-only declaration, so it remains non-executable.
		return errors.New("operation-specific effect declaration has no supported source-fact mapping")
	default:
		return errors.New("operation effect authority is unsupported")
	}
	var effectReviewRef *healthOperationPlanEvidenceRef
	for index := range contract.OperationEffect.EvidenceRefs {
		ref := &contract.OperationEffect.EvidenceRefs[index]
		if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath == healthOperationPolicyArtifactPath && strings.HasSuffix(ref.JSONPointer, "/effect_review") {
			if effectReviewRef != nil {
				return errors.New("operation effect has multiple selected read-only policies")
			}
			effectReviewRef = ref
		}
	}
	if effectReviewRef == nil {
		return errors.New("operation effect has no selected read-only policy")
	}
	reviewValue, ok := policy.resolve(effectReviewRef.JSONPointer)
	review, isObject := reviewValue.(map[string]any)
	if !ok || !isObject || review["classification"] != "read_only" || review["basis"] != "rfc9110_safe_method_and_retrieval_purpose" || review["rfc_reference"] != "https://www.rfc-editor.org/rfc/rfc9110#section-9.2.1" || strings.TrimSpace(policyString(review["rationale"])) == "" {
		return errors.New("selected read-only policy does not carry the approved effect basis")
	}
	key, _, ok := parseHealthOperationPolicyPointer(effectReviewRef.JSONPointer)
	if !ok {
		return errors.New("selected read-only policy pointer is invalid")
	}
	row := policy.Rows[key]
	if row == nil {
		return errors.New("selected read-only policy row is unavailable")
	}
	selector, _ := row["selector"].(map[string]any)
	if selector != nil {
		if selector["source_id"] != plan.SourceBinding.SourceID || selector["provider"] != plan.SourceBinding.Provider || selector["protocol"] != plan.OperationIdentity.Protocol || selector["method"] != contract.Transport.HTTPMethod {
			return errors.New("selected read-only policy selector differs from the operation")
		}
		if selectedEffect, exists := selector["effect"]; exists && selectedEffect != "read_only" {
			return errors.New("selected read-only policy selector has another effect")
		}
	}
	if key.Section == "policies" {
		identity, _ := row["identity"].(map[string]any)
		if !healthOperationPolicyIdentityMatches(identity, plan) {
			return errors.New("selected read-only policy identity differs from the operation")
		}
	}
	if !healthOperationEffectPolicyReviewRefsBound(contract.OperationEffect.EvidenceRefs, effectReviewRef.JSONPointer) {
		return errors.New("selected read-only policy is missing its reviewed decision reference")
	}
	if err := validateHealthOperationEffectPolicyReferenceSet(contract.OperationEffect.EvidenceRefs, effectReviewRef.JSONPointer); err != nil {
		return err
	}
	return validateHealthOperationEffectSourceFacts(plan, contract, effectReviewRef.JSONPointer, review, documents)
}

func validateSelectedHealthOperationDocumentEffect(plan healthOperationPlanRecord, contract *healthOperationPlanRequestContract, documents map[string]map[string]any) error {
	soapTransport := false
	switch contract.Transport.Protocol {
	case "REST":
		if contract.Transport.HTTPMethod != "GET" && contract.Transport.HTTPMethod != "HEAD" {
			return errors.New("documented REST read-only effect requires the exact GET or HEAD method")
		}
	case "SOAP":
		if contract.Transport.HTTPMethod != "POST" || contract.Transport.BodyEncoding != "document_literal" || !healthOperationPlanSOAPEnvelopeMatches(contract.Transport.SOAPVersion, contract.Transport.EnvelopeNamespace) {
			return errors.New("documented SOAP read-only effect requires the exact POST document-literal envelope")
		}
		// SOAP uses POST at the HTTP layer. Admit it only through this
		// operation-document authority path; a reviewed HTTP-safe method policy
		// cannot classify SOAP POST as read-only.
		soapTransport = true
	default:
		return errors.New("documented read-only effect uses an unsupported protocol")
	}
	var effectRef *healthOperationPlanEvidenceRef
	for index := range contract.OperationEffect.EvidenceRefs {
		ref := &contract.OperationEffect.EvidenceRefs[index]
		if ref.EvidenceKind != "operation_document" || ref.JSONPointer != "#/effect" {
			return errors.New("documented operation effect contains an unsupported source reference")
		}
		if ref.JSONPointer == "#/effect" {
			if effectRef != nil {
				return errors.New("documented operation effect has ambiguous effect facts")
			}
			effectRef = ref
		}
	}
	if effectRef == nil || !validSHA256(effectRef.SHA256) {
		return errors.New("documented operation effect has no digest-bound effect fact")
	}
	document := documents[effectRef.ArtifactPath]
	if document == nil || stringValueFromJSON(document["schema_version"]) != "datapan.operation-document-evidence.v2" {
		return errors.New("documented operation effect requires the source-bound evidence-v2 identity")
	}
	if soapTransport {
		refs, err := healthOperationPlanEvidenceRefs(plan)
		if err != nil {
			return err
		}
		if err := validateHealthOperationDocumentTransportFacts(plan, refs, documents); err != nil {
			return errors.New("documented SOAP transport facts do not match the selected operation")
		}
		for _, ref := range contract.Transport.EvidenceRefs {
			if ref.EvidenceKind != "operation_document" || ref.ArtifactPath != effectRef.ArtifactPath || !strings.EqualFold(ref.SHA256, effectRef.SHA256) {
				return errors.New("SOAP transport facts and read-only effect must use one exact source document")
			}
		}
	}
	if err := validateHealthOperationDocumentEvidenceIdentity(document, plan); err != nil {
		return err
	}
	effectValue, ok := healthJSONPointer(document, "#/effect")
	effect, isObject := effectValue.(map[string]any)
	if !ok || !isObject || effect["status"] != "documented" || effect["classification"] != "read_only" || effect["authority"] != "operation_document" || !healthPolicySourceRefsContain(effect["source_refs"], "operation_effect") {
		return errors.New("operation-document effect fact does not explicitly document read-only behavior")
	}
	var methodRef *healthOperationPlanEvidenceRef
	for index := range contract.Transport.EvidenceRefs {
		ref := &contract.Transport.EvidenceRefs[index]
		if ref.EvidenceKind == "operation_document" && ref.JSONPointer == "#/transport/http_method" {
			if methodRef != nil {
				return errors.New("documented HTTP method has ambiguous source facts")
			}
			methodRef = ref
		}
	}
	if methodRef == nil || methodRef.ArtifactPath != effectRef.ArtifactPath || !strings.EqualFold(methodRef.SHA256, effectRef.SHA256) {
		return errors.New("documented effect and exact HTTP method are not bound to the same evidence artifact")
	}
	methodValue, ok := healthJSONPointer(document, methodRef.JSONPointer)
	methodFact, isObject := methodValue.(map[string]any)
	if !ok || !isObject || methodFact["status"] != "documented" || methodFact["authority_scope"] != "operation_specific" || methodFact["value"] != contract.Transport.HTTPMethod || !healthPolicySourceRefsContain(methodFact["source_refs"], "operation_http_method") {
		return errors.New("documented effect lacks the exact operation-specific HTTP method evidence")
	}
	operationDocument, _ := document["operation_document"].(map[string]any)
	for _, fieldName := range []string{"title", "purpose"} {
		fact, _ := operationDocument[fieldName].(map[string]any)
		if fact != nil && fact["status"] == "documented" && healthOperationPurposeContainsMutation(strings.ToLower(policyString(fact["value"]))) {
			return errors.New("source title or purpose conflicts with the documented read-only effect")
		}
	}
	return validateHealthOperationEffectActionSelectors(document)
}

func healthOperationEffectPolicyReviewRefsBound(refs []healthOperationPlanEvidenceRef, effectReviewPointer string) bool {
	key, _, ok := parseHealthOperationPolicyPointer(effectReviewPointer)
	if !ok {
		return false
	}
	base := fmt.Sprintf("#/%s/%d", key.Section, key.Index)
	gotReview := false
	gotSelector := key.Section == "policies"
	for _, ref := range refs {
		if ref.EvidenceKind != "reviewed_policy" || ref.ArtifactPath != healthOperationPolicyArtifactPath {
			continue
		}
		gotReview = gotReview || ref.JSONPointer == base+"/review"
		gotSelector = gotSelector || ref.JSONPointer == base+"/selector"
	}
	return gotReview && gotSelector
}

func validateHealthOperationEffectPolicyReferenceSet(refs []healthOperationPlanEvidenceRef, effectReviewPointer string) error {
	key, _, ok := parseHealthOperationPolicyPointer(effectReviewPointer)
	if !ok {
		return errors.New("selected effect policy pointer is malformed")
	}
	base := fmt.Sprintf("#/%s/%d", key.Section, key.Index)
	effectReviewCount, reviewCount, selectorCount := 0, 0, 0
	for _, ref := range refs {
		if ref.EvidenceKind != "reviewed_policy" {
			continue
		}
		if ref.ArtifactPath != healthOperationPolicyArtifactPath {
			return errors.New("read-only effect points to an unrecognized policy artifact")
		}
		switch ref.JSONPointer {
		case effectReviewPointer:
			effectReviewCount++
		case base + "/review":
			reviewCount++
		case base + "/selector":
			selectorCount++
		case base:
			// A whole-row reference is a supplementary binding, not authority by itself.
		default:
			return errors.New("read-only effect contains an unrelated policy pointer")
		}
	}
	wantSelector := 0
	if key.Section == "profiles" || key.Section == "effect_profiles" {
		wantSelector = 1
	}
	if effectReviewCount != 1 || reviewCount != 1 || selectorCount != wantSelector {
		return errors.New("read-only effect policy references are incomplete or duplicated")
	}
	return nil
}

func validateHealthOperationEffectSourceFacts(plan healthOperationPlanRecord, contract *healthOperationPlanRequestContract, effectReviewPointer string, effectReview map[string]any, documents map[string]map[string]any) error {
	if contract.Transport.Protocol != "REST" || (contract.Transport.HTTPMethod != "GET" && contract.Transport.HTTPMethod != "HEAD") {
		return errors.New("reviewed read-only policy only supports an explicitly documented REST GET or HEAD")
	}
	var document map[string]any
	selectedDocumentPath, selectedDocumentSHA := "", ""
	documentPointerCounts := map[string]int{}
	for _, ref := range contract.OperationEffect.EvidenceRefs {
		if ref.EvidenceKind != "operation_document" || (ref.JSONPointer != "#/transport/http_method" && ref.JSONPointer != "#/operation_document/title" && ref.JSONPointer != "#/operation_document/purpose") {
			continue
		}
		documentPointerCounts[ref.JSONPointer]++
		candidate := documents[ref.ArtifactPath]
		if candidate == nil {
			return errors.New("operation effect source evidence is unavailable")
		}
		if document == nil {
			document, selectedDocumentPath, selectedDocumentSHA = candidate, ref.ArtifactPath, ref.SHA256
		} else if ref.ArtifactPath != selectedDocumentPath || !strings.EqualFold(ref.SHA256, selectedDocumentSHA) {
			return errors.New("read-only operation facts resolve across different source documents")
		}
		fact, ok := healthJSONPointer(candidate, ref.JSONPointer)
		factMap, isObject := fact.(map[string]any)
		if !ok || !isObject || factMap["status"] != "documented" {
			return errors.New("read-only operation fact is not source-documented")
		}
		switch ref.JSONPointer {
		case "#/transport/http_method":
			if factMap["authority_scope"] != "operation_specific" || factMap["value"] != contract.Transport.HTTPMethod || !healthPolicySourceRefsContain(factMap["source_refs"], "operation_http_method") {
				return errors.New("operation-specific method evidence does not match the selected GET or HEAD")
			}
		case "#/operation_document/title":
			if policyString(factMap["value"]) == "" || !healthPolicySourceRefsContain(factMap["source_refs"], "official_operation_title") {
				return errors.New("operation title does not have official source evidence")
			}
		case "#/operation_document/purpose":
			if policyString(factMap["value"]) == "" || !healthPolicySourceRefsContain(factMap["source_refs"], "official_operation_purpose") {
				return errors.New("operation purpose does not have official source evidence")
			}
		}
	}
	if document == nil || documentPointerCounts["#/transport/http_method"] != 1 || documentPointerCounts["#/operation_document/title"] != 1 || documentPointerCounts["#/operation_document/purpose"] != 1 {
		return errors.New("read-only operation facts are incomplete or duplicated")
	}
	identity, _ := document["identity"].(map[string]any)
	if identity["source_id"] != plan.SourceBinding.SourceID || identity["provider"] != plan.SourceBinding.Provider || identity["operation_id"] != plan.OperationIdentity.OperationID || identity["protocol"] != plan.OperationIdentity.Protocol {
		return errors.New("operation-document identity differs from the selected plan")
	}
	sourceEffect, _ := document["effect"].(map[string]any)
	if sourceEffect == nil || sourceEffect["status"] == "documented" && sourceEffect["classification"] != "read_only" || sourceEffect["status"] != "documented" && sourceEffect["status"] != "unknown" {
		return errors.New("source operation effect conflicts with reviewed read-only policy")
	}
	operationDocument, _ := document["operation_document"].(map[string]any)
	title, titleOK := operationDocument["title"].(map[string]any)
	purpose, purposeOK := operationDocument["purpose"].(map[string]any)
	if !titleOK || !purposeOK || title["status"] != "documented" || purpose["status"] != "documented" || !healthPolicySourceRefsContain(title["source_refs"], "official_operation_title") || !healthPolicySourceRefsContain(purpose["source_refs"], "official_operation_purpose") {
		return errors.New("operation title or retrieval purpose is not source-bound")
	}
	titleText, purposeText := strings.ToLower(policyString(title["value"])), strings.ToLower(policyString(purpose["value"]))
	if titleText == "" || purposeText == "" || healthOperationPurposeContainsMutation(titleText+" "+purposeText) {
		return errors.New("operation title or purpose conflicts with read-only classification")
	}
	terms, ok := effectReview["purpose_terms"].([]any)
	if !ok || len(terms) == 0 || len(terms) > 9 {
		return errors.New("read-only policy purpose terms are invalid")
	}
	matchedPurpose := false
	for _, value := range terms {
		term := policyString(value)
		marker, valid := healthOperationReadPurposeMarker(term)
		if !valid {
			return errors.New("read-only policy contains an unsupported purpose term")
		}
		if strings.Contains(purposeText, marker) {
			matchedPurpose = true
		}
	}
	if !matchedPurpose {
		return errors.New("source purpose does not support the reviewed retrieval classification")
	}
	return validateHealthOperationEffectActionSelectors(document)
}

func validateHealthOperationEffectActionSelectors(document map[string]any) error {
	transport, _ := document["transport"].(map[string]any)
	if selectors, ok := transport["fixed_query_selectors"].([]any); ok {
		for _, raw := range selectors {
			selector, _ := raw.(map[string]any)
			if selector["status"] == "documented" && (selector["role"] == "operation_selector" || selector["role"] == "action_selector" || selector["role"] == "command_selector") {
				return errors.New("operation contains a caller-controlled or conflicting action selector")
			}
		}
	}
	if parameters, ok := document["parameters"].([]any); ok {
		for _, raw := range parameters {
			parameter, _ := raw.(map[string]any)
			name := strings.ToLower(policyString(parameter["name"]))
			if name == "action" || name == "actiontype" || name == "command" || name == "method" || name == "op" || name == "operation" || name == "operationid" {
				return errors.New("operation parameter is a caller-controlled action selector")
			}
		}
	}
	return nil
}

func healthOperationReadPurposeMarker(term string) (string, bool) {
	switch term {
	case "조회", "목록", "검색", "현황", "retrieve", "list", "search", "read", "lookup":
		return strings.ToLower(term), true
	default:
		return "", false
	}
}

func healthOperationPurposeContainsMutation(text string) bool {
	for _, marker := range []string{"등록", "수정", "삭제", "변경", "추가", "신청", "취소", "발급", "전송", "처리", "실행", "갱신", "업데이트", "작성", "제출", "create", "update", "delete", "write", "submit", "insert", "modify", "cancel", "issue", "send", "execute", "apply", "withdraw", "remove"} {
		if strings.Contains(text, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func healthPolicySourceRefsContain(value any, evidenceKind string) bool {
	refs, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range refs {
		ref, _ := raw.(map[string]any)
		if ref["evidence_kind"] == evidenceKind {
			return true
		}
	}
	return false
}

func healthOperationPolicyIdentityMatches(identity map[string]any, plan healthOperationPlanRecord) bool {
	if identity == nil {
		return false
	}
	return identity["source_id"] == plan.SourceBinding.SourceID && identity["provider"] == plan.SourceBinding.Provider && identity["protocol"] == plan.OperationIdentity.Protocol &&
		identity["operation_id"] == plan.OperationIdentity.OperationID && identity["dataset_id"] == plan.OperationIdentity.DatasetID && identity["operation_name"] == plan.OperationIdentity.OperationName && identity["upstream_operation_key"] == plan.OperationIdentity.UpstreamOperationKey
}

func policyString(value any) string {
	text, _ := value.(string)
	return text
}

func validateHealthResponseAssertionPolicyBindings(plan healthOperationPlanRecord, index healthOperationPlanIndex, manifest releaseManifest, artifact healthOperationResponseAssertionV2Artifact, selected healthSelectedOperationPolicy) error {
	contract := plan.RequestPlan.RequestContract
	if contract == nil {
		return errors.New("assertion policy binding has no request contract")
	}
	var requestRowRef *healthOperationPlanEvidenceRef
	allRefs, err := healthOperationPlanEvidenceRefs(plan)
	if err != nil {
		return err
	}
	for index := range allRefs {
		ref := &allRefs[index]
		if ref.EvidenceKind != "reviewed_policy" || ref.ArtifactPath != healthOperationPolicyArtifactPath {
			continue
		}
		key, suffix, ok := parseHealthOperationPolicyPointer(ref.JSONPointer)
		if !ok || suffix != "#" || (key.Section != "policies" && key.Section != "profiles") {
			continue
		}
		if requestRowRef != nil {
			return errors.New("request contract binds multiple reviewed policy rows")
		}
		requestRowRef = ref
	}
	if requestRowRef == nil {
		return errors.New("request contract has no exact reviewed policy row")
	}
	key, _, ok := parseHealthOperationPolicyPointer(requestRowRef.JSONPointer)
	row := selected.Rows[key]
	if !ok || row == nil {
		return errors.New("selected request policy row is unavailable")
	}
	request, ok := row["request"].(map[string]any)
	if !ok {
		return errors.New("selected request policy row has no request policy")
	}
	if key.Section == "policies" {
		identity, _ := row["identity"].(map[string]any)
		if !healthOperationPolicyIdentityMatches(identity, plan) {
			return errors.New("selected operation policy identity differs from its plan")
		}
		documentRef, _ := row["document_evidence"].(map[string]any)
		if policyString(documentRef["path"]) != artifact.DocumentEvidence.Path || policyString(documentRef["sha256"]) != artifact.DocumentEvidence.SHA256 || policyInt(documentRef["bytes"]) != artifact.DocumentEvidence.Bytes {
			return errors.New("selected operation policy is not bound to the plan document evidence")
		}
		if !healthPolicyJSONValuesEqual(row["review"], artifact.Review) {
			return errors.New("selected operation policy review differs from the response assertion review")
		}
		if err := validateHealthOperationPolicyLimits(request, contract); err != nil {
			return err
		}
		if err := validateHealthOperationPolicyParameterStrategies(request, contract); err != nil {
			return err
		}
		assertionRef, _ := request["response_assertion_artifact"].(map[string]any)
		assertionPath, _, hasPointer := strings.Cut(contract.ResponseAssertion.AssertionRef, "#")
		manifestRef, exists := manifestArtifact(manifest, assertionPath)
		if !hasPointer || !exists || manifestRef.Bytes > healthOperationResponseAssertionMaxBytes || policyString(assertionRef["path"]) != assertionPath || !strings.EqualFold(policyString(assertionRef["sha256"]), manifestRef.SHA256) || policyInt(assertionRef["bytes"]) != manifestRef.Bytes {
			return errors.New("selected operation policy assertion artifact differs from the plan")
		}
		for _, ref := range contract.ResponseAssertion.EvidenceRefs {
			if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath == assertionPath && ref.JSONPointer == "#/assertion" && strings.EqualFold(ref.SHA256, manifestRef.SHA256) {
				return nil
			}
		}
		return errors.New("selected operation policy assertion binding is missing from the plan")
	}
	selector, _ := row["selector"].(map[string]any)
	if selector == nil || selector["source_id"] != plan.SourceBinding.SourceID || selector["provider"] != plan.SourceBinding.Provider || selector["protocol"] != plan.OperationIdentity.Protocol || selector["effect"] != "read_only" || selector["method"] != contract.Transport.HTTPMethod {
		return errors.New("selected reusable request profile does not match the operation scope")
	}
	auth, _ := selector["authentication"].(map[string]any)
	if auth == nil || auth["requirement"] != contract.Authentication.Requirement || auth["mechanism"] != contract.Authentication.Mechanism || auth["placement"] != contract.Authentication.Placement {
		return errors.New("selected reusable request profile authentication scope differs from the plan")
	}
	if parameterName := policyString(auth["parameter_name"]); parameterName != contract.Authentication.ParameterName {
		return errors.New("selected reusable request profile credential parameter differs from the plan")
	}
	if err := validateHealthOperationPolicyLimits(request, contract); err != nil {
		return err
	}
	if err := validateHealthOperationPolicyParameterStrategies(request, contract); err != nil {
		return err
	}
	profileResponse, _ := request["response"].(map[string]any)
	if profileResponse == nil || artifact.Review == nil || !healthPolicyJSONValuesEqual(row["review"], artifact.Review) {
		return errors.New("selected reusable request profile response payload differs from the assertion")
	}
	if artifact.Assertion.Mode == "observation_only" {
		if artifact.Assertion.PayloadKind != "" || len(artifact.Assertion.Branches) != 0 || profileResponse["mode"] != "observation_only" || len(profileResponse) != 1 {
			return errors.New("selected reusable request profile response mode differs from the observation-only assertion")
		}
		return nil
	}
	if profileResponse["payload_kind"] != artifact.Assertion.PayloadKind {
		return errors.New("selected reusable request profile response payload differs from the assertion")
	}
	branches, _ := profileResponse["branches"].([]any)
	if len(branches) != len(artifact.Assertion.Branches) {
		return errors.New("selected reusable request profile branch count differs from the assertion")
	}
	for index, branch := range artifact.Assertion.Branches {
		profileBranch, _ := branches[index].(map[string]any)
		branchRef := healthOperationPolicyBranchReviewRef(requestRowRef, index)
		if profileBranch == nil || !healthAssertionBranchReviewBound(branch, branchRef) || profileBranch["branch_id"] != branch.ID || profileBranch["classification"] != branch.Classification || profileBranch["empty_result_semantics"] != branch.EmptyResultSemantics || !healthOperationPolicyProfileBranchMatches(profileBranch, branch) {
			return errors.New("selected response assertion branch differs from its reviewed request profile")
		}
	}
	return nil
}

func validateHealthOperationPolicyLimits(request map[string]any, contract *healthOperationPlanRequestContract) error {
	limits, _ := request["limits"].(map[string]any)
	if limits == nil || policyInt(limits["request_budget"]) != int64(contract.Limits.RequestBudget) || policyInt(limits["timeout_ms"]) != int64(contract.Limits.TimeoutMS) || policyInt(limits["max_request_bytes"]) != contract.Limits.MaxRequestBytes || policyInt(limits["max_response_bytes"]) != contract.Limits.MaxResponseBytes {
		return errors.New("selected request profile limits differ from the compiled plan")
	}
	return nil
}

func validateHealthOperationPolicyParameterStrategies(request map[string]any, contract *healthOperationPlanRequestContract) error {
	strategies := healthPolicySelectedRequestStrategySet(request)
	parameters := make(map[string]healthOperationPlanParameter, len(contract.Parameters))
	for _, parameter := range contract.Parameters {
		if parameter.ValueStrategy.Kind == "credential_reference" {
			continue
		}
		if _, duplicate := parameters[parameter.Name]; duplicate {
			return errors.New("compiled request parameters contain a duplicate name")
		}
		parameters[parameter.Name] = parameter
	}
	if len(strategies) != len(parameters) {
		return errors.New("selected request policy strategy set differs from compiled parameters")
	}
	for name, strategy := range strategies {
		parameter, ok := parameters[name]
		if !ok || parameter.ValueStrategy.Authority != "reviewed_policy" || !healthPolicyStrategyMatchesParameter(strategy, parameter) {
			return errors.New("selected request policy strategy differs from compiled parameter values")
		}
	}
	return nil
}

func healthOperationPolicyProfileBranchMatches(profile map[string]any, assertion healthOperationResponseAssertionV2) bool {
	selector, _ := profile["selector"].(map[string]any)
	if selector == nil || profile["code_mode"] == "" || !healthPolicyJSONValuesEqual(selector["accepted_http_status_codes"], assertion.Selector.AcceptedHTTPStatusCodes) || selector["root_kind"] != assertion.Selector.RootKind {
		return false
	}
	if profile["code_mode"] == "none" && assertion.ProviderResultCodeStatus != "none_by_policy" || profile["code_mode"] == "source" && assertion.ProviderResultCodeStatus == "none_by_policy" {
		return false
	}
	if assertion.Selector.RootQName == nil {
		if _, present := selector["root_qname"]; present {
			return false
		}
	} else if !healthPolicyJSONValuesEqual(selector["root_qname"], assertion.Selector.RootQName) {
		return false
	}
	policyDiscriminators, _ := selector["discriminators"].([]any)
	if len(policyDiscriminators) != len(assertion.Selector.Discriminators) {
		return false
	}
	for index, discriminator := range assertion.Selector.Discriminators {
		policyDiscriminator, _ := policyDiscriminators[index].(map[string]any)
		if policyDiscriminator == nil || policyDiscriminator["predicate"] != discriminator.Predicate || !healthPolicyOptionalStringMatches(policyDiscriminator, "value_type", discriminator.ValueType) || !healthPolicyJSONValuesEqual(policyDiscriminator["path"], discriminator.Path) || !healthPolicyJSONValuesEqual(policyDiscriminator["values"], discriminator.Values) {
			return false
		}
	}
	policyFields, _ := profile["required_fields"].([]any)
	if len(policyFields) != len(assertion.RequiredFields) {
		return false
	}
	for index, field := range assertion.RequiredFields {
		policyField, _ := policyFields[index].(map[string]any)
		cardinality := map[string]any{"minimum": field.Cardinality.Minimum, "maximum": field.Cardinality.Maximum}
		if policyField == nil || policyField["value_type"] != field.ValueType || !healthPolicyJSONValuesEqual(policyField["path"], field.Path) || !healthPolicyJSONValuesEqual(map[string]any{"minimum": policyField["minimum"], "maximum": policyField["maximum"]}, cardinality) {
			return false
		}
	}
	collection, hasCollection := profile["result_collection"]
	if collection == nil {
		hasCollection = false
	}
	if hasCollection != (assertion.ResultCollection != nil) {
		return false
	}
	if hasCollection {
		policyCollection, ok := collection.(map[string]any)
		if !ok || !healthPolicyJSONValuesEqual(policyCollection["path"], assertion.ResultCollection.Path) || !healthPolicyJSONValuesEqual(policyCollection["container_path"], assertion.ResultCollection.ContainerPath) || !healthPolicyJSONValuesEqual(policyCollection["item_path"], assertion.ResultCollection.ItemPath) || assertion.ResultCollection.Semantics != profile["empty_result_semantics"] {
			return false
		}
	}
	return true
}

func healthPolicyOptionalStringMatches(object map[string]any, key, expected string) bool {
	value, present := object[key]
	if expected == "" {
		return !present
	}
	actual, ok := value.(string)
	return ok && actual == expected
}

func healthPolicyJSONValuesEqual(left, right any) bool {
	leftRaw, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightRaw, err := json.Marshal(right)
	if err != nil {
		return false
	}
	decode := func(raw []byte) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
	leftValue, err := decode(leftRaw)
	if err != nil {
		return false
	}
	rightValue, err := decode(rightRaw)
	return err == nil && reflect.DeepEqual(leftValue, rightValue)
}

func policyInt(value any) int64 {
	switch number := value.(type) {
	case json.Number:
		integer, _ := number.Int64()
		return integer
	case int:
		return int64(number)
	case int64:
		return number
	default:
		return -1
	}
}

func healthOperationPolicyBranchReviewRef(requestRowRef *healthOperationPlanEvidenceRef, index int) string {
	if requestRowRef == nil {
		return ""
	}
	return requestRowRef.JSONPointer + "/request/response/branches/" + strconv.Itoa(index)
}

func healthAssertionBranchReviewBound(branch healthOperationResponseAssertionV2, expectedPointer string) bool {
	if expectedPointer == "" {
		return false
	}
	for _, ref := range branch.ReviewRefs {
		if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath == healthOperationPolicyArtifactPath && ref.JSONPointer == expectedPointer {
			return true
		}
	}
	return false
}

func healthPolicySelectedRequestStrategySet(request map[string]any) map[string]map[string]any {
	strategies := map[string]map[string]any{}
	rows, _ := request["parameter_strategies"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		name := policyString(row["name"])
		strategy, _ := row["strategy"].(map[string]any)
		if name != "" && strategy != nil {
			strategies[name] = strategy
		}
	}
	return strategies
}

func healthPolicyStrategyMatchesParameter(strategy map[string]any, parameter healthOperationPlanParameter) bool {
	if strategy == nil || strategy["kind"] != parameter.ValueStrategy.Kind {
		return false
	}
	encoded, err := json.Marshal(parameter.ValueStrategy)
	if err != nil {
		return false
	}
	var expected map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&expected) != nil {
		return false
	}
	delete(expected, "authority")
	return reflect.DeepEqual(expected, strategy)
}

func (key healthOperationPolicyRowKey) String() string {
	return fmt.Sprintf("%s[%d]", key.Section, key.Index)
}
