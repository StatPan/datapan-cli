package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	// The installed runtime projection is the complete local closure needed to
	// select any operation plan. It excludes the raw Registry snapshot and
	// unrelated source material, which remain covered by the release manifest.
	datapanRegistryPlanProjectionMaxBytes  = 128 << 20
	datapanRegistryPlanProjectionMaxFiles  = healthOperationPlanMaxIndexArtifactRefs + healthOperationPlanMaxIndexShards + 16
	datapanRegistryZipDefaultEntryMaxBytes = 64 << 20
	datapanRegistryZipRegistryMaxBytes     = 256 << 20
)

type datapanRegistryPlanProjectionFetch func(path string, maximumBytes int64) ([]byte, error)

// datapanRegistryHealthOperationPlanProjection loads the minimum manifest-bound
// files the installed plan selector can later read: the index, every indexed
// shard, pinned schemas, shared policy, and only the document/assertion/policy
// artifacts referenced by plan records. Every byte is verified against the
// release manifest before it is returned for installation.
func datapanRegistryHealthOperationPlanProjection(manifestData []byte, available map[string][]byte, fetch datapanRegistryPlanProjectionFetch) (map[string][]byte, error) {
	if len(manifestData) == 0 || len(manifestData) > 4<<20 {
		return nil, errors.New("Registry release manifest is missing or exceeds its install bound")
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.SchemaVersion != "datapan.release-manifest.v1" {
		return nil, errors.New("Registry release manifest is invalid for operation-plan projection")
	}
	hasPlanIndex := false
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == healthOperationPlanIndexPath {
			hasPlanIndex = true
			break
		}
	}
	if !hasPlanIndex {
		// Older Registry releases have no observation-plan projection and
		// retain the existing install path without plan-specific closure gates.
		return map[string][]byte{}, nil
	}
	if manifest.ArtifactCount != len(manifest.Artifacts) {
		return nil, errors.New("Registry release manifest is invalid for operation-plan projection")
	}
	manifestArtifacts, err := datapanRegistryUniqueManifestArtifacts(manifest)
	if err != nil {
		return nil, err
	}
	if manifest.SourceRegistry != healthOperationPlanSourceRegistryPath {
		return nil, errors.New("Registry plan release names an unsupported source snapshot")
	}
	if fetch == nil {
		return nil, errors.New("Registry operation-plan artifact source is unavailable")
	}

	projection := make(map[string][]byte)
	var totalBytes int64
	loadArtifact := func(artifactPath string, maximumBytes int64, expectedKind string) ([]byte, error) {
		if !datapanRegistrySafeProjectionPath(artifactPath) || maximumBytes < 1 {
			return nil, fmt.Errorf("Registry operation-plan artifact path is invalid")
		}
		artifact, ok := manifestArtifacts[artifactPath]
		if !ok || artifact.Bytes < 1 || artifact.Bytes > maximumBytes || !validSHA256(artifact.SHA256) || expectedKind != "" && artifact.Kind != expectedKind {
			return nil, fmt.Errorf("Registry operation-plan artifact %s is absent, unbounded, or has the wrong manifest kind", artifactPath)
		}
		if data, exists := projection[artifactPath]; exists {
			return data, nil
		}
		if totalBytes > datapanRegistryPlanProjectionMaxBytes-artifact.Bytes || len(projection) >= datapanRegistryPlanProjectionMaxFiles {
			return nil, errors.New("Registry operation-plan projection exceeds its aggregate resource ceiling")
		}
		data, exists := available[artifactPath]
		if !exists {
			data, err = fetch(artifactPath, maximumBytes)
			if err != nil {
				return nil, fmt.Errorf("Registry operation-plan artifact %s is unavailable", artifactPath)
			}
		}
		if int64(len(data)) != artifact.Bytes || !healthOperationPlanDigestMatches(artifact.SHA256, data) {
			return nil, fmt.Errorf("Registry operation-plan artifact %s differs from its manifest digest", artifactPath)
		}
		projection[artifactPath] = data
		totalBytes += artifact.Bytes
		return data, nil
	}

	indexData, err := loadArtifact(healthOperationPlanIndexPath, healthOperationPlanIndexMaxBytes, "operation_observation_plan")
	if err != nil {
		return nil, err
	}
	if err := validateHealthOperationPlanIndexJSON(indexData); err != nil {
		return nil, errors.New("Registry operation-plan index does not match the pinned schema or bounds")
	}
	var index healthOperationPlanIndex
	if err := json.Unmarshal(indexData, &index); err != nil || index.SchemaVersion != healthOperationPlanSchemaVersion || index.ArtifactKind != "index" {
		return nil, errors.New("Registry operation-plan index identity is invalid")
	}
	if err := validateHealthOperationPlanIndexBounds(index); err != nil || validateHealthOperationPlanArtifactReferences(indexData, manifest) != nil || validateHealthOperationPlanIndexManifest(index, manifest) != nil {
		return nil, errors.New("Registry operation-plan index references are not manifest-bound")
	}
	for _, schema := range []struct {
		path   string
		digest string
		data   []byte
	}{
		{healthOperationPlanSchemaPath, healthOperationPlanSchemaSHA256, embeddedHealthOperationPlanSchema},
		{healthOperationPolicySchemaPath, healthOperationPolicySchemaSHA256, embeddedHealthOperationPolicySchema},
		{healthOperationResponseAssertionSchemaPath, healthOperationResponseAssertionSchemaSHA256, embeddedHealthOperationResponseAssertionSchema},
		{healthOperationDocumentEvidencePath, healthOperationDocumentEvidenceSHA256, embeddedHealthOperationDocumentEvidenceSchema},
		{healthOperationDocumentEvidenceV2Path, healthOperationDocumentEvidenceV2SHA256, embeddedHealthOperationDocumentEvidenceV2Schema},
	} {
		data, err := loadArtifact(schema.path, 1<<20, "schema")
		if err != nil || int64(len(data)) != int64(len(schema.data)) || !healthOperationPlanDigestMatches(schema.digest, data) || !bytes.Equal(data, schema.data) {
			return nil, fmt.Errorf("Registry operation-plan schema %s differs from the pinned CLI contract", schema.path)
		}
	}
	if _, err = loadArtifact(healthOperationPolicyArtifactPath, healthOperationPolicyMaxBytes, "operation_observation_policy"); err != nil {
		return nil, err
	}

	documentArtifacts := make(map[string]healthOperationPlanArtifactRef, len(index.GenerationInputs.DocumentEvidence))
	for _, artifact := range index.GenerationInputs.DocumentEvidence {
		if _, duplicate := documentArtifacts[artifact.Path]; duplicate {
			return nil, errors.New("Registry operation-plan index duplicates a document artifact")
		}
		if err := validateHealthOperationPlanArtifactRef(artifact, manifest); err != nil || artifact.Bytes > healthOperationDocumentEvidenceMaxBytes || !datapanRegistrySafeProjectionPath(artifact.Path) {
			return nil, errors.New("Registry operation-plan document references are not bounded by the release manifest")
		}
		documentArtifacts[artifact.Path] = artifact
	}

	policyPaths := make(map[string]string)
	assertionPaths := make(map[string]string)
	for _, shardRef := range index.Shards {
		if !strings.HasPrefix(shardRef.Path, "reports/operation-observation-plan/shards/") || !strings.HasSuffix(shardRef.Path, ".json") {
			return nil, errors.New("Registry operation-plan shard path is outside the bounded shard directory")
		}
		artifact, ok := manifestArtifacts[shardRef.Path]
		if !ok || artifact.Kind != "operation_observation_plan_shard" || artifact.Bytes != shardRef.Bytes || !strings.EqualFold(artifact.SHA256, shardRef.SHA256) {
			return nil, errors.New("Registry operation-plan shard is not bound by its index and manifest")
		}
		shardData, err := loadArtifact(shardRef.Path, healthOperationPlanShardMaxBytes, "operation_observation_plan_shard")
		if err != nil || int64(len(shardData)) != shardRef.Bytes || !healthOperationPlanDigestMatches(shardRef.SHA256, shardData) {
			return nil, fmt.Errorf("Registry operation-plan shard %s differs from its indexed digest", shardRef.Path)
		}
		if err := validateHealthOperationPlanShardJSON(shardData); err != nil || validateHealthOperationPlanArtifactReferences(shardData, manifest) != nil {
			return nil, fmt.Errorf("Registry operation-plan shard %s is invalid or has an unbound reference", shardRef.Path)
		}
		var shard healthOperationPlanShard
		if err := json.Unmarshal(shardData, &shard); err != nil || shard.SchemaVersion != healthOperationPlanSchemaVersion || shard.ArtifactKind != "shard" || shard.SourceID != shardRef.SourceID || shard.ShardIndex != shardRef.ShardIndex || len(shard.Records) != shardRef.RecordCount || len(shard.Records) == 0 {
			return nil, fmt.Errorf("Registry operation-plan shard %s identity is invalid", shardRef.Path)
		}
		sourceScope, ok := healthOperationPlanSourceScopeByID(index.SourceScopes, shardRef.SourceID)
		if !ok {
			return nil, errors.New("Registry operation-plan shard has no source scope")
		}
		previousID := ""
		for _, raw := range shard.Records {
			var plan healthOperationPlanRecord
			if err := json.Unmarshal(raw, &plan); err != nil || plan.SchemaVersion != healthOperationPlanSchemaVersion || plan.ArtifactKind != "operation_plan" || plan.SourceBinding.SourceID != shard.SourceID || plan.SourceBinding.Provider != sourceScope.Provider || plan.SourceBinding.AdapterID != sourceScope.AdapterID || plan.SourceBinding.InventoryStatus != sourceScope.InventoryStatus || plan.SourceBinding.InventoryUnknown != sourceScope.InventoryUnknown || plan.SourceBinding.TestOnly != sourceScope.TestOnly {
				return nil, fmt.Errorf("Registry operation-plan shard %s contains an unbound operation identity", shardRef.Path)
			}
			operationID := plan.OperationIdentity.OperationID
			if operationID == "" || previousID != "" && operationID <= previousID {
				return nil, fmt.Errorf("Registry operation-plan shard %s operation ordering is invalid", shardRef.Path)
			}
			previousID = operationID
			refs, err := healthOperationPlanEvidenceRefs(plan)
			if err != nil {
				return nil, errors.New("Registry operation-plan evidence reference count exceeds its ceiling")
			}
			for _, ref := range refs {
				switch ref.EvidenceKind {
				case "operation_document":
					artifact, ok := documentArtifacts[ref.ArtifactPath]
					if !ok || !strings.EqualFold(artifact.SHA256, ref.SHA256) || !validHealthOperationDocumentPointer(ref.JSONPointer) {
						return nil, errors.New("Registry operation-document reference is not bound by the plan index")
					}
					if err := addProjectionReference(ref.ArtifactPath, ref.SHA256, healthOperationDocumentEvidenceMaxBytes, "operation_document_evidence", manifestArtifacts, documentArtifacts, policyPaths); err != nil {
						return nil, err
					}
				case "reviewed_policy":
					if strings.HasPrefix(ref.ArtifactPath, "policy/") {
						if err := addProjectionReference(ref.ArtifactPath, ref.SHA256, healthOperationPolicyMaxBytes, "", manifestArtifacts, documentArtifacts, policyPaths); err != nil {
							return nil, err
						}
					} else if strings.HasPrefix(ref.ArtifactPath, healthOperationResponseAssertionArtifactPathPrefix) {
						if (ref.JSONPointer != "#/assertion" && ref.JSONPointer != "#/review") || ref.ArtifactPath != healthOperationResponseAssertionArtifactPathPrefix+plan.OperationIdentity.OperationID+".json" {
							return nil, errors.New("Registry response assertion reference is not operation-bound")
						}
						if err := addProjectionReference(ref.ArtifactPath, ref.SHA256, healthOperationResponseAssertionMaxBytes, "operation_response_assertion", manifestArtifacts, documentArtifacts, assertionPaths); err != nil {
							return nil, err
						}
					}
				}
			}
			if contract := plan.RequestPlan.RequestContract; contract != nil && contract.ResponseAssertion.AssertionRef != "" {
				refPath, pointer, ok := strings.Cut(contract.ResponseAssertion.AssertionRef, "#")
				if !ok || pointer != "/assertion" || refPath != healthOperationResponseAssertionArtifactPathPrefix+plan.OperationIdentity.OperationID+".json" || assertionPaths[refPath] == "" {
					return nil, errors.New("Registry operation-plan assertion pointer is not transitively bound")
				}
			}
		}
		var first, last healthOperationPlanRecord
		if json.Unmarshal(shard.Records[0], &first) != nil || json.Unmarshal(shard.Records[len(shard.Records)-1], &last) != nil || first.OperationIdentity.OperationID != shardRef.FirstOperationID || last.OperationIdentity.OperationID != shardRef.LastOperationID {
			return nil, fmt.Errorf("Registry operation-plan shard %s range differs from its index", shardRef.Path)
		}
	}

	for artifactPath, digest := range policyPaths {
		if _, err := loadArtifact(artifactPath, healthOperationPolicyMaxBytes, ""); err != nil {
			return nil, err
		}
		artifact := manifestArtifacts[artifactPath]
		if !strings.EqualFold(artifact.SHA256, digest) {
			return nil, errors.New("Registry policy digest differs from its operation-plan reference")
		}
	}
	for artifactPath, digest := range assertionPaths {
		if _, err := loadArtifact(artifactPath, healthOperationResponseAssertionMaxBytes, "operation_response_assertion"); err != nil {
			return nil, err
		}
		artifact := manifestArtifacts[artifactPath]
		if !strings.EqualFold(artifact.SHA256, digest) {
			return nil, errors.New("Registry assertion digest differs from its operation-plan reference")
		}
	}
	if len(projection) > datapanRegistryPlanProjectionMaxFiles || totalBytes > datapanRegistryPlanProjectionMaxBytes {
		return nil, errors.New("Registry operation-plan projection exceeds its aggregate resource ceiling")
	}
	return projection, nil
}

