package cli

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	healthOperationPlanSchemaVersion                     = "datapan.operation-observation-plan.v1"
	healthOperationPlanSchemaID                          = "https://schemas.datapan.dev/datapan.operation-observation-plan.v1.schema.json"
	healthOperationPlanSchemaSHA256                      = "cafa93014d7a32ef072f74df1a730f681e5b206440e4a83e9cdf426f6686e162"
	healthOperationPlanSchemaSourceRevision              = "123e9cdaa82998e77b14c4f1007791d39cfc7b96"
	healthOperationPlanSchemaPath                        = "schemas/datapan.operation-observation-plan.v1.schema.json"
	healthOperationPolicySchemaID                        = "https://schemas.datapan.dev/datapan.operation-observation-policy.v1.schema.json"
	healthOperationPolicySchemaSHA256                    = "acd9e80d3f41e4a0f16b010975fc716bf1ead5c5a3b128c12d03dd51b025f31d"
	healthOperationPolicySchemaPath                      = "schemas/datapan.operation-observation-policy.v1.schema.json"
	healthOperationResponseAssertionSchemaID             = "https://schemas.datapan.dev/datapan.operation-response-assertion.v2.schema.json"
	healthOperationResponseAssertionSchemaSHA256         = "bba64ddd581b41b77f1b3ae2de36f66261d25d7606e1c13c8146e5030787172d"
	healthOperationResponseAssertionSchemaSourceRevision = "446b8dcd86ceeb0a540baabcef479aec47820e9b"
	healthOperationResponseAssertionSchemaPath           = "schemas/datapan.operation-response-assertion.v2.schema.json"
	healthOperationResponseAssertionMaxBytes             = 1 << 20
	healthOperationDocumentEvidenceSchemaID              = "https://schemas.datapan.dev/datapan.operation-document-evidence.v1.schema.json"
	healthOperationDocumentEvidenceSHA256                = "0b4a5a7ab10eeccb523d2af8a8e62e76f14a6243eea00558ac49e9959e7a3d1d"
	healthOperationDocumentEvidenceRevision              = "18d75eef2977afdc58f1830a3fae2b8875956711"
	healthOperationDocumentEvidencePath                  = "schemas/datapan.operation-document-evidence.v1.schema.json"
	healthOperationDocumentEvidenceV2SchemaID            = "https://schemas.datapan.dev/datapan.operation-document-evidence.v2.schema.json"
	healthOperationDocumentEvidenceV2SHA256              = "d6edb7dad63b9d7cdac6753fc02cba962cb8d96d7c01119c031935abfc973108"
	healthOperationDocumentEvidenceV2Revision            = "6e52aa59d79afa0371423ead8c287a05c4ab6210"
	healthOperationDocumentEvidenceV2Path                = "schemas/datapan.operation-document-evidence.v2.schema.json"
	healthOperationPlanIndexPath                         = "reports/operation-observation-plan/index.json"
	healthOperationPlanSourceRegistryPath                = "data/data-go-kr.registry.json"
	healthOperationPlanManifestMaxBytes                  = 16 << 20
	// The manifest is a fleet-sized artifact: its bounded 32,000 refs need a
	// separate token budget from a single plan, while depth, duplicate-key,
	// and total-ref checks still reject structurally excessive input.
	healthOperationPlanMaxManifestJSONTokens = 500_000
	healthOperationPlanIndexMaxBytes         = 8 << 20
	healthOperationPlanShardMaxBytes         = 16 << 20
	healthOperationPlanMaxJSONTokens         = 100_000
	// A Registry shard can contain up to 256 operation plans plus their
	// manifest-bound evidence references. The largest current production shard
	// is 246,400 JSON decoder tokens; keep a separate 500,000-token ceiling for
	// that bounded fleet artifact without widening the per-operation or
	// provider-response budget.
	healthOperationPlanMaxShardJSONTokens = 500_000
	// An index can carry tens of thousands of compact artifact refs. Keep its
	// larger token budget separate from the plan and provider-response budget.
	healthOperationPlanMaxIndexJSONTokens = 500_000
	healthOperationPlanMaxJSONDepth       = 64
	healthOperationPlanMaxIndexShards     = 2048
	healthOperationPlanMaxSourceScopes    = 4096
	// Counts generation refs, source artifact refs, and shard artifact refs.
	healthOperationPlanMaxIndexArtifactRefs = 32_000
	healthOperationPlanMaxRequestParameters = 256
	healthOperationPlanMinTimeout           = time.Millisecond
	healthOperationPlanMaxTimeout           = 30 * time.Second
)

// These schema files are byte-identical mirrors of the Registry-owned pinned
// contracts. Package-local copies are embedded so an installed CLI does not
// depend on its working directory; repository-level copies ship as CLI release
// evidence.
//
//go:embed testdata/operation-observation-plan/schema.json
var embeddedHealthOperationPlanSchema []byte

//go:embed testdata/operation-observation-plan/operation-document-evidence.schema.json
var embeddedHealthOperationDocumentEvidenceSchema []byte

//go:embed testdata/operation-observation-plan/operation-document-evidence-v2.schema.json
var embeddedHealthOperationDocumentEvidenceV2Schema []byte

//go:embed testdata/operation-observation-plan/operation-observation-policy.schema.json
var embeddedHealthOperationPolicySchema []byte

//go:embed testdata/operation-observation-plan/operation-response-assertion.schema.json
var embeddedHealthOperationResponseAssertionSchema []byte

var (
	healthOperationPlanSchemaOnce              sync.Once
	healthOperationPlanSchema                  *jsonschema.Schema
	healthOperationPlanSchemaErr               error
	healthOperationPolicySchemaOnce            sync.Once
	healthOperationPolicySchema                *jsonschema.Schema
	healthOperationPolicySchemaErr             error
	healthOperationResponseAssertionSchemaOnce sync.Once
	healthOperationResponseAssertionSchema     *jsonschema.Schema
	healthOperationResponseAssertionSchemaErr  error
)

type healthOperationPlanOptions struct {
	IndexPath              string
	RegistryRevision       string
	OperationID            string
	SourceID               string
	CredentialBindingsPath string
	AttemptID              string
	CLIVersion             string
	Deadline               time.Time
}

type healthOperationPlanArtifactRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type healthOperationPlanEvidenceRef struct {
	ArtifactPath string `json:"artifact_path"`
	SHA256       string `json:"sha256"`
	JSONPointer  string `json:"json_pointer"`
	EvidenceKind string `json:"evidence_kind"`
}

type healthOperationPlanIndex struct {
	SchemaVersion    string `json:"schema_version"`
	ArtifactKind     string `json:"artifact_kind"`
	RegistryRevision string `json:"registry_revision"`
	GenerationInputs struct {
		GeneratorPath         string                           `json:"generator_path"`
		GeneratorSHA256       string                           `json:"generator_sha256"`
		OperationManifest     healthOperationPlanArtifactRef   `json:"operation_manifest"`
		OperationDenominators []healthOperationPlanArtifactRef `json:"operation_denominators"`
		LegacyPolicy          healthOperationPlanArtifactRef   `json:"legacy_policy"`
		ProviderIndex         healthOperationPlanArtifactRef   `json:"provider_index"`
		DocumentEvidence      []healthOperationPlanArtifactRef `json:"document_evidence,omitempty"`
	} `json:"generation_inputs"`
	InventoryContext struct {
		SeparateLinkOperations                  int  `json:"separate_link_operations"`
		ProviderIndexAdapterEntries             int  `json:"provider_index_adapter_entries"`
		ProviderIndexEntriesCountedAsOperations bool `json:"provider_index_entries_counted_as_operations"`
	} `json:"inventory_context"`
	Summary struct {
		KnownOperations        int `json:"known_operations"`
		RequestPlansComplete   int `json:"request_plans_complete"`
		RequestPlansIncomplete int `json:"request_plans_incomplete"`
		RuntimeBindingsBound   int `json:"runtime_bindings_bound"`
		RuntimeBindingsUnbound int `json:"runtime_bindings_unbound"`
		Admitted               int `json:"admitted"`
		NotAdmitted            int `json:"not_admitted"`
		InventoryUnknownScopes int `json:"inventory_unknown_scopes"`
	} `json:"summary"`
	SourceScopes []healthOperationPlanSourceScope `json:"source_scopes"`
	Shards       []healthOperationPlanShardRef    `json:"shards"`
}

