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
		{"testdata/operation-observation-plan/synthetic-rest-list.json", "c5fb5c247dd451fc3a4999641c328fcddd39b145871f8d4427b2b6375ed7f321"},
		{"testdata/operation-observation-plan/synthetic-soap-read.json", "1790b74d3ef4aab7c09451c5d917010ffda47801688ddd357aafc4ec6926c936"},
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

func TestHealthOperationPlanShardUsesSeparateBoundedTokenBudget(t *testing.T) {
	// Real Registry shards contain up to 256 operation records, so their
	// aggregate token count can exceed the per-operation/response limit while
	// remaining within a separately bounded artifact budget.
	withinBudget := []byte("[" + strings.TrimSuffix(strings.Repeat("0,", healthOperationPlanMaxJSONTokens+1), ",") + "]")
	tokens, err := countJSONDecoderTokens(withinBudget)
	if err != nil {
		t.Fatal(err)
	}
	if tokens <= healthOperationPlanMaxJSONTokens || tokens >= healthOperationPlanMaxShardJSONTokens {
		t.Fatalf("synthetic shard has %d tokens; expected above the per-operation limit and below the shard limit", tokens)
	}
	if err := preflightHealthOperationPlanJSON(withinBudget); err == nil {
		t.Fatal("generic operation preflight accepted a shard-sized token stream")
	}
	if err := preflightHealthOperationPlanShardJSON(withinBudget); err != nil {
		t.Fatalf("shard-specific bounded preflight rejected a valid-sized token stream: %v", err)
	}

	overBudget := []byte("[" + strings.TrimSuffix(strings.Repeat("0,", healthOperationPlanMaxShardJSONTokens), ",") + "]")
	if int64(len(overBudget)) > healthOperationPlanShardMaxBytes {
		t.Fatalf("synthetic over-budget shard is %d bytes, above the shard byte ceiling", len(overBudget))
	}
	tokens, err = countJSONDecoderTokens(overBudget)
	if err != nil {
		t.Fatal(err)
	}
	if tokens <= healthOperationPlanMaxShardJSONTokens {
		t.Fatalf("synthetic over-budget shard has only %d tokens", tokens)
	}
	if err := preflightHealthOperationPlanShardJSON(overBudget); err == nil {
		t.Fatal("shard preflight accepted a token stream above its explicit ceiling")
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

func TestHealthOperationPlanManifestSupportsFullPopulationEvidenceClosure(t *testing.T) {
	// The #95 population has 12,666 identities. Two operation-specific evidence
	// artifacts per identity plus the compact
	// release metadata exceed the former 4 MiB install ceiling.
	const populationSize = 12_666
	const evidenceArtifactCount = 2*populationSize + 18
	const manifestArtifactCount = evidenceArtifactCount + 1
	manifest := releaseManifest{
		SchemaVersion:  "datapan.release-manifest.v1",
		SourceRegistry: healthOperationPlanSourceRegistryPath,
		ArtifactCount:  manifestArtifactCount,
		Artifacts:      make([]releaseManifestArtifact, 0, manifestArtifactCount),
	}
	for index := 0; index < evidenceArtifactCount; index++ {
		manifest.Artifacts = append(manifest.Artifacts, releaseManifestArtifact{
			Path:   "reports/operation-evidence/" + strconv.Itoa(index) + strings.Repeat("x", 88) + ".json",
			Kind:   "operation_document_evidence",
			Bytes:  1024,
			SHA256: strings.Repeat("a", 64),
		})
	}
	invalidIndex := []byte(`{}`)
	invalidIndexSHA := sha256.Sum256(invalidIndex)
	manifest.Artifacts = append(manifest.Artifacts, releaseManifestArtifact{
		Path: healthOperationPlanIndexPath, Kind: "operation_observation_plan", Bytes: int64(len(invalidIndex)), SHA256: hex.EncodeToString(invalidIndexSHA[:]),
	})
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 4<<20 || int64(len(data)) > healthOperationPlanManifestMaxBytes {
		t.Fatalf("full-population manifest fixture is %d bytes; want above 4 MiB and within the %d-byte ceiling", len(data), healthOperationPlanManifestMaxBytes)
	}
	tokens, err := countJSONDecoderTokens(data)
	if err != nil {
		t.Fatal(err)
	}
	if tokens > healthOperationPlanMaxManifestJSONTokens {
		t.Fatalf("full-population manifest has %d tokens, above the %d-token ceiling", tokens, healthOperationPlanMaxManifestJSONTokens)
	}
	t.Logf("12,666-identity manifest fixture: %d artifacts, %d bytes, %d JSON tokens", manifestArtifactCount, len(data), tokens)
	if err := preflightHealthOperationPlanManifestJSON(data); err != nil {
		t.Fatalf("bounded full-population evidence manifest was rejected: %v", err)
	}
	projection, err := datapanRegistryHealthOperationPlanProjection(data, map[string][]byte{healthOperationPlanIndexPath: invalidIndex}, func(string, int64) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	if err == nil || !strings.Contains(err.Error(), "operation-plan index does not match") {
		t.Fatalf("Registry plan projection did not pass the bounded manifest gate and reach index validation: files=%d err=%v", len(projection), err)
	}

	path := filepath.Join(t.TempDir(), "release-manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedFile(path, healthOperationPlanManifestMaxBytes); err != nil {
		t.Fatalf("bounded manifest reader rejected the full-population fixture: %v", err)
	}
}

func TestHealthOperationPlanManifestKeepsByteTokenAndReferenceBounds(t *testing.T) {
	withinByteLimit := []byte(`{"schema_version":"datapan.release-manifest.v1","artifact_count":0,"artifacts":[]}`)
	withinByteLimit = append(withinByteLimit, bytes.Repeat([]byte{' '}, int(healthOperationPlanManifestMaxBytes)-len(withinByteLimit))...)
	if err := preflightHealthOperationPlanManifestJSON(withinByteLimit); err != nil {
		t.Fatalf("manifest exactly at the byte ceiling was rejected: %v", err)
	}
	overByteLimit := append(append([]byte(nil), withinByteLimit...), ' ')
	if err := preflightHealthOperationPlanManifestJSON(overByteLimit); err == nil {
		t.Fatal("manifest above the byte ceiling was accepted")
	}

	var tokenBomb strings.Builder
	tokenBomb.Grow(3 * healthOperationPlanMaxManifestJSONTokens)
	tokenBomb.WriteByte('[')
	for index := 0; index < healthOperationPlanMaxManifestJSONTokens; index++ {
		if index > 0 {
			tokenBomb.WriteByte(',')
		}
		tokenBomb.WriteString("null")
	}
	tokenBomb.WriteByte(']')
	if int64(tokenBomb.Len()) > healthOperationPlanManifestMaxBytes {
		t.Fatalf("token-limit fixture unexpectedly exceeds the manifest byte ceiling: %d", tokenBomb.Len())
	}
	if err := preflightHealthOperationPlanManifestJSON([]byte(tokenBomb.String())); err == nil {
		t.Fatal("manifest above the token ceiling was accepted")
	}

	overReferenceLimit := releaseManifest{SchemaVersion: "datapan.release-manifest.v1", ArtifactCount: healthOperationPlanMaxIndexArtifactRefs + 1}
	overReferenceLimit.Artifacts = make([]releaseManifestArtifact, 0, overReferenceLimit.ArtifactCount)
	for index := 0; index < overReferenceLimit.ArtifactCount; index++ {
		overReferenceLimit.Artifacts = append(overReferenceLimit.Artifacts, releaseManifestArtifact{
			Path: "reports/ref/" + strconv.Itoa(index) + ".json", Kind: "source", Bytes: 1, SHA256: strings.Repeat("b", 64),
		})
	}
	referenceData, err := json.Marshal(overReferenceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(referenceData)) > healthOperationPlanManifestMaxBytes {
		t.Fatalf("reference-limit fixture unexpectedly exceeds the manifest byte ceiling: %d", len(referenceData))
	}
	if err := preflightHealthOperationPlanManifestJSON(referenceData); err == nil {
		t.Fatal("manifest above the 32,000-artifact reference ceiling was accepted")
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

func TestRegistryZipInstallPreservesManifestBoundHealthPlanClosure(t *testing.T) {
	t.Chdir(t.TempDir())
	operationID := "synthetic-incomplete-operation"
	indexPath := writeSyntheticIncompletePlanInstallation(t, strings.Repeat("a", 40), strings.Repeat("d", 40), operationID)
	_ = indexPath
	manifestData, err := os.ReadFile(defaultReleaseManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"manifest.json": string(manifestData)}
	for _, artifact := range manifest.Artifacts {
		data, err := os.ReadFile(filepath.Join(".", filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		files[artifact.Path] = string(data)
	}
	snapshot, err := datapanRegistrySnapshotFromZip(zipFilesForTest(t, files))
	if err != nil {
		t.Fatalf("manifest-bound operation-plan closure was not installable: %v", err)
	}
	for _, path := range []string{
		healthOperationPlanIndexPath,
		"reports/operation-observation-plan/shards/synthetic_scope/0000.json",
		"reports/operation-document-evidence/synthetic/synthetic-incomplete-operation.json",
		healthOperationResponseAssertionArtifactPathPrefix + operationID + ".json",
		healthOperationPlanSchemaPath, healthOperationPolicySchemaPath,
		healthOperationResponseAssertionSchemaPath, healthOperationDocumentEvidencePath,
		healthOperationDocumentEvidenceV2Path, healthOperationPolicyArtifactPath,
	} {
		if _, ok := snapshot.ReleaseFiles[path]; !ok {
			t.Fatalf("installed Registry release omitted transitive runtime file %s", path)
		}
	}
	if _, copied := snapshot.ReleaseFiles[healthOperationPlanSourceRegistryPath]; copied {
		t.Fatal("operation-plan runtime projection copied the 139 MiB source snapshot into release evidence")
	}

	missing := make(map[string]string, len(files)-1)
	for path, data := range files {
		if path != "reports/operation-document-evidence/synthetic/synthetic-incomplete-operation.json" {
			missing[path] = data
		}
	}
	if _, err := datapanRegistrySnapshotFromZip(zipFilesForTest(t, missing)); err == nil || !strings.Contains(err.Error(), "operation-plan") {
		t.Fatalf("install accepted a ZIP with a missing referenced document sidecar: %v", err)
	}

	altered := make(map[string]string, len(files))
	for path, data := range files {
		altered[path] = data
	}
	altered["reports/operation-document-evidence/synthetic/synthetic-incomplete-operation.json"] += " "
	if _, err := datapanRegistrySnapshotFromZip(zipFilesForTest(t, altered)); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("install accepted a modified referenced document sidecar: %v", err)
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
		schema := ""
		switch kind {
		case "schema":
			schema = map[string]string{
				healthOperationPlanSchemaPath:              healthOperationPlanSchemaID,
				healthOperationPolicySchemaPath:            healthOperationPolicySchemaID,
				healthOperationResponseAssertionSchemaPath: healthOperationResponseAssertionSchemaID,
				healthOperationDocumentEvidencePath:        healthOperationDocumentEvidenceSchemaID,
				healthOperationDocumentEvidenceV2Path:      healthOperationDocumentEvidenceV2SchemaID,
			}[path]
		case "operation_document_evidence":
			schema = healthOperationDocumentEvidenceSchemaID
		case "operation_observation_policy":
			schema = healthOperationPolicySchemaID
		case "operation_response_assertion":
			schema = healthOperationResponseAssertionSchemaID
		case "operation_observation_plan":
			schema = healthOperationPlanSchemaID
		case "operation_observation_plan_shard":
			schema = healthOperationPlanSchemaID
		}
		manifest.Artifacts = append(manifest.Artifacts, releaseManifestArtifact{Path: path, Kind: kind, Schema: schema, Bytes: ref.Bytes, SHA256: ref.SHA256})
		return ref
	}

	registryRef := store("data/data-go-kr.registry.json", []byte("synthetic pinned source snapshot"), "registry")
	operationManifestRef := store("reports/data-go-kr/operation-manifest.json", []byte(`{"operations":[]}`), "operation_manifest")
	sourceProfileRef := store("sources/data_go_kr.json", []byte(`{"source_id":"data_go_kr"}`), "source_profile")
	generator := store("scripts/generate-operation-observation-plan.py", []byte("synthetic generator digest fixture"), "generator")
	legacyPolicyRef := store("policy/health-probe-canaries.json", []byte(`{"selectors":[]}`), "policy")
	providerIndexRef := store("data/provider-index.json", []byte(`{"adapters":[]}`), "provider_index")
	documentPath := "reports/operation-document-evidence/synthetic/" + operationID + ".json"
	documentRef := store(documentPath, []byte(`{"schema_version":"datapan.operation-document-evidence.v1"}`), "operation_document_evidence")
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
	store(healthOperationPolicyArtifactPath, []byte(`{"schema_version":"datapan.operation-observation-policy.v1","artifact_kind":"operation_observation_policy_set","policies":[],"profiles":[],"effect_profiles":[]}`), "operation_observation_policy")
	assertionPath := healthOperationResponseAssertionArtifactPathPrefix + operationID + ".json"
	assertionData, err := json.Marshal(map[string]any{
		"schema_version": "datapan.operation-response-assertion.v2",
		"artifact_kind":  "operation_response_assertion",
		"source_binding": map[string]any{"source_id": "synthetic_scope", "provider": "data.go.kr", "protocol": "REST"},
		"operation_identity": map[string]any{
			"operation_id": operationID, "dataset_id": "synthetic-dataset", "operation_name": operationID, "upstream_operation_key": operationID,
		},
		"document_evidence": map[string]any{"path": documentRef.Path, "sha256": documentRef.SHA256, "bytes": documentRef.Bytes},
		"review":            map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "synthetic test", "rationale": "test pointer closure only"},
		"assertion":         map[string]any{"mode": "observation_only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertionRef := store(assertionPath, assertionData, "operation_response_assertion")
	_ = planSchema
	_ = evidenceSchema

	operationEvidence := healthOperationPlanEvidenceRef{ArtifactPath: operationManifestRef.Path, SHA256: operationManifestRef.SHA256, JSONPointer: "#/operations/0", EvidenceKind: "operation_manifest"}
	plan := healthOperationPlanRecord{
		SchemaVersion: healthOperationPlanSchemaVersion,
		ArtifactKind:  "operation_plan",
		SourceBinding: healthOperationPlanSourceBinding{
			SourceID: "synthetic_scope", Provider: "data.go.kr", AdapterID: "data-go-kr", InventoryStatus: "source_complete",
		},
		OperationIdentity: healthOperationPlanIdentity{
			OperationID: operationID, Protocol: "REST",
			RegisteredEndpoint: &healthOperationPlanRegisteredEndpoint{Host: "api.example.invalid", Path: "/v1/items"},
		},
		RequestPlan: healthOperationPlanRequestPlan{
			Status: "incomplete", EvidenceRefs: []healthOperationPlanEvidenceRef{
				operationEvidence,
				{ArtifactPath: documentRef.Path, SHA256: documentRef.SHA256, JSONPointer: "#/identity", EvidenceKind: "operation_document"},
				{ArtifactPath: assertionRef.Path, SHA256: assertionRef.SHA256, JSONPointer: "#/assertion", EvidenceKind: "reviewed_policy"},
				{ArtifactPath: assertionRef.Path, SHA256: assertionRef.SHA256, JSONPointer: "#/review", EvidenceKind: "reviewed_policy"},
			},
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
	shardFile := store("reports/operation-observation-plan/shards/synthetic_scope/0000.json", shardData, "operation_observation_plan_shard")
	identitySet := sha256.Sum256([]byte(operationID))
	index := healthOperationPlanIndex{SchemaVersion: healthOperationPlanSchemaVersion, ArtifactKind: "index", RegistryRevision: registryRevision}
	index.GenerationInputs.GeneratorPath = generator.Path
	index.GenerationInputs.GeneratorSHA256 = generator.SHA256
	index.GenerationInputs.OperationManifest = operationManifestRef
	index.GenerationInputs.OperationDenominators = denominators
	index.GenerationInputs.LegacyPolicy = legacyPolicyRef
	index.GenerationInputs.ProviderIndex = providerIndexRef
	index.GenerationInputs.DocumentEvidence = []healthOperationPlanArtifactRef{documentRef}
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
	indexFile := store(healthOperationPlanIndexPath, indexData, "operation_observation_plan")
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
