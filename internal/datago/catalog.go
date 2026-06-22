package datago

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
)

//go:embed catalog.seed.json
var catalogSeed []byte

var KeyEnvNames = []string{
	"DATAPAN_DATA_GO_KR_KEY",
	"DATA_PORTAL_API_KEY",
	"DATA_GO_KR_SERVICE_KEY",
}

const PurposeTextKO = "StatPan Datapan은 공공데이터 기반 제품 및 연구 파이프라인을 위한 source-of-record 데이터 플랫폼입니다. 해당 data.go.kr API는 원천, freshness, coverage, 계약, runtime evidence를 갖춘 데이터 계층을 구축하기 위해 사용합니다. 인증키는 관리형 runtime secret에만 보관하며, 정기 수집이나 백필 전에 제한된 smoke check로 호출 가능 여부를 검증합니다."

type Registry struct {
	specs []Spec
}

type Spec struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Provider    string      `json:"provider"`
	Sector      string      `json:"sector"`
	Priority    string      `json:"priority"`
	Keywords    []string    `json:"keywords"`
	Operations  []Operation `json:"operations"`
	Smoke       *Smoke      `json:"smoke,omitempty"`
	Description string      `json:"description,omitempty"`
}

type Operation struct {
	Name          string            `json:"name"`
	Endpoint      string            `json:"endpoint,omitempty"`
	DefaultParams map[string]string `json:"default_params,omitempty"`
}

type Smoke struct {
	Operation string            `json:"operation,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

func DefaultRegistry() Registry {
	var specs []Spec
	if err := json.Unmarshal(catalogSeed, &specs); err != nil {
		panic(err)
	}
	return Registry{specs: specs}
}

func (r Registry) Search(query string, limit int) []Spec {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		spec  Spec
		score int
	}
	var matches []scored
	for _, spec := range r.specs {
		haystack := strings.ToLower(strings.Join(spec.searchText(), " "))
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, scored{spec: spec, score: score})
		}
	}
	slices.SortFunc(matches, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(a.spec.ID, b.spec.ID)
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	results := make([]Spec, len(matches))
	for i, match := range matches {
		results[i] = match.spec
	}
	return results
}

func (r Registry) ByID(id string) (Spec, bool) {
	for _, spec := range r.specs {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

func (s Spec) ApplicationURL() string {
	return "https://www.data.go.kr/data/" + s.ID + "/openapi.do"
}

func (s Spec) Operation(name string) (Operation, bool) {
	if len(s.Operations) == 0 {
		return Operation{}, false
	}
	if name == "" {
		return s.Operations[0], s.Operations[0].Endpoint != ""
	}
	for _, op := range s.Operations {
		if op.Name == name {
			return op, op.Endpoint != ""
		}
	}
	return Operation{}, false
}

func (s Spec) SmokeCommand() string {
	if s.Smoke == nil {
		return ""
	}
	args := []string{"datapan", "call", s.ID}
	if s.Smoke.Operation != "" {
		args = append(args, "--operation", s.Smoke.Operation)
	}
	keys := make([]string, 0, len(s.Smoke.Params))
	for key := range s.Smoke.Params {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		args = append(args, "--param", key+"="+s.Smoke.Params[key])
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func (s Spec) searchText() []string {
	parts := []string{s.ID, s.Title, s.Provider, s.Sector, s.Priority, s.Description}
	parts = append(parts, s.Keywords...)
	for _, op := range s.Operations {
		parts = append(parts, op.Name, op.Endpoint)
	}
	return parts
}