type healthOperationPlanSourceScope struct {
	SourceID             string                           `json:"source_id"`
	Provider             string                           `json:"provider"`
	AdapterID            string                           `json:"adapter_id"`
	InventoryStatus      string                           `json:"inventory_status"`
	InventoryUnknown     bool                             `json:"inventory_unknown"`
	TestOnly             bool                             `json:"test_only"`
	RegisteredOperations int                              `json:"registered_operations"`
	IdentitySetSHA256    string                           `json:"identity_set_sha256"`
	SourceArtifacts      []healthOperationPlanArtifactRef `json:"source_artifacts"`
}

type healthOperationPlanSourceBinding struct {
	SourceID         string `json:"source_id"`
	Provider         string `json:"provider"`
	AdapterID        string `json:"adapter_id"`
	InventoryStatus  string `json:"inventory_status"`
	InventoryUnknown bool   `json:"inventory_unknown"`
	TestOnly         bool   `json:"test_only"`
}

type healthOperationPlanShardRef struct {
	SourceID         string `json:"source_id"`
	ShardIndex       int    `json:"shard_index"`
	Path             string `json:"path"`
	SHA256           string `json:"sha256"`
	Bytes            int64  `json:"bytes"`
	RecordCount      int    `json:"record_count"`
	FirstOperationID string `json:"first_operation_id"`
	LastOperationID  string `json:"last_operation_id"`
}

type healthOperationPlanShard struct {
	SchemaVersion string            `json:"schema_version"`
	ArtifactKind  string            `json:"artifact_kind"`
	SourceID      string            `json:"source_id"`
	ShardIndex    int               `json:"shard_index"`
	Records       []json.RawMessage `json:"records"`
}

type healthOperationPlanRecord struct {
	SchemaVersion     string                            `json:"schema_version"`
	ArtifactKind      string                            `json:"artifact_kind"`
	SourceBinding     healthOperationPlanSourceBinding  `json:"source_binding"`
	OperationIdentity healthOperationPlanIdentity       `json:"operation_identity"`
	RequestPlan       healthOperationPlanRequestPlan    `json:"request_plan"`
	RuntimeBinding    healthOperationPlanRuntimeBinding `json:"runtime_binding"`
	Admission         healthOperationPlanAdmission      `json:"admission"`
	LegacyPolicy      *healthOperationPlanLegacyPolicy  `json:"legacy_policy,omitempty"`
}

type healthOperationPlanIdentity struct {
	OperationID          string                                 `json:"operation_id"`
	Protocol             string                                 `json:"protocol"`
	DatasetID            string                                 `json:"dataset_id,omitempty"`
	OperationName        string                                 `json:"operation_name,omitempty"`
	UpstreamOperationKey string                                 `json:"upstream_operation_key,omitempty"`
	LegacySelectors      []string                               `json:"legacy_selectors,omitempty"`
	RegisteredEndpoint   *healthOperationPlanRegisteredEndpoint `json:"registered_endpoint,omitempty"`
}

type healthOperationPlanRegisteredEndpoint struct {
	Host string `json:"host"`
	Port *int   `json:"port,omitempty"`
	Path string `json:"path"`
}

type healthOperationPlanRequestPlan struct {
	Status          string                              `json:"status"`
	EvidenceRefs    []healthOperationPlanEvidenceRef    `json:"evidence_refs"`
	MissingFields   []string                            `json:"missing_fields,omitempty"`
	RequestContract *healthOperationPlanRequestContract `json:"request_contract,omitempty"`
}

type healthOperationPlanRequestContract struct {
	Transport struct {
		Protocol          string                           `json:"protocol"`
		Scheme            string                           `json:"scheme"`
		Host              string                           `json:"host"`
		Port              *int                             `json:"port,omitempty"`
		Path              string                           `json:"path"`
		HTTPMethod        string                           `json:"http_method"`
		SOAPAction        string                           `json:"soap_action,omitempty"`
		SOAPVersion       string                           `json:"soap_version,omitempty"`
		EnvelopeNamespace string                           `json:"envelope_namespace,omitempty"`
		OperationQName    healthOperationPlanQName         `json:"operation_qname,omitempty"`
		BodyEncoding      string                           `json:"body_encoding,omitempty"`
		Authority         string                           `json:"authority"`
		EvidenceRefs      []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	} `json:"transport"`
	OperationEffect struct {
		Classification string                           `json:"classification"`
		Authority      string                           `json:"authority"`
		EvidenceRefs   []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	} `json:"operation_effect"`
	ParameterInventoryEvidenceRefs []healthOperationPlanEvidenceRef `json:"parameter_inventory_evidence_refs"`
	Parameters                     []healthOperationPlanParameter   `json:"parameters"`
	Authentication                 struct {
		Requirement                 string                           `json:"requirement"`
		Mechanism                   string                           `json:"mechanism"`
		Placement                   string                           `json:"placement"`
		ParameterName               string                           `json:"parameter_name,omitempty"`
		HeaderName                  string                           `json:"header_name,omitempty"`
		HeaderQName                 *healthOperationPlanQName        `json:"header_qname,omitempty"`
		Cardinality                 string                           `json:"cardinality,omitempty"`
		CredentialReferenceRequired bool                             `json:"credential_reference_required"`
		EvidenceRefs                []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	} `json:"authentication"`
	Limits struct {
		RequestBudget    int                              `json:"request_budget"`
		TimeoutMS        int                              `json:"timeout_ms"`
		MaxRequestBytes  int64                            `json:"max_request_bytes"`
		MaxResponseBytes int64                            `json:"max_response_bytes"`
		EvidenceRefs     []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	} `json:"limits"`
	ResponseAssertion struct {
		Kind                 string                           `json:"kind"`
		ExpectedStatusCodes  []int                            `json:"expected_status_codes,omitempty"`
		EmptyResultSemantics string                           `json:"empty_result_semantics"`
		AssertionRef         string                           `json:"assertion_ref,omitempty"`
		EvidenceRefs         []healthOperationPlanEvidenceRef `json:"evidence_refs"`
	} `json:"response_assertion"`
}

type healthOperationPlanQName struct {
	Namespace string `json:"namespace"`
	LocalName string `json:"local_name"`
}

type healthOperationPlanParameter struct {
	Name          string                    `json:"name"`
	QualifiedName *healthOperationPlanQName `json:"qualified_name,omitempty"`
	Location      string                    `json:"location"`
	Cardinality   string                    `json:"cardinality"`
	ValueStrategy struct {
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
	} `json:"value_strategy"`
	EvidenceRefs []healthOperationPlanEvidenceRef `json:"evidence_refs"`
}

type healthOperationPlanRuntimeBinding struct {
	Status                   string                           `json:"status"`
	MissingFields            []string                         `json:"missing_fields,omitempty"`
	CredentialReference      string                           `json:"credential_reference,omitempty"`
	CredentialScopeKey       string                           `json:"credential_scope_key,omitempty"`
	QuotaPolicies            []healthOperationPlanQuotaPolicy `json:"quota_policies,omitempty"`
	ObservationPeriodSeconds int                              `json:"observation_period_seconds,omitempty"`
	EvidenceRefs             []healthOperationPlanEvidenceRef `json:"evidence_refs"`
}

type healthOperationPlanQuotaPolicy struct {
	ScopeKind              string                           `json:"scope_kind"`
	ScopeKey               string                           `json:"scope_key"`
	ScopeSHA256            string                           `json:"scope_sha256"`
	MaxConcurrent          int                              `json:"max_concurrent"`
	RequestsPerWindow      int                              `json:"requests_per_window"`
	WindowSeconds          int                              `json:"window_seconds"`
	MinimumIntervalSeconds int                              `json:"minimum_interval_seconds"`
	EvidenceRefs           []healthOperationPlanEvidenceRef `json:"evidence_refs"`
}

type healthOperationPlanAdmission struct {
	Status       string                           `json:"status"`
	Reasons      []string                         `json:"reasons"`
	EvidenceRefs []healthOperationPlanEvidenceRef `json:"evidence_refs"`
}

