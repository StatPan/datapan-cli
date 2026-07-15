package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const (
	healthCatalogSchema       = "datapan.health-probe-catalog.v1"
	healthCatalogArtifactPath = "reports/health-probe-catalog.json"
	healthCatalogMaxBytes     = 64 << 10
)

type manifestHealthCatalog struct {
	SchemaVersion  string `json:"schema_version"`
	Authority      string `json:"authority"`
	SourceRegistry struct {
		SHA256 string `json:"sha256"`
	} `json:"source_registry"`
	Entries []manifestHealthCatalogEntry `json:"entries"`
}

type manifestHealthCatalogEntry struct {
	OperationID string `json:"operation_id"`
	Policy      struct {
		Key       string `json:"key"`
		Version   int    `json:"version"`
		Authority string `json:"authority"`
		MaxLevel  string `json:"max_level"`
	} `json:"policy"`
	Aliases struct {
		DatasetID       string `json:"dataset_id"`
		OperationName   string `json:"operation_name"`
		CLIOperationKey string `json:"cli_operation_key"`
	} `json:"aliases"`
	Provider string `json:"provider"`
	Endpoint struct {
		Host            string `json:"host"`
		Path            string `json:"path"`
		DependencyClass string `json:"dependency_class"`
	} `json:"endpoint"`
	Eligibility struct {
		Status string `json:"status"`
	} `json:"eligibility"`
	Execution struct {
		TimeoutCeilingMS int                       `json:"timeout_ceiling_ms"`
		RequestBudget    int                       `json:"request_budget"`
		SafeParameters   []manifestHealthParameter `json:"safe_parameters"`
	} `json:"execution"`
}

type manifestHealthParameter struct {
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	Minimum     int    `json:"minimum"`
	Maximum     int    `json:"maximum"`
	OffsetYears int    `json:"offset_years"`
	MinimumYear int    `json:"minimum_year"`
	MaximumYear int    `json:"maximum_year"`
}

type healthCatalogOptions struct {
	Path             string
	RegistryRevision string
}

func healthCatalogInvocation(args []string) (healthCatalogOptions, bool, error) {
	options := healthCatalogOptions{}
	health := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--health":
			health = true
		case "--health-catalog", "--health-registry-revision":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return healthCatalogOptions{}, false, fmt.Errorf("%s requires a value", args[i])
			}
			if args[i] == "--health-catalog" {
				options.Path = strings.TrimSpace(args[i+1])
			} else {
				options.RegistryRevision = strings.TrimSpace(args[i+1])
			}
			i++
		}
	}
	if options.Path == "" && options.RegistryRevision == "" {
		return healthCatalogOptions{}, false, nil
	}
	if options.Path == "" || !validImmutableRevision(options.RegistryRevision) {
		return healthCatalogOptions{}, false, errors.New("--health-catalog requires an immutable --health-registry-revision")
	}
	if !health {
		return healthCatalogOptions{}, false, errors.New("--health-catalog requires --health")
	}
	if len(args) == 0 || (args[0] != "verify" && !(len(args) > 1 && args[0] == "catalog" && args[1] == "verify")) {
		return healthCatalogOptions{}, false, errors.New("--health-catalog is limited to verify --health")
	}
	return options, true, nil
}

