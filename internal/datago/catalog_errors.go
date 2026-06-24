package datago

import (
	"slices"
	"strings"
)

type CatalogErrorReport struct {
	GeneratedAt  string                  `json:"generated_at"`
	Provider     string                  `json:"provider"`
	Registry     string                  `json:"registry,omitempty"`
	Limit        int                     `json:"limit"`
	Truncated    bool                    `json:"truncated"`
	Summary      CatalogErrorSummary     `json:"summary"`
	StatusFields []CatalogErrorFieldStat `json:"status_fields"`
	Operations   []CatalogErrorOperation `json:"operations"`
}

type CatalogErrorSummary struct {
	SpecsTotal                   int `json:"specs_total"`
	OperationsTotal              int `json:"operations_total"`
	OperationsWithStatusFields   int `json:"operations_with_status_fields"`
	OperationsWithResultCode     int `json:"operations_with_result_code"`
	OperationsWithResultMessage  int `json:"operations_with_result_message"`
	OperationsWithReasonCode     int `json:"operations_with_reason_code"`
	OperationsWithAuthMessage    int `json:"operations_with_auth_message"`
	OperationsWithErrorMessage   int `json:"operations_with_error_message"`
	DistinctStatusFieldNames     int `json:"distinct_status_field_names"`
	DistinctStatusFieldNameRoles int `json:"distinct_status_field_name_roles"`
}

type CatalogErrorFieldStat struct {
	Name       string   `json:"name"`
	Label      string   `json:"label,omitempty"`
	Role       string   `json:"role"`
	Source     string   `json:"source"`
	Specs      int      `json:"specs"`
	Operations int      `json:"operations"`
	SampleIDs  []string `json:"sample_ids,omitempty"`
}

type CatalogErrorOperation struct {
	DatasetID       string              `json:"dataset_id"`
	Title           string              `json:"title"`
	Operation       string              `json:"operation"`
	Provider        string              `json:"provider"`
	EndpointHost    string              `json:"endpoint_host,omitempty"`
	DependencyClass string              `json:"dependency_class"`
	Fields          []CatalogErrorField `json:"fields"`
}

type CatalogErrorField struct {
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"`
	Role   string `json:"role"`
	Source string `json:"source"`
}

func AnalyzeCatalogErrors(reg Registry, limit int) CatalogErrorReport {
	report := CatalogErrorReport{
		Provider:     "data.go.kr",
		Limit:        limit,
		StatusFields: []CatalogErrorFieldStat{},
		Operations:   []CatalogErrorOperation{},
	}
	fieldStats := map[string]*catalogErrorFieldStatBuilder{}
	for _, spec := range reg.Specs() {
		report.Summary.SpecsTotal++
		for _, op := range spec.Operations {
			report.Summary.OperationsTotal++
			fields := CatalogErrorFields(op.ResponseParams)
			if len(fields) == 0 {
				continue
			}
			report.Summary.OperationsWithStatusFields++
			countErrorRoles(&report.Summary, fields)
			operation := CatalogErrorOperation{
				DatasetID:       spec.ID,
				Title:           spec.Title,
				Operation:       op.Name,
				Provider:        spec.Provider,
				EndpointHost:    hostOrEmpty(op.Endpoint),
				DependencyClass: OperationDependencyClass(spec, op),
				Fields:          fields,
			}
			if limit <= 0 || len(report.Operations) < limit {
				report.Operations = append(report.Operations, operation)
			} else {
				report.Truncated = true
			}
			for _, field := range fields {
				key := field.Name + "\x00" + field.Label + "\x00" + field.Role
				stat, ok := fieldStats[key]
				if !ok {
					stat = &catalogErrorFieldStatBuilder{
						CatalogErrorFieldStat: CatalogErrorFieldStat{
							Name:   field.Name,
							Label:  field.Label,
							Role:   field.Role,
							Source: field.Source,
						},
						specs: map[string]bool{},
					}
					fieldStats[key] = stat
				}
				stat.Operations++
				stat.specs[spec.ID] = true
				if len(stat.SampleIDs) < 5 && !containsString(stat.SampleIDs, spec.ID) {
					stat.SampleIDs = append(stat.SampleIDs, spec.ID)
				}
			}
		}
	}
	nameSet := map[string]bool{}
	stats := make([]CatalogErrorFieldStat, 0, len(fieldStats))
	for _, stat := range fieldStats {
		stat.Specs = len(stat.specs)
		stats = append(stats, stat.CatalogErrorFieldStat)
		nameSet[strings.ToLower(stat.Name)] = true
	}
	slices.SortFunc(stats, func(a, b CatalogErrorFieldStat) int {
		if a.Operations != b.Operations {
			return b.Operations - a.Operations
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		if a.Role != b.Role {
			return strings.Compare(a.Role, b.Role)
		}
		if a.Source != b.Source {
			return strings.Compare(a.Source, b.Source)
		}
		if a.Label != b.Label {
			return strings.Compare(a.Label, b.Label)
		}
		if a.Specs != b.Specs {
			return b.Specs - a.Specs
		}
		return strings.Compare(strings.Join(a.SampleIDs, ","), strings.Join(b.SampleIDs, ","))
	})
	report.StatusFields = stats
	report.Summary.DistinctStatusFieldNames = len(nameSet)
	report.Summary.DistinctStatusFieldNameRoles = len(stats)
	return report
}

type catalogErrorFieldStatBuilder struct {
	CatalogErrorFieldStat
	specs map[string]bool
}

func CatalogErrorFields(params []Param) []CatalogErrorField {
	fields := make([]CatalogErrorField, 0)
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		role := CatalogErrorFieldRole(name, param.Label)
		if role == "" {
			continue
		}
		fields = append(fields, CatalogErrorField{
			Name:   name,
			Label:  strings.TrimSpace(param.Label),
			Role:   role,
			Source: "response_params",
		})
	}
	return fields
}

func CatalogErrorFieldRole(name, label string) string {
	normalized := normalizeParamName(name)
	switch normalized {
	case "resultcode", "result_code":
		return "result_code"
	case "resultmsg", "result_msg":
		return "result_message"
	case "returnreasoncode", "return_reason_code":
		return "reason_code"
	case "returnauthmsg", "return_auth_msg":
		return "auth_message"
	case "errmsg", "err_msg", "errormsg", "error_msg", "errormessage", "error_message":
		return "error_message"
	case "errorcode", "error_code":
		return "result_code"
	}
	label = strings.ToLower(strings.TrimSpace(label))
	if normalized == "code" && statusLabel(label) {
		return "result_code"
	}
	if (normalized == "message" || normalized == "msg") && statusLabel(label) {
		return "result_message"
	}
	return ""
}

func statusLabel(label string) bool {
	for _, marker := range []string{"결과", "오류", "에러", "상태", "메시지", "message", "error", "status", "result"} {
		if strings.Contains(label, marker) {
			return true
		}
	}
	return false
}

func countErrorRoles(summary *CatalogErrorSummary, fields []CatalogErrorField) {
	seen := map[string]bool{}
	for _, field := range fields {
		seen[field.Role] = true
	}
	if seen["result_code"] {
		summary.OperationsWithResultCode++
	}
	if seen["result_message"] {
		summary.OperationsWithResultMessage++
	}
	if seen["reason_code"] {
		summary.OperationsWithReasonCode++
	}
	if seen["auth_message"] {
		summary.OperationsWithAuthMessage++
	}
	if seen["error_message"] {
		summary.OperationsWithErrorMessage++
	}
}

func hostOrEmpty(rawURL string) string {
	host, malformed := urlHost(rawURL)
	if malformed {
		return ""
	}
	return host
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
