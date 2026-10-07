package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestHealthOperationPlanSchemaMirrorsAndRegistryFixtureJSON(t *testing.T) {
	schemaPaths := []string{
		"testdata/operation-observation-plan/schema.json",
		"../../schemas/datapan.operation-observation-plan.v1.schema.json",
	}
	for _, path := range schemaPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !healthOperationPlanDigestMatches(healthOperationPlanSchemaSHA256, data) || !bytes.Equal(data, embeddedHealthOperationPlanSchema) {
			t.Fatalf("schema mirror %s differs from the pinned Registry contract", path)
		}
	}
	for _, test := range []struct {
		path   string
		digest string
	}{
		{"testdata/operation-observation-plan/synthetic-rest-list.json", "9d0f1d3dd060387e8a835a4abd5d57c28237f8d293cb649d894740beb0ddab91"},
		{"testdata/operation-observation-plan/synthetic-soap-read.json", "4aba5f4007e7b96517c03e0d3e806fd342ac85b5ea4c30ed5986eebe662f8a8f"},
		{"testdata/operation-observation-plan/fixtures/synthetic-source.json", "1532cc4a9d728d0cea6c614d4a0ade1f9ed22bfc33b28101de05f7855bf07d3c"},
	} {
		data, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if !healthOperationPlanDigestMatches(test.digest, data) {
			t.Fatalf("Registry fixture %s differs from its source commit", test.path)
		}
		if strings.Contains(test.path, "synthetic-source.json") {
			if err := preflightHealthOperationPlanJSON(data); err != nil {
				t.Fatalf("Registry source fixture failed bounded JSON preflight: %v", err)
			}
			continue
		}
		if err := validateHealthOperationPlanJSON(data); err != nil {
			t.Fatalf("Registry operation fixture %s does not conform to the pinned schema: %v", test.path, err)
		}
	}
	evidenceSchema, err := os.ReadFile("testdata/operation-observation-plan/operation-document-evidence.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	projectEvidenceSchema, err := os.ReadFile("../../schemas/datapan.operation-document-evidence.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !healthOperationPlanDigestMatches(healthOperationDocumentEvidenceSHA256, evidenceSchema) || !bytes.Equal(evidenceSchema, embeddedHealthOperationDocumentEvidenceSchema) || !bytes.Equal(evidenceSchema, projectEvidenceSchema) {
		t.Fatal("Registry operation-document evidence schema differs from the pinned source contract")
	}
	for _, test := range []struct {
		localPath   string
		projectPath string
		digest      string
		embedded    []byte
		compile     func() (*jsonschema.Schema, error)
	}{
		{"testdata/operation-observation-plan/operation-observation-policy.schema.json", "../../schemas/datapan.operation-observation-policy.v1.schema.json", healthOperationPolicySchemaSHA256, embeddedHealthOperationPolicySchema, healthOperationPolicyJSONSchema},
		{"testdata/operation-observation-plan/operation-response-assertion.schema.json", "../../schemas/datapan.operation-response-assertion.v2.schema.json", healthOperationResponseAssertionSchemaSHA256, embeddedHealthOperationResponseAssertionSchema, healthOperationResponseAssertionJSONSchema},
		{"testdata/operation-observation-plan/operation-document-evidence-v2.schema.json", "../../schemas/datapan.operation-document-evidence.v2.schema.json", healthOperationDocumentEvidenceV2SHA256, embeddedHealthOperationDocumentEvidenceV2Schema, healthOperationDocumentEvidenceV2JSONSchema},
	} {
		local, err := os.ReadFile(test.localPath)
		if err != nil {
			t.Fatal(err)
		}
		project, err := os.ReadFile(test.projectPath)
		if err != nil {
			t.Fatal(err)
		}
		if !healthOperationPlanDigestMatches(test.digest, local) || !bytes.Equal(local, project) || !bytes.Equal(local, test.embedded) {
			t.Fatalf("Registry schema mirror %s differs from its pinned contract", test.localPath)
		}
		if _, err := test.compile(); err != nil {
			t.Fatalf("pinned schema %s failed compilation: %v", test.localPath, err)
		}
	}
}