func loadManifestBoundHealthCatalog(options healthCatalogOptions, now time.Time) (datago.Registry, registryTrustContext, error) {
	data, err := readBoundedFile(options.Path, healthCatalogMaxBytes)
	if err != nil {
		return datago.Registry{}, registryTrustContext{}, err
	}
	var catalog manifestHealthCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("decode health catalog")
	}
	if catalog.SchemaVersion != healthCatalogSchema || catalog.Authority != "datapan-registry" || len(catalog.Entries) != 10 || !validSHA256(catalog.SourceRegistry.SHA256) {
		return datago.Registry{}, registryTrustContext{}, errors.New("health catalog contract is invalid")
	}

	provenance, err := readRegistryInstallProvenance(defaultRegistryInstallProvenancePath)
	if err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("read installed Registry provenance")
	}
	manifestData, err := readBoundedFile(defaultReleaseManifestPath, 4<<20)
	if err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("read installed release manifest")
	}
	manifestSum := sha256.Sum256(manifestData)
	if !strings.EqualFold(provenance.ReleaseManifestSHA256, hex.EncodeToString(manifestSum[:])) || provenance.ManifestRegistryVerified == nil || !*provenance.ManifestRegistryVerified {
		return datago.Registry{}, registryTrustContext{}, errors.New("installed release manifest provenance is invalid")
	}
	if provenance.DatasetRevision != "" && provenance.DatasetRevision != options.RegistryRevision {
		return datago.Registry{}, registryTrustContext{}, errors.New("health Registry revision differs from installed provenance")
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.SchemaVersion != "datapan.release-manifest.v1" {
		return datago.Registry{}, registryTrustContext{}, errors.New("decode installed release manifest")
	}
	catalogArtifact, catalogOK := manifestArtifact(manifest, healthCatalogArtifactPath)
	registryArtifact, registryOK := manifestArtifact(manifest, "data/data-go-kr.registry.json")
	catalogSum := sha256.Sum256(data)
	if !catalogOK || !registryOK || catalogArtifact.Bytes != int64(len(data)) || !strings.EqualFold(catalogArtifact.SHA256, hex.EncodeToString(catalogSum[:])) || !strings.EqualFold(registryArtifact.SHA256, catalog.SourceRegistry.SHA256) || !strings.EqualFold(provenance.RegistrySHA256, catalog.SourceRegistry.SHA256) {
		return datago.Registry{}, registryTrustContext{}, errors.New("health catalog is not bound to the installed Registry release")
	}

	specs := make([]datago.Spec, 0, len(catalog.Entries))
	seenIDs := map[string]bool{}
	seenSelectors := map[string]bool{}
	for _, entry := range catalog.Entries {
		if entry.OperationID == "" || seenIDs[entry.OperationID] || entry.Policy.Key != entry.OperationID || entry.Policy.Version < 1 || entry.Policy.Authority != "datapan-registry" || entry.Execution.RequestBudget != 1 || entry.Execution.TimeoutCeilingMS < 1000 || entry.Execution.TimeoutCeilingMS > 30000 || (entry.Eligibility.Status != "eligible" && entry.Eligibility.Status != "credential_required") || !validSHA256(entry.Aliases.CLIOperationKey) {
			return datago.Registry{}, registryTrustContext{}, errors.New("health catalog entry policy is invalid")
		}
		seenIDs[entry.OperationID] = true
		selector := entry.Aliases.DatasetID + "\x00" + entry.Aliases.OperationName
		if entry.Aliases.DatasetID == "" || entry.Aliases.OperationName == "" || seenSelectors[selector] || entry.Provider == "" || entry.Endpoint.Host == "" || !strings.HasPrefix(entry.Endpoint.Path, "/") {
			return datago.Registry{}, registryTrustContext{}, errors.New("health catalog selector is invalid")
		}
		seenSelectors[selector] = true
		params := map[string]string{}
		requestParams := []datago.Param{{Name: "serviceKey"}}
		for _, parameter := range entry.Execution.SafeParameters {
			value, err := healthParameterValue(parameter, now)
			if err != nil || parameter.Name == "" {
				return datago.Registry{}, registryTrustContext{}, errors.New("health catalog parameter policy is invalid")
			}
			params[parameter.Name] = value
			requestParams = append(requestParams, datago.Param{Name: parameter.Name})
		}
		spec := datago.Spec{ID: entry.Aliases.DatasetID, Title: entry.Aliases.DatasetID, Provider: entry.Provider, Priority: "P2", Operations: []datago.Operation{{Name: entry.Aliases.OperationName, Endpoint: "https://" + strings.ToLower(entry.Endpoint.Host) + entry.Endpoint.Path, DefaultParams: params, RequestParams: requestParams}}}
		op := spec.Operations[0]
		dependency := datago.OperationDependencyClass(spec, op)
		if dependency != entry.Endpoint.DependencyClass {
			return datago.Registry{}, registryTrustContext{}, errors.New("health catalog dependency class drift")
		}
		host, endpointPath := healthEndpoint(op.Endpoint)
		key := healthOperationKey(healthProbeOperation{DatasetID: spec.ID, OperationName: op.Name, Provider: spec.Provider, EndpointHost: host, EndpointPath: endpointPath, DependencyClass: dependency})
		if key != strings.ToLower(entry.Aliases.CLIOperationKey) {
			return datago.Registry{}, registryTrustContext{}, errors.New("health catalog operation key drift")
		}
		specs = append(specs, spec)
	}
	matches := true
	datasetID := provenance.DatasetID
	if datasetID == "" {
		datasetID = datapanRegistryHFDatasetID
	}
	trust := registryTrustContext{Status: "trusted", RegistrySource: "health_catalog", RegistryPath: defaultRegistryPath, ProvenancePresent: true, ReleaseTag: provenance.ReleaseTag, RegistrySHA256: strings.ToLower(provenance.RegistrySHA256), Distribution: provenance.Distribution, DatasetID: datasetID, DatasetRevision: options.RegistryRevision, Integrity: "verified", ManifestBinding: "verified", RegistryDigestMatches: &matches, ReleaseReadiness: "health_catalog_bound", VerificationEvidence: "manifest_bound_health_catalog", VerificationFreshness: "not_evaluated", ExecutionAllowed: true}
	return datago.NewRegistry(specs), trust, nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return nil, errors.New("bounded file is unavailable")
	}
	return os.ReadFile(path)
}

func manifestArtifact(manifest releaseManifest, path string) (releaseManifestArtifact, bool) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == path && validSHA256(artifact.SHA256) {
			return artifact, true
		}
	}
	return releaseManifestArtifact{}, false
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func healthParameterValue(parameter manifestHealthParameter, now time.Time) (string, error) {
	switch parameter.Strategy {
	case "bounded_integer":
		if parameter.Minimum < 0 || parameter.Maximum < parameter.Minimum {
			return "", errors.New("invalid bounded integer")
		}
		return strconv.Itoa(parameter.Minimum), nil
	case "relative_year":
		year := now.UTC().Year() + parameter.OffsetYears
		if year < parameter.MinimumYear || year > parameter.MaximumYear {
			return "", errors.New("relative year outside bounds")
		}
		return strconv.Itoa(year), nil
	default:
		return "", fmt.Errorf("unsupported health parameter strategy")
	}
}
