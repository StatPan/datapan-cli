package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	providers "github.com/StatPan/datapan-cli/internal/provider"
)

const (
	healthCredentialBindingsSchemaVersion = "datapan.health-credential-bindings.v1"
	healthCredentialBindingsMaxBytes      = 256 << 10
	healthCredentialBindingsMaxEntries    = 1024
	healthCredentialBindingMaxTokens      = 20_000
)

var (
	errHealthCredentialBindingsUnavailable = errors.New("health credential bindings unavailable")
	errHealthCredentialBindingMismatch     = errors.New("health credential binding mismatch")
	errHealthCredentialSourceUnavailable   = errors.New("health credential source unavailable")
)

type healthCredentialBindingsConfig struct {
	SchemaVersion string                    `json:"schema_version"`
	Bindings      []healthCredentialBinding `json:"bindings"`
}

type healthCredentialBinding struct {
	CredentialReference string `json:"credential_reference"`
	CredentialScopeKey  string `json:"credential_scope_key"`
	Provider            string `json:"provider"`
	AdapterID           string `json:"adapter_id"`
	AuthKind            string `json:"auth_kind"`
	CredentialGroupID   string `json:"credential_group_id"`
	CredentialEnvName   string `json:"credential_env_name"`
}

var credentialEnvNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
var credentialScopeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
var healthAdapterIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var credentialGroupIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

func (a app) resolveHealthPlanCredential(plan healthOperationPlanRecord, path string) (providers.Credential, error) {
	config, err := readHealthCredentialBindings(path)
	if err != nil {
		return providers.Credential{}, errHealthCredentialBindingsUnavailable
	}
	return resolveHealthPlanCredential(plan, config, credentialGroupByID, func(name string) (string, bool) {
		return a.env.LookupEnv(name)
	})
}

