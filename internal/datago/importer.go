package datago

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const DataGoKrOpenDataListURL = "https://api.odcloud.kr/api/15077093/v1/open-data-list"

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ImportOptions struct {
	ServiceKey string
	Page       int
	PerPage    int
	Pages      int
	Query      string
	Org        string
	Category   string
}

type ImportResult struct {
	Provider     string `json:"provider"`
	SourceURL    string `json:"source_url"`
	Page         int    `json:"page"`
	PerPage      int    `json:"per_page"`
	PagesFetched int    `json:"pages_fetched"`
	RowsFetched  int    `json:"rows_fetched"`
	TotalCount   int    `json:"total_count"`
	SpecsWritten int    `json:"specs_written"`
	Operations   int    `json:"operations"`
}

type OpenDataListResponse struct {
	CurrentCount int               `json:"currentCount"`
	Data         []OpenDataListRow `json:"data"`
	MatchCount   int               `json:"matchCount"`
	Page         int               `json:"page"`
	PerPage      int               `json:"perPage"`
	TotalCount   int               `json:"totalCount"`
}

type OpenDataListRow struct {
	APIType                string `json:"api_type"`
	CategoryName           string `json:"category_nm"`
	CreatedAt              string `json:"created_at"`
	DataFormat             string `json:"data_format"`
	DepartmentName         string `json:"dept_nm"`
	Description            string `json:"desc"`
	EndpointURL            string `json:"end_point_url"`
	GuideURL               string `json:"guide_url"`
	ID                     string `json:"id"`
	IsCharged              string `json:"is_charged"`
	IsConfirmedForDevName  string `json:"is_confirmed_for_dev_nm"`
	IsConfirmedForProdName string `json:"is_confirmed_for_prod_nm"`
	IsDeleted              string `json:"is_deleted"`
	IsListDeleted          string `json:"is_list_deleted"`
	Keywords               string `json:"keywords"`
	ListID                 string `json:"list_id"`
	ListTitle              string `json:"list_title"`
	ListType               string `json:"list_type"`
	MetaURL                string `json:"meta_url"`
	NewCategoryCode        string `json:"new_category_cd"`
	NewCategoryName        string `json:"new_category_nm"`
	OperationName          string `json:"operation_nm"`
	OperationSeq           string `json:"operation_seq"`
	OperationURL           string `json:"operation_url"`
	OrgCode                string `json:"org_cd"`
	OrgName                string `json:"org_nm"`
	OwnershipGrounds       string `json:"ownership_grounds"`
	RegisterStatus         string `json:"register_status"`
	RequestCount           int    `json:"request_cnt"`
	RequestParamNames      string `json:"request_param_nm"`
	RequestParamNamesEN    string `json:"request_param_nm_en"`
	ResponseParamNames     string `json:"response_param_nm"`
	ResponseParamNamesEN   string `json:"response_param_nm_en"`
	ShareScopeName         string `json:"share_scope_nm"`
	Title                  string `json:"title"`
	TitleEN                string `json:"title_en"`
	UpdatedAt              string `json:"updated_at"`
}