func addProjectionReference(artifactPath, digest string, maximumBytes int64, expectedKind string, manifestArtifacts map[string]releaseManifestArtifact, documentArtifacts map[string]healthOperationPlanArtifactRef, selected map[string]string) error {
	if !datapanRegistrySafeProjectionPath(artifactPath) || !validSHA256(digest) {
		return errors.New("Registry operation-plan runtime reference path or digest is invalid")
	}
	artifact, ok := manifestArtifacts[artifactPath]
	if !ok || artifact.Bytes < 1 || artifact.Bytes > maximumBytes || expectedKind != "" && artifact.Kind != expectedKind || !strings.EqualFold(artifact.SHA256, digest) {
		return fmt.Errorf("Registry operation-plan runtime reference %s is not bounded by the release manifest", artifactPath)
	}
	if reference, indexed := documentArtifacts[artifactPath]; indexed && (reference.Bytes != artifact.Bytes || !strings.EqualFold(reference.SHA256, digest)) {
		return errors.New("Registry operation-document bytes differ from the indexed reference")
	}
	if previous := selected[artifactPath]; previous != "" && !strings.EqualFold(previous, digest) {
		return errors.New("Registry operation-plan runtime references disagree on digest")
	}
	selected[artifactPath] = digest
	return nil
}