func readHealthCredentialBindings(path string) (healthCredentialBindingsConfig, error) {
	if path == "" || len(path) > 4096 || strings.ContainsAny(path, "\r\n\x00") {
		return healthCredentialBindingsConfig{}, errors.New("credential binding file path is invalid")
	}
	data, err := readPrivateBoundedFile(path, healthCredentialBindingsMaxBytes)
	if err != nil || preflightHealthCredentialBindingsJSON(data) != nil || validateHealthCredentialBindingFieldNames(data) != nil {
		return healthCredentialBindingsConfig{}, errors.New("credential binding file is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config healthCredentialBindingsConfig
	if err := decoder.Decode(&config); err != nil {
		return healthCredentialBindingsConfig{}, errors.New("credential binding file is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return healthCredentialBindingsConfig{}, errors.New("credential binding file has trailing data")
	}
	if err := validateHealthCredentialBindings(config); err != nil {
		return healthCredentialBindingsConfig{}, errors.New("credential binding file is invalid")
	}
	return config, nil
}

func validateHealthCredentialBindingFieldNames(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || len(object) != 2 {
		return errors.New("credential binding object shape is invalid")
	}
	if _, ok := object["schema_version"]; !ok {
		return errors.New("credential binding schema_version is missing")
	}
	bindingsData, ok := object["bindings"]
	if !ok {
		return errors.New("credential bindings are missing")
	}
	var bindings []json.RawMessage
	if err := json.Unmarshal(bindingsData, &bindings); err != nil || len(bindings) > healthCredentialBindingsMaxEntries {
		return errors.New("credential binding count is invalid")
	}
	want := map[string]struct{}{
		"credential_reference": {}, "credential_scope_key": {}, "provider": {}, "adapter_id": {},
		"auth_kind": {}, "credential_group_id": {}, "credential_env_name": {},
	}
	for _, raw := range bindings {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil || len(entry) != len(want) {
			return errors.New("credential binding entry shape is invalid")
		}
		for key := range entry {
			if _, ok := want[key]; !ok {
				return errors.New("credential binding entry field is unknown")
			}
		}
	}
	return nil
}

func preflightHealthCredentialBindingsJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	scan := healthOperationPlanJSONScan{maxTokens: healthCredentialBindingMaxTokens, maxDepth: 16}
	opening, err := scan.token(decoder)
	if err != nil || opening != json.Delim('{') {
		return errors.New("credential binding file must be a JSON object")
	}
	keys := map[string]struct{}{}
	bindingsCount := 0
	for decoder.More() {
		keyToken, err := scan.token(decoder)
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("credential binding field is invalid")
		}
		folded := strings.ToLower(key)
		if _, duplicate := keys[folded]; duplicate {
			return errors.New("credential binding field is duplicated")
		}
		keys[folded] = struct{}{}
		if folded != "bindings" {
			if err := scan.value(decoder, 1); err != nil {
				return err
			}
			continue
		}
		array, err := scan.token(decoder)
		if err != nil || array != json.Delim('[') {
			return errors.New("credential bindings are not an array")
		}
		for decoder.More() {
			bindingsCount++
			if bindingsCount > healthCredentialBindingsMaxEntries {
				return errors.New("credential binding count exceeds the local ceiling")
			}
			if err := scan.value(decoder, 2); err != nil {
				return err
			}
		}
		closingArray, err := scan.token(decoder)
		if err != nil || closingArray != json.Delim(']') {
			return errors.New("credential bindings are incomplete")
		}
	}
	closing, err := scan.token(decoder)
	if err != nil || closing != json.Delim('}') || bindingsCount == 0 {
		return errors.New("credential binding object is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("credential binding file has trailing JSON")
	}
	return nil
}

func validateHealthCredentialBindings(config healthCredentialBindingsConfig) error {
	if config.SchemaVersion != healthCredentialBindingsSchemaVersion || len(config.Bindings) == 0 || len(config.Bindings) > healthCredentialBindingsMaxEntries {
		return errors.New("credential binding schema or size is invalid")
	}
	seen := make(map[string]struct{}, len(config.Bindings))
	for _, binding := range config.Bindings {
		if binding.CredentialReference == "" || len(binding.CredentialReference) > 1024 || strings.TrimSpace(binding.CredentialReference) != binding.CredentialReference || binding.CredentialScopeKey == "" || len(binding.CredentialScopeKey) > 256 || !credentialScopeKeyPattern.MatchString(binding.CredentialScopeKey) || strings.TrimSpace(binding.Provider) == "" || len(binding.Provider) > 128 || strings.TrimSpace(binding.AdapterID) == "" || len(binding.AdapterID) > 128 || !healthAdapterIDPattern.MatchString(binding.AdapterID) || (binding.AuthKind != "service_key" && binding.AuthKind != "api_key") || binding.CredentialGroupID == "" || len(binding.CredentialGroupID) > 64 || !credentialGroupIDPattern.MatchString(binding.CredentialGroupID) || !credentialEnvNamePattern.MatchString(binding.CredentialEnvName) {
			return errors.New("credential binding entry is invalid")
		}
		key := strings.Join([]string{binding.CredentialReference, binding.CredentialScopeKey, binding.Provider, binding.AdapterID, binding.AuthKind}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			return errors.New("credential binding selector is duplicated")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func resolveHealthPlanCredential(
	plan healthOperationPlanRecord,
	config healthCredentialBindingsConfig,
	groupByID func(string) (credentialGroup, bool),
	lookupEnv func(string) (string, bool),
) (providers.Credential, error) {
	if err := validateHealthCredentialBindings(config); err != nil {
		return providers.Credential{}, errors.New("credential binding is invalid")
	}
	contract := plan.RequestPlan.RequestContract
	if contract == nil || contract.Authentication.Requirement != "required" || plan.RuntimeBinding.Status != "bound" {
		return providers.Credential{}, errors.New("plan does not require a bound credential")
	}
	var selected *healthCredentialBinding
	matches := 0
	for index := range config.Bindings {
		binding := &config.Bindings[index]
		if binding.CredentialReference == plan.RuntimeBinding.CredentialReference && binding.CredentialScopeKey == plan.RuntimeBinding.CredentialScopeKey && binding.Provider == plan.SourceBinding.Provider && binding.AdapterID == plan.SourceBinding.AdapterID && binding.AuthKind == contract.Authentication.Mechanism {
			selected = binding
			matches++
		}
	}
	if matches != 1 || selected == nil || groupByID == nil || lookupEnv == nil {
		return providers.Credential{}, errHealthCredentialBindingMismatch
	}
	group, ok := groupByID(selected.CredentialGroupID)
	if !ok || group.ID != selected.CredentialGroupID || group.Provider != selected.Provider || !stringSliceContains(group.EnvNames, selected.CredentialEnvName) {
		return providers.Credential{}, errHealthCredentialBindingMismatch
	}
	value, ok := lookupEnv(selected.CredentialEnvName)
	if !ok || strings.TrimSpace(value) == "" {
		return providers.Credential{}, errHealthCredentialSourceUnavailable
	}
	return providers.Credential{Name: selected.CredentialEnvName, Value: value}, nil
}

func stringSliceContains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func readPrivateBoundedFile(path string, maximum int64) ([]byte, error) {
	if maximum < 1 {
		return nil, errors.New("private bounded file is unavailable")
	}
	pathInfo, err := os.Stat(path)
	if err != nil || !pathInfo.Mode().IsRegular() {
		return nil, errors.New("private bounded file is unavailable")
	}
	file, err := openBoundedCandidate(path)
	if err != nil {
		return nil, errors.New("private bounded file is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !privateHealthCredentialBindingFile(file) {
		return nil, errors.New("private bounded file is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maximum {
		return nil, errors.New("private bounded file is unavailable")
	}
	return data, nil
}
