package cli

import (
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
	providers "github.com/StatPan/datapan-cli/internal/provider"
)

// credentialGroup is deliberately value-free metadata. Credential material is
// resolved only at the point a request needs it and is never returned in a
// readiness report or receipt.
type credentialGroup struct {
	ID       string
	Provider string
	EnvNames []string
}

var credentialGroups = []credentialGroup{
	{ID: "data_go_kr", Provider: "data.go.kr", EnvNames: datago.KeyEnvNames},
	{ID: "opendart", Provider: "OpenDART", EnvNames: []string{"OPENDART_API_KEY", "DART_API_KEY"}},
	{ID: "seoul_open_data", Provider: "Seoul Open Data", EnvNames: []string{"SEOUL_OPEN_DATA_KEY"}},
}

func credentialGroupByID(id string) (credentialGroup, bool) {
	for _, group := range credentialGroups {
		if group.ID == id {
			return group, true
		}
	}
	return credentialGroup{}, false
}

func credentialGroupForAdapter(adapter providers.Adapter) credentialGroup {
	if adapter != nil {
		switch strings.ToLower(strings.TrimSpace(adapter.Name())) {
		case "opendart":
			group, _ := credentialGroupByID("opendart")
			return group
		case "seoul-open-data":
			group, _ := credentialGroupByID("seoul_open_data")
			return group
		}
	}
	group, _ := credentialGroupByID("data_go_kr")
	return group
}

func (a app) credentialForGroup(group credentialGroup) (providers.Credential, bool) {
	for _, name := range group.EnvNames {
		if value, ok := a.env.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			return providers.Credential{Name: name, Value: value}, true
		}
	}
	return providers.Credential{}, false
}

func (a app) credentialForOperation(op datago.Operation) (credentialGroup, providers.Credential, bool, error) {
	registry, err := providers.DefaultRegistry()
	if err != nil {
		return credentialGroup{}, providers.Credential{}, false, err
	}
	host, err := endpointHostForCredential(op.Endpoint)
	if err != nil {
		return credentialGroup{}, providers.Credential{}, false, err
	}
	adapter, _ := registry.MatchHost(host)
	group := credentialGroupForAdapter(adapter)
	credential, ok := a.credentialForGroup(group)
	return group, credential, ok, nil
}

func endpointHostForCredential(endpoint string) (string, error) {
	u, err := parseCallableEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}

func (a app) credentialReadiness() []map[string]any {
	out := make([]map[string]any, 0, len(credentialGroups))
	for _, group := range credentialGroups {
		credential, present := a.credentialForGroup(group)
		out = append(out, map[string]any{
			"group":              group.ID,
			"provider":           group.Provider,
			"accepted_env_vars":  group.EnvNames,
			"credential_present": present,
			"selected_env_var":   credential.Name,
			"live_verified":      false,
		})
	}
	return out
}
