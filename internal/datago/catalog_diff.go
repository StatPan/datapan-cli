package datago

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type CatalogDiff struct {
	OldCount int            `json:"old_count"`
	NewCount int            `json:"new_count"`
	Added    []Spec         `json:"added"`
	Removed  []Spec         `json:"removed"`
	Changed  []SpecChange   `json:"changed"`
	Stable   int            `json:"stable"`
	Summary  CatalogSummary `json:"summary"`
}

type CatalogDiffReport struct {
	GeneratedAt string                   `json:"generated_at"`
	Provider    string                   `json:"provider"`
	Old         string                   `json:"old"`
	New         string                   `json:"new"`
	Limit       int                      `json:"limit"`
	Truncated   bool                     `json:"truncated"`
	Counts      CatalogDiffCounts        `json:"counts"`
	Summary     CatalogSummary           `json:"summary"`
	Added       []CatalogDiffSpecSummary `json:"added"`
	Removed     []CatalogDiffSpecSummary `json:"removed"`
	Changed     []SpecChange             `json:"changed"`
}

type CatalogDiffCounts struct {
	Old int `json:"old"`
	New int `json:"new"`
}

type CatalogSummary struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
	Stable  int `json:"stable"`
}

type CatalogDiffSpecSummary struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Provider        string   `json:"provider"`
	Organization    string   `json:"organization,omitempty"`
	SourceCategory  string   `json:"source_category,omitempty"`
	Priority        string   `json:"priority,omitempty"`
	OperationsCount int      `json:"operations_count"`
	SourceKeywords  []string `json:"source_keywords,omitempty"`
	SearchTerms     []string `json:"search_terms,omitempty"`
	Description     string   `json:"description,omitempty"`
}

type SpecChange struct {
	ID        string   `json:"id"`
	OldTitle  string   `json:"old_title,omitempty"`
	NewTitle  string   `json:"new_title,omitempty"`
	Fields    []string `json:"fields"`
	OldDigest string   `json:"old_digest"`
	NewDigest string   `json:"new_digest"`
}

func DiffRegistries(oldReg, newReg Registry) CatalogDiff {
	oldSpecs := oldReg.Specs()
	newSpecs := newReg.Specs()
	oldByID := specMap(oldSpecs)
	newByID := specMap(newSpecs)
	diff := CatalogDiff{
		OldCount: len(oldSpecs),
		NewCount: len(newSpecs),
	}
	for id, newSpec := range newByID {
		if _, ok := oldByID[id]; !ok {
			diff.Added = append(diff.Added, newSpec)
		}
	}
	for id, oldSpec := range oldByID {
		newSpec, ok := newByID[id]
		if !ok {
			diff.Removed = append(diff.Removed, oldSpec)
			continue
		}
		change, changed := compareSpec(oldSpec, newSpec)
		if changed {
			diff.Changed = append(diff.Changed, change)
		} else {
			diff.Stable++
		}
	}
	sortSpecs(diff.Added)
	sortSpecs(diff.Removed)
	slices.SortFunc(diff.Changed, func(a, b SpecChange) int {
		return strings.Compare(a.ID, b.ID)
	})
	diff.Summary = CatalogSummary{
		Added:   len(diff.Added),
		Removed: len(diff.Removed),
		Changed: len(diff.Changed),
		Stable:  diff.Stable,
	}
	return diff
}

func NewCatalogDiffReport(generatedAt, provider, oldPath, newPath string, limit int, diff CatalogDiff) CatalogDiffReport {
	return CatalogDiffReport{
		GeneratedAt: generatedAt,
		Provider:    provider,
		Old:         oldPath,
		New:         newPath,
		Limit:       limit,
		Truncated:   DiffTruncated(diff, limit),
		Counts: CatalogDiffCounts{
			Old: diff.OldCount,
			New: diff.NewCount,
		},
		Summary: diff.Summary,
		Added:   CatalogDiffSpecSummaries(LimitCatalogDiffSpecs(diff.Added, limit)),
		Removed: CatalogDiffSpecSummaries(LimitCatalogDiffSpecs(diff.Removed, limit)),
		Changed: LimitCatalogDiffChanges(diff.Changed, limit),
	}
}

func CatalogDiffSpecSummaries(specs []Spec) []CatalogDiffSpecSummary {
	out := make([]CatalogDiffSpecSummary, 0, len(specs))
	for _, spec := range specs {
		out = append(out, CatalogDiffSpecSummary{
			ID:              spec.ID,
			Title:           spec.Title,
			Provider:        spec.Provider,
			Organization:    spec.Organization,
			SourceCategory:  spec.SourceCategory,
			Priority:        spec.Priority,
			OperationsCount: len(spec.Operations),
			SourceKeywords:  spec.SourceKeywords,
			SearchTerms:     spec.SearchTerms,
			Description:     spec.Description,
		})
	}
	return out
}

func LimitCatalogDiffSpecs(specs []Spec, limit int) []Spec {
	if specs == nil {
		return []Spec{}
	}
	if limit <= 0 || len(specs) <= limit {
		return specs
	}
	return specs[:limit]
}

func LimitCatalogDiffChanges(changes []SpecChange, limit int) []SpecChange {
	if changes == nil {
		return []SpecChange{}
	}
	if limit <= 0 || len(changes) <= limit {
		return changes
	}
	return changes[:limit]
}

func DiffTruncated(diff CatalogDiff, limit int) bool {
	if limit <= 0 {
		return false
	}
	return len(diff.Added) > limit || len(diff.Removed) > limit || len(diff.Changed) > limit
}

func specMap(specs []Spec) map[string]Spec {
	out := make(map[string]Spec, len(specs))
	for _, spec := range specs {
		if spec.ID != "" {
			out[spec.ID] = spec
		}
	}
	return out
}

func compareSpec(oldSpec, newSpec Spec) (SpecChange, bool) {
	oldDigest := specDigest(oldSpec)
	newDigest := specDigest(newSpec)
	if oldDigest == newDigest {
		return SpecChange{}, false
	}
	return SpecChange{
		ID:        newSpec.ID,
		OldTitle:  oldSpec.Title,
		NewTitle:  newSpec.Title,
		Fields:    changedFields(oldSpec, newSpec),
		OldDigest: oldDigest,
		NewDigest: newDigest,
	}, true
}

func changedFields(oldSpec, newSpec Spec) []string {
	checks := []struct {
		name string
		old  any
		new  any
	}{
		{"title", oldSpec.Title, newSpec.Title},
		{"provider", oldSpec.Provider, newSpec.Provider},
		{"organization", oldSpec.Organization, newSpec.Organization},
		{"source_category", oldSpec.SourceCategory, newSpec.SourceCategory},
		{"priority", oldSpec.Priority, newSpec.Priority},
		{"source_keywords", oldSpec.SourceKeywords, newSpec.SourceKeywords},
		{"search_terms", oldSpec.SearchTerms, newSpec.SearchTerms},
		{"operations", oldSpec.Operations, newSpec.Operations},
		{"smoke", oldSpec.Smoke, newSpec.Smoke},
		{"source", oldSpec.Source, newSpec.Source},
		{"description", oldSpec.Description, newSpec.Description},
	}
	fields := make([]string, 0, len(checks))
	for _, check := range checks {
		if digest(check.old) != digest(check.new) {
			fields = append(fields, check.name)
		}
	}
	return fields
}

func specDigest(spec Spec) string {
	return digest(spec)
}

func digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("marshal-error:%v", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func sortSpecs(specs []Spec) {
	slices.SortFunc(specs, func(a, b Spec) int {
		return strings.Compare(a.ID, b.ID)
	})
}