type healthOperationPlanLegacyPolicy struct {
	PolicyRef          healthOperationPlanEvidenceRef       `json:"policy_ref"`
	SafeParameters     []healthOperationPlanLegacyParameter `json:"safe_parameters"`
	EndpointCorrection *struct {
		SourcePath           string `json:"source_path"`
		RequestPath          string `json:"request_path"`
		UpstreamOperationSeq string `json:"upstream_operation_seq"`
	} `json:"endpoint_correction,omitempty"`
}

type healthOperationPlanLegacyParameter struct {
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	Minimum     *int   `json:"minimum,omitempty"`
	Maximum     *int   `json:"maximum,omitempty"`
	OffsetYears *int   `json:"offset_years,omitempty"`
	MinimumYear *int   `json:"minimum_year,omitempty"`
	MaximumYear *int   `json:"maximum_year,omitempty"`
}

type healthOperationPlanLoadResult struct {
	Options           healthOperationPlanOptions
	ArtifactRoot      string
	Plan              healthOperationPlanRecord
	Index             healthOperationPlanIndex
	IndexPath         string
	IndexSHA256       string
	IndexBytes        int64
	IndexArtifactPath string
	Shard             healthOperationPlanShardRef
	SourceScope       healthOperationPlanSourceScope
	RegistryTrust     registryTrustContext
	ManifestSHA256    string
	PolicySHA256      string
	ResponseAssertion healthNormalizedResponseAssertion
}

func healthOperationPlanInvocation(args []string) (healthOperationPlanOptions, bool, error) {
	options := healthOperationPlanOptions{}
	planSelectorSeen := false
	sourceSelectorSeen := false
	health := false
	jsonOut := false
	for index := 0; index < len(args); index++ {
		if args[index] == "--health-source-id" || strings.HasPrefix(args[index], "--health-source-id=") {
			if sourceSelectorSeen {
				return healthOperationPlanOptions{}, false, errors.New("--health-source-id may be provided only once")
			}
			sourceSelectorSeen = true
			value := ""
			if strings.HasPrefix(args[index], "--health-source-id=") {
				value = strings.TrimPrefix(args[index], "--health-source-id=")
			} else {
				if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
					return healthOperationPlanOptions{}, false, errors.New("--health-source-id requires a value")
				}
				index++
				value = args[index]
			}
			if !validHealthOperationPlanSourceID(value) {
				return healthOperationPlanOptions{}, false, errors.New("--health-source-id must be a canonical source ID")
			}
			options.SourceID = value
			continue
		}
		switch args[index] {
		case "--health":
			health = true
		case "--json":
			jsonOut = true
		case "--health-plan-index", "--health-operation-id", "--health-registry-revision", "--health-credential-bindings", "--health-attempt-id", "--health-cli-version", "--health-deadline":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return healthOperationPlanOptions{}, false, fmt.Errorf("%s requires a value", args[index])
			}
			value := strings.TrimSpace(args[index+1])
			switch args[index] {
			case "--health-plan-index":
				planSelectorSeen = true
				options.IndexPath = value
			case "--health-operation-id":
				planSelectorSeen = true
				options.OperationID = value
			case "--health-registry-revision":
				options.RegistryRevision = value
			case "--health-credential-bindings":
				options.CredentialBindingsPath = value
			case "--health-attempt-id":
				options.AttemptID = value
			case "--health-cli-version":
				options.CLIVersion = value
			case "--health-deadline":
				deadline, err := time.Parse(time.RFC3339Nano, value)
				if err != nil || deadline.Location() != time.UTC || deadline.UTC().Format(time.RFC3339Nano) != value {
					return healthOperationPlanOptions{}, false, errors.New("--health-deadline must be an absolute RFC3339Nano UTC timestamp")
				}
				options.Deadline = deadline
			}
			index++
		}
	}
	if !planSelectorSeen {
		if sourceSelectorSeen {
			return healthOperationPlanOptions{}, false, errors.New("--health-source-id requires --health-plan-index and --health-operation-id")
		}
		return healthOperationPlanOptions{}, false, nil
	}
	if options.IndexPath == "" || options.OperationID == "" || !validGitCommitRevision(options.RegistryRevision) || options.CredentialBindingsPath == "" || !validCanonicalUUID(options.AttemptID) || options.CLIVersion == "" || options.CLIVersion != version || options.Deadline.IsZero() || !options.Deadline.After(time.Now()) {
		return healthOperationPlanOptions{}, false, errors.New("plan mode requires the immutable selector, private credential-binding file, attempt ID, exact CLI version, and future UTC deadline")
	}
	if !health {
		return healthOperationPlanOptions{}, false, errors.New("--health plan execution requires --health")
	}
	if !jsonOut {
		return healthOperationPlanOptions{}, false, errors.New("--health plan execution requires --json")
	}
	if len(args) == 0 || (args[0] != "verify" && !(len(args) > 1 && args[0] == "catalog" && args[1] == "verify")) {
		return healthOperationPlanOptions{}, false, errors.New("--health plan execution is limited to verify --health")
	}
	if strings.ContainsAny(options.IndexPath, "\r\n\x00") || strings.ContainsAny(options.OperationID, "\r\n\x00") || strings.ContainsAny(options.CredentialBindingsPath, "\r\n\x00") {
		return healthOperationPlanOptions{}, false, errors.New("health operation plan selector is invalid")
	}
	return options, true, nil
}

func validHealthOperationPlanSourceID(value string) bool {
	if value == "" {
		return false
	}
	previousUnderscore := true
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			previousUnderscore = false
			continue
		}
		if character == '_' && !previousUnderscore {
			previousUnderscore = true
			continue
		}
		return false
	}
	return !previousUnderscore
}

func validCanonicalUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validGitCommitRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func loadManifestBoundHealthOperationPlan(options healthOperationPlanOptions, now time.Time) (healthOperationPlanLoadResult, error) {
	if options.IndexPath == "" || options.OperationID == "" || !validGitCommitRevision(options.RegistryRevision) {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan selector is invalid")
	}
	root, err := healthOperationPlanRoot(options.IndexPath)
	if err != nil {
		return healthOperationPlanLoadResult{}, err
	}
	indexData, err := readBoundedFile(options.IndexPath, healthOperationPlanIndexMaxBytes)
	if err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index is unavailable")
	}
	if err := validateHealthOperationPlanIndexJSON(indexData); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index contract is invalid")
	}
	var index healthOperationPlanIndex
	if err := json.Unmarshal(indexData, &index); err != nil || index.SchemaVersion != healthOperationPlanSchemaVersion || index.ArtifactKind != "index" || index.RegistryRevision != options.RegistryRevision {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index contract is invalid")
	}
	if err := validateHealthOperationPlanIndexBounds(index); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index contract is invalid")
	}

	provenance, manifest, manifestData, err := readTrustedHealthOperationPlanManifest()
	if err != nil {
		return healthOperationPlanLoadResult{}, errors.New("installed Registry manifest provenance is invalid")
	}
	manifestSum := sha256.Sum256(manifestData)
	manifestDigest := hex.EncodeToString(manifestSum[:])
	indexArtifact, indexOK := manifestArtifact(manifest, healthOperationPlanIndexPath)
	indexSum := sha256.Sum256(indexData)
	indexDigest := hex.EncodeToString(indexSum[:])
	if !indexOK || indexArtifact.Bytes != int64(len(indexData)) || !strings.EqualFold(indexArtifact.SHA256, indexDigest) {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index is not bound to the installed Registry release")
	}
	if err := verifyHealthOperationPlanSchemaBinding(root, manifest); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("installed Registry observation-plan schema binding is invalid")
	}
	if err := validateHealthOperationPlanArtifactReferences(indexData, manifest); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index references are not bound to the installed Registry release")
	}
	if err := validateHealthOperationPlanIndexManifest(index, manifest); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan index references are not bound to the installed Registry release")
	}

	shardRef, found, ambiguous := selectHealthOperationPlanShard(index, options.SourceID, options.OperationID)
	if !found || ambiguous {
		if ambiguous {
			return healthOperationPlanLoadResult{}, errors.New("health operation ID is ambiguous across Registry source scopes")
		}
		return healthOperationPlanLoadResult{}, errors.New("health operation ID is not present in the Registry plan index")
	}
	shardPath, ok := releaseArtifactPath(root, shardRef.Path)
	if !ok {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard path is invalid")
	}
	shardData, err := readBoundedFile(shardPath, healthOperationPlanShardMaxBytes)
	if err != nil || int64(len(shardData)) != shardRef.Bytes || !healthOperationPlanDigestMatches(shardRef.SHA256, shardData) {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard is unavailable or altered")
	}
	if err := validateHealthOperationPlanShardJSON(shardData); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard contract is invalid")
	}
	if err := validateHealthOperationPlanArtifactReferences(shardData, manifest); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard references are not bound to the installed Registry release")
	}
	var shard healthOperationPlanShard
	if err := json.Unmarshal(shardData, &shard); err != nil || shard.SchemaVersion != healthOperationPlanSchemaVersion || shard.ArtifactKind != "shard" || shard.SourceID != shardRef.SourceID || shard.ShardIndex != shardRef.ShardIndex || len(shard.Records) != shardRef.RecordCount {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard identity is invalid")
	}
	if options.SourceID != "" && shard.SourceID != options.SourceID {
		return healthOperationPlanLoadResult{}, errors.New("selected Registry plan shard does not match the requested source")
	}
	sourceScope, ok := healthOperationPlanSourceScopeByID(index.SourceScopes, shardRef.SourceID)
	if !ok {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan source scope is missing")
	}
	if options.SourceID != "" && sourceScope.SourceID != options.SourceID {
		return healthOperationPlanLoadResult{}, errors.New("selected Registry source scope does not match the requested source")
	}
	var selected healthOperationPlanRecord
	selectedCount := 0
	previousID := ""
	for _, raw := range shard.Records {
		var plan healthOperationPlanRecord
		if err := json.Unmarshal(raw, &plan); err != nil || plan.ArtifactKind != "operation_plan" || plan.SchemaVersion != healthOperationPlanSchemaVersion || plan.SourceBinding.SourceID != shard.SourceID {
			return healthOperationPlanLoadResult{}, errors.New("health operation plan record identity is invalid")
		}
		operationID := plan.OperationIdentity.OperationID
		if operationID == "" || (previousID != "" && operationID <= previousID) || plan.SourceBinding.Provider != sourceScope.Provider || plan.SourceBinding.AdapterID != sourceScope.AdapterID || plan.SourceBinding.InventoryStatus != sourceScope.InventoryStatus || plan.SourceBinding.InventoryUnknown != sourceScope.InventoryUnknown || plan.SourceBinding.TestOnly != sourceScope.TestOnly {
			return healthOperationPlanLoadResult{}, errors.New("health operation plan record ordering or source binding is invalid")
		}
		previousID = operationID
		if operationID == options.OperationID {
			selected, selectedCount = plan, selectedCount+1
		}
	}
	if len(shard.Records) == 0 || shard.Records[0] == nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard is empty")
	}
	var first, last healthOperationPlanRecord
	if err := json.Unmarshal(shard.Records[0], &first); err != nil || json.Unmarshal(shard.Records[len(shard.Records)-1], &last) != nil || first.OperationIdentity.OperationID != shardRef.FirstOperationID || last.OperationIdentity.OperationID != shardRef.LastOperationID {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan shard range does not match its index")
	}
	if selectedCount != 1 {
		if selectedCount > 1 {
			return healthOperationPlanLoadResult{}, errors.New("health operation ID is duplicated within its Registry source scope")
		}
		return healthOperationPlanLoadResult{}, errors.New("health operation ID is not present in the selected Registry plan shard")
	}
	if options.SourceID != "" && selected.SourceBinding.SourceID != options.SourceID {
		return healthOperationPlanLoadResult{}, errors.New("selected operation record does not match the requested source")
	}
	if err := validateHealthOperationPlanRecord(selected); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("health operation plan is not executable under its declared bounds")
	}
	documents, err := loadSelectedHealthOperationDocumentEvidence(root, selected, index, manifest)
	if err != nil {
		return healthOperationPlanLoadResult{}, errors.New("selected operation-document evidence is invalid")
	}
	selectedPolicy, err := loadSelectedHealthOperationPolicy(root, selected, manifest)
	if err != nil {
		return healthOperationPlanLoadResult{}, errors.New("selected reviewed operation policy is invalid")
	}
	if err := validateSelectedHealthOperationEffectPolicy(selected, selectedPolicy, documents); err != nil {
		return healthOperationPlanLoadResult{}, errors.New("selected read-only effect policy does not match its source facts")
	}
	responseAssertion, err := loadSelectedHealthResponseAssertion(root, selected, index, manifest, documents, selectedPolicy)
	if err != nil {
		return healthOperationPlanLoadResult{}, errors.New("selected response assertion is invalid")
	}
	policyDigest := strings.ToLower(index.GenerationInputs.LegacyPolicy.SHA256)
	if selected.LegacyPolicy != nil {
		policyDigest = strings.ToLower(selected.LegacyPolicy.PolicyRef.SHA256)
	}
	registrySum := strings.ToLower(provenance.RegistrySHA256)
	registryMatch := true
	trust := registryTrustContext{
		Status: "trusted", RegistrySource: "operation_observation_plan", RegistryPath: defaultRegistryPath,
		ProvenancePresent: true, ReleaseTag: provenance.ReleaseTag, RegistrySHA256: registrySum,
		Distribution: provenance.Distribution, DatasetID: defaultIfEmpty(provenance.DatasetID, datapanRegistryHFDatasetID),
		DatasetRevision: provenance.DatasetRevision, Integrity: "verified", ManifestBinding: "verified",
		RegistryDigestMatches: &registryMatch, ReleaseReadiness: "operation_observation_plan_bound",
		VerificationEvidence: "manifest_bound_operation_observation_plan", VerificationFreshness: "not_evaluated",
		ExecutionAllowed: true,
	}
	return healthOperationPlanLoadResult{
		Options: options, ArtifactRoot: root, Plan: selected, Index: index, IndexPath: options.IndexPath, IndexSHA256: indexDigest, IndexBytes: int64(len(indexData)),
		IndexArtifactPath: healthOperationPlanIndexPath, Shard: shardRef, SourceScope: sourceScope,
		RegistryTrust: trust, ManifestSHA256: manifestDigest, PolicySHA256: policyDigest,
		ResponseAssertion: responseAssertion,
	}, nil
}

func healthOperationPlanRoot(indexPath string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(indexPath))
	suffix := healthOperationPlanIndexPath
	if clean == suffix {
		return ".", nil
	}
	if !strings.HasSuffix(clean, "/"+suffix) {
		return "", errors.New("health operation plan index path must end in reports/operation-observation-plan/index.json")
	}
	root := strings.TrimSuffix(clean, "/"+suffix)
	if root == "" {
		root = "/"
	}
	return filepath.FromSlash(root), nil
}

func readTrustedHealthOperationPlanManifest() (registryInstallProvenance, releaseManifest, []byte, error) {
	provenance, err := readBoundedRegistryInstallProvenance(defaultRegistryInstallProvenancePath)
	if err != nil || provenance.ManifestRegistryVerified == nil || !*provenance.ManifestRegistryVerified || provenance.ReleaseManifestSHA256 == "" || !validSHA256(provenance.RegistrySHA256) {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry provenance is invalid")
	}
	manifestData, err := readBoundedFile(defaultReleaseManifestPath, healthOperationPlanManifestMaxBytes)
	if err != nil {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry release manifest is unavailable")
	}
	if err := preflightHealthOperationPlanManifestJSON(manifestData); err != nil {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry release manifest exceeds operation-plan bounds")
	}
	manifestSum := sha256.Sum256(manifestData)
	if !strings.EqualFold(provenance.ReleaseManifestSHA256, hex.EncodeToString(manifestSum[:])) {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry release manifest digest mismatch")
	}
	var manifest releaseManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.SchemaVersion != "datapan.release-manifest.v1" {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry release manifest is invalid")
	}
	registryArtifact, ok := manifestArtifact(manifest, healthOperationPlanSourceRegistryPath)
	if !ok || manifest.SourceRegistry != healthOperationPlanSourceRegistryPath || !strings.EqualFold(registryArtifact.SHA256, provenance.RegistrySHA256) {
		return registryInstallProvenance{}, releaseManifest{}, nil, errors.New("installed Registry artifact binding mismatch")
	}
	return provenance, manifest, manifestData, nil
}