func FetchDataGoKrOpenDataList(ctx context.Context, client HTTPClient, opts ImportOptions) ([]OpenDataListRow, ImportResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(opts.ServiceKey) == "" {
		return nil, ImportResult{}, fmt.Errorf("missing data.go.kr service key")
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PerPage <= 0 {
		opts.PerPage = 100
	}
	if opts.Pages <= 0 {
		opts.Pages = 1
	}

	var rows []OpenDataListRow
	result := ImportResult{
		Provider:  "data.go.kr",
		SourceURL: DataGoKrOpenDataListURL,
		Page:      opts.Page,
		PerPage:   opts.PerPage,
	}
	for page := opts.Page; page < opts.Page+opts.Pages; page++ {
		resp, err := fetchOpenDataListPage(ctx, client, opts, page)
		if err != nil {
			return nil, result, err
		}
		result.PagesFetched++
		result.RowsFetched += len(resp.Data)
		result.TotalCount = resp.TotalCount
		rows = append(rows, resp.Data...)
		if len(resp.Data) == 0 || len(rows) >= resp.TotalCount {
			break
		}
	}
	return rows, result, nil
}

func NormalizeOpenDataRows(rows []OpenDataListRow) ([]Spec, int) {
	byID := map[string]*Spec{}
	operations := 0
	for _, row := range rows {
		id := strings.TrimSpace(row.ListID)
		if id == "" {
			continue
		}
		spec, ok := byID[id]
		if !ok {
			spec = &Spec{
				ID:             id,
				Title:          firstNonEmpty(row.ListTitle, row.Title),
				Provider:       "data.go.kr",
				Organization:   strings.TrimSpace(row.OrgName),
				SourceCategory: firstNonEmpty(row.NewCategoryName, row.CategoryName),
				Priority:       "P2",
				SourceKeywords: splitCSVLike(row.Keywords),
				Description:    strings.TrimSpace(row.Description),
				Source: &Source{
					System: "data.go.kr",
					URL:    firstNonEmpty(row.MetaURL, dataGoKrApplicationURL(id)),
					Raw:    row.raw(),
				},
			}
			byID[id] = spec
		}
		op := row.operation()
		if op.Name == "" && op.Endpoint == "" {
			continue
		}
		if !hasOperation(*spec, op) {
			spec.Operations = append(spec.Operations, op)
			operations++
		}
	}

	specs := make([]Spec, 0, len(byID))
	for _, spec := range byID {
		slices.SortFunc(spec.Operations, func(a, b Operation) int {
			return strings.Compare(a.Name, b.Name)
		})
		specs = append(specs, *spec)
	}
	slices.SortFunc(specs, func(a, b Spec) int {
		return strings.Compare(a.ID, b.ID)
	})
	return specs, operations
}

func EncodeRegistry(w io.Writer, specs []Spec) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(specs)
}

func fetchOpenDataListPage(ctx context.Context, client HTTPClient, opts ImportOptions, page int) (OpenDataListResponse, error) {
	u, err := url.Parse(DataGoKrOpenDataListURL)
	if err != nil {
		return OpenDataListResponse{}, err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("perPage", strconv.Itoa(opts.PerPage))
	q.Set("returnType", "JSON")
	q.Set("serviceKey", opts.ServiceKey)
	if opts.Query != "" {
		q.Set("cond[list_title::LIKE]", opts.Query)
	}
	if opts.Org != "" {
		q.Set("cond[org_nm::LIKE]", opts.Org)
	}
	if opts.Category != "" {
		q.Set("cond[new_category_nm::LIKE]", opts.Category)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return OpenDataListResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return OpenDataListResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return OpenDataListResponse{}, fmt.Errorf("data.go.kr catalog import failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload OpenDataListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return OpenDataListResponse{}, err
	}
	return payload, nil
}

func (r OpenDataListRow) operation() Operation {
	return Operation{
		Name:           firstNonEmpty(r.OperationName, r.Title),
		Endpoint:       operationEndpoint(r.EndpointURL, r.OperationURL),
		DefaultParams:  map[string]string{},
		RequestParams:  pairParams(r.RequestParamNamesEN, r.RequestParamNames),
		ResponseParams: pairParams(r.ResponseParamNamesEN, r.ResponseParamNames),
		Source: &Source{
			System: "data.go.kr",
			URL:    strings.TrimSpace(r.MetaURL),
			Raw:    r.raw(),
		},
	}
}

func (r OpenDataListRow) raw() map[string]any {
	data, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func operationEndpoint(base, op string) string {
	base = strings.TrimSpace(base)
	op = strings.TrimSpace(op)
	if op == "" || op == " " {
		return base
	}
	if strings.HasPrefix(op, "http://") || strings.HasPrefix(op, "https://") {
		return op
	}
	if base == "" {
		return op
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(op, "/")
}

func hasOperation(spec Spec, op Operation) bool {
	for _, existing := range spec.Operations {
		if existing.Name == op.Name && existing.Endpoint == op.Endpoint {
			return true
		}
	}
	return false
}

func pairParams(namesEN, labels string) []Param {
	names := splitCSVLike(namesEN)
	korean := splitCSVLike(labels)
	params := make([]Param, 0, len(names))
	for i, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		param := Param{Name: name}
		if i < len(korean) {
			param.Label = korean[i]
		}
		params = append(params, param)
	}
	return params
}

func splitCSVLike(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	reader := csv.NewReader(strings.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	values, err := reader.Read()
	if err != nil {
		values = strings.Split(raw, ",")
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dataGoKrApplicationURL(id string) string {
	return "https://www.data.go.kr/data/" + id + "/openapi.do"
}
