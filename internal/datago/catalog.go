package datago

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
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
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Provider       string      `json:"provider"`
	Organization   string      `json:"organization,omitempty"`
	SourceCategory string      `json:"source_category,omitempty"`
	Priority       string      `json:"priority"`
	SourceKeywords []string    `json:"source_keywords,omitempty"`
	SearchTerms    []string    `json:"search_terms,omitempty"`
	Operations     []Operation `json:"operations"`
	Smoke          *Smoke      `json:"smoke,omitempty"`
	Source         *Source     `json:"source,omitempty"`
	Description    string      `json:"description,omitempty"`
}

type Operation struct {
	Name           string            `json:"name"`
	Endpoint       string            `json:"endpoint,omitempty"`
	DefaultParams  map[string]string `json:"default_params,omitempty"`
	RequestParams  []Param           `json:"request_params,omitempty"`
	ResponseParams []Param           `json:"response_params,omitempty"`
	Source         *Source           `json:"source,omitempty"`
}

type Smoke struct {
	Operation string            `json:"operation,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

type Param struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

type Source struct {
	System string         `json:"system"`
	URL    string         `json:"url,omitempty"`
	Raw    map[string]any `json:"raw,omitempty"`
}

type SearchFilters struct {
	Provider       string `json:"provider"`
	Organization   string `json:"organization"`
	SourceCategory string `json:"source_category"`
	Priority       string `json:"priority"`
}

type ResolveStatus string

const (
	ResolveFound     ResolveStatus = "found"
	ResolveNotFound  ResolveStatus = "not_found"
	ResolveAmbiguous ResolveStatus = "ambiguous"
)

type ResolveResult struct {
	Status     ResolveStatus `json:"status"`
	Ref        string        `json:"ref"`
	Mode       string        `json:"mode,omitempty"`
	Spec       Spec          `json:"spec,omitempty"`
	Candidates []Spec        `json:"candidates,omitempty"`
}

func DefaultRegistry() Registry {
	var specs []Spec
	if err := json.Unmarshal(catalogSeed, &specs); err != nil {
		panic(err)
	}
	return Registry{specs: specs}
}

func NewRegistry(specs []Spec) Registry {
	return Registry{specs: append([]Spec(nil), specs...)}
}

func LoadRegistry(path string) (Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Registry{}, err
	}
	defer f.Close()
	return DecodeRegistry(f)
}

func DecodeRegistry(r io.Reader) (Registry, error) {
	var specs []Spec
	if err := json.NewDecoder(r).Decode(&specs); err != nil {
		return Registry{}, fmt.Errorf("decode registry: %w", err)
	}
	return NewRegistry(specs), nil
}

func (r Registry) Specs() []Spec {
	return append([]Spec(nil), r.specs...)
}

func (r Registry) Search(query string, limit int, filters SearchFilters) []Spec {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 && filters.empty() {
		return nil
	}
	type scored struct {
		spec  Spec
		score int
	}
	var matches []scored
	for _, spec := range r.specs {
		if !filters.match(spec) {
			continue
		}
		haystack := strings.ToLower(strings.Join(spec.searchText(), " "))
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 || len(terms) == 0 {
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

func (f SearchFilters) empty() bool {
	return f.Provider == "" && f.Organization == "" && f.SourceCategory == "" && f.Priority == ""
}

func (f SearchFilters) match(spec Spec) bool {
	return containsFold(spec.Provider, f.Provider) &&
		containsFold(spec.Organization, f.Organization) &&
		containsFold(spec.SourceCategory, f.SourceCategory) &&
		containsFold(spec.Priority, f.Priority)
}

func containsFold(value, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(want))
}

func (r Registry) ByID(id string) (Spec, bool) {
	for _, spec := range r.specs {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

func (r Registry) Resolve(ref string, limit int) ResolveResult {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ResolveResult{Status: ResolveNotFound, Ref: ref}
	}
	if id := ExtractDataGoKrListID(ref); id != "" {
		if spec, ok := r.ByID(id); ok {
			return ResolveResult{Status: ResolveFound, Ref: ref, Mode: "url", Spec: spec}
		}
		return ResolveResult{Status: ResolveNotFound, Ref: ref, Mode: "url"}
	}
	if spec, ok := r.ByID(ref); ok {
		return ResolveResult{Status: ResolveFound, Ref: ref, Mode: "id", Spec: spec}
	}
	for _, spec := range r.specs {
		if strings.EqualFold(strings.TrimSpace(spec.Title), ref) {
			return ResolveResult{Status: ResolveFound, Ref: ref, Mode: "title", Spec: spec}
		}
	}
	if limit <= 0 {
		limit = 10
	}
	matches := r.Search(ref, limit, SearchFilters{})
	switch len(matches) {
	case 0:
		return ResolveResult{Status: ResolveNotFound, Ref: ref, Mode: "query"}
	case 1:
		return ResolveResult{Status: ResolveFound, Ref: ref, Mode: "query", Spec: matches[0]}
	default:
		return ResolveResult{Status: ResolveAmbiguous, Ref: ref, Mode: "query", Candidates: matches}
	}
}

var dataGoKrListIDPattern = regexp.MustCompile(`/data/([0-9]+)/`)

func ExtractDataGoKrListID(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if parsed, err := url.Parse(ref); err == nil && parsed.Host != "" {
		match := dataGoKrListIDPattern.FindStringSubmatch(parsed.Path + "/")
		if len(match) == 2 {
			return match[1]
		}
	}
	return ""
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
	args := []string{"datapan", "get", s.ID}
	if s.Smoke.Operation != "" {
		args = append(args, "--operation", s.Smoke.Operation)
	}
	keys := make([]string, 0, len(s.Smoke.Params))
	for key := range s.Smoke.Params {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		args = append(args, key+"="+s.Smoke.Params[key])
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func (s Spec) searchText() []string {
	parts := []string{s.ID, s.Title, s.Provider, s.Organization, s.SourceCategory, s.Priority, s.Description}
	parts = append(parts, s.SourceKeywords...)
	parts = append(parts, s.SearchTerms...)
	for _, op := range s.Operations {
		parts = append(parts, op.Name, op.Endpoint)
	}
	return parts
}
