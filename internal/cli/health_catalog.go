package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const (
	healthCatalogSchema       = "datapan.health-probe-catalog.v1"
	healthCatalogArtifactPath = "reports/health-probe-catalog.json"
	// These limits cap v1 input and validation work for the CLI. They do not
	// describe or imply admission of the Registry's full operation set.
	healthCatalogMaxBytes                  = 32 << 20
	healthCatalogMaxEntries                = 32_000
	healthCatalogMaxSafeParametersPerEntry = 256
	// The aggregate budget allows four policy parameters per maximum entry on
	// average while bounding decoded catalog and execution-policy allocations.
	healthCatalogMaxTotalSafeParameters = 128_000
)

var (
	errHealthCatalogEntryLimit         = errors.New("health catalog entry count exceeds limit")
	errHealthCatalogSafeParameterLimit = errors.New("health catalog safe parameter count exceeds limit")
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
		Scheme          string `json:"scheme,omitempty"`
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
	operationPlanRequested := hasAnyArg(args, "--health-plan-index")
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
	if options.Path == "" && (options.RegistryRevision == "" || operationPlanRequested) {
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
	if err := preflightHealthCatalogJSON(data); err != nil {
		if errors.Is(err, errHealthCatalogEntryLimit) || errors.Is(err, errHealthCatalogSafeParameterLimit) {
			return datago.Registry{}, registryTrustContext{}, errors.New("health catalog contract is invalid")
		}
		return datago.Registry{}, registryTrustContext{}, errors.New("decode health catalog")
	}
	var catalog manifestHealthCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("decode health catalog")
	}
	if catalog.SchemaVersion != healthCatalogSchema || catalog.Authority != "datapan-registry" || len(catalog.Entries) < 1 || len(catalog.Entries) > healthCatalogMaxEntries || !validSHA256(catalog.SourceRegistry.SHA256) {
		return datago.Registry{}, registryTrustContext{}, errors.New("health catalog contract is invalid")
	}

	provenance, err := readRegistryInstallProvenance(defaultRegistryInstallProvenancePath)
	if err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("read installed Registry provenance")
	}
	manifestData, err := readBoundedFile(defaultReleaseManifestPath, healthOperationPlanManifestMaxBytes)
	if err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("read installed release manifest")
	}
	if err := preflightHealthReleaseManifestJSON(manifestData); err != nil {
		return datago.Registry{}, registryTrustContext{}, errors.New("installed release manifest exceeds its bounded structure")
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
	healthPolicies := map[string]*healthProbePolicy{}
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
		endpoint, err := healthCatalogEndpoint(entry)
		if err != nil {
			return datago.Registry{}, registryTrustContext{}, err
		}
		spec := datago.Spec{ID: entry.Aliases.DatasetID, Title: entry.Aliases.DatasetID, Provider: entry.Provider, Priority: "P2", Operations: []datago.Operation{{Name: entry.Aliases.OperationName, Endpoint: endpoint, DefaultParams: params, RequestParams: requestParams}}}
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
		healthPolicies[key] = &healthProbePolicy{Key: entry.Policy.Key, Version: entry.Policy.Version, Authority: entry.Policy.Authority, MaxLevel: entry.Policy.MaxLevel}
		specs = append(specs, spec)
	}
	matches := true
	datasetID := provenance.DatasetID
	if datasetID == "" {
		datasetID = datapanRegistryHFDatasetID
	}
	trust := registryTrustContext{Status: "trusted", RegistrySource: "health_catalog", RegistryPath: defaultRegistryPath, ProvenancePresent: true, ReleaseTag: provenance.ReleaseTag, RegistrySHA256: strings.ToLower(provenance.RegistrySHA256), Distribution: provenance.Distribution, DatasetID: datasetID, DatasetRevision: options.RegistryRevision, Integrity: "verified", ManifestBinding: "verified", RegistryDigestMatches: &matches, ReleaseReadiness: "health_catalog_bound", VerificationEvidence: "manifest_bound_health_catalog", VerificationFreshness: "not_evaluated", ExecutionAllowed: true, HealthPolicies: healthPolicies}
	return datago.NewRegistry(specs), trust, nil
}

// The scheme is Registry-owned and covered by the catalog's manifest digest.
// Older catalogs preserve their existing HTTPS behavior. A failed request is
// never retried using a different protocol.
func healthCatalogEndpoint(entry manifestHealthCatalogEntry) (string, error) {
	scheme := entry.Endpoint.Scheme
	if scheme == "" {
		scheme = "https"
	}
	if scheme != "http" && scheme != "https" {
		return "", errors.New("health catalog endpoint scheme is invalid")
	}
	host := strings.ToLower(entry.Endpoint.Host)
	if len(host) == 0 || len(host) > 253 {
		return "", errors.New("health catalog endpoint host is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("health catalog endpoint host is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("health catalog endpoint host is invalid")
			}
		}
	}
	path := entry.Endpoint.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\r\n") {
		return "", errors.New("health catalog endpoint path is invalid")
	}
	u, err := url.Parse(scheme + "://" + host + path)
	if err != nil || u.Host != host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.EscapedPath() != path {
		return "", errors.New("health catalog endpoint path is invalid")
	}
	return u.String(), nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	return readBoundedFileWithOpener(path, maximum, openBoundedCandidate)
}