func TestPinnedRegistryPathPatternNormalizesToEquivalentRE2(t *testing.T) {
	document := map[string]any{"pattern": "^(?!/)[A-Za-z0-9._/-]+$"}
	if err := normalizeHealthJSONSchemaRegexps(document); err != nil {
		t.Fatal(err)
	}
	pattern, ok := document["pattern"].(string)
	if !ok {
		t.Fatal("normalized path schema pattern is not a string")
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value string
		want  bool
	}{{"reports/operation.json", true}, {"policy/a-b.json", true}, {"/absolute/file.json", false}, {"", false}, {"reports/has space.json", false}} {
		if got := compiled.MatchString(test.value); got != test.want {
			t.Fatalf("normalized path pattern accepted %q = %t, want %t", test.value, got, test.want)
		}
	}
}

func TestHealthOperationPlanJSONPreflightBoundsAndStrictness(t *testing.T) {
	valid := []byte(`{"records":[{"operation_id":"synthetic-rest-list"}]}`)
	if err := preflightHealthOperationPlanJSON(valid); err != nil {
		t.Fatalf("valid JSON was rejected: %v", err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"x":1,"x":2}`),
		[]byte(`{"x":1,"X":2}`),
		[]byte(`{} {}`),
		[]byte(strings.Repeat("[", healthOperationPlanMaxJSONDepth+1) + "0" + strings.Repeat("]", healthOperationPlanMaxJSONDepth+1)),
	} {
		if err := preflightHealthOperationPlanJSON(invalid); err == nil {
			t.Fatalf("preflight accepted invalid JSON %q", invalid)
		}
	}

	var bomb strings.Builder
	bomb.Grow(2 * healthOperationPlanMaxJSONTokens)
	bomb.WriteByte('[')
	for index := 0; index < healthOperationPlanMaxJSONTokens; index++ {
		if index != 0 {
			bomb.WriteByte(',')
		}
		bomb.WriteString("null")
	}
	bomb.WriteByte(']')
	if err := preflightHealthOperationPlanJSON([]byte(bomb.String())); err == nil {
		t.Fatal("preflight accepted a JSON token-count bomb")
	}
}

func TestHealthOperationPlanIndexUsesFleetSizedTokenBudget(t *testing.T) {
	index := syntheticOperationPlanIndexWithDocumentEvidence(t, 12_666)
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > healthOperationPlanIndexMaxBytes {
		t.Fatalf("synthetic index is %d bytes, above the %d-byte index ceiling", len(data), healthOperationPlanIndexMaxBytes)
	}
	tokens, err := countJSONDecoderTokens(data)
	if err != nil {
		t.Fatal(err)
	}
	if tokens <= healthOperationPlanMaxJSONTokens {
		t.Fatalf("synthetic 12,666-ref index has %d tokens; expected to reproduce the old 100,000-token rejection", tokens)
	}
	t.Logf("synthetic 12,666-ref index: %d bytes, %d independently counted JSON decoder tokens", len(data), tokens)
	if err := validateHealthOperationPlanJSON(data); err == nil {
		t.Fatal("generic plan validator accepted the index beyond its 100,000-token budget")
	}
	if err := validateHealthOperationPlanIndexJSON(data); err != nil {
		t.Fatalf("index-specific bounded validator rejected a valid fleet-sized index: %v", err)
	}
	if err := validateHealthOperationPlanIndexBounds(index); err != nil {
		t.Fatalf("fleet-sized index exceeded its explicit structure bounds: %v", err)
	}
}

func TestHealthOperationPlanIndexRejectsArtifactReferenceCountAboveCeiling(t *testing.T) {
	index := syntheticOperationPlanIndexWithDocumentEvidence(t, healthOperationPlanMaxIndexArtifactRefs-8)
	artifactRefs := 3 + len(index.GenerationInputs.OperationDenominators) + len(index.GenerationInputs.DocumentEvidence) + len(index.Shards)
	for _, scope := range index.SourceScopes {
		artifactRefs += len(scope.SourceArtifacts)
	}
	if artifactRefs != healthOperationPlanMaxIndexArtifactRefs+1 {
		t.Fatalf("synthetic index has %d artifact refs, want exactly %d", artifactRefs, healthOperationPlanMaxIndexArtifactRefs+1)
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > healthOperationPlanIndexMaxBytes {
		t.Fatalf("synthetic over-count index is %d bytes, unexpectedly above the byte ceiling", len(data))
	}
	if err := preflightHealthOperationPlanIndexJSON(data); err == nil {
		t.Fatal("index preflight accepted more artifact references than the documented ceiling")
	}
	if err := validateHealthOperationPlanIndexJSON(data); err == nil {
		t.Fatal("index validator accepted more artifact references than the documented ceiling")
	}
	if err := validateHealthOperationPlanIndexBounds(index); err == nil {
		t.Fatal("index with more artifact references than the documented ceiling was accepted")
	}
}

func syntheticOperationPlanIndexWithDocumentEvidence(t *testing.T, documentCount int) healthOperationPlanIndex {
	t.Helper()
	ref := func(path, value string) healthOperationPlanArtifactRef {
		sum := sha256.Sum256([]byte(value))
		return healthOperationPlanArtifactRef{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: 1}
	}
	index := healthOperationPlanIndex{SchemaVersion: healthOperationPlanSchemaVersion, ArtifactKind: "index", RegistryRevision: strings.Repeat("a", 40)}
	index.GenerationInputs.GeneratorPath = "scripts/generate-operation-observation-plan.py"
	index.GenerationInputs.GeneratorSHA256 = ref("generator", "generator").SHA256
	index.GenerationInputs.OperationManifest = ref("reports/synthetic/operation-manifest.json", "operation-manifest")
	index.GenerationInputs.OperationDenominators = make([]healthOperationPlanArtifactRef, 4)
	for position := range index.GenerationInputs.OperationDenominators {
		index.GenerationInputs.OperationDenominators[position] = ref("reports/synthetic/denominator-"+strconv.Itoa(position)+".json", "denominator-"+strconv.Itoa(position))
	}
	index.GenerationInputs.LegacyPolicy = ref("policy/health-probe-canaries.json", "legacy-policy")
	index.GenerationInputs.ProviderIndex = ref("data/provider-index.json", "provider-index")
	index.GenerationInputs.DocumentEvidence = make([]healthOperationPlanArtifactRef, documentCount)
	for position := range index.GenerationInputs.DocumentEvidence {
		index.GenerationInputs.DocumentEvidence[position] = ref("reports/operation-document-evidence/synthetic/"+strconv.Itoa(position)+".json", "document-evidence-"+strconv.Itoa(position))
	}
	index.InventoryContext = struct {
		SeparateLinkOperations                  int  `json:"separate_link_operations"`
		ProviderIndexAdapterEntries             int  `json:"provider_index_adapter_entries"`
		ProviderIndexEntriesCountedAsOperations bool `json:"provider_index_entries_counted_as_operations"`
	}{}
	index.Summary.KnownOperations = 1
	index.Summary.RequestPlansIncomplete = 1
	index.Summary.RuntimeBindingsUnbound = 1
	index.Summary.NotAdmitted = 1
	identity := ref("identity", "synthetic-operation").SHA256
	index.SourceScopes = []healthOperationPlanSourceScope{{
		SourceID: "synthetic_scope", Provider: "synthetic provider", AdapterID: "synthetic-adapter", InventoryStatus: "source_complete",
		RegisteredOperations: 1, IdentitySetSHA256: identity, SourceArtifacts: []healthOperationPlanArtifactRef{ref("sources/synthetic.json", "source")},
	}}
	index.Shards = []healthOperationPlanShardRef{{
		SourceID: "synthetic_scope", ShardIndex: 0, Path: "reports/operation-observation-plan/shards/synthetic_scope/0000.json",
		SHA256: ref("shard", "synthetic-shard").SHA256, Bytes: 1, RecordCount: 1,
		FirstOperationID: "synthetic-operation", LastOperationID: "synthetic-operation",
	}}
	return index
}

func countJSONDecoderTokens(data []byte) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	tokens := 0
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return tokens, nil
		}
		if err != nil {
			return 0, err
		}
		tokens++
	}
}

func TestRegistryInstallProvenanceHasBoundedInput(t *testing.T) {
	path := t.TempDir() + "/registry-install.json"
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(registryInstallProvenanceMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegistryInstallProvenance(path); err == nil {
		t.Fatal("oversized Registry installation provenance was accepted")
	}
}

func TestIncompletePlanWithDistinctDatasetRevisionStopsBeforeHTTP(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	registryRevision := strings.Repeat("a", 40) // index.registry_revision: generator source Git commit
	datasetRevision := strings.Repeat("d", 40)  // installer provenance: immutable HF dataset revision
	operationID := "synthetic-incomplete-operation"
	indexPath := writeSyntheticIncompletePlanInstallation(t, registryRevision, datasetRevision, operationID)
	client := &healthPlanCaptureClient{}
	var stdout, stderr bytes.Buffer
	deadline := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	args := []string{
		"verify", "--health", "--json",
		"--health-plan-index", indexPath,
		"--health-operation-id", operationID,
		"--health-registry-revision", registryRevision,
		"--health-credential-bindings", filepath.Join(root, "private-bindings.json"),
		"--health-attempt-id", "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c",
		"--health-cli-version", version,
		"--health-deadline", deadline,
	}
	code := Run(args, &stdout, &stderr, fakeEnv{}, client)
	if code == exitOK || client.calls != 0 {
		t.Fatalf("incomplete plan result code=%d HTTP calls=%d, want nonzero and zero calls; stderr=%s", code, client.calls, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not executable under its declared bounds") || strings.Contains(stderr.String(), "revision differs") {
		t.Fatalf("loader did not reach the incomplete-plan admission gate: %s", stderr.String())
	}
}

func TestOperationPlanManifestMustNamePinnedSourceSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	registryRevision := strings.Repeat("a", 40)
	datasetRevision := strings.Repeat("d", 40)
	indexPath := writeSyntheticIncompletePlanInstallation(t, registryRevision, datasetRevision, "synthetic-incomplete-operation")
	manifestData, err := os.ReadFile(defaultReleaseManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.SourceRegistry = "data/other-registry.json"
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReleaseManifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	provenanceData, err := os.ReadFile(defaultRegistryInstallProvenancePath)
	if err != nil {
		t.Fatal(err)
	}
	var provenance registryInstallProvenance
	if err := json.Unmarshal(provenanceData, &provenance); err != nil {
		t.Fatal(err)
	}
	provenance.ReleaseManifestSHA256 = hex.EncodeToString(manifestSum[:])
	provenanceData, err = json.Marshal(provenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultRegistryInstallProvenancePath, provenanceData, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadManifestBoundHealthOperationPlan(healthOperationPlanOptions{
		IndexPath: indexPath, OperationID: "synthetic-incomplete-operation", RegistryRevision: registryRevision,
	}, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "manifest provenance is invalid") {
		t.Fatalf("manifest source snapshot mismatch was not rejected before plan admission: %v", err)
	}
}

func TestPlanArtifactReferencesMustBePresentInInstalledManifest(t *testing.T) {
	data := []byte(`{"generation_inputs":{"legacy_policy":{"path":"policy/health-probe-canaries.json","sha256":"` + strings.Repeat("a", 64) + `","bytes":17}}}`)
	if err := validateHealthOperationPlanArtifactReferences(data, releaseManifest{}); err == nil {
		t.Fatal("index reference absent from release manifest was accepted")
	}
	manifest := releaseManifest{Artifacts: []releaseManifestArtifact{{
		Path: "policy/health-probe-canaries.json", Bytes: 17, SHA256: strings.Repeat("b", 64),
	}}}
	if err := validateHealthOperationPlanArtifactReferences(data, manifest); err == nil {
		t.Fatal("index reference with a digest different from the manifest was accepted")
	}
}

func writeSyntheticIncompletePlanInstallation(t *testing.T, registryRevision, datasetRevision, operationID string) string {
	t.Helper()
	manifest := releaseManifest{SchemaVersion: "datapan.release-manifest.v1", Provider: "datapan-registry", OutputDir: "."}
	artifacts := make(map[string]healthOperationPlanArtifactRef)
	store := func(path string, data []byte, kind string) healthOperationPlanArtifactRef {
		t.Helper()
		if previous, ok := artifacts[path]; ok {
			return previous
		}
		fullPath := filepath.Join(".", filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		ref := healthOperationPlanArtifactRef{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
		artifacts[path] = ref
		manifest.Artifacts = append(manifest.Artifacts, releaseManifestArtifact{Path: path, Kind: kind, Bytes: ref.Bytes, SHA256: ref.SHA256})
		return ref
	}

	registryRef := store("data/data-go-kr.registry.json", []byte("synthetic pinned source snapshot"), "registry")
	operationManifestRef := store("reports/data-go-kr/operation-manifest.json", []byte(`{"operations":[]}`), "operation_manifest")
	sourceProfileRef := store("sources/data_go_kr.json", []byte(`{"source_id":"data_go_kr"}`), "source_profile")
	generator := store("scripts/generate-operation-observation-plan.py", []byte("synthetic generator digest fixture"), "generator")
	legacyPolicyRef := store("policy/health-probe-canaries.json", []byte(`{"selectors":[]}`), "policy")
	providerIndexRef := store("data/provider-index.json", []byte(`{"adapters":[]}`), "provider_index")
	denominators := make([]healthOperationPlanArtifactRef, 4)
	for index := range denominators {
		path := "reports/synthetic/operation-denominator-" + string(rune('1'+index)) + ".json"
		denominators[index] = store(path, []byte(`{"registered":0}`), "operation_denominator")
	}
	planSchema := store(healthOperationPlanSchemaPath, embeddedHealthOperationPlanSchema, "schema")
	evidenceSchema := store(healthOperationDocumentEvidencePath, embeddedHealthOperationDocumentEvidenceSchema, "schema")
	store(healthOperationPolicySchemaPath, embeddedHealthOperationPolicySchema, "schema")
	store(healthOperationResponseAssertionSchemaPath, embeddedHealthOperationResponseAssertionSchema, "schema")
	store(healthOperationDocumentEvidenceV2Path, embeddedHealthOperationDocumentEvidenceV2Schema, "schema")
	_ = planSchema
	_ = evidenceSchema

	operationEvidence := healthOperationPlanEvidenceRef{ArtifactPath: operationManifestRef.Path, SHA256: operationManifestRef.SHA256, JSONPointer: "#/operations/0", EvidenceKind: "operation_manifest"}
	plan := healthOperationPlanRecord{
		SchemaVersion: healthOperationPlanSchemaVersion,
		ArtifactKind:  "operation_plan",
		SourceBinding: healthOperationPlanSourceBinding{
			SourceID: "synthetic_scope", Provider: "data.go.kr", AdapterID: "data-go-kr", InventoryStatus: "source_complete",
			SourceArtifacts: []healthOperationPlanArtifactRef{registryRef, operationManifestRef, sourceProfileRef},
		},
		OperationIdentity: healthOperationPlanIdentity{
			OperationID: operationID, Protocol: "REST",
			RegisteredEndpoint: &struct {
				Host string `json:"host"`
				Path string `json:"path"`
			}{Host: "api.example.invalid", Path: "/v1/items"},
		},
		RequestPlan: healthOperationPlanRequestPlan{
			Status: "incomplete", EvidenceRefs: []healthOperationPlanEvidenceRef{operationEvidence},
			MissingFields: []string{"response_assertion_and_empty_result_semantics"},
		},
		RuntimeBinding: healthOperationPlanRuntimeBinding{
			Status: "unbound", MissingFields: []string{"shared_quota_scopes_and_limits"}, EvidenceRefs: []healthOperationPlanEvidenceRef{},
		},
		Admission: healthOperationPlanAdmission{Status: "not_admitted", Reasons: []string{"request_plan_incomplete"}, EvidenceRefs: []healthOperationPlanEvidenceRef{}},
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	shard := healthOperationPlanShard{SchemaVersion: healthOperationPlanSchemaVersion, ArtifactKind: "shard", SourceID: "synthetic_scope", ShardIndex: 0, Records: []json.RawMessage{planData}}
	shardData, err := json.Marshal(shard)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthOperationPlanJSON(shardData); err != nil {
		t.Fatalf("synthetic incomplete plan shard does not match the pinned schema: %v", err)
	}
	shardFile := store("reports/operation-observation-plan/shards/synthetic_scope/0000.json", shardData, "operation_plan_shard")
	identitySet := sha256.Sum256([]byte(operationID))
	index := healthOperationPlanIndex{SchemaVersion: healthOperationPlanSchemaVersion, ArtifactKind: "index", RegistryRevision: registryRevision}
	index.GenerationInputs.GeneratorPath = generator.Path
	index.GenerationInputs.GeneratorSHA256 = generator.SHA256
	index.GenerationInputs.OperationManifest = operationManifestRef
	index.GenerationInputs.OperationDenominators = denominators
	index.GenerationInputs.LegacyPolicy = legacyPolicyRef
	index.GenerationInputs.ProviderIndex = providerIndexRef
	index.InventoryContext = struct {
		SeparateLinkOperations                  int  `json:"separate_link_operations"`
		ProviderIndexAdapterEntries             int  `json:"provider_index_adapter_entries"`
		ProviderIndexEntriesCountedAsOperations bool `json:"provider_index_entries_counted_as_operations"`
	}{}
	index.Summary.KnownOperations = 1
	index.Summary.RequestPlansIncomplete = 1
	index.Summary.RuntimeBindingsUnbound = 1
	index.Summary.NotAdmitted = 1
	index.SourceScopes = []healthOperationPlanSourceScope{{
		SourceID: "synthetic_scope", Provider: "data.go.kr", AdapterID: "data-go-kr", InventoryStatus: "source_complete",
		RegisteredOperations: 1, IdentitySetSHA256: hex.EncodeToString(identitySet[:]),
		SourceArtifacts: []healthOperationPlanArtifactRef{registryRef, operationManifestRef, sourceProfileRef},
	}}
	index.Shards = []healthOperationPlanShardRef{{
		SourceID: "synthetic_scope", ShardIndex: 0, Path: shardFile.Path, SHA256: shardFile.SHA256,
		Bytes: shardFile.Bytes, RecordCount: 1, FirstOperationID: operationID, LastOperationID: operationID,
	}}
	indexData, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	indexFile := store(healthOperationPlanIndexPath, indexData, "operation_plan_index")
	manifest.SourceRegistry = registryRef.Path
	manifest.ArtifactCount = len(manifest.Artifacts)
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	if err := os.MkdirAll(filepath.Dir(defaultReleaseManifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReleaseManifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	verified := true
	provenance := registryInstallProvenance{
		SchemaVersion: "datapan.registry-install.v1", Provider: "datapan-registry", RegistryPath: defaultRegistryPath,
		RegistrySHA256: registryRef.SHA256, ReleaseTag: datasetRevision, ReleaseManifestSHA256: hex.EncodeToString(manifestSum[:]),
		ManifestRegistryVerified: &verified, PinMode: "pinned", Distribution: "huggingface_dataset",
		DatasetID: "StatPan/datapan-registry", DatasetRevision: datasetRevision,
		DatasetManifestURL:    "https://huggingface.co/datasets/StatPan/datapan-registry/resolve/" + datasetRevision + "/manifest.json",
		DatasetManifestSHA256: strings.Repeat("f", 64),
	}
	provenanceData, err := json.Marshal(provenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultRegistryInstallProvenancePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultRegistryInstallProvenancePath, provenanceData, 0o600); err != nil {
		t.Fatal(err)
	}
	return indexFile.Path
}

func TestHealthOperationPlanInvocationSeparatesSharedRevisionFlag(t *testing.T) {
	revision := strings.Repeat("a", 40)
	planArgs := []string{"--health", "--json", "--health-plan-index", "reports/operation-observation-plan/index.json", "--health-operation-id", "synthetic-rest-list", "--health-registry-revision", revision, "--health-credential-bindings", "private-bindings.json", "--health-attempt-id", "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", "--health-cli-version", version, "--health-deadline", "2099-01-01T00:00:00Z"}
	for _, test := range []struct {
		name      string
		args      []string
		wantPlan  bool
		wantError bool
	}{
		{
			name: "v1 catalog revision is not plan mode",
			args: []string{"verify", "--health", "--health-registry-revision", revision},
		},
		{
			name:     "valid plan selectors",
			args:     append([]string{"verify"}, planArgs...),
			wantPlan: true,
		},
		{
			name:      "missing operation ID",
			args:      []string{"verify", "--health", "--json", "--health-plan-index", "reports/operation-observation-plan/index.json", "--health-registry-revision", revision, "--health-credential-bindings", "private-bindings.json", "--health-attempt-id", "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", "--health-cli-version", version, "--health-deadline", "2099-01-01T00:00:00Z"},
			wantError: true,
		},
		{
			name:      "missing index",
			args:      []string{"verify", "--health", "--json", "--health-operation-id", "synthetic-rest-list", "--health-registry-revision", revision, "--health-credential-bindings", "private-bindings.json", "--health-attempt-id", "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", "--health-cli-version", version, "--health-deadline", "2099-01-01T00:00:00Z"},
			wantError: true,
		},
		{
			name:      "health gate required",
			args:      append([]string{"verify"}, planArgs[1:]...),
			wantError: true,
		},
		{
			name:      "immutable revision required",
			args:      []string{"verify", "--health", "--json", "--health-plan-index", "reports/operation-observation-plan/index.json", "--health-operation-id", "synthetic-rest-list", "--health-registry-revision", "main", "--health-credential-bindings", "private-bindings.json", "--health-attempt-id", "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", "--health-cli-version", version, "--health-deadline", "2099-01-01T00:00:00Z"},
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, plan, err := healthOperationPlanInvocation(test.args)
			if (err != nil) != test.wantError || plan != test.wantPlan {
				t.Fatalf("plan=%t err=%v; wantPlan=%t wantError=%t", plan, err, test.wantPlan, test.wantError)
			}
		})
	}
}