func datapanRegistryUniqueManifestArtifacts(manifest releaseManifest) (map[string]releaseManifestArtifact, error) {
	if manifest.ArtifactCount != len(manifest.Artifacts) || len(manifest.Artifacts) == 0 {
		return nil, errors.New("Registry release manifest artifact count is invalid")
	}
	artifacts := make(map[string]releaseManifestArtifact, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if !datapanRegistrySafeProjectionPath(artifact.Path) || artifact.Bytes < 1 || !validSHA256(artifact.SHA256) {
			return nil, errors.New("Registry release manifest contains an invalid artifact record")
		}
		if _, duplicate := artifacts[artifact.Path]; duplicate {
			return nil, fmt.Errorf("Registry release manifest duplicates artifact %s", artifact.Path)
		}
		artifacts[artifact.Path] = artifact
	}
	return artifacts, nil
}

func datapanRegistrySafeProjectionPath(value string) bool {
	if value == "" || path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\\:\x00?#") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func datapanRegistryIsPlanProjectionPath(value string) bool {
	if value == healthOperationPlanIndexPath || value == healthOperationPolicyArtifactPath || value == healthOperationDocumentEvidencePath || value == healthOperationDocumentEvidenceV2Path || value == healthOperationPlanSchemaPath || value == healthOperationPolicySchemaPath || value == healthOperationResponseAssertionSchemaPath {
		return true
	}
	return strings.HasPrefix(value, "reports/operation-observation-plan/shards/") && strings.HasSuffix(value, ".json") || strings.HasPrefix(value, "reports/operation-document-evidence/") && strings.HasSuffix(value, ".json") || strings.HasPrefix(value, healthOperationResponseAssertionArtifactPathPrefix) && strings.HasSuffix(value, ".json")
}

func datapanRegistryDeferZipProjectionPath(value string) bool {
	if datapanRegistryIsPlanProjectionPath(value) {
		return true
	}
	return strings.HasPrefix(value, "policy/") && strings.HasSuffix(value, ".json") && value != "policy/sustainable-coverage.json"
}

func readBoundedRegistryZipFile(file *zip.File, maximumBytes int64) ([]byte, error) {
	if file == nil || maximumBytes < 1 || file.UncompressedSize64 > uint64(maximumBytes) {
		return nil, errors.New("Registry ZIP entry exceeds its uncompressed size ceiling")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximumBytes || uint64(len(data)) != file.UncompressedSize64 {
		return nil, errors.New("Registry ZIP entry actual size differs from its bounded header")
	}
	return data, nil
}