func verifyHealthOperationPlanSchemaBinding(root string, manifest releaseManifest) error {
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
		if err := verifyHealthPinnedSchemaArtifact(root, manifest, schema.path, schema.digest, schema.data); err != nil {
			return err
		}
	}
	return nil
}

func verifyHealthPinnedSchemaArtifact(root string, manifest releaseManifest, schemaPath, digest string, embedded []byte) error {
	if !healthOperationPlanDigestMatches(digest, embedded) {
		return errors.New("embedded Registry schema does not match its source pin")
	}
	artifact, ok := manifestArtifact(manifest, schemaPath)
	if !ok || artifact.Bytes != int64(len(embedded)) || !strings.EqualFold(artifact.SHA256, digest) {
		return errors.New("Registry manifest does not bind a pinned operation schema")
	}
	path, ok := releaseArtifactPath(root, schemaPath)
	if !ok {
		return errors.New("Registry operation schema path is invalid")
	}
	data, err := readBoundedFile(path, 1<<20)
	if err != nil || int64(len(data)) != artifact.Bytes || !healthOperationPlanDigestMatches(artifact.SHA256, data) || !bytes.Equal(data, embedded) {
		return errors.New("Registry operation schema differs from the pinned CLI contract")
	}
	return nil
}

func validateHealthOperationPlanJSON(data []byte) error {
	if err := preflightHealthOperationPlanJSON(data); err != nil {
		return err
	}
	return validateHealthOperationPlanSchemaJSON(data)
}

func validateHealthOperationPlanShardJSON(data []byte) error {
	if err := preflightHealthOperationPlanShardJSON(data); err != nil {
		return err
	}
	return validateHealthOperationPlanSchemaJSON(data)
}

func validateHealthOperationPlanIndexJSON(data []byte) error {
	if err := preflightHealthOperationPlanIndexJSON(data); err != nil {
		return err
	}
	return validateHealthOperationPlanSchemaJSON(data)
}

func validateHealthOperationPlanSchemaJSON(data []byte) error {
	compiler, err := healthOperationPlanJSONSchema()
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return compiler.Validate(instance)
}

func healthOperationPlanJSONSchema() (*jsonschema.Schema, error) {
	healthOperationPlanSchemaOnce.Do(func() {
		if !healthOperationPlanDigestMatches(healthOperationPlanSchemaSHA256, embeddedHealthOperationPlanSchema) {
			healthOperationPlanSchemaErr = errors.New("embedded schema digest mismatch")
			return
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(embeddedHealthOperationPlanSchema))
		if err != nil {
			healthOperationPlanSchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(healthOperationPlanSchemaID, document); err != nil {
			healthOperationPlanSchemaErr = err
			return
		}
		healthOperationPlanSchema, healthOperationPlanSchemaErr = compiler.Compile(healthOperationPlanSchemaID)
	})
	return healthOperationPlanSchema, healthOperationPlanSchemaErr
}

func healthOperationResponseAssertionJSONSchema() (*jsonschema.Schema, error) {
	healthOperationResponseAssertionSchemaOnce.Do(func() {
		healthOperationResponseAssertionSchema, healthOperationResponseAssertionSchemaErr = compileHealthPinnedJSONSchema(
			healthOperationResponseAssertionSchemaID,
			healthOperationResponseAssertionSchemaSHA256,
			embeddedHealthOperationResponseAssertionSchema,
		)
	})
	return healthOperationResponseAssertionSchema, healthOperationResponseAssertionSchemaErr
}

func healthOperationPolicyJSONSchema() (*jsonschema.Schema, error) {
	healthOperationPolicySchemaOnce.Do(func() {
		healthOperationPolicySchema, healthOperationPolicySchemaErr = compileHealthPinnedJSONSchema(
			healthOperationPolicySchemaID,
			healthOperationPolicySchemaSHA256,
			embeddedHealthOperationPolicySchema,
		)
	})
	return healthOperationPolicySchema, healthOperationPolicySchemaErr
}

func compileHealthPinnedJSONSchema(schemaID, digest string, data []byte) (*jsonschema.Schema, error) {
	if !healthOperationPlanDigestMatches(digest, data) {
		return nil, errors.New("embedded schema digest mismatch")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := normalizeHealthJSONSchemaRegexps(document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaID, document); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaID)
}

// The pinned Registry JSON Schemas are hashed and compared as their original
// bytes. This narrowly rewrites one ECMAScript negative-lookahead path regex
// into its equivalent RE2 form because Go's regexp engine rejects lookahead.
// It excludes a leading slash and permits the same remaining path characters.
func normalizeHealthJSONSchemaRegexps(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "pattern" {
				pattern, ok := child.(string)
				if !ok {
					return errors.New("JSON schema pattern is not a string")
				}
				if pattern == "^(?!/)[A-Za-z0-9._/-]+$" {
					typed[key] = "^[A-Za-z0-9._-][A-Za-z0-9._/-]*$"
				}
				continue
			}
			if err := normalizeHealthJSONSchemaRegexps(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := normalizeHealthJSONSchemaRegexps(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func preflightHealthOperationPlanJSON(data []byte) error {
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxJSONTokens, 0, true)
}

func preflightHealthOperationPlanShardJSON(data []byte) error {
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxShardJSONTokens, 0, true)
}

func preflightHealthOperationPlanIndexJSON(data []byte) error {
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxIndexJSONTokens, healthOperationPlanMaxIndexArtifactRefs, true)
}

func preflightHealthOperationPlanManifestJSON(data []byte) error {
	if err := validateHealthReleaseManifestSize(data); err != nil {
		return err
	}
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxManifestJSONTokens, healthOperationPlanMaxIndexArtifactRefs, true)
}

func preflightHealthReleaseManifestJSON(data []byte) error {
	if err := validateHealthReleaseManifestSize(data); err != nil {
		return err
	}
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxManifestJSONTokens, 0, true)
}

func validateHealthReleaseManifestSize(data []byte) error {
	if len(data) == 0 || int64(len(data)) > healthOperationPlanManifestMaxBytes {
		return errors.New("release manifest byte ceiling exceeded")
	}
	return nil
}

// Provider response objects are decoded into maps, so case-distinct member
// names are distinct values and must remain usable (unlike plan structs,
// where encoding/json's case-insensitive field matching makes them ambiguous).
// Exact duplicate decoded names, including escaped aliases, are still rejected.
func preflightHealthResponseJSON(data []byte) error {
	return preflightHealthJSONWithLimits(data, healthOperationPlanMaxJSONTokens, 0, false)
}