func readBoundedFileWithOpener(path string, maximum int64, open func(string) (*os.File, error)) ([]byte, error) {
	if maximum < 1 {
		return nil, errors.New("bounded file is unavailable")
	}
	pathInfo, err := os.Stat(path)
	if err != nil || !pathInfo.Mode().IsRegular() {
		return nil, errors.New("bounded file is unavailable")
	}
	file, err := open(path)
	if err != nil {
		return nil, errors.New("bounded file is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("bounded file is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maximum {
		return nil, errors.New("bounded file is unavailable")
	}
	return data, nil
}

// preflightHealthCatalogJSON walks the complete JSON document on success and
// counts entries and safe parameters before typed slices can be allocated.
func preflightHealthCatalogJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("health catalog is not an object")
	}
	entryCount, totalSafeParameters := 0, 0
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("health catalog field name is invalid")
		}
		if strings.EqualFold(key, "entries") {
			entryCount, totalSafeParameters, err = scanHealthCatalogEntries(decoder, entryCount, totalSafeParameters)
		} else {
			err = skipHealthCatalogJSONValue(decoder)
		}
		if err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("health catalog object is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("health catalog has trailing JSON")
		}
		return err
	}
	return nil
}

func scanHealthCatalogEntries(decoder *json.Decoder, currentCount, totalSafeParameters int) (int, int, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return currentCount, totalSafeParameters, errors.New("health catalog entries are not an array")
	}
	for decoder.More() {
		currentCount++
		if currentCount > healthCatalogMaxEntries {
			return currentCount, totalSafeParameters, errHealthCatalogEntryLimit
		}
		totalSafeParameters, err = scanHealthCatalogEntry(decoder, totalSafeParameters)
		if err != nil {
			return currentCount, totalSafeParameters, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return currentCount, totalSafeParameters, errors.New("health catalog entries are incomplete")
	}
	return currentCount, totalSafeParameters, nil
}

func scanHealthCatalogEntry(decoder *json.Decoder, totalSafeParameters int) (int, error) {
	opening, err := decoder.Token()
	if err != nil {
		return totalSafeParameters, err
	}
	if opening != json.Delim('{') {
		if delim, ok := opening.(json.Delim); ok && (delim == '[' || delim == '{') {
			return totalSafeParameters, skipHealthCatalogJSONContainer(decoder, delim)
		}
		return totalSafeParameters, nil
	}
	entrySafeParameters := 0
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return totalSafeParameters, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return totalSafeParameters, errors.New("health catalog entry field name is invalid")
		}
		if strings.EqualFold(key, "execution") {
			totalSafeParameters, entrySafeParameters, err = scanHealthCatalogExecution(decoder, totalSafeParameters, entrySafeParameters)
		} else {
			err = skipHealthCatalogJSONValue(decoder)
		}
		if err != nil {
			return totalSafeParameters, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return totalSafeParameters, errors.New("health catalog entry is incomplete")
	}
	return totalSafeParameters, nil
}

func scanHealthCatalogExecution(decoder *json.Decoder, totalSafeParameters, entrySafeParameters int) (int, int, error) {
	opening, err := decoder.Token()
	if err != nil {
		return totalSafeParameters, entrySafeParameters, err
	}
	if opening != json.Delim('{') {
		if delim, ok := opening.(json.Delim); ok && (delim == '[' || delim == '{') {
			return totalSafeParameters, entrySafeParameters, skipHealthCatalogJSONContainer(decoder, delim)
		}
		return totalSafeParameters, entrySafeParameters, nil
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return totalSafeParameters, entrySafeParameters, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return totalSafeParameters, entrySafeParameters, errors.New("health catalog execution field name is invalid")
		}
		if strings.EqualFold(key, "safe_parameters") {
			totalSafeParameters, entrySafeParameters, err = scanHealthCatalogSafeParameters(decoder, totalSafeParameters, entrySafeParameters)
		} else {
			err = skipHealthCatalogJSONValue(decoder)
		}
		if err != nil {
			return totalSafeParameters, entrySafeParameters, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return totalSafeParameters, entrySafeParameters, errors.New("health catalog execution object is incomplete")
	}
	return totalSafeParameters, entrySafeParameters, nil
}

func scanHealthCatalogSafeParameters(decoder *json.Decoder, totalSafeParameters, entrySafeParameters int) (int, int, error) {
	opening, err := decoder.Token()
	if err != nil {
		return totalSafeParameters, entrySafeParameters, err
	}
	if opening != json.Delim('[') {
		if delim, ok := opening.(json.Delim); ok && (delim == '[' || delim == '{') {
			return totalSafeParameters, entrySafeParameters, skipHealthCatalogJSONContainer(decoder, delim)
		}
		return totalSafeParameters, entrySafeParameters, nil
	}
	for decoder.More() {
		entrySafeParameters++
		totalSafeParameters++
		if entrySafeParameters > healthCatalogMaxSafeParametersPerEntry || totalSafeParameters > healthCatalogMaxTotalSafeParameters {
			return totalSafeParameters, entrySafeParameters, errHealthCatalogSafeParameterLimit
		}
		if err := skipHealthCatalogJSONValue(decoder); err != nil {
			return totalSafeParameters, entrySafeParameters, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return totalSafeParameters, entrySafeParameters, errors.New("health catalog safe parameters are incomplete")
	}
	return totalSafeParameters, entrySafeParameters, nil
}

func skipHealthCatalogJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	return skipHealthCatalogJSONContainer(decoder, delim)
}

func skipHealthCatalogJSONContainer(decoder *json.Decoder, delim json.Delim) error {
	switch delim {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipHealthCatalogJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("health catalog object value is incomplete")
		}
	case '[':
		for decoder.More() {
			if err := skipHealthCatalogJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("health catalog array value is incomplete")
		}
	default:
		return errors.New("health catalog delimiter is invalid")
	}
	return nil
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