func preflightHealthJSONWithLimits(data []byte, maxTokens, maxArtifactRefs int, rejectCaseFoldDuplicates bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	state := healthOperationPlanJSONScan{
		maxTokens:                maxTokens,
		maxArtifactRefs:          maxArtifactRefs,
		maxDepth:                 healthOperationPlanMaxJSONDepth,
		rejectCaseFoldDuplicates: rejectCaseFoldDuplicates,
	}
	if err := state.value(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

type healthOperationPlanJSONScan struct {
	tokens                   int
	maxTokens                int
	artifactRefs             int
	maxArtifactRefs          int
	maxDepth                 int
	rejectCaseFoldDuplicates bool
}

func (s *healthOperationPlanJSONScan) token(decoder *json.Decoder) (json.Token, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	s.tokens++
	if s.tokens > s.maxTokens {
		return nil, errors.New("JSON token budget exceeded")
	}
	return token, nil
}

func (s *healthOperationPlanJSONScan) value(decoder *json.Decoder, depth int) error {
	if depth > s.maxDepth {
		return errors.New("JSON nesting budget exceeded")
	}
	token, err := s.token(decoder)
	if err != nil {
		return err
	}
	delim, isContainer := token.(json.Delim)
	if !isContainer {
		return nil
	}
	if delim == '{' {
		keys := make(map[string]struct{})
		hasPath, hasSHA256, hasBytes := false, false, false
		for decoder.More() {
			keyToken, err := s.token(decoder)
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is invalid")
			}
			duplicateKey := key
			if s.rejectCaseFoldDuplicates {
				duplicateKey = strings.ToLower(key)
			}
			if _, duplicate := keys[duplicateKey]; duplicate {
				return errors.New("duplicate JSON object key")
			}
			keys[duplicateKey] = struct{}{}
			switch strings.ToLower(key) {
			case "path":
				hasPath = true
			case "sha256":
				hasSHA256 = true
			case "bytes":
				hasBytes = true
			}
			if err := s.value(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := s.token(decoder)
		if err != nil || closing != json.Delim('}') {
			return errors.New("JSON object is incomplete")
		}
		if hasPath && hasSHA256 && hasBytes && s.maxArtifactRefs > 0 {
			s.artifactRefs++
			if s.artifactRefs > s.maxArtifactRefs {
				return errors.New("JSON artifact reference budget exceeded")
			}
		}
		return nil
	}
	if delim == '[' {
		for decoder.More() {
			if err := s.value(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := s.token(decoder)
		if err != nil || closing != json.Delim(']') {
			return errors.New("JSON array is incomplete")
		}
		return nil
	}
	return errors.New("JSON container is invalid")
}

func validateHealthOperationPlanIndexBounds(index healthOperationPlanIndex) error {
	if len(index.Shards) == 0 || len(index.Shards) > healthOperationPlanMaxIndexShards || len(index.SourceScopes) == 0 || len(index.SourceScopes) > healthOperationPlanMaxSourceScopes {
		return errors.New("index resource ceiling exceeded")
	}
	artifactRefs := 3 + len(index.GenerationInputs.OperationDenominators) + len(index.GenerationInputs.DocumentEvidence) + len(index.Shards)
	sourceCounts := make(map[string]int, len(index.SourceScopes))
	unknownScopes := 0
	for _, source := range index.SourceScopes {
		if source.SourceID == "" || strings.TrimSpace(source.Provider) == "" || strings.TrimSpace(source.AdapterID) == "" || source.RegisteredOperations < 0 || !validSHA256(source.IdentitySetSHA256) || len(source.SourceArtifacts) == 0 {
			return errors.New("source scope is incomplete")
		}
		artifactRefs += len(source.SourceArtifacts)
		if _, duplicate := sourceCounts[source.SourceID]; duplicate {
			return errors.New("source scope is duplicated")
		}
		sourceCounts[source.SourceID] = 0
		if source.InventoryUnknown {
			unknownScopes++
		}
	}
	if artifactRefs > healthOperationPlanMaxIndexArtifactRefs {
		return errors.New("index artifact reference ceiling exceeded")
	}
	shardIndices := make(map[string]map[int]healthOperationPlanShardRef, len(index.SourceScopes))
	for _, shard := range index.Shards {
		if shard.SourceID == "" || shard.ShardIndex < 0 || shard.Bytes < 1 || shard.Bytes > healthOperationPlanShardMaxBytes || shard.RecordCount < 1 || shard.RecordCount > 256 || shard.FirstOperationID == "" || shard.LastOperationID == "" || shard.FirstOperationID > shard.LastOperationID || !validSHA256(shard.SHA256) {
			return errors.New("shard reference is invalid")
		}
		indices, exists := shardIndices[shard.SourceID]
		if !exists {
			indices = map[int]healthOperationPlanShardRef{}
			shardIndices[shard.SourceID] = indices
		}
		if _, duplicate := indices[shard.ShardIndex]; duplicate {
			return errors.New("shard index is duplicated")
		}
		indices[shard.ShardIndex] = shard
		count, known := sourceCounts[shard.SourceID]
		if !known {
			return errors.New("shard has unknown source scope")
		}
		sourceCounts[shard.SourceID] = count + shard.RecordCount
	}
	total := 0
	for sourceID, count := range sourceCounts {
		source, _ := healthOperationPlanSourceScopeByID(index.SourceScopes, sourceID)
		if count != source.RegisteredOperations {
			return errors.New("source operation count does not match its shards")
		}
		total += count
		indices := shardIndices[sourceID]
		ordered := make([]healthOperationPlanShardRef, 0, len(indices))
		for shardIndex, shard := range indices {
			if shardIndex != len(ordered) {
				// The map is unordered; sort below and validate contiguous ordinals after.
			}
			ordered = append(ordered, shard)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ShardIndex < ordered[j].ShardIndex })
		for position, shard := range ordered {
			if shard.ShardIndex != position || (position > 0 && ordered[position-1].LastOperationID >= shard.FirstOperationID) {
				return errors.New("shard ranges overlap or indices are not contiguous")
			}
		}
	}
	if total != index.Summary.KnownOperations || index.Summary.KnownOperations < 0 || index.Summary.RequestPlansComplete < 0 || index.Summary.RequestPlansIncomplete < 0 || index.Summary.RequestPlansComplete+index.Summary.RequestPlansIncomplete != index.Summary.KnownOperations || index.Summary.RuntimeBindingsBound < 0 || index.Summary.RuntimeBindingsUnbound < 0 || index.Summary.RuntimeBindingsBound+index.Summary.RuntimeBindingsUnbound != index.Summary.KnownOperations || index.Summary.Admitted < 0 || index.Summary.NotAdmitted < 0 || index.Summary.Admitted+index.Summary.NotAdmitted != index.Summary.KnownOperations || index.Summary.InventoryUnknownScopes != unknownScopes {
		return errors.New("index summary does not reconcile with shard and source scopes")
	}
	if len(index.GenerationInputs.OperationDenominators) < 4 || !validSHA256(index.GenerationInputs.GeneratorSHA256) || index.GenerationInputs.GeneratorPath == "" {
		return errors.New("index generation inputs are incomplete")
	}
	return nil
}

func validateHealthOperationPlanIndexManifest(index healthOperationPlanIndex, manifest releaseManifest) error {
	generator, ok := manifestArtifact(manifest, index.GenerationInputs.GeneratorPath)
	if !ok || !strings.EqualFold(generator.SHA256, index.GenerationInputs.GeneratorSHA256) {
		return errors.New("generator digest is not manifest-bound")
	}
	return nil
}

func validateHealthOperationPlanArtifactReferences(data []byte, manifest releaseManifest) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return validateHealthOperationPlanReferencesInValue(instance, manifest)
}

func validateHealthOperationPlanReferencesInValue(value any, manifest releaseManifest) error {
	switch current := value.(type) {
	case map[string]any:
		if path, pathOK := current["path"].(string); pathOK {
			if digest, digestOK := current["sha256"].(string); digestOK {
				if byteCount, bytesOK := current["bytes"].(json.Number); bytesOK {
					bytes, parseErr := byteCount.Int64()
					if parseErr != nil || bytes < 1 || validateHealthOperationPlanArtifactRef(healthOperationPlanArtifactRef{Path: path, SHA256: digest, Bytes: bytes}, manifest) != nil {
						return errors.New("artifact reference is not bound")
					}
				}
			}
		}
		if path, pathOK := current["artifact_path"].(string); pathOK {
			if digest, digestOK := current["sha256"].(string); digestOK {
				if validateHealthOperationPlanEvidenceRef(healthOperationPlanEvidenceRef{ArtifactPath: path, SHA256: digest}, manifest) != nil {
					return errors.New("evidence reference is not bound")
				}
			}
		}
		for _, child := range current {
			if err := validateHealthOperationPlanReferencesInValue(child, manifest); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := validateHealthOperationPlanReferencesInValue(child, manifest); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateHealthOperationPlanArtifactRef(reference healthOperationPlanArtifactRef, manifest releaseManifest) error {
	if _, ok := releaseArtifactPath(".", reference.Path); !ok || !validSHA256(reference.SHA256) || reference.Bytes < 1 {
		return errors.New("artifact reference is invalid")
	}
	artifact, ok := manifestArtifact(manifest, reference.Path)
	if !ok || artifact.Bytes != reference.Bytes || !strings.EqualFold(artifact.SHA256, reference.SHA256) {
		return errors.New("artifact reference does not match release manifest")
	}
	return nil
}

func validateHealthOperationPlanEvidenceRef(reference healthOperationPlanEvidenceRef, manifest releaseManifest) error {
	if _, ok := releaseArtifactPath(".", reference.ArtifactPath); !ok || !validSHA256(reference.SHA256) {
		return errors.New("evidence reference is invalid")
	}
	artifact, ok := manifestArtifact(manifest, reference.ArtifactPath)
	if !ok || !strings.EqualFold(artifact.SHA256, reference.SHA256) {
		return errors.New("evidence reference does not match release manifest")
	}
	return nil
}

func selectHealthOperationPlanShard(index healthOperationPlanIndex, sourceID, operationID string) (healthOperationPlanShardRef, bool, bool) {
	var selected healthOperationPlanShardRef
	count := 0
	for _, shard := range index.Shards {
		if sourceID != "" && shard.SourceID != sourceID {
			continue
		}
		if operationID >= shard.FirstOperationID && operationID <= shard.LastOperationID {
			selected = shard
			count++
		}
	}
	return selected, count == 1, count > 1
}

func healthOperationPlanSourceScopeByID(scopes []healthOperationPlanSourceScope, sourceID string) (healthOperationPlanSourceScope, bool) {
	for _, scope := range scopes {
		if scope.SourceID == sourceID {
			return scope, true
		}
	}
	return healthOperationPlanSourceScope{}, false
}

func validateHealthOperationPlanRecord(plan healthOperationPlanRecord) error {
	if plan.RequestPlan.Status != "complete" || plan.RequestPlan.RequestContract == nil || plan.RuntimeBinding.Status != "bound" || plan.Admission.Status != "admitted" || plan.SourceBinding.TestOnly {
		return errors.New("plan is incomplete, unbound, or not admitted")
	}
	contract := plan.RequestPlan.RequestContract
	if contract.Transport.Authority == "synthetic_fixture" || contract.OperationEffect.Authority == "synthetic_fixture" {
		return errors.New("synthetic fixture authority cannot admit a production operation plan")
	}
	if plan.OperationIdentity.OperationID == "" || plan.SourceBinding.SourceID == "" || plan.SourceBinding.Provider == "" || plan.SourceBinding.AdapterID == "" || contract.OperationEffect.Classification != "read_only" || contract.Limits.RequestBudget != 1 || contract.Limits.TimeoutMS < int(healthOperationPlanMinTimeout/time.Millisecond) || contract.Limits.TimeoutMS > int(healthOperationPlanMaxTimeout/time.Millisecond) || contract.Limits.MaxRequestBytes < 1 || contract.Limits.MaxResponseBytes < 1 || contract.Limits.MaxRequestBytes > healthTransportMaxBytes || contract.Limits.MaxResponseBytes > healthTransportMaxBytes || len(contract.Parameters) > healthOperationPlanMaxRequestParameters || len(plan.RuntimeBinding.QuotaPolicies) == 0 || len(plan.RuntimeBinding.QuotaPolicies) > healthOperationPlanMaxQuotaPolicies || plan.RuntimeBinding.ObservationPeriodSeconds < 1 {
		return errors.New("plan request and runtime bounds are unsupported")
	}
	if plan.OperationIdentity.Protocol != contract.Transport.Protocol || (contract.Transport.Protocol != "REST" && contract.Transport.Protocol != "SOAP") {
		return errors.New("plan transport is unsupported")
	}
	if contract.ResponseAssertion.EmptyResultSemantics != "valid" && contract.ResponseAssertion.EmptyResultSemantics != "invalid" && contract.ResponseAssertion.EmptyResultSemantics != "not_applicable" {
		return errors.New("plan empty-result semantics are unsupported")
	}
	switch contract.ResponseAssertion.Kind {
	case "json_contract", "xml_contract", "soap_fault_free":
		if len(contract.ResponseAssertion.ExpectedStatusCodes) == 0 || len(contract.ResponseAssertion.ExpectedStatusCodes) > 256 {
			return errors.New("plan typed response assertion is incomplete")
		}
		statuses := make(map[int]struct{}, len(contract.ResponseAssertion.ExpectedStatusCodes))
		for _, status := range contract.ResponseAssertion.ExpectedStatusCodes {
			if status < 100 || status > 599 {
				return errors.New("plan typed response assertion contains an invalid status")
			}
			if _, duplicate := statuses[status]; duplicate {
				return errors.New("plan typed response assertion contains duplicate statuses")
			}
			statuses[status] = struct{}{}
		}
		expectedAssertionRef := healthOperationResponseAssertionArtifactPathPrefix + plan.OperationIdentity.OperationID + ".json#/assertion"
		if contract.ResponseAssertion.AssertionRef != expectedAssertionRef {
			return errors.New("plan typed response assertion reference is not operation-bound")
		}
		artifactBindings := 0
		for _, ref := range contract.ResponseAssertion.EvidenceRefs {
			if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath+ref.JSONPointer == expectedAssertionRef {
				artifactBindings++
			}
		}
		if artifactBindings != 1 {
			return errors.New("plan typed response assertion does not have one reviewed artifact binding")
		}
		if contract.Transport.Protocol == "SOAP" && contract.ResponseAssertion.Kind != "soap_fault_free" || contract.Transport.Protocol == "REST" && contract.ResponseAssertion.Kind == "soap_fault_free" {
			return errors.New("plan response assertion kind is incompatible with its transport")
		}
	case "observation_only":
		if len(contract.ResponseAssertion.ExpectedStatusCodes) != 0 || contract.ResponseAssertion.EmptyResultSemantics != "not_applicable" {
			return errors.New("observation-only response contract contains semantic predicates")
		}
		expectedAssertionRef := healthOperationResponseAssertionArtifactPathPrefix + plan.OperationIdentity.OperationID + ".json#/assertion"
		if contract.ResponseAssertion.AssertionRef != expectedAssertionRef {
			return errors.New("observation-only response assertion reference is not operation-bound")
		}
		artifactBindings := 0
		for _, ref := range contract.ResponseAssertion.EvidenceRefs {
			if ref.EvidenceKind == "reviewed_policy" && ref.ArtifactPath+ref.JSONPointer == expectedAssertionRef && validSHA256Digest(ref.SHA256) {
				artifactBindings++
			}
		}
		if artifactBindings != 1 {
			return errors.New("observation-only response assertion does not have one reviewed artifact binding")
		}
	default:
		return errors.New("plan response assertion is unsupported")
	}
	if (contract.Transport.Protocol == "REST" && contract.Transport.HTTPMethod != "GET" && contract.Transport.HTTPMethod != "HEAD") || (contract.Transport.Protocol == "SOAP" && (contract.Transport.HTTPMethod != "POST" || contract.Transport.BodyEncoding != "document_literal" || !healthOperationPlanSOAPEnvelopeMatches(contract.Transport.SOAPVersion, contract.Transport.EnvelopeNamespace))) {
		return errors.New("plan request method or SOAP encoding is unsupported")
	}
	switch contract.OperationEffect.Authority {
	case "operation_document", "operation_specific_declaration":
		// The selected operation-document evidence validator checks source facts.
	case "reviewed_policy":
		if contract.Transport.Protocol != "REST" || (contract.Transport.HTTPMethod != "GET" && contract.Transport.HTTPMethod != "HEAD") || !healthOperationPlanEffectEvidenceBound(plan) {
			return errors.New("reviewed read-only operation effect lacks an exact GET or HEAD and source evidence")
		}
	default:
		return errors.New("plan read-only operation effect authority is unsupported")
	}
	if !operationPlanEndpointMatches(plan.OperationIdentity.RegisteredEndpoint, contract.Transport.Host, contract.Transport.Path, contract.Transport.Scheme, contract.Transport.Port) {
		return errors.New("plan endpoint identity does not match request contract")
	}
	if err := validateHealthOperationPlanQuotas(plan.RuntimeBinding); err != nil {
		return err
	}
	credentialParameterCount := 0
	for _, parameter := range contract.Parameters {
		if parameter.ValueStrategy.Authority == "synthetic_fixture" {
			return errors.New("synthetic fixture value authority cannot admit a production operation plan")
		}
		if strings.TrimSpace(parameter.Name) == "" || (parameter.Cardinality != "required_single" && parameter.Cardinality != "optional_single") || !healthOperationPlanSupportsValueStrategy(parameter.ValueStrategy.Kind) || parameter.ValueStrategy.Kind == "credential_reference" && parameter.ValueStrategy.BindingField != "credential_reference" {
			return errors.New("plan contains an unsupported parameter capability")
		}
		if parameter.ValueStrategy.Kind == "credential_reference" {
			credentialParameterCount++
		}
		if !healthOperationPlanParameterLocationSupported(contract.Transport.Protocol, parameter.Location) || (parameter.Location == "soap_header" && parameter.QualifiedName == nil) {
			return errors.New("plan parameter location is unsupported")
		}
	}
	if contract.Authentication.Requirement == "none" {
		if contract.Authentication.Mechanism != "none" || contract.Authentication.Placement != "none" || contract.Authentication.CredentialReferenceRequired || credentialParameterCount != 0 || plan.RuntimeBinding.CredentialReference != "" || plan.RuntimeBinding.CredentialScopeKey != "" {
			return errors.New("unauthenticated plan contains a credential binding")
		}
	} else if contract.Authentication.Requirement == "required" {
		if contract.Transport.Scheme != "https" || (contract.Authentication.Mechanism != "service_key" && contract.Authentication.Mechanism != "api_key") || !contract.Authentication.CredentialReferenceRequired || strings.TrimSpace(plan.RuntimeBinding.CredentialReference) == "" || strings.TrimSpace(plan.RuntimeBinding.CredentialScopeKey) == "" || credentialParameterCount != 1 || contract.Authentication.Cardinality != "required_single" {
			return errors.New("authenticated plan credential binding is incomplete or unsupported")
		}
		matched := 0
		for _, parameter := range contract.Parameters {
			if parameter.ValueStrategy.Kind == "credential_reference" && healthOperationPlanAuthParameterMatches(contract.Authentication, parameter) {
				matched++
			}
		}
		if matched != 1 {
			return errors.New("credential reference is not bound to exactly one declared authentication parameter")
		}
	} else {
		return errors.New("plan authentication requirement is unsupported")
	}
	return nil
}

func healthOperationPlanEffectEvidenceBound(plan healthOperationPlanRecord) bool {
	contract := plan.RequestPlan.RequestContract
	if contract == nil || contract.OperationEffect.Authority != "reviewed_policy" || contract.OperationEffect.Classification != "read_only" {
		return false
	}
	policyRefs := 0
	documentPointers := make(map[string]bool, 3)
	for _, ref := range contract.OperationEffect.EvidenceRefs {
		switch ref.EvidenceKind {
		case "reviewed_policy":
			if _, suffix, ok := parseHealthOperationPolicyPointer(ref.JSONPointer); ok && suffix != "#" && strings.HasSuffix(suffix, "/effect_review") {
				policyRefs++
			}
		case "operation_document":
			if ref.JSONPointer == "#/transport/http_method" || ref.JSONPointer == "#/operation_document/title" || ref.JSONPointer == "#/operation_document/purpose" {
				documentPointers[ref.JSONPointer] = true
			}
		}
	}
	return policyRefs == 1 && documentPointers["#/transport/http_method"] && documentPointers["#/operation_document/title"] && documentPointers["#/operation_document/purpose"]
}

const healthOperationPlanMaxQuotaPolicies = 32

func healthOperationPlanSOAPEnvelopeMatches(version, namespace string) bool {
	switch version {
	case "1.1":
		return namespace == "http://schemas.xmlsoap.org/soap/envelope/"
	case "1.2":
		return namespace == "http://www.w3.org/2003/05/soap-envelope"
	default:
		return false
	}
}

func healthOperationPlanParameterLocationSupported(protocol, location string) bool {
	if protocol == "REST" {
		return location == "query" || location == "header"
	}
	return protocol == "SOAP" && (location == "query" || location == "body" || location == "soap_header")
}

func healthOperationPlanAuthParameterMatches(authentication struct {
	Requirement                 string                           `json:"requirement"`
	Mechanism                   string                           `json:"mechanism"`
	Placement                   string                           `json:"placement"`
	ParameterName               string                           `json:"parameter_name,omitempty"`
	HeaderName                  string                           `json:"header_name,omitempty"`
	HeaderQName                 *healthOperationPlanQName        `json:"header_qname,omitempty"`
	Cardinality                 string                           `json:"cardinality,omitempty"`
	CredentialReferenceRequired bool                             `json:"credential_reference_required"`
	EvidenceRefs                []healthOperationPlanEvidenceRef `json:"evidence_refs"`
}, parameter healthOperationPlanParameter) bool {
	if parameter.Cardinality != authentication.Cardinality {
		return false
	}
	switch authentication.Placement {
	case "query":
		return parameter.Location == "query" && parameter.Name == authentication.ParameterName
	case "header":
		return parameter.Location == "header" && parameter.Name == authentication.HeaderName
	case "soap_header":
		return parameter.Location == "soap_header" && parameter.QualifiedName != nil && authentication.HeaderQName != nil && *parameter.QualifiedName == *authentication.HeaderQName
	default:
		return false
	}
}

func validateHealthOperationPlanQuotas(binding healthOperationPlanRuntimeBinding) error {
	seen := make(map[string]struct{}, len(binding.QuotaPolicies))
	credentialScopes := 0
	for _, quota := range binding.QuotaPolicies {
		if quota.ScopeKind == "" || quota.ScopeKey == "" || quota.MaxConcurrent < 1 || quota.RequestsPerWindow < 1 || quota.WindowSeconds < 1 || quota.MinimumIntervalSeconds < 0 || !validSHA256(quota.ScopeSHA256) {
			return errors.New("plan quota policy is incomplete")
		}
		key := quota.ScopeKind + "\x00" + quota.ScopeKey
		if _, duplicate := seen[key]; duplicate {
			return errors.New("plan quota policy is duplicated")
		}
		seen[key] = struct{}{}
		if !healthOperationPlanDigestMatches(quota.ScopeSHA256, []byte("datapan.quota-scope.v1\x00"+quota.ScopeKind+"\x00"+quota.ScopeKey)) {
			return errors.New("plan quota scope digest is invalid")
		}
		if quota.ScopeKind == "credential" && quota.ScopeKey == binding.CredentialScopeKey {
			credentialScopes++
		}
	}
	if binding.CredentialReference != "" && credentialScopes != 1 {
		return errors.New("credential scope does not match exactly one quota policy")
	}
	return nil
}

func operationPlanEndpointMatches(identity *healthOperationPlanRegisteredEndpoint, host, path, scheme string, port *int) bool {
	if identity == nil || identity.Host == "" || identity.Path == "" || identity.Host != host || identity.Path != path || (scheme != "https" && scheme != "http") {
		return false
	}
	if (identity.Port == nil) != (port == nil) || identity.Port != nil && *identity.Port != *port {
		return false
	}
	registeredAuthority, ok := healthOperationPlanAuthority(identity.Host, identity.Port)
	if !ok {
		return false
	}
	requestAuthority, ok := healthOperationPlanAuthority(host, port)
	if !ok || requestAuthority != registeredAuthority {
		return false
	}
	u, err := url.Parse(scheme + "://" + requestAuthority + path)
	return err == nil && u.Host == requestAuthority && u.EscapedPath() == path && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func healthOperationPlanAuthority(host string, port *int) (string, bool) {
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "\r\n\x00") {
		return "", false
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Host != host || parsed.Hostname() == "" || parsed.Port() != "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if port == nil {
		return host, true
	}
	if *port < 1 || *port > 65535 {
		return "", false
	}
	return net.JoinHostPort(parsed.Hostname(), strconv.Itoa(*port)), true
}

func healthOperationPlanSupportsValueStrategy(kind string) bool {
	switch kind {
	case "bounded_integer", "relative_year", "reviewed_enum", "reviewed_literal", "credential_reference", "opaque_reviewed_value":
		return true
	default:
		return false
	}
}

func healthOperationPlanDigestMatches(expected string, data []byte) bool {
	if !validSHA256(expected) {
		return false
	}
	sum := sha256.Sum256(data)
	return strings.EqualFold(expected, hex.EncodeToString(sum[:]))
}

func healthOperationPlanPathExists(root, artifactPath string) bool {
	path, ok := releaseArtifactPath(root, artifactPath)
	if !ok {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
