package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	providers "github.com/StatPan/datapan-cli/internal/provider"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	exitOK        = 0
	exitUsage     = 1
	exitNotFound  = 2
	exitAuth      = 3
	exitRequest   = 4
	exitAmbiguous = 5
)

const version = "0.1.0-dev"

const defaultStorageStatePath = ".datapan/data-go-kr-browser-state.json"
const defaultBrowserProfilePath = ".datapan/browser-profile"
const defaultRegistryPath = ".datapan/data-go-kr.registry.json"
const defaultDiffLimit = 20

type Env interface {
	LookupEnv(string) (string, bool)
}

type RealEnv struct{}

func (RealEnv) LookupEnv(name string) (string, bool) { return os.LookupEnv(name) }

type dotEnv struct {
	base   Env
	values map[string]string
}

func (d dotEnv) LookupEnv(name string) (string, bool) {
	if value, ok := d.base.LookupEnv(name); ok {
		return value, ok
	}
	value, ok := d.values[name]
	return value, ok
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type RealHTTPClient struct{}

func (RealHTTPClient) Do(req *http.Request) (*http.Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

var (
	openURLFunc         = openURL
	copyToClipboardFunc = copyToClipboard
)

type app struct {
	args   []string
	stdout io.Writer
	stderr io.Writer
	env    Env
	http   HTTPClient
	reg    datago.Registry
}

func Run(args []string, stdout, stderr io.Writer, env Env, httpClient HTTPClient) int {
	env = maybeLoadDotEnv(env)
	reg := datago.DefaultRegistry()
	if path, ok := env.LookupEnv("DATAPAN_REGISTRY_PATH"); ok && strings.TrimSpace(path) != "" {
		loaded, err := datago.LoadRegistry(strings.TrimSpace(path))
		if err != nil {
			a := app{args: args, stdout: stdout, stderr: stderr, env: env, http: httpClient, reg: reg}
			return a.fail(exitUsage, "failed to load DATAPAN_REGISTRY_PATH: %v", err)
		}
		reg = loaded
	}
	a := app{
		args:   args,
		stdout: stdout,
		stderr: stderr,
		env:    env,
		http:   httpClient,
		reg:    reg,
	}
	return a.run()
}

func maybeLoadDotEnv(env Env) Env {
	if _, ok := env.(RealEnv); !ok {
		return env
	}
	values, err := readDotEnv(".env")
	if err != nil || len(values) == 0 {
		return env
	}
	return dotEnv{base: env, values: values}
}

func readDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "export ") {
			key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		}
		if key == "" {
			continue
		}
		values[key] = trimEnvValue(value)
	}
	return values, nil
}

func trimEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return strings.TrimSpace(value)
}

func (a app) run() int {
	args := append([]string(nil), a.args...)
	jsonOut, args := consumeBool(args, "--json")
	if len(args) == 0 {
		a.printHelp()
		return exitOK
	}

	switch args[0] {
	case "help", "-h", "--help":
		a.printHelp()
		return exitOK
	case "version":
		if jsonOut {
			return a.writeJSON(map[string]string{"version": version})
		}
		fmt.Fprintf(a.stdout, "datapan %s\n", version)
		return exitOK
	case "search":
		return a.search(args[1:], jsonOut)
	case "info":
		return a.info(args[1:], jsonOut)
	case "show":
		return a.info(args[1:], jsonOut)
	case "auth":
		return a.auth(args[1:], jsonOut)
	case "catalog":
		return a.catalog(args[1:], jsonOut)
	case "access":
		return a.access(args[1:], jsonOut)
	case "apply":
		return a.access(args[1:], jsonOut)
	case "call":
		return a.call(args[1:], jsonOut, false)
	case "get":
		return a.call(args[1:], jsonOut, false)
	case "curl":
		return a.curl(args[1:], jsonOut)
	case "export":
		return a.export(args[1:], jsonOut)
	case "codegen":
		return a.codegen(args[1:], jsonOut)
	case "save":
		return a.save(args[1:], jsonOut)
	default:
		return a.fail(exitUsage, "unknown command %q\n\nRun `datapan help`.", args[0])
	}
}

func (a app) search(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	limit, args, err := consumeInt(args, "--limit", 20)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	provider, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	organization, args, err := consumeString(args, "--org", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if organization == "" {
		organization, args, err = consumeString(args, "--organization", "")
		if err != nil {
			return a.fail(exitUsage, "%v", err)
		}
	}
	category, args, err := consumeString(args, "--category", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if category == "" {
		category, args, err = consumeString(args, "--source-category", "")
		if err != nil {
			return a.fail(exitUsage, "%v", err)
		}
	}
	if hasAnyArg(args, "--sector") {
		return a.fail(exitUsage, "--sector is not a source metadata field; use --category only for upstream source categories")
	}
	priority, args, err := consumeString(args, "--priority", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	filters := datago.SearchFilters{
		Provider:       provider,
		Organization:   organization,
		SourceCategory: category,
		Priority:       priority,
	}
	if query == "" && filters == (datago.SearchFilters{}) {
		return a.fail(exitUsage, "usage: datapan search [query] [--org NAME] [--category NAME] [--priority P0] [--provider NAME] [--json] [--limit N]")
	}
	results := a.reg.Search(query, limit, filters)
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":      true,
			"query":   query,
			"filters": filters,
			"count":   len(results),
			"results": specSummaries(results),
		})
	}
	if len(results) == 0 {
		fmt.Fprintf(a.stdout, "No matching data.go.kr specs for %q.\n", query)
		return exitOK
	}
	for _, spec := range results {
		fmt.Fprintf(a.stdout, "%s  %s  [%s]\n", spec.ID, spec.Title, spec.Priority)
		if spec.Organization != "" {
			fmt.Fprintf(a.stdout, "  organization: %s\n", spec.Organization)
		}
		if spec.SourceCategory != "" {
			fmt.Fprintf(a.stdout, "  source category: %s\n", spec.SourceCategory)
		}
		if len(spec.Operations) > 0 {
			fmt.Fprintf(a.stdout, "  default operation: %s\n", spec.Operations[0].Name)
		}
	}
	return exitOK
}

func (a app) catalog(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) == 0 {
		return a.fail(exitUsage, "usage: datapan catalog import data-go-kr ... | datapan catalog update data-go-kr ... | datapan catalog diff --old OLD --new NEW [--limit N] [--output PATH|-] [--json] | datapan catalog audit [--registry PATH] [--output PATH|-] [--json] | datapan catalog errors [--registry PATH] [--output PATH|-] [--json] | datapan catalog providers [--registry PATH] [--status STATUS] [--kind KIND] [--output PATH] [--json] | datapan catalog dependencies [--registry PATH] [--kind KIND] [--status STATUS] [--output PATH|-] [--json] | datapan catalog adapter-targets [--registry PATH] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json] | datapan catalog verify [--registry PATH|--input REPORT|summary] [--json] | datapan catalog release draft --registry PATH [--previous-registry PATH] [--json] | datapan catalog release verify --manifest PATH [--output PATH|-] [--json] | datapan catalog release readiness --manifest PATH [--output PATH|-] [--json]")
	}
	switch args[0] {
	case "import":
		return a.catalogImport(args[1:], jsonOut)
	case "update":
		return a.catalogUpdate(args[1:], jsonOut)
	case "diff":
		return a.catalogDiff(args[1:], jsonOut)
	case "audit":
		return a.catalogAudit(args[1:], jsonOut)
	case "errors":
		return a.catalogErrors(args[1:], jsonOut)
	case "providers":
		return a.catalogProviders(args[1:], jsonOut)
	case "dependencies":
		return a.catalogDependencies(args[1:], jsonOut)
	case "adapter-targets":
		return a.catalogAdapterTargets(args[1:], jsonOut)
	case "verify":
		return a.catalogVerify(args[1:], jsonOut)
	case "release":
		return a.catalogRelease(args[1:], jsonOut)
	default:
		return a.fail(exitUsage, "unknown catalog command %q", args[0])
	}
}

func (a app) catalogImport(args []string, jsonOut bool) int {
	if len(args) == 0 || args[0] != "data-go-kr" {
		return a.fail(exitUsage, "usage: datapan catalog import data-go-kr [--output PATH|-] [--page N] [--per-page N] [--pages N|--all] [--max-pages N] [--retries N] [--retry-delay-ms N] [--query TEXT] [--org NAME] [--category NAME] [--json]")
	}
	args = args[1:]
	all, args := consumeBool(args, "--all")
	output, args, err := consumeString(args, "--output", defaultRegistryPath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	page, args, err := consumeInt(args, "--page", 1)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	perPage, args, err := consumeInt(args, "--per-page", 100)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	pages, args, err := consumeInt(args, "--pages", 1)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	maxPages, args, err := consumeInt(args, "--max-pages", datago.DefaultImportMaxPages)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	retries, args, err := consumeInt(args, "--retries", datago.DefaultImportRetries)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	retryDelayMS, args, err := consumeInt(args, "--retry-delay-ms", int(datago.DefaultImportRetryDelay/time.Millisecond))
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	query, args, err := consumeString(args, "--query", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	org, args, err := consumeString(args, "--org", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if org == "" {
		org, args, err = consumeString(args, "--organization", "")
		if err != nil {
			return a.fail(exitUsage, "%v", err)
		}
	}
	category, args, err := consumeString(args, "--category", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog import data-go-kr [--output PATH|-] [--page N] [--per-page N] [--pages N|--all] [--max-pages N] [--retries N] [--retry-delay-ms N] [--query TEXT] [--org NAME] [--category NAME] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the registry JSON to stdout")
	}
	keyName, key, ok := a.resolveKeyValue()
	if !ok {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":                false,
				"error":             "missing_auth",
				"accepted_env_vars": datago.KeyEnvNames,
			}); code != exitOK {
				return code
			}
			return exitAuth
		}
		return a.fail(exitAuth, "missing data.go.kr API key; set one of: %s", strings.Join(datago.KeyEnvNames, ", "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, result, err := datago.FetchDataGoKrOpenDataList(ctx, a.http, datago.ImportOptions{
		ServiceKey: key,
		Page:       page,
		PerPage:    perPage,
		Pages:      pages,
		All:        all,
		MaxPages:   maxPages,
		Query:      query,
		Org:        org,
		Category:   category,
		Retries:    retries,
		RetryDelay: time.Duration(retryDelayMS) * time.Millisecond,
	})
	if err != nil {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":      false,
				"error":   "request_failed",
				"message": err.Error(),
				"catalog_import": map[string]any{
					"provider":      result.Provider,
					"source_url":    result.SourceURL,
					"page":          result.Page,
					"per_page":      result.PerPage,
					"pages_fetched": result.PagesFetched,
					"max_pages":     result.MaxPages,
					"rows_fetched":  result.RowsFetched,
					"total_count":   result.TotalCount,
					"retries":       result.Retries,
					"failed_page":   result.FailedPage,
				},
			}); code != exitOK {
				return code
			}
			return exitRequest
		}
		return a.fail(exitRequest, "%v", err)
	}
	specs, operations := datago.NormalizeOpenDataRows(rows)
	result.SpecsWritten = len(specs)
	result.Operations = operations
	payload := map[string]any{
		"ok":                  true,
		"provider":            "data.go.kr",
		"selected_env_var":    keyName,
		"output":              output,
		"catalog_import":      result,
		"registry_format":     "datapan.specs.v1",
		"source_preservation": "source_category/source_keywords are upstream values; search_terms are Datapan search helpers",
	}
	if err := writeRegistryOutput(output, specs, a.stdout); err != nil {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":      false,
				"error":   "request_failed",
				"message": err.Error(),
			}); code != exitOK {
				return code
			}
			return exitRequest
		}
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		return a.writeJSON(payload)
	}
	if output == "-" {
		return exitOK
	}
	fmt.Fprintf(a.stdout, "Imported %d data.go.kr rows into %d specs (%d operations).\n", result.RowsFetched, len(specs), operations)
	fmt.Fprintf(a.stdout, "Registry: %s\n", output)
	return exitOK
}

func (a app) catalogUpdate(args []string, jsonOut bool) int {
	if len(args) == 0 || args[0] != "data-go-kr" {
		return a.fail(exitUsage, "usage: datapan catalog update data-go-kr [--registry PATH] [--apply] [--backup] [--diff-limit N] [--per-page N] [--max-pages N] [--retries N] [--retry-delay-ms N] [--json]")
	}
	args = args[1:]
	apply, args := consumeBool(args, "--apply")
	backup, args := consumeBool(args, "--backup")
	registryPath, args, err := consumeString(args, "--registry", defaultRegistryPath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	diffLimit, args, err := consumeInt(args, "--diff-limit", defaultDiffLimit)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	perPage, args, err := consumeInt(args, "--per-page", 100)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	maxPages, args, err := consumeInt(args, "--max-pages", datago.DefaultImportMaxPages)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	retries, args, err := consumeInt(args, "--retries", datago.DefaultImportRetries)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	retryDelayMS, args, err := consumeInt(args, "--retry-delay-ms", int(datago.DefaultImportRetryDelay/time.Millisecond))
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog update data-go-kr [--registry PATH] [--apply] [--backup] [--diff-limit N] [--per-page N] [--max-pages N] [--retries N] [--retry-delay-ms N] [--json]")
	}
	keyName, key, ok := a.resolveKeyValue()
	if !ok {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":                false,
				"error":             "missing_auth",
				"accepted_env_vars": datago.KeyEnvNames,
			}); code != exitOK {
				return code
			}
			return exitAuth
		}
		return a.fail(exitAuth, "missing data.go.kr API key; set one of: %s", strings.Join(datago.KeyEnvNames, ", "))
	}
	oldReg, oldExists, err := loadRegistryOrEmpty(registryPath)
	if err != nil {
		return a.catalogDiffFailure(jsonOut, "existing", registryPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, result, err := datago.FetchDataGoKrOpenDataList(ctx, a.http, datago.ImportOptions{
		ServiceKey: key,
		Page:       1,
		PerPage:    perPage,
		All:        true,
		MaxPages:   maxPages,
		Retries:    retries,
		RetryDelay: time.Duration(retryDelayMS) * time.Millisecond,
	})
	if err != nil {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":      false,
				"error":   "request_failed",
				"message": err.Error(),
				"catalog_import": map[string]any{
					"provider":      result.Provider,
					"source_url":    result.SourceURL,
					"per_page":      result.PerPage,
					"pages_fetched": result.PagesFetched,
					"max_pages":     result.MaxPages,
					"rows_fetched":  result.RowsFetched,
					"total_count":   result.TotalCount,
					"retries":       result.Retries,
					"failed_page":   result.FailedPage,
				},
			}); code != exitOK {
				return code
			}
			return exitRequest
		}
		return a.fail(exitRequest, "%v", err)
	}
	specs, operations := datago.NormalizeOpenDataRows(rows)
	result.SpecsWritten = len(specs)
	result.Operations = operations
	newReg := datago.NewRegistry(specs)
	diff := datago.DiffRegistries(oldReg, newReg)
	audit := datago.AuditRegistry(newReg, 5)
	applied := false
	backupPath := ""
	if apply {
		if oldExists && backup {
			backupPath = registryPath + ".bak." + time.Now().UTC().Format("20060102T150405Z")
			if err := copyFile(registryPath, backupPath); err != nil {
				return a.catalogUpdateWriteFailure(jsonOut, err)
			}
		}
		if err := writeRegistryAtomic(registryPath, specs); err != nil {
			return a.catalogUpdateWriteFailure(jsonOut, err)
		}
		applied = true
	}
	payload := map[string]any{
		"ok":               true,
		"provider":         "data.go.kr",
		"selected_env_var": keyName,
		"registry":         registryPath,
		"old_exists":       oldExists,
		"dry_run":          !apply,
		"applied":          applied,
		"backup":           backupPath,
		"catalog_import":   result,
		"summary":          diff.Summary,
		"diff_limit":       diffLimit,
		"diff_truncated":   diffTruncated(diff, diffLimit),
		"audit":            audit,
		"added":            specSummaries(limitSpecs(diff.Added, diffLimit)),
		"removed":          specSummaries(limitSpecs(diff.Removed, diffLimit)),
		"changed":          limitChanges(diff.Changed, diffLimit),
	}
	if jsonOut {
		return a.writeJSON(payload)
	}
	fmt.Fprintf(a.stdout, "Catalog update: %s\n", registryPath)
	fmt.Fprintf(a.stdout, "  fetched rows: %d\n", result.RowsFetched)
	fmt.Fprintf(a.stdout, "  specs: %d\n", result.SpecsWritten)
	fmt.Fprintf(a.stdout, "  added: %d\n", diff.Summary.Added)
	fmt.Fprintf(a.stdout, "  removed: %d\n", diff.Summary.Removed)
	fmt.Fprintf(a.stdout, "  changed: %d\n", diff.Summary.Changed)
	fmt.Fprintf(a.stdout, "  callable operations: %d/%d\n", audit.CallableOperations, audit.OperationsTotal)
	if applied {
		fmt.Fprintln(a.stdout, "  applied: true")
		if backupPath != "" {
			fmt.Fprintf(a.stdout, "  backup: %s\n", backupPath)
		}
	} else {
		fmt.Fprintln(a.stdout, "  dry-run: true (use --apply to replace the registry)")
	}
	return exitOK
}

func (a app) catalogUpdateWriteFailure(jsonOut bool, err error) int {
	if jsonOut {
		if code := a.writeJSON(map[string]any{
			"ok":      false,
			"error":   "request_failed",
			"message": err.Error(),
		}); code != exitOK {
			return code
		}
		return exitRequest
	}
	return a.fail(exitRequest, "%v", err)
}

func (a app) catalogDiff(args []string, jsonOut bool) int {
	oldPath, args, err := consumeString(args, "--old", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	newPath, args, err := consumeString(args, "--new", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", defaultDiffLimit)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if oldPath == "" && len(args) > 0 {
		oldPath = args[0]
		args = args[1:]
	}
	if newPath == "" && len(args) > 0 {
		newPath = args[0]
		args = args[1:]
	}
	if oldPath == "" || newPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog diff --old OLD --new NEW [--limit N] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the catalog diff report JSON to stdout")
	}
	oldReg, err := datago.LoadRegistry(oldPath)
	if err != nil {
		return a.catalogDiffFailure(jsonOut, "old", oldPath, err)
	}
	newReg, err := datago.LoadRegistry(newPath)
	if err != nil {
		return a.catalogDiffFailure(jsonOut, "new", newPath, err)
	}
	diff := datago.DiffRegistries(oldReg, newReg)
	report := datago.NewCatalogDiffReport(time.Now().UTC().Format(time.RFC3339), "data.go.kr", oldPath, newPath, limit, diff)
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":        true,
			"output":    output,
			"old":       oldPath,
			"new":       newPath,
			"report":    report,
			"summary":   diff.Summary,
			"limit":     limit,
			"truncated": report.Truncated,
			"added":     report.Added,
			"removed":   report.Removed,
			"changed":   report.Changed,
			"counts":    report.Counts,
		})
	}
	fmt.Fprintf(a.stdout, "Catalog diff: %s -> %s\n", oldPath, newPath)
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  added: %d\n", diff.Summary.Added)
	fmt.Fprintf(a.stdout, "  removed: %d\n", diff.Summary.Removed)
	fmt.Fprintf(a.stdout, "  changed: %d\n", diff.Summary.Changed)
	fmt.Fprintf(a.stdout, "  stable: %d\n", diff.Summary.Stable)
	for _, spec := range datago.LimitCatalogDiffSpecs(diff.Added, limit) {
		fmt.Fprintf(a.stdout, "+ %s  %s\n", spec.ID, spec.Title)
	}
	for _, spec := range datago.LimitCatalogDiffSpecs(diff.Removed, limit) {
		fmt.Fprintf(a.stdout, "- %s  %s\n", spec.ID, spec.Title)
	}
	for _, change := range datago.LimitCatalogDiffChanges(diff.Changed, limit) {
		fmt.Fprintf(a.stdout, "~ %s  %s\n", change.ID, strings.Join(change.Fields, ","))
	}
	if report.Truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d items per section; use --limit 0 for all\n", limit)
	}
	return exitOK
}

func (a app) catalogDiffFailure(jsonOut bool, side, path string, err error) int {
	if jsonOut {
		if code := a.writeJSON(map[string]any{
			"ok":      false,
			"error":   "request_failed",
			"side":    side,
			"path":    path,
			"message": err.Error(),
		}); code != exitOK {
			return code
		}
		return exitRequest
	}
	return a.fail(exitRequest, "failed to load %s registry %q: %v", side, path, err)
}

func (a app) catalogAudit(args []string, jsonOut bool) int {
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	sampleLimit, args, err := consumeInt(args, "--sample", 5)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog audit [--registry PATH] [--sample N] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the catalog audit report JSON to stdout")
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	audit := datago.AuditRegistry(reg, sampleLimit)
	report := datago.CatalogAuditReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Provider:    "data.go.kr",
		Registry:    registryPath,
		SampleLimit: sampleLimit,
		Audit:       audit,
	}
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":       true,
			"output":   output,
			"registry": registryPath,
			"report":   report,
			"audit":    audit,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog audit")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  specs: %d\n", audit.SpecsTotal)
	fmt.Fprintf(a.stdout, "  operations: %d\n", audit.OperationsTotal)
	fmt.Fprintf(a.stdout, "  callable operations: %d\n", audit.CallableOperations)
	fmt.Fprintf(a.stdout, "  specs without operations: %d\n", audit.SpecsWithoutOperations)
	fmt.Fprintf(a.stdout, "  specs without callable operation: %d\n", audit.SpecsWithoutCallableOperation)
	fmt.Fprintf(a.stdout, "  operations without endpoint: %d\n", audit.OperationsWithoutEndpoint)
	fmt.Fprintf(a.stdout, "  operations without request params: %d\n", audit.OperationsWithoutRequestParams)
	fmt.Fprintf(a.stdout, "  operations without response params: %d\n", audit.OperationsWithoutResponseParams)
	fmt.Fprintf(a.stdout, "  specs missing organization: %d\n", audit.SpecsMissingOrganization)
	fmt.Fprintf(a.stdout, "  specs missing source URL: %d\n", audit.SpecsMissingSourceURL)
	fmt.Fprintf(a.stdout, "  specs missing updated_at: %d\n", audit.SpecsMissingUpdatedAt)
	fmt.Fprintf(a.stdout, "  data.go.kr gateway operations: %d\n", audit.Dependency.DataGoKrGatewayOperations)
	fmt.Fprintf(a.stdout, "  external endpoint specs: %d\n", audit.Dependency.ExternalEndpointSpecs)
	fmt.Fprintf(a.stdout, "  gateway specs with external guide: %d\n", audit.Dependency.GatewayWithExternalGuideSpecs)
	fmt.Fprintf(a.stdout, "  service-root-only operations: %d\n", audit.Dependency.ServiceRootOnlyOperations)
	fmt.Fprintf(a.stdout, "  SOAP operations: %d\n", audit.Dependency.SOAPOperations)
	fmt.Fprintf(a.stdout, "  WMS operations: %d\n", audit.Dependency.WMSOperations)
	fmt.Fprintf(a.stdout, "  dev approval required operations: %d\n", audit.Dependency.DevApprovalRequiredOperations)
	fmt.Fprintf(a.stdout, "  prod approval required operations: %d\n", audit.Dependency.ProdApprovalRequiredOperations)
	return exitOK
}

func (a app) catalogErrors(args []string, jsonOut bool) int {
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 20)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog errors [--registry PATH] [--limit N] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the catalog error report JSON to stdout")
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	report := datago.AnalyzeCatalogErrors(reg, limit)
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	report.Registry = registryPath
	if output != "" {
		if err := writeJSONFile(output, report); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":       true,
			"output":   output,
			"registry": registryPath,
			"report":   report,
			"summary":  report.Summary,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog errors")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  operations: %d\n", report.Summary.OperationsTotal)
	fmt.Fprintf(a.stdout, "  operations with status fields: %d\n", report.Summary.OperationsWithStatusFields)
	fmt.Fprintf(a.stdout, "  distinct status fields: %d\n", report.Summary.DistinctStatusFieldNameRoles)
	for _, field := range limitErrorFields(report.StatusFields, 10) {
		fmt.Fprintf(a.stdout, "  %s (%s): %d operations\n", field.Name, field.Role, field.Operations)
	}
	return exitOK
}

func (a app) catalogProviders(args []string, jsonOut bool) int {
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 20)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	sampleLimit, args, err := consumeInt(args, "--sample", 3)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	statusFilter, args, err := consumeString(args, "--status", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	kindFilter, args, err := consumeString(args, "--kind", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	providerFilter, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog providers [--registry PATH] [--limit N] [--sample N] [--status STATUS] [--kind KIND] [--provider NAME] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the provider backlog report JSON to stdout")
	}
	filters, err := providerFilters(statusFilter, kindFilter, providerFilter)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	backlog := datago.ProviderBacklogForRegistryWithAdapters(reg, sampleLimit, defaultProviderHosts())
	filteredProviders := filterProviders(backlog.Providers, filters)
	truncated := limit > 0 && len(filteredProviders) > limit
	providers := limitProviders(filteredProviders, limit)
	report := datago.ProviderBacklogReport{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Provider:      "data.go.kr",
		Registry:      registryPath,
		Limit:         limit,
		Truncated:     truncated,
		Filters:       filters,
		FilteredCount: len(filteredProviders),
		Summary:       backlog.Summary,
		Providers:     providers,
	}
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":             true,
			"output":         output,
			"registry":       registryPath,
			"limit":          limit,
			"truncated":      truncated,
			"filters":        filters,
			"filtered_count": len(filteredProviders),
			"summary":        backlog.Summary,
			"providers":      providers,
			"report":         report,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog providers")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	if filters != nil {
		fmt.Fprintf(a.stdout, "  filtered providers: %d\n", len(filteredProviders))
	}
	fmt.Fprintf(a.stdout, "  hosts: %d\n", backlog.Summary.Hosts)
	fmt.Fprintf(a.stdout, "  data.go.kr gateway hosts: %d\n", backlog.Summary.DataGoKrGatewayHosts)
	fmt.Fprintf(a.stdout, "  external endpoint hosts: %d\n", backlog.Summary.ExternalEndpointHosts)
	fmt.Fprintf(a.stdout, "  external guide hosts: %d\n", backlog.Summary.ExternalGuideHosts)
	fmt.Fprintf(a.stdout, "  missing adapter hosts: %d\n", backlog.Summary.MissingAdapterHosts)
	fmt.Fprintf(a.stdout, "  needs adapter operations: %d\n", backlog.Summary.NeedsAdapterOperations)
	for _, provider := range providers {
		fmt.Fprintf(a.stdout, "- %s", provider.Host)
		if provider.Provider != "" {
			fmt.Fprintf(a.stdout, " [%s]", provider.Provider)
		}
		fmt.Fprintf(a.stdout, "  status=%s specs=%d ops=%d kinds=%s\n", provider.AdapterStatus, provider.Specs, provider.Operations, strings.Join(provider.Kinds, ","))
		if provider.ExternalEndpointOperations > 0 {
			fmt.Fprintf(a.stdout, "  external endpoint operations: %d\n", provider.ExternalEndpointOperations)
		}
		if provider.ExternalGuideSpecs > 0 {
			fmt.Fprintf(a.stdout, "  external guide specs: %d\n", provider.ExternalGuideSpecs)
		}
		if len(provider.SampleIDs) > 0 {
			fmt.Fprintf(a.stdout, "  sample ids: %s\n", strings.Join(provider.SampleIDs, ", "))
		}
	}
	if truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d providers; use --limit 0 for all\n", limit)
	}
	return exitOK
}

func (a app) catalogDependencies(args []string, jsonOut bool) int {
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 50)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	statusFilter, args, err := consumeString(args, "--status", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	kindFilter, args, err := consumeString(args, "--kind", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	providerFilter, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	hostFilter, args, err := consumeString(args, "--host", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog dependencies [--registry PATH] [--limit N] [--kind KIND] [--status STATUS] [--provider NAME] [--host HOST] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the dependency inventory JSON to stdout")
	}
	filters, err := dependencyFilters(providerFilter, hostFilter, kindFilter, statusFilter)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	summary, dependencies := datago.DependencyInventoryForRegistry(reg, defaultProviderHosts())
	filteredDependencies := datago.FilterDependencyOperations(dependencies, filters)
	truncated := limit > 0 && len(filteredDependencies) > limit
	limitedDependencies := limitDependencies(filteredDependencies, limit)
	report := datago.DependencyInventoryReport{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Provider:      "data.go.kr",
		Registry:      registryPath,
		Limit:         limit,
		Truncated:     truncated,
		Filters:       filters,
		FilteredCount: len(filteredDependencies),
		Summary:       summary,
		Dependencies:  limitedDependencies,
	}
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":             true,
			"output":         output,
			"registry":       registryPath,
			"limit":          limit,
			"truncated":      truncated,
			"filters":        filters,
			"filtered_count": len(filteredDependencies),
			"summary":        summary,
			"dependencies":   limitedDependencies,
			"report":         report,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog dependencies")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	if filters != nil {
		fmt.Fprintf(a.stdout, "  filtered operations: %d\n", len(filteredDependencies))
	}
	fmt.Fprintf(a.stdout, "  operations: %d\n", summary.OperationsTotal)
	fmt.Fprintf(a.stdout, "  data.go.kr gateway operations: %d\n", summary.DataGoKrGatewayOperations)
	fmt.Fprintf(a.stdout, "  external endpoint operations: %d\n", summary.ExternalEndpointOps)
	fmt.Fprintf(a.stdout, "  missing adapter operations: %d\n", summary.MissingAdapterOps)
	for _, dep := range limitedDependencies {
		fmt.Fprintf(a.stdout, "- %s %s  kind=%s status=%s", dep.DatasetID, dep.Operation, dep.DependencyClass, dep.AdapterStatus)
		if dep.ProviderFamily != "" {
			fmt.Fprintf(a.stdout, " provider=%s", dep.ProviderFamily)
		}
		if dep.EndpointHost != "" {
			fmt.Fprintf(a.stdout, " host=%s", dep.EndpointHost)
		}
		fmt.Fprintln(a.stdout)
	}
	if truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d operations; use --limit 0 for all\n", limit)
	}
	return exitOK
}

func (a app) catalogAdapterTargets(args []string, jsonOut bool) int {
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 20)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	sampleLimit, args, err := consumeInt(args, "--sample", 3)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	providerFilter, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	hostFilter, args, err := consumeString(args, "--host", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	kindFilter, args, err := consumeString(args, "--kind", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog adapter-targets [--registry PATH] [--limit N] [--sample N] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the adapter target report JSON to stdout")
	}
	filters, err := adapterTargetFilters(providerFilter, hostFilter, kindFilter)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	summary, targets := datago.AdapterTargetsForRegistry(reg, defaultProviderHosts(), sampleLimit)
	filteredTargets := datago.FilterAdapterTargets(targets, filters)
	truncated := limit > 0 && len(filteredTargets) > limit
	limitedTargets := limitAdapterTargets(filteredTargets, limit)
	report := datago.AdapterTargetReport{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Provider:      "data.go.kr",
		Registry:      registryPath,
		Limit:         limit,
		Truncated:     truncated,
		Filters:       filters,
		FilteredCount: len(filteredTargets),
		Summary:       summary,
		Targets:       limitedTargets,
	}
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":             true,
			"output":         output,
			"registry":       registryPath,
			"limit":          limit,
			"truncated":      truncated,
			"filters":        filters,
			"filtered_count": len(filteredTargets),
			"summary":        summary,
			"targets":        limitedTargets,
			"report":         report,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog adapter targets")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	if filters != nil {
		fmt.Fprintf(a.stdout, "  filtered targets: %d\n", len(filteredTargets))
	}
	fmt.Fprintf(a.stdout, "  target hosts: %d\n", summary.TargetHosts)
	fmt.Fprintf(a.stdout, "  target operations: %d\n", summary.TargetOperations)
	for _, target := range limitedTargets {
		fmt.Fprintf(a.stdout, "- #%d %s ops=%d specs=%d kinds=%s", target.Rank, target.Host, target.Operations, target.Specs, strings.Join(target.Kinds, ","))
		if target.ProviderFamily != "" {
			fmt.Fprintf(a.stdout, " provider=%s", target.ProviderFamily)
		}
		fmt.Fprintln(a.stdout)
	}
	if truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d targets; use --limit 0 for all\n", limit)
	}
	return exitOK
}

func (a app) catalogVerify(args []string, jsonOut bool) int {
	if len(args) > 0 && args[0] == "summary" {
		return a.catalogVerifySummary(args[1:], jsonOut)
	}
	input, args, err := consumeString(args, "--input", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	ref, args, err := consumeString(args, "--ref", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	operation, args, err := consumeString(args, "--operation", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limitRaw, args, err := consumeString(args, "--limit", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit := 10
	if input != "" {
		limit = 0
	}
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 0 {
			return a.fail(exitUsage, "--limit requires a non-negative integer")
		}
		limit = parsed
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	statusFilter, args, err := consumeString(args, "--status", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	providerFilter, args, err := consumeString(args, "--provider", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	hostFilter, args, err := consumeString(args, "--host", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	kindFilter, args, err := consumeString(args, "--kind", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if ref == "" && len(args) > 0 {
		ref = args[0]
		args = args[1:]
	}
	if operation != "" && ref == "" {
		return a.fail(exitUsage, "--operation requires --ref or a positional ref")
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog verify [--registry PATH] [--ref REF] [--operation NAME] [--limit N] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json]\n       datapan catalog verify --input REPORT [--status STATUS] [--limit N] [--json]\n       datapan catalog verify summary --input REPORT [--limit N] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the verification report JSON to stdout")
	}
	if statusFilter != "" && !validVerificationStatus(statusFilter) {
		return a.fail(exitUsage, "--status must be one of: verified, failed, skipped, unknown")
	}
	if input != "" {
		if registryPath != "" || ref != "" || operation != "" || providerFilter != "" || hostFilter != "" || kindFilter != "" {
			return a.fail(exitUsage, "--input cannot be combined with --registry, --ref, positional ref, --operation, --provider, --host, or --kind")
		}
		return a.catalogVerifyInput(input, output, statusFilter, limit, jsonOut)
	}
	candidateFilters, reportFilters, err := a.verificationFilters(providerFilter, hostFilter, kindFilter)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	reg := a.reg
	if registryPath != "" {
		loaded, err := datago.LoadRegistry(registryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
		}
		reg = loaded
	}
	candidates, truncated, err := datago.VerificationCandidatesWithFilters(reg, ref, operation, limit, candidateFilters)
	if err != nil {
		var resolveErr datago.VerificationResolveError
		if errors.As(err, &resolveErr) {
			if resolveErr.Status() == datago.ResolveAmbiguous {
				return a.mapError(errAmbiguous{ref: resolveErr.Ref(), candidates: resolveErr.Candidates()}, jsonOut)
			}
			return a.mapError(errNotFound{resolveErr.Ref()}, jsonOut)
		}
		return a.fail(exitUsage, "%v", err)
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]datago.VerificationResult, 0, len(candidates))
	authMissing := false
	providerRegistry, _ := providers.DefaultRegistry()
	for _, candidate := range candidates {
		if adapter, ok := providerRegistry.MatchHost(candidate.EndpointHost); ok && candidate.DependencyClass == "external_endpoint" {
			keyName, key, ok := a.resolveKeyValue()
			if !ok {
				authMissing = true
				results = append(results, datago.NewSkippedVerificationResult(candidate, "missing_auth"))
				continue
			}
			results = append(results, adapter.Verify(context.Background(), providers.VerificationRequest{
				Spec:          candidate.Spec,
				Operation:     candidate.Operation,
				Params:        candidate.Params,
				MissingParams: candidate.MissingParams,
				Credential:    providers.Credential{Name: keyName, Value: key},
				HTTP:          a.http,
				VerifiedAt:    generatedAt,
			}))
			continue
		}
		if candidate.SkipReason != "" {
			results = append(results, datago.NewSkippedVerificationResult(candidate, ""))
			continue
		}
		if _, _, ok := a.resolveKeyValue(); !ok {
			authMissing = true
			results = append(results, datago.NewSkippedVerificationResult(candidate, "missing_auth"))
			continue
		}
		plan, _, err := a.requestPlanForOperation(candidate.Spec, candidate.Operation, candidate.Params)
		if err != nil {
			results = append(results, datago.VerificationResult{
				DatasetID:       candidate.Spec.ID,
				Title:           candidate.Spec.Title,
				Operation:       candidate.Operation.Name,
				Provider:        candidate.Spec.Provider,
				EndpointHost:    candidate.EndpointHost,
				DependencyClass: candidate.DependencyClass,
				Status:          "failed",
				Reason:          err.Error(),
				VerifiedAt:      generatedAt,
				Params:          candidate.Params,
			})
			continue
		}
		envelope, err := a.execute(plan)
		if err != nil {
			results = append(results, datago.VerificationResult{
				DatasetID:       candidate.Spec.ID,
				Title:           candidate.Spec.Title,
				Operation:       candidate.Operation.Name,
				Provider:        candidate.Spec.Provider,
				EndpointHost:    candidate.EndpointHost,
				DependencyClass: candidate.DependencyClass,
				Status:          "failed",
				Reason:          err.Error(),
				VerifiedAt:      generatedAt,
				URL:             plan.RedactedURL,
				Params:          candidate.Params,
			})
			continue
		}
		status := "verified"
		reason := ""
		if !envelope.OK {
			status = "failed"
			reason = envelope.Message
		}
		results = append(results, datago.VerificationResult{
			DatasetID:       candidate.Spec.ID,
			Title:           candidate.Spec.Title,
			Operation:       candidate.Operation.Name,
			Provider:        candidate.Spec.Provider,
			EndpointHost:    candidate.EndpointHost,
			DependencyClass: candidate.DependencyClass,
			Status:          status,
			Reason:          reason,
			VerifiedAt:      generatedAt,
			HTTPStatus:      envelope.StatusCode,
			SemanticStatus:  envelope.SemanticStatus,
			ProviderStatus:  envelope.ProviderStatus,
			URL:             envelope.URL,
			Params:          candidate.Params,
			BodyShape:       verificationBodyShape(envelope),
		})
	}
	report := datago.VerificationReport{
		GeneratedAt:   generatedAt,
		Provider:      "data.go.kr",
		Registry:      registryPath,
		Ref:           ref,
		Operation:     operation,
		Limit:         limit,
		Truncated:     truncated,
		Filters:       reportFilters,
		FilteredCount: len(results),
		Results:       results,
	}
	report.Summary = datago.SummarizeVerification(results)
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return verificationExitCode(report.Summary, authMissing)
		}
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{
			"ok":     !authMissing && report.Summary.Failed == 0,
			"output": output,
			"report": report,
		}); code != exitOK {
			return code
		}
		return verificationExitCode(report.Summary, authMissing)
	}
	fmt.Fprintln(a.stdout, "Catalog verification")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
	}
	if ref != "" {
		fmt.Fprintf(a.stdout, "  ref: %s\n", ref)
	}
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  verified: %d\n", report.Summary.Verified)
	fmt.Fprintf(a.stdout, "  failed: %d\n", report.Summary.Failed)
	fmt.Fprintf(a.stdout, "  skipped: %d\n", report.Summary.Skipped)
	if truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d operations; use --limit 0 for all\n", limit)
	}
	for _, result := range results {
		fmt.Fprintf(a.stdout, "- %s %s %s", result.Status, result.DatasetID, result.Operation)
		if result.Reason != "" {
			fmt.Fprintf(a.stdout, " (%s)", result.Reason)
		}
		fmt.Fprintln(a.stdout)
	}
	return verificationExitCode(report.Summary, authMissing)
}

func (a app) catalogRelease(args []string, jsonOut bool) int {
	if len(args) == 0 {
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--previous-registry PATH] [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]\n       datapan catalog release verify --manifest PATH [--output PATH|-] [--json]\n       datapan catalog release readiness --manifest PATH [--output PATH|-] [--json]")
	}
	switch args[0] {
	case "draft":
		return a.catalogReleaseDraft(args[1:], jsonOut)
	case "verify":
		return a.catalogReleaseVerify(args[1:], jsonOut)
	case "readiness":
		return a.catalogReleaseReadiness(args[1:], jsonOut)
	default:
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--previous-registry PATH] [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]\n       datapan catalog release verify --manifest PATH [--output PATH|-] [--json]\n       datapan catalog release readiness --manifest PATH [--output PATH|-] [--json]")
	}
}

func (a app) catalogReleaseDraft(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	registryPath, args, err := consumeString(args, "--registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	previousRegistryPath, args, err := consumeString(args, "--previous-registry", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	outputDir, args, err := consumeString(args, "--output-dir", ".datapan/release")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	verificationPath, args, err := consumeString(args, "--verification", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	providerLimit, args, err := consumeInt(args, "--provider-limit", 0)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if registryPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--previous-registry PATH] [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]")
	}
	reg, err := datago.LoadRegistry(registryPath)
	if err != nil {
		return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
	}
	return a.writeReleaseDraft(reg, registryPath, previousRegistryPath, outputDir, verificationPath, providerLimit, jsonOut)
}

func (a app) catalogReleaseVerify(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	manifestPath, args, err := consumeString(args, "--manifest", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if manifestPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog release verify --manifest PATH [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the release verification report JSON to stdout")
	}
	report, err := verifyReleaseManifest(manifestPath)
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if output != "" {
		if err := writeJSONFile(output, report); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			if !report.OK {
				return exitRequest
			}
			return exitOK
		}
	}
	if jsonOut {
		code := exitOK
		if !report.OK {
			code = exitRequest
		}
		if writeCode := a.writeJSON(map[string]any{
			"ok":     report.OK,
			"output": output,
			"report": report,
		}); writeCode != exitOK {
			return writeCode
		}
		return code
	}
	fmt.Fprintln(a.stdout, "Release manifest verification")
	fmt.Fprintf(a.stdout, "  manifest: %s\n", report.Manifest)
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  checked: %d\n", report.Checked)
	fmt.Fprintf(a.stdout, "  failed: %d\n", report.Failed)
	for _, result := range report.Results {
		if result.Status == "failed" {
			fmt.Fprintf(a.stdout, "- failed %s: %s\n", result.Path, result.Reason)
		}
	}
	if !report.OK {
		return exitRequest
	}
	return exitOK
}

func (a app) catalogReleaseReadiness(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	manifestPath, args, err := consumeString(args, "--manifest", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if manifestPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog release readiness --manifest PATH [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the release readiness report JSON to stdout")
	}
	report, err := releaseReadinessReportForManifest(manifestPath, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if output != "" {
		if err := writeJSONFile(output, report); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			if !report.Ready {
				return exitRequest
			}
			return exitOK
		}
	}
	if jsonOut {
		code := exitOK
		if !report.Ready {
			code = exitRequest
		}
		if writeCode := a.writeJSON(map[string]any{
			"ok":     report.Ready,
			"output": output,
			"report": report,
		}); writeCode != exitOK {
			return writeCode
		}
		return code
	}
	fmt.Fprintln(a.stdout, "Release readiness")
	fmt.Fprintf(a.stdout, "  manifest: %s\n", report.Manifest)
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  ready: %t\n", report.Ready)
	fmt.Fprintf(a.stdout, "  passed: %d\n", report.Summary.Passed)
	fmt.Fprintf(a.stdout, "  warned: %d\n", report.Summary.Warned)
	fmt.Fprintf(a.stdout, "  failed: %d\n", report.Summary.Failed)
	for _, gate := range report.Gates {
		if gate.Status != "pass" {
			fmt.Fprintf(a.stdout, "- %s %s: %s\n", gate.Status, gate.ID, gate.Message)
		}
	}
	if !report.Ready {
		return exitRequest
	}
	return exitOK
}

func (a app) writeReleaseDraft(reg datago.Registry, registryPath, previousRegistryPath, outputDir, verificationPath string, providerLimit int, jsonOut bool) int {
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	paths := releaseDraftPaths(outputDir)
	for _, dir := range []string{paths.SchemaDir, paths.DataDir, paths.ReportsDir, paths.ProvenanceDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return a.releaseFailure(jsonOut, err)
		}
	}
	schemaSources, err := datapanSchemaSources()
	if err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	for _, schema := range schemaSources {
		if err := copyFile(schema, joinPath(paths.SchemaDir, schemaFileName(schema))); err != nil {
			return a.releaseFailure(jsonOut, err)
		}
	}
	schemaIndex, err := buildSchemaIndex(generatedAt, paths)
	if err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	if err := writeJSONFile(paths.SchemaIndexPath, schemaIndex); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	specs := reg.Specs()
	if err := writeRegistryAtomic(paths.RegistryPath, specs); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	providerRegistry, err := providers.DefaultRegistry()
	if err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	providerIndex := providerRegistry.IndexReport(generatedAt, version)
	if err := writeJSONFile(paths.ProviderIndexPath, providerIndex); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	diffWritten := false
	if previousRegistryPath != "" {
		previousReg, err := datago.LoadRegistry(previousRegistryPath)
		if err != nil {
			return a.catalogDiffFailure(jsonOut, "previous_registry", previousRegistryPath, err)
		}
		diff := datago.DiffRegistries(previousReg, reg)
		diffReport := datago.NewCatalogDiffReport(generatedAt, "data.go.kr", previousRegistryPath, paths.RegistryPath, 0, diff)
		if err := writeJSONFile(paths.CatalogDiffPath, diffReport); err != nil {
			return a.releaseFailure(jsonOut, err)
		}
		diffWritten = true
	}
	auditReport := datago.CatalogAuditReport{
		GeneratedAt: generatedAt,
		Provider:    "data.go.kr",
		Registry:    paths.RegistryPath,
		SampleLimit: 5,
		Audit:       datago.AuditRegistry(reg, 5),
	}
	if err := writeJSONFile(paths.CatalogAuditPath, auditReport); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	errorReport := datago.AnalyzeCatalogErrors(reg, 0)
	errorReport.GeneratedAt = generatedAt
	errorReport.Registry = paths.RegistryPath
	if err := writeJSONFile(paths.ErrorCatalogPath, errorReport); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	dependencySummary, dependencyOps := datago.DependencyInventoryForRegistry(reg, defaultProviderHosts())
	dependencyReport := datago.DependencyInventoryReport{
		GeneratedAt:   generatedAt,
		Provider:      "data.go.kr",
		Registry:      paths.RegistryPath,
		Limit:         0,
		FilteredCount: len(dependencyOps),
		Summary:       dependencySummary,
		Dependencies:  dependencyOps,
	}
	if err := writeJSONFile(paths.DependencyInventoryPath, dependencyReport); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	adapterTargetSummary, adapterTargets := datago.AdapterTargetsFromDependencies(dependencyOps, 3)
	adapterTargetReport := datago.AdapterTargetReport{
		GeneratedAt:   generatedAt,
		Provider:      "data.go.kr",
		Registry:      paths.RegistryPath,
		Limit:         0,
		FilteredCount: len(adapterTargets),
		Summary:       adapterTargetSummary,
		Targets:       adapterTargets,
	}
	if err := writeJSONFile(paths.AdapterTargetsPath, adapterTargetReport); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	backlog := datago.ProviderBacklogForRegistryWithAdapters(reg, 3, defaultProviderHosts())
	providers := limitProviders(backlog.Providers, providerLimit)
	providerReport := datago.ProviderBacklogReport{
		GeneratedAt:   generatedAt,
		Provider:      "data.go.kr",
		Registry:      paths.RegistryPath,
		Limit:         providerLimit,
		Truncated:     providerLimit > 0 && len(backlog.Providers) > providerLimit,
		FilteredCount: len(backlog.Providers),
		Summary:       backlog.Summary,
		Providers:     providers,
	}
	if err := writeJSONFile(paths.ProviderBacklogPath, providerReport); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	verificationCopied := false
	verificationSummaryWritten := false
	if verificationPath != "" {
		report, err := readVerificationReport(verificationPath)
		if err != nil {
			return a.releaseFailure(jsonOut, err)
		}
		if report.FilteredCount == 0 && len(report.Results) > 0 {
			report.FilteredCount = len(report.Results)
		}
		if err := writeJSONFile(paths.VerificationPath, report); err != nil {
			return a.releaseFailure(jsonOut, err)
		}
		summary := datago.SummarizeVerificationReport(report, paths.VerificationPath, 0)
		if err := writeJSONFile(paths.VerificationSummaryPath, summary); err != nil {
			return a.releaseFailure(jsonOut, err)
		}
		verificationCopied = true
		verificationSummaryWritten = true
	}
	provenance := releaseProvenance(generatedAt, registryPath, previousRegistryPath, verificationPath, providerLimit, paths)
	if err := writeOutput(paths.ProvenancePath, []byte(provenance), a.stdout); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	manifest, err := writeReleaseManifest(generatedAt, registryPath, diffWritten, verificationCopied, paths)
	if err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	payload := map[string]any{
		"ok":                           true,
		"output_dir":                   outputDir,
		"generated_at":                 generatedAt,
		"registry":                     paths.RegistryPath,
		"previous_registry":            previousRegistryPath,
		"provider_index":               paths.ProviderIndexPath,
		"schemas":                      datapanSchemaFileNames(),
		"catalog_diff":                 emptyIfFalse(paths.CatalogDiffPath, diffWritten),
		"catalog_audit":                paths.CatalogAuditPath,
		"error_catalog":                paths.ErrorCatalogPath,
		"dependencies":                 paths.DependencyInventoryPath,
		"adapter_targets":              paths.AdapterTargetsPath,
		"provider_backlog":             paths.ProviderBacklogPath,
		"verification":                 emptyIfFalse(paths.VerificationPath, verificationCopied),
		"verification_summary":         emptyIfFalse(paths.VerificationSummaryPath, verificationSummaryWritten),
		"provenance":                   paths.ProvenancePath,
		"manifest":                     paths.ManifestPath,
		"artifacts":                    manifest.ArtifactCount,
		"specs":                        len(specs),
		"providers":                    len(providers),
		"provider_truncated":           providerReport.Truncated,
		"verification_copied":          verificationCopied,
		"verification_summary_written": verificationSummaryWritten,
	}
	if jsonOut {
		return a.writeJSON(payload)
	}
	fmt.Fprintln(a.stdout, "Catalog release draft")
	fmt.Fprintf(a.stdout, "  output: %s\n", outputDir)
	fmt.Fprintf(a.stdout, "  registry: %s\n", paths.RegistryPath)
	if diffWritten {
		fmt.Fprintf(a.stdout, "  catalog diff: %s\n", paths.CatalogDiffPath)
	}
	fmt.Fprintf(a.stdout, "  provider index: %s\n", paths.ProviderIndexPath)
	fmt.Fprintf(a.stdout, "  catalog audit: %s\n", paths.CatalogAuditPath)
	fmt.Fprintf(a.stdout, "  error catalog: %s\n", paths.ErrorCatalogPath)
	fmt.Fprintf(a.stdout, "  dependencies: %s\n", paths.DependencyInventoryPath)
	fmt.Fprintf(a.stdout, "  adapter targets: %s\n", paths.AdapterTargetsPath)
	fmt.Fprintf(a.stdout, "  provider backlog: %s\n", paths.ProviderBacklogPath)
	if verificationCopied {
		fmt.Fprintf(a.stdout, "  verification: %s\n", paths.VerificationPath)
		fmt.Fprintf(a.stdout, "  verification summary: %s\n", paths.VerificationSummaryPath)
	}
	fmt.Fprintf(a.stdout, "  provenance: %s\n", paths.ProvenancePath)
	fmt.Fprintf(a.stdout, "  manifest: %s\n", paths.ManifestPath)
	fmt.Fprintf(a.stdout, "  specs: %d\n", len(specs))
	fmt.Fprintf(a.stdout, "  providers: %d\n", len(providers))
	return exitOK
}

func (a app) releaseFailure(jsonOut bool, err error) int {
	if jsonOut {
		if code := a.writeJSON(map[string]any{"ok": false, "error": "request_failed", "message": err.Error()}); code != exitOK {
			return code
		}
		return exitRequest
	}
	return a.fail(exitRequest, "%v", err)
}

func (a app) catalogVerifyInput(input, output, statusFilter string, limit int, jsonOut bool) int {
	data, err := readAllInput(input, os.Stdin)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	var report datago.VerificationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return a.fail(exitUsage, "verification report must be JSON: %v", err)
	}
	filtered := filterVerificationResults(report.Results, statusFilter)
	truncated := limit > 0 && len(filtered) > limit
	results := limitVerificationResults(filtered, limit)
	report.Results = results
	report.Summary = datago.SummarizeVerification(results)
	report.Limit = limit
	report.Truncated = truncated
	report.Filters = verificationReportFilters(statusFilter)
	report.FilteredCount = len(filtered)
	if output != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return verificationExitCode(report.Summary, false)
		}
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{
			"ok":             report.Summary.Failed == 0,
			"input":          input,
			"output":         output,
			"status":         statusFilter,
			"filtered_count": len(filtered),
			"truncated":      truncated,
			"report":         report,
		}); code != exitOK {
			return code
		}
		return verificationExitCode(report.Summary, false)
	}
	fmt.Fprintln(a.stdout, "Verification report")
	fmt.Fprintf(a.stdout, "  input: %s\n", input)
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	if statusFilter != "" {
		fmt.Fprintf(a.stdout, "  status: %s\n", statusFilter)
	}
	fmt.Fprintf(a.stdout, "  total: %d\n", report.Summary.Total)
	fmt.Fprintf(a.stdout, "  verified: %d\n", report.Summary.Verified)
	fmt.Fprintf(a.stdout, "  failed: %d\n", report.Summary.Failed)
	fmt.Fprintf(a.stdout, "  skipped: %d\n", report.Summary.Skipped)
	if truncated {
		fmt.Fprintf(a.stdout, "  output truncated to %d results; use --limit 0 for all\n", limit)
	}
	for _, result := range report.Results {
		fmt.Fprintf(a.stdout, "- %s %s %s", result.Status, result.DatasetID, result.Operation)
		if result.Reason != "" {
			fmt.Fprintf(a.stdout, " (%s)", result.Reason)
		}
		fmt.Fprintln(a.stdout)
	}
	return verificationExitCode(report.Summary, false)
}

func (a app) catalogVerifySummary(args []string, jsonOut bool) int {
	input, args, err := consumeString(args, "--input", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 20)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if input == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog verify summary --input REPORT [--limit N] [--output PATH|-] [--json]")
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the verification summary report JSON to stdout")
	}
	data, err := readAllInput(input, os.Stdin)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	var report datago.VerificationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return a.fail(exitUsage, "verification report must be JSON: %v", err)
	}
	summary := datago.SummarizeVerificationReport(report, input, limit)
	if output != "" {
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		data = append(data, '\n')
		if err := writeOutput(output, data, a.stdout); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
		if output == "-" {
			return exitOK
		}
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":      true,
			"input":   input,
			"output":  output,
			"summary": summary,
		})
	}
	fmt.Fprintln(a.stdout, "Verification summary")
	fmt.Fprintf(a.stdout, "  input: %s\n", input)
	if output != "" {
		fmt.Fprintf(a.stdout, "  output: %s\n", output)
	}
	fmt.Fprintf(a.stdout, "  total: %d\n", summary.Summary.Total)
	fmt.Fprintf(a.stdout, "  verified: %d\n", summary.Summary.Verified)
	fmt.Fprintf(a.stdout, "  failed: %d\n", summary.Summary.Failed)
	fmt.Fprintf(a.stdout, "  skipped: %d\n", summary.Summary.Skipped)
	for _, group := range summary.Groups.ByReason {
		fmt.Fprintf(a.stdout, "- reason %s: %d\n", group.Key, group.Count)
	}
	return exitOK
}

func (a app) info(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) != 1 {
		return a.fail(exitUsage, "usage: datapan show <ref> [--json]")
	}
	spec, code, ok := a.resolveOne(args[0], jsonOut)
	if !ok {
		return code
	}
	if jsonOut {
		return a.writeJSON(showPayload(spec))
	}
	fmt.Fprintf(a.stdout, "%s\n", spec.Title)
	fmt.Fprintf(a.stdout, "  list id: %s\n", spec.ID)
	fmt.Fprintf(a.stdout, "  provider: %s\n", spec.Provider)
	if spec.Organization != "" {
		fmt.Fprintf(a.stdout, "  organization: %s\n", spec.Organization)
	}
	if spec.SourceCategory != "" {
		fmt.Fprintf(a.stdout, "  source category: %s\n", spec.SourceCategory)
	}
	fmt.Fprintf(a.stdout, "  priority: %s\n", spec.Priority)
	fmt.Fprintf(a.stdout, "  application: %s\n", spec.ApplicationURL())
	fmt.Fprintf(a.stdout, "  env vars: %s\n", strings.Join(datago.KeyEnvNames, ", "))
	access := showAccessSummary(spec)
	if len(access) > 1 {
		fmt.Fprintln(a.stdout, "  access:")
		for _, key := range []string{"dev_approval", "prod_approval", "charge", "register_status", "request_count", "data_format", "updated_at"} {
			if value, ok := access[key]; ok && fmt.Sprint(value) != "" {
				fmt.Fprintf(a.stdout, "    %s: %v\n", key, value)
			}
		}
	}
	if len(spec.Operations) > 0 {
		fmt.Fprintln(a.stdout, "  operations:")
		for _, summary := range showOperationSummaries(spec) {
			opName := fmt.Sprint(summary["name"])
			fmt.Fprintf(a.stdout, "    - %s", opName)
			if endpoint, ok := summary["endpoint"].(string); ok && endpoint != "" {
				fmt.Fprintf(a.stdout, " (%s)", endpoint)
			}
			if callable, ok := summary["callable"].(bool); ok && !callable {
				fmt.Fprint(a.stdout, " [not callable yet]")
			}
			fmt.Fprintln(a.stdout)
			if params, ok := summary["request_params"].([]map[string]string); ok && len(params) > 0 {
				fmt.Fprint(a.stdout, "      params:")
				for _, param := range params {
					if param["label"] != "" {
						fmt.Fprintf(a.stdout, " %s(%s)", param["name"], param["label"])
					} else {
						fmt.Fprintf(a.stdout, " %s", param["name"])
					}
				}
				fmt.Fprintln(a.stdout)
			}
			if defaults, ok := summary["default_params"].(map[string]string); ok && len(defaults) > 0 {
				fmt.Fprintf(a.stdout, "      defaults: %s\n", formatParamMap(defaults))
			}
			if example, ok := summary["example"].(string); ok && example != "" {
				fmt.Fprintf(a.stdout, "      example: %s\n", example)
			}
		}
	}
	if example := exampleGetCommand(spec); example != "" {
		fmt.Fprintf(a.stdout, "  next get: %s\n", example)
	}
	return exitOK
}

func showPayload(spec datago.Spec) map[string]any {
	return map[string]any{
		"ok":         true,
		"spec":       spec,
		"access":     showAccessSummary(spec),
		"operations": showOperationSummaries(spec),
		"examples": map[string]string{
			"access": "datapan access " + spec.ID + " --start",
			"get":    exampleGetCommand(spec),
		},
	}
}

func showAccessSummary(spec datago.Spec) map[string]any {
	raw := specRaw(spec)
	out := map[string]any{
		"application_url": spec.ApplicationURL(),
	}
	for _, pair := range []struct {
		outKey string
		rawKey string
	}{
		{"dev_approval", "is_confirmed_for_dev_nm"},
		{"prod_approval", "is_confirmed_for_prod_nm"},
		{"charge", "is_charged"},
		{"register_status", "register_status"},
		{"request_count", "request_cnt"},
		{"data_format", "data_format"},
		{"updated_at", "updated_at"},
	} {
		if value, ok := raw[pair.rawKey]; ok && fmt.Sprint(value) != "" {
			out[pair.outKey] = value
		}
	}
	if sourceURL := sourceURL(spec); sourceURL != "" {
		out["source_url"] = sourceURL
	}
	return out
}

func showOperationSummaries(spec datago.Spec) []map[string]any {
	out := make([]map[string]any, 0, len(spec.Operations))
	for _, op := range spec.Operations {
		requestParams := nonAuthParams(op.RequestParams)
		authParams := authParamSummaries(op.RequestParams)
		item := map[string]any{
			"name":                  op.Name,
			"endpoint":              op.Endpoint,
			"callable":              op.Endpoint != "",
			"default_params":        op.DefaultParams,
			"request_params":        paramSummaries(requestParams),
			"response_params_count": len(op.ResponseParams),
			"example":               exampleCommandForOperation(spec, op),
		}
		if len(authParams) > 0 {
			item["auth_params"] = authParams
			item["auth_env_vars"] = datago.KeyEnvNames
		}
		if len(op.ResponseParams) > 0 {
			item["response_params_sample"] = paramSummaries(limitParams(op.ResponseParams, 10))
		}
		out = append(out, item)
	}
	return out
}

func authParamSummaries(params []datago.Param) []map[string]string {
	return paramSummaries(authParams(params))
}

func authParams(params []datago.Param) []datago.Param {
	out := make([]datago.Param, 0, len(params))
	for _, param := range params {
		if isAuthParam(param.Name) {
			out = append(out, param)
		}
	}
	return out
}

func nonAuthParams(params []datago.Param) []datago.Param {
	out := make([]datago.Param, 0, len(params))
	for _, param := range params {
		if !isAuthParam(param.Name) {
			out = append(out, param)
		}
	}
	return out
}

func isAuthParam(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", ""))
	return normalized == "servicekey" || normalized == "apikey" || normalized == "authkey"
}

func paramSummaries(params []datago.Param) []map[string]string {
	out := make([]map[string]string, 0, len(params))
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		item := map[string]string{"name": name}
		if label := strings.TrimSpace(param.Label); label != "" {
			item["label"] = label
		}
		out = append(out, item)
	}
	return out
}

func limitParams(params []datago.Param, limit int) []datago.Param {
	if limit > 0 && len(params) > limit {
		return params[:limit]
	}
	return params
}

func exampleGetCommand(spec datago.Spec) string {
	if smoke := spec.SmokeCommand(); smoke != "" {
		return smoke
	}
	if len(spec.Operations) == 0 {
		return ""
	}
	return exampleCommandForOperation(spec, spec.Operations[0])
}

func exampleCommandForOperation(spec datago.Spec, op datago.Operation) string {
	if spec.Smoke != nil && spec.Smoke.Operation == op.Name {
		if smoke := spec.SmokeCommand(); smoke != "" {
			return smoke
		}
	}
	if op.Endpoint == "" {
		return ""
	}
	args := []string{"datapan", "get", spec.ID}
	if op.Name != "" {
		args = append(args, "--operation", op.Name)
	}
	for _, param := range nonAuthParams(op.RequestParams) {
		name := strings.TrimSpace(param.Name)
		if name != "" {
			args = append(args, name+"=VALUE")
		}
	}
	args = append(args, "--json")
	return datago.CommandString(args)
}

func formatParamMap(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, " ")
}

func specRaw(spec datago.Spec) map[string]any {
	if spec.Source != nil && spec.Source.Raw != nil {
		return spec.Source.Raw
	}
	for _, op := range spec.Operations {
		if op.Source != nil && op.Source.Raw != nil {
			return op.Source.Raw
		}
	}
	return map[string]any{}
}

func sourceURL(spec datago.Spec) string {
	if spec.Source != nil && strings.TrimSpace(spec.Source.URL) != "" {
		return strings.TrimSpace(spec.Source.URL)
	}
	for _, op := range spec.Operations {
		if op.Source != nil && strings.TrimSpace(op.Source.URL) != "" {
			return strings.TrimSpace(op.Source.URL)
		}
	}
	return ""
}

func specSummaries(specs []datago.Spec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		item := map[string]any{
			"id":               spec.ID,
			"title":            spec.Title,
			"provider":         spec.Provider,
			"organization":     spec.Organization,
			"source_category":  spec.SourceCategory,
			"priority":         spec.Priority,
			"operations_count": len(spec.Operations),
		}
		if len(spec.SourceKeywords) > 0 {
			item["source_keywords"] = spec.SourceKeywords
		}
		if len(spec.SearchTerms) > 0 {
			item["search_terms"] = spec.SearchTerms
		}
		if spec.Description != "" {
			item["description"] = spec.Description
		}
		out = append(out, item)
	}
	return out
}

func limitSpecs(specs []datago.Spec, limit int) []datago.Spec {
	if limit <= 0 || len(specs) <= limit {
		return specs
	}
	return specs[:limit]
}

func limitChanges(changes []datago.SpecChange, limit int) []datago.SpecChange {
	if limit <= 0 || len(changes) <= limit {
		return changes
	}
	return changes[:limit]
}

func limitProviders(providers []datago.ProviderSummary, limit int) []datago.ProviderSummary {
	if limit <= 0 || len(providers) <= limit {
		return providers
	}
	return providers[:limit]
}

func limitDependencies(dependencies []datago.DependencyOperationSummary, limit int) []datago.DependencyOperationSummary {
	if limit <= 0 || len(dependencies) <= limit {
		return dependencies
	}
	return dependencies[:limit]
}

func limitAdapterTargets(targets []datago.AdapterTarget, limit int) []datago.AdapterTarget {
	if limit <= 0 || len(targets) <= limit {
		return targets
	}
	return targets[:limit]
}

func limitErrorFields(fields []datago.CatalogErrorFieldStat, limit int) []datago.CatalogErrorFieldStat {
	if limit <= 0 || len(fields) <= limit {
		return fields
	}
	return fields[:limit]
}

func providerFilters(status, kind, provider string) (*datago.ProviderBacklogFilters, error) {
	status = strings.TrimSpace(status)
	kind = strings.TrimSpace(kind)
	provider = strings.TrimSpace(provider)
	if status == "" && kind == "" && provider == "" {
		return nil, nil
	}
	if status != "" && status != "missing" && status != "adapter" && status != "builtin" && status != "guide_only" {
		return nil, fmt.Errorf("--status must be one of: missing, adapter, builtin, guide_only")
	}
	if kind != "" && kind != "data_go_kr_gateway" && kind != "external_endpoint" && kind != "external_guide" && kind != "service_root" {
		return nil, fmt.Errorf("--kind must be one of: data_go_kr_gateway, external_endpoint, external_guide, service_root")
	}
	return &datago.ProviderBacklogFilters{Status: status, Kind: kind, Provider: provider}, nil
}

func dependencyFilters(providerName, host, kind, status string) (*datago.DependencyInventoryFilters, error) {
	providerName = strings.TrimSpace(providerName)
	host = strings.ToLower(strings.TrimSpace(host))
	kind = strings.TrimSpace(kind)
	status = strings.TrimSpace(status)
	if providerName == "" && host == "" && kind == "" && status == "" {
		return nil, nil
	}
	if status != "" && status != "missing" && status != "adapter" && status != "builtin" && status != "not_applicable" {
		return nil, fmt.Errorf("--status must be one of: missing, adapter, builtin, not_applicable")
	}
	if kind != "" &&
		kind != "data_go_kr_gateway" &&
		kind != "external_endpoint" &&
		kind != "service_root" &&
		kind != "no_endpoint" &&
		kind != "malformed_endpoint" &&
		kind != "soap" &&
		kind != "wms" {
		return nil, fmt.Errorf("--kind must be one of: data_go_kr_gateway, external_endpoint, service_root, no_endpoint, malformed_endpoint, soap, wms")
	}
	return &datago.DependencyInventoryFilters{Provider: providerName, Host: host, Kind: kind, Status: status}, nil
}

func adapterTargetFilters(providerName, host, kind string) (*datago.AdapterTargetFilters, error) {
	providerName = strings.TrimSpace(providerName)
	host = strings.ToLower(strings.TrimSpace(host))
	kind = strings.TrimSpace(kind)
	if providerName == "" && host == "" && kind == "" {
		return nil, nil
	}
	if kind != "" && kind != "external_endpoint" && kind != "service_root" {
		return nil, fmt.Errorf("--kind must be one of: external_endpoint, service_root")
	}
	return &datago.AdapterTargetFilters{Provider: providerName, Host: host, Kind: kind}, nil
}

func defaultProviderHosts() []string {
	registry, err := providers.DefaultRegistry()
	if err != nil {
		return nil
	}
	return registry.Hosts()
}

func filterProviders(providers []datago.ProviderSummary, filters *datago.ProviderBacklogFilters) []datago.ProviderSummary {
	if filters == nil {
		return providers
	}
	out := make([]datago.ProviderSummary, 0, len(providers))
	for _, provider := range providers {
		if filters.Status != "" && provider.AdapterStatus != filters.Status {
			continue
		}
		if filters.Kind != "" && !providerHasKind(provider, filters.Kind) {
			continue
		}
		if filters.Provider != "" && !strings.EqualFold(provider.Provider, filters.Provider) {
			continue
		}
		out = append(out, provider)
	}
	return out
}

func providerHasKind(provider datago.ProviderSummary, kind string) bool {
	for _, candidate := range provider.Kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func diffTruncated(diff datago.CatalogDiff, limit int) bool {
	if limit <= 0 {
		return false
	}
	return len(diff.Added) > limit || len(diff.Removed) > limit || len(diff.Changed) > limit
}

func verificationExitCode(summary datago.VerificationSummary, authMissing bool) int {
	if authMissing {
		return exitAuth
	}
	if summary.Failed > 0 {
		return exitRequest
	}
	return exitOK
}

func validVerificationStatus(status string) bool {
	switch status {
	case "verified", "failed", "skipped", "unknown":
		return true
	default:
		return false
	}
}

func (a app) verificationFilters(providerName, host, kind string) (datago.VerificationCandidateFilters, *datago.VerificationReportFilters, error) {
	providerName = strings.TrimSpace(providerName)
	host = strings.ToLower(strings.TrimSpace(host))
	kind = strings.TrimSpace(kind)
	if kind != "" && !validVerificationKind(kind) {
		return datago.VerificationCandidateFilters{}, nil, fmt.Errorf("--kind must be one of: data_go_kr_gateway, external_endpoint, service_root, no_endpoint, malformed_endpoint, soap, wms")
	}
	if providerName == "" && host == "" && kind == "" {
		return datago.VerificationCandidateFilters{}, nil, nil
	}
	var hosts []string
	providerRegistry, err := providers.DefaultRegistry()
	if err != nil {
		return datago.VerificationCandidateFilters{}, nil, err
	}
	if providerName != "" {
		for _, adapter := range providerRegistry.Adapters() {
			if strings.EqualFold(adapter.Name(), providerName) {
				hosts = adapter.Hosts()
				break
			}
		}
		if len(hosts) == 0 {
			return datago.VerificationCandidateFilters{}, nil, fmt.Errorf("--provider %q is not a registered provider adapter", providerName)
		}
	}
	if host != "" {
		if providerName != "" {
			adapter, ok := providerRegistry.MatchHost(host)
			if !ok || !strings.EqualFold(adapter.Name(), providerName) {
				return datago.VerificationCandidateFilters{}, nil, fmt.Errorf("--host %s is not owned by provider %s", host, providerName)
			}
		}
		hosts = []string{host}
	}
	reportFilters := &datago.VerificationReportFilters{Provider: providerName, Host: host, Kind: kind}
	return datago.VerificationCandidateFilters{Hosts: hosts, Kind: kind}, reportFilters, nil
}

func validVerificationKind(kind string) bool {
	switch kind {
	case "data_go_kr_gateway", "external_endpoint", "service_root", "no_endpoint", "malformed_endpoint", "soap", "wms":
		return true
	default:
		return false
	}
}

func filterVerificationResults(results []datago.VerificationResult, status string) []datago.VerificationResult {
	if status == "" {
		return results
	}
	filtered := make([]datago.VerificationResult, 0, len(results))
	for _, result := range results {
		if result.Status == status {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func verificationReportFilters(status string) *datago.VerificationReportFilters {
	if status == "" {
		return nil
	}
	return &datago.VerificationReportFilters{Status: status}
}

func limitVerificationResults(results []datago.VerificationResult, limit int) []datago.VerificationResult {
	if limit <= 0 || len(results) <= limit {
		return results
	}
	return results[:limit]
}

func verificationBodyShape(envelope datago.ResponseEnvelope) string {
	if rows, err := datago.RowsFromJSON([]byte(envelope.Body)); err == nil {
		return fmt.Sprintf("rows:%d", len(rows))
	}
	if strings.Contains(strings.ToLower(envelope.ContentType), "json") {
		return "json"
	}
	if strings.Contains(strings.ToLower(envelope.ContentType), "xml") {
		return "xml"
	}
	if strings.Contains(strings.ToLower(envelope.ContentType), "html") {
		return "html"
	}
	if envelope.Body == "" {
		return "empty"
	}
	return "raw"
}

func (a app) resolveOne(ref string, jsonOut bool) (datago.Spec, int, bool) {
	result := a.reg.Resolve(ref, 10)
	switch result.Status {
	case datago.ResolveFound:
		return result.Spec, exitOK, true
	case datago.ResolveAmbiguous:
		payload := map[string]any{
			"ok":         false,
			"error":      "ambiguous_ref",
			"ref":        ref,
			"candidates": specSummaries(result.Candidates),
		}
		if jsonOut {
			_ = a.writeJSON(payload)
		} else {
			fmt.Fprintf(a.stderr, "ambiguous data.go.kr ref %q; candidates:\n", ref)
			for _, spec := range result.Candidates {
				fmt.Fprintf(a.stderr, "  %s  %s", spec.ID, spec.Title)
				if spec.Organization != "" {
					fmt.Fprintf(a.stderr, "  (%s)", spec.Organization)
				}
				fmt.Fprintln(a.stderr)
			}
		}
		return datago.Spec{}, exitAmbiguous, false
	default:
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":    false,
				"error": "not_found",
				"ref":   ref,
			}); code != exitOK {
				return datago.Spec{}, code, false
			}
			return datago.Spec{}, exitNotFound, false
		}
		return datago.Spec{}, a.fail(exitNotFound, "unknown data.go.kr ref %q", ref), false
	}
}

func (a app) auth(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) != 1 || args[0] != "check" {
		return a.fail(exitUsage, "usage: datapan auth check [--json]")
	}
	name, ok := a.resolveKey()
	status := map[string]any{
		"ok":                 ok,
		"provider":           "data.go.kr",
		"selected_env_var":   name,
		"accepted_env_vars":  datago.KeyEnvNames,
		"credential_present": ok,
	}
	if jsonOut {
		if code := a.writeJSON(status); code != exitOK {
			return code
		}
		if !ok {
			return exitAuth
		}
		return exitOK
	}
	if ok {
		fmt.Fprintf(a.stdout, "data.go.kr API key found in %s.\n", name)
		return exitOK
	}
	fmt.Fprintf(a.stdout, "No data.go.kr API key found. Set one of: %s\n", strings.Join(datago.KeyEnvNames, ", "))
	return exitAuth
}

func (a app) access(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) > 0 {
		switch args[0] {
		case "login":
			return a.accessLogin(args[1:], jsonOut)
		case "request", "submit":
			return a.accessRequest(args[1:], jsonOut)
		}
	}
	if hasAnyArg(args, "--dry-run", "--apply", "--profile-dir", "--storage-state", "--browser-path", "--output") {
		return a.accessRequest(args, jsonOut)
	}
	openBrowser, args := consumeBool(args, "--open")
	copyPurpose, args := consumeBool(args, "--copy-purpose")
	start, args := consumeBool(args, "--start")
	showPurpose, args := consumeBool(args, "--purpose")
	if start {
		openBrowser = true
		copyPurpose = true
		showPurpose = true
	}
	if len(args) != 1 {
		return a.fail(exitUsage, "usage: datapan access <ref> [--open] [--copy-purpose] [--start] [--purpose] [--json]")
	}
	spec, code, ok := a.resolveOne(args[0], jsonOut)
	if !ok {
		return code
	}
	opened := false
	if openBrowser {
		if err := openURLFunc(spec.ApplicationURL()); err != nil {
			if jsonOut {
				if code := a.writeJSON(map[string]any{
					"ok":              false,
					"error":           "open_failed",
					"provider":        "data.go.kr",
					"list_id":         spec.ID,
					"title":           spec.Title,
					"application_url": spec.ApplicationURL(),
					"message":         err.Error(),
				}); code != exitOK {
					return code
				}
				return exitRequest
			}
			return a.fail(exitRequest, "failed to open browser: %v", err)
		}
		opened = true
	}
	copied := false
	copyError := ""
	if copyPurpose {
		if err := copyToClipboardFunc(datago.PurposeTextKO); err != nil {
			copyError = err.Error()
			if jsonOut {
				if code := a.writeJSON(map[string]any{
					"ok":              false,
					"error":           "copy_failed",
					"provider":        "data.go.kr",
					"list_id":         spec.ID,
					"title":           spec.Title,
					"application_url": spec.ApplicationURL(),
					"opened":          opened,
					"purpose_copied":  false,
					"purpose_text":    datago.PurposeTextKO,
					"message":         err.Error(),
				}); code != exitOK {
					return code
				}
				return exitRequest
			}
		} else {
			copied = true
		}
	}
	smokeCommand := exampleGetCommand(spec)
	nextSteps := applyNextSteps(spec, smokeCommand)
	payload := map[string]any{
		"ok":              true,
		"provider":        "data.go.kr",
		"list_id":         spec.ID,
		"title":           spec.Title,
		"application_url": spec.ApplicationURL(),
		"opened":          opened,
		"purpose_copied":  copied,
		"purpose_text":    datago.PurposeTextKO,
		"smoke_command":   smokeCommand,
		"next_steps":      nextSteps,
		"notes": []string{
			"Do not paste API keys into issues, logs, screenshots, or chat.",
			"data.go.kr usually requires per-service usage approval even when a generic service key exists.",
		},
	}
	if copyError != "" {
		payload["copy_error"] = copyError
	}
	if jsonOut {
		return a.writeJSON(payload)
	}
	fmt.Fprintf(a.stdout, "%s\n", spec.Title)
	fmt.Fprintf(a.stdout, "Application page: %s\n", spec.ApplicationURL())
	if opened {
		fmt.Fprintln(a.stdout, "Opened application page in your browser.")
	}
	if copied {
		fmt.Fprintln(a.stdout, "Copied purpose text to clipboard.")
	} else if copyError != "" {
		fmt.Fprintf(a.stdout, "Could not copy purpose text: %s\n", copyError)
	}
	if showPurpose || !openBrowser {
		fmt.Fprintf(a.stdout, "\nPurpose text:\n%s\n", datago.PurposeTextKO)
	}
	fmt.Fprintln(a.stdout, "\nNext steps:")
	for i, step := range nextSteps {
		fmt.Fprintf(a.stdout, "  %d. %s\n", i+1, step)
	}
	if smokeCommand != "" {
		fmt.Fprintf(a.stdout, "\nAfter approval smoke:\n  %s\n", smokeCommand)
	}
	return exitOK
}

func (a app) accessLogin(args []string, jsonOut bool) int {
	_ = jsonOut
	_, args = consumeBool(args, "--json")
	headed, args := consumeBool(args, "--headed")
	profileDir, args, err := consumeString(args, "--profile-dir", defaultBrowserProfilePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	storageState, args, err := consumeString(args, "--storage-state", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if storageState != "" {
		profileDir = storageState
	}
	browserPath, args, err := consumeString(args, "--browser-path", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if browserPath == "" {
		if value, ok := a.env.LookupEnv("DATAPAN_BROWSER_PATH"); ok {
			browserPath = strings.TrimSpace(value)
		}
	}
	waitMS, args, err := consumeInt(args, "--manual-login-wait-ms", 120000)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan access login [--headed] [--manual-login-wait-ms N] [--profile-dir PATH] [--browser-path PATH] [--json]")
	}
	return runBrowserWorkflow(browserWorkflowOptions{
		Command:     "login",
		ProfileDir:  profileDir,
		BrowserPath: browserPath,
		ManualWait:  time.Duration(waitMS) * time.Millisecond,
		Headed:      headed,
	}, a.stdout, a.stderr)
}

func (a app) accessRequest(args []string, jsonOut bool) int {
	_ = jsonOut
	_, args = consumeBool(args, "--json")
	apply, args := consumeBool(args, "--apply")
	dryRun, args := consumeBool(args, "--dry-run")
	if apply && dryRun {
		return a.fail(exitUsage, "use either --dry-run or --apply, not both")
	}
	profileDir, args, err := consumeString(args, "--profile-dir", defaultBrowserProfilePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	storageState, args, err := consumeString(args, "--storage-state", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if storageState != "" {
		profileDir = storageState
	}
	browserPath, args, err := consumeString(args, "--browser-path", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if browserPath == "" {
		if value, ok := a.env.LookupEnv("DATAPAN_BROWSER_PATH"); ok {
			browserPath = strings.TrimSpace(value)
		}
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) != 1 {
		return a.fail(exitUsage, "usage: datapan access <ref> [--dry-run|--apply] [--profile-dir PATH] [--browser-path PATH] [--output PATH] [--json]")
	}
	spec, code, ok := a.resolveOne(args[0], jsonOut)
	if !ok {
		return code
	}
	return runBrowserWorkflow(browserWorkflowOptions{
		Command:        "submit",
		ListID:         spec.ID,
		ApplicationURL: spec.ApplicationURL(),
		ProfileDir:     profileDir,
		BrowserPath:    browserPath,
		PurposeText:    datago.PurposeTextKO,
		Apply:          apply,
		Output:         output,
	}, a.stdout, a.stderr)
}

func (a app) call(args []string, jsonOut bool, exportMode bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	dryRun, args := consumeBool(args, "--dry-run")
	operation, args, err := consumeString(args, "--operation", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	paramsFile, args, err := consumeString(args, "--params-file", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	params, args, err := consumeParams(args)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if len(args) < 1 {
		return a.fail(exitUsage, "usage: datapan get <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]")
	}
	positionalParams, err := parseKeyValueArgs(args[1:])
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	for k, v := range positionalParams {
		params[k] = v
	}
	fileParams, err := readParamsFile(paramsFile, os.Stdin)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	for k, v := range fileParams {
		params[k] = v
	}
	reqPlan, keyName, err := a.requestPlan(args[0], operation, params)
	if err != nil {
		return a.mapError(err, jsonOut || exportMode)
	}
	if dryRun {
		payload := map[string]any{
			"ok":           true,
			"dry_run":      true,
			"dataset":      reqPlan.Spec.ID,
			"operation":    reqPlan.Operation.Name,
			"method":       http.MethodGet,
			"url":          reqPlan.RedactedURL,
			"env_var":      keyName,
			"query_params": reqPlan.PublicParams,
		}
		if jsonOut || exportMode {
			return a.writeJSON(payload)
		}
		fmt.Fprintf(a.stdout, "GET %s\n", reqPlan.RedactedURL)
		return exitOK
	}

	respPayload, err := a.execute(reqPlan)
	if err != nil {
		if jsonOut || exportMode {
			if code := a.writeJSON(map[string]any{
				"ok":        false,
				"error":     "request_failed",
				"dataset":   reqPlan.Spec.ID,
				"operation": reqPlan.Operation.Name,
				"message":   err.Error(),
			}); code != exitOK {
				return code
			}
			return exitRequest
		}
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut || exportMode {
		if code := a.writeJSON(respPayload); code != exitOK {
			return code
		}
		if !respPayload.OK {
			return exitRequest
		}
		return exitOK
	}
	fmt.Fprintln(a.stdout, respPayload.Body)
	if !respPayload.OK {
		return exitRequest
	}
	return exitOK
}

func (a app) curl(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	plan, err := a.curlExportPlan(args)
	if err != nil {
		return a.mapError(err, jsonOut)
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":           true,
			"dataset":      plan.Spec.ID,
			"operation":    plan.Operation.Name,
			"method":       http.MethodGet,
			"url":          plan.URL,
			"command":      plan.Command,
			"env_var":      plan.EnvVar,
			"query_params": plan.PublicParams,
		})
	}
	fmt.Fprintln(a.stdout, plan.Command)
	return exitOK
}

func (a app) postman(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	output, args, err := consumeString(args, "--output", "-")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the Postman collection JSON to stdout")
	}
	plan, err := a.curlExportPlan(args)
	if err != nil {
		return a.mapError(err, jsonOut)
	}
	collection := postmanCollectionForPlan(plan)
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(collection); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if err := writeOutput(output, data.Bytes(), a.stdout); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":        true,
			"format":    "postman",
			"output":    output,
			"dataset":   plan.Spec.ID,
			"operation": plan.Operation.Name,
		})
	}
	return exitOK
}

func (a app) openAPI(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	output, args, err := consumeString(args, "--output", "-")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes the OpenAPI document JSON to stdout")
	}
	plan, err := a.curlExportPlan(args)
	if err != nil {
		return a.mapError(err, jsonOut)
	}
	doc := openAPIDocumentForPlan(plan)
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if err := writeOutput(output, data.Bytes(), a.stdout); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":        true,
			"format":    "openapi",
			"output":    output,
			"dataset":   plan.Spec.ID,
			"operation": plan.Operation.Name,
		})
	}
	return exitOK
}

func (a app) export(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	format, args, err := consumeString(args, "--format", "csv")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	input, args, err := consumeString(args, "--input", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if format == "curl" {
		if input != "" {
			return a.fail(exitUsage, "--format curl cannot be combined with --input")
		}
		return a.curl(args, jsonOut)
	}
	if format == "postman" {
		if input != "" {
			return a.fail(exitUsage, "--format postman cannot be combined with --input")
		}
		return a.postman(args, jsonOut)
	}
	if format == "openapi" {
		if input != "" {
			return a.fail(exitUsage, "--format openapi cannot be combined with --input")
		}
		return a.openAPI(args, jsonOut)
	}
	if input == "" {
		return a.exportFromCall(args, jsonOut, format)
	}
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan export --input PATH|- [--format csv|json] [--json]")
	}
	data, err := readAllInput(input, os.Stdin)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	rows, err := datago.RowsFromJSON(data)
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	return a.writeRows(rows, format, jsonOut)
}

func (a app) codegen(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) == 0 {
		return a.fail(exitUsage, "usage: datapan codegen go <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--package NAME] [--output PATH|-] [--json]")
	}
	switch args[0] {
	case "go", "golang":
		return a.codegenGo(args[1:], jsonOut)
	default:
		return a.fail(exitUsage, "unsupported codegen target %q; use go", args[0])
	}
}

func (a app) codegenGo(args []string, jsonOut bool) int {
	pkg, args, err := consumeString(args, "--package", "datapanclient")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if !validGoPackageName(pkg) {
		return a.fail(exitUsage, "--package must be a valid Go package identifier")
	}
	output, args, err := consumeString(args, "--output", "-")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes generated Go code to stdout")
	}
	plan, err := a.curlExportPlan(args)
	if err != nil {
		return a.mapError(err, jsonOut)
	}
	source := goClientForPlan(plan, pkg)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return a.fail(exitRequest, "generated Go code is not formattable: %v", err)
	}
	source = string(formatted)
	if err := writeOutput(output, []byte(source), a.stdout); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":        true,
			"target":    "go",
			"output":    output,
			"package":   pkg,
			"dataset":   plan.Spec.ID,
			"operation": plan.Operation.Name,
			"function":  goFunctionName(plan),
		})
	}
	return exitOK
}

func (a app) save(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	format, args, err := consumeString(args, "--format", "csv")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "-")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if jsonOut && output == "-" {
		return a.fail(exitUsage, "use --output PATH with --json; --output - writes data to stdout")
	}
	capture := bytes.Buffer{}
	code := app{args: a.args, stdout: &capture, stderr: a.stderr, env: a.env, http: a.http, reg: a.reg}.call(append(args, "--json"), true, true)
	if code != exitOK {
		if jsonOut && capture.Len() > 0 {
			_, _ = a.stdout.Write(capture.Bytes())
		}
		return code
	}
	rows, err := datago.RowsFromJSON(capture.Bytes())
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	var out bytes.Buffer
	switch format {
	case "json":
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"rows": rows}); err != nil {
			return a.fail(exitRequest, "%v", err)
		}
	case "csv":
		if code := writeCSV(&out, rows); code != exitOK {
			return code
		}
	default:
		return a.fail(exitUsage, "unsupported save format %q; use csv or json", format)
	}
	if err := writeOutput(output, out.Bytes(), a.stdout); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		return a.writeJSON(map[string]any{"ok": true, "format": format, "output": output, "count": len(rows)})
	}
	return exitOK
}

func (a app) exportFromCall(args []string, jsonOut bool, format string) int {
	capture := bytes.Buffer{}
	code := app{args: a.args, stdout: &capture, stderr: a.stderr, env: a.env, http: a.http, reg: a.reg}.call(args, true, true)
	if code != exitOK {
		if jsonOut && capture.Len() > 0 {
			_, _ = a.stdout.Write(capture.Bytes())
		}
		return code
	}
	rows, err := datago.RowsFromJSON(capture.Bytes())
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	return a.writeRows(rows, format, jsonOut)
}

func (a app) writeRows(rows []map[string]any, format string, jsonOut bool) int {
	switch format {
	case "json":
		return a.writeJSON(map[string]any{"ok": true, "count": len(rows), "rows": rows})
	case "csv":
		if jsonOut {
			return a.writeJSON(map[string]any{"ok": true, "format": "csv", "count": len(rows)})
		}
		return writeCSV(a.stdout, rows)
	default:
		return a.fail(exitUsage, "unsupported export format %q; use csv or json", format)
	}
}

type requestPlan struct {
	Spec         datago.Spec
	Operation    datago.Operation
	URL          string
	RedactedURL  string
	PublicParams map[string]string
}

type curlExportPlan struct {
	Spec         datago.Spec
	Operation    datago.Operation
	URL          string
	Command      string
	EnvVar       string
	PublicParams map[string]string
}

func (a app) curlExportPlan(args []string) (curlExportPlan, error) {
	operation, args, err := consumeString(args, "--operation", "")
	if err != nil {
		return curlExportPlan{}, errUsage{err.Error()}
	}
	paramsFile, args, err := consumeString(args, "--params-file", "")
	if err != nil {
		return curlExportPlan{}, errUsage{err.Error()}
	}
	params, args, err := consumeParams(args)
	if err != nil {
		return curlExportPlan{}, errUsage{err.Error()}
	}
	if len(args) < 1 {
		return curlExportPlan{}, errUsage{"usage: datapan curl <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--json]"}
	}
	positionalParams, err := parseKeyValueArgs(args[1:])
	if err != nil {
		return curlExportPlan{}, errUsage{err.Error()}
	}
	for k, v := range positionalParams {
		params[k] = v
	}
	fileParams, err := readParamsFile(paramsFile, os.Stdin)
	if err != nil {
		return curlExportPlan{}, errUsage{err.Error()}
	}
	for k, v := range fileParams {
		params[k] = v
	}
	return a.curlExportPlanForRef(args[0], operation, params)
}

func (a app) curlExportPlanForRef(ref, operation string, params map[string]string) (curlExportPlan, error) {
	result := a.reg.Resolve(ref, 10)
	if result.Status != datago.ResolveFound {
		if result.Status == datago.ResolveAmbiguous {
			return curlExportPlan{}, errAmbiguous{ref: ref, candidates: result.Candidates}
		}
		return curlExportPlan{}, errNotFound{ref}
	}
	spec := result.Spec
	op, ok := spec.Operation(operation)
	if !ok {
		if operation == "" {
			return curlExportPlan{}, fmt.Errorf("spec %s has no callable operation endpoint yet", spec.ID)
		}
		return curlExportPlan{}, fmt.Errorf("unknown operation %q for %s", operation, spec.ID)
	}
	envVar := datago.KeyEnvNames[0]
	if selected, ok := a.resolveKey(); ok {
		envVar = selected
	}
	urlText, publicParams, err := curlURLForOperation(op, params, envVar)
	if err != nil {
		return curlExportPlan{}, err
	}
	return curlExportPlan{
		Spec:         spec,
		Operation:    op,
		URL:          urlText,
		Command:      datago.CommandString([]string{"curl", "-fsS", urlText}),
		EnvVar:       envVar,
		PublicParams: publicParams,
	}, nil
}

func curlURLForOperation(op datago.Operation, params map[string]string, envVar string) (string, map[string]string, error) {
	u, err := url.Parse(op.Endpoint)
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	for k, v := range params {
		if !isAuthParam(k) {
			q.Set(k, v)
		}
	}
	for k, v := range op.DefaultParams {
		if !isAuthParam(k) && q.Get(k) == "" {
			q.Set(k, v)
		}
	}
	for key := range q {
		if isAuthParam(key) {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	placeholder := "${" + envVar + "}"
	if u.RawQuery == "" {
		u.RawQuery = "serviceKey=" + placeholder
	} else {
		u.RawQuery += "&serviceKey=" + placeholder
	}
	publicParams := map[string]string{}
	for k, values := range q {
		if len(values) > 0 {
			publicParams[k] = values[0]
		}
	}
	publicParams["serviceKey"] = placeholder
	return u.String(), publicParams, nil
}

func postmanCollectionForPlan(plan curlExportPlan) map[string]any {
	endpoint, _ := url.Parse(plan.Operation.Endpoint)
	host := []string{}
	if endpoint.Host != "" {
		host = strings.Split(endpoint.Host, ".")
	}
	path := []string{}
	for _, part := range strings.Split(strings.Trim(endpoint.EscapedPath(), "/"), "/") {
		if part != "" {
			path = append(path, part)
		}
	}
	query := make([]map[string]string, 0, len(plan.PublicParams))
	keys := make([]string, 0, len(plan.PublicParams))
	for key := range plan.PublicParams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := plan.PublicParams[key]
		if key == "serviceKey" {
			value = "{{" + plan.EnvVar + "}}"
		}
		query = append(query, map[string]string{"key": key, "value": value})
	}
	return map[string]any{
		"info": map[string]any{
			"name":   plan.Spec.Title,
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"variable": []map[string]string{
			{
				"key":   plan.EnvVar,
				"value": "",
				"type":  "string",
			},
		},
		"item": []map[string]any{
			{
				"name": plan.Operation.Name,
				"request": map[string]any{
					"method": http.MethodGet,
					"url": map[string]any{
						"raw":      postmanRawURL(plan),
						"protocol": endpoint.Scheme,
						"host":     host,
						"path":     path,
						"query":    query,
					},
				},
			},
		},
	}
}

func postmanRawURL(plan curlExportPlan) string {
	raw := plan.URL
	return strings.ReplaceAll(raw, "${"+plan.EnvVar+"}", "{{"+plan.EnvVar+"}}")
}

func openAPIDocumentForPlan(plan curlExportPlan) map[string]any {
	endpoint, _ := url.Parse(plan.Operation.Endpoint)
	serverURL := endpoint.Scheme + "://" + endpoint.Host
	path := endpoint.EscapedPath()
	if strings.TrimSpace(path) == "" {
		path = "/"
	}
	parameters := openAPIParameters(plan)
	responseSchema := openAPIResponseSchema(plan.Operation.ResponseParams)
	operationID := openAPIOperationID(plan.Spec.ID, plan.Operation.Name)
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       plan.Spec.Title,
			"version":     "0.1.0",
			"description": strings.TrimSpace(plan.Spec.Description),
		},
		"servers": []map[string]string{{"url": serverURL}},
		"paths": map[string]any{
			path: map[string]any{
				"get": map[string]any{
					"operationId": operationID,
					"summary":     plan.Operation.Name,
					"tags":        []string{openAPITag(plan.Spec)},
					"parameters":  parameters,
					"security":    []map[string][]string{{"serviceKey": []string{}}},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Upstream provider response",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": responseSchema,
								},
								"application/xml": map[string]any{
									"schema": responseSchema,
								},
							},
						},
					},
					"x-datapan": map[string]any{
						"dataset_id": plan.Spec.ID,
						"provider":   plan.Spec.Provider,
						"env_var":    plan.EnvVar,
						"curl":       plan.Command,
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"serviceKey": map[string]any{
					"type":        "apiKey",
					"in":          "query",
					"name":        "serviceKey",
					"description": "data.go.kr service key. Datapan plans this from the " + plan.EnvVar + " environment variable.",
				},
			},
		},
		"x-datapan": map[string]any{
			"dataset_id":      plan.Spec.ID,
			"title":           plan.Spec.Title,
			"organization":    plan.Spec.Organization,
			"source_category": plan.Spec.SourceCategory,
			"operation":       plan.Operation.Name,
			"env_var":         plan.EnvVar,
		},
	}
}

func openAPIParameters(plan curlExportPlan) []map[string]any {
	byName := map[string]datago.Param{}
	for _, param := range plan.Operation.RequestParams {
		name := strings.TrimSpace(param.Name)
		if name == "" || isAuthParam(name) {
			continue
		}
		byName[name] = param
	}
	for name := range plan.PublicParams {
		if strings.TrimSpace(name) == "" || isAuthParam(name) {
			continue
		}
		if _, ok := byName[name]; !ok {
			byName[name] = datago.Param{Name: name}
		}
	}
	keys := make([]string, 0, len(byName))
	for name := range byName {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys)+1)
	for _, name := range keys {
		param := byName[name]
		schema := map[string]any{"type": "string"}
		if value := strings.TrimSpace(plan.PublicParams[name]); value != "" {
			schema["default"] = value
		}
		entry := map[string]any{
			"name":     name,
			"in":       "query",
			"required": false,
			"schema":   schema,
		}
		if strings.TrimSpace(param.Label) != "" {
			entry["description"] = param.Label
		}
		out = append(out, entry)
	}
	out = append(out, map[string]any{
		"name":        "serviceKey",
		"in":          "query",
		"required":    true,
		"description": "Supplied from the " + plan.EnvVar + " environment variable.",
		"schema": map[string]any{
			"type":    "string",
			"default": "${" + plan.EnvVar + "}",
		},
	})
	return out
}

func openAPIResponseSchema(params []datago.Param) map[string]any {
	properties := map[string]any{}
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		prop := map[string]any{"type": "string"}
		if strings.TrimSpace(param.Label) != "" {
			prop["description"] = param.Label
		}
		properties[name] = prop
	}
	if len(properties) == 0 {
		return map[string]any{"type": "object", "additionalProperties": true}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": true,
	}
}

func openAPIOperationID(datasetID, operation string) string {
	parts := []string{"datapan", datasetID, operation}
	raw := strings.Join(parts, "_")
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "datapan_operation"
	}
	return out
}

func openAPITag(spec datago.Spec) string {
	if strings.TrimSpace(spec.Organization) != "" {
		return spec.Organization
	}
	if strings.TrimSpace(spec.SourceCategory) != "" {
		return spec.SourceCategory
	}
	return "data.go.kr"
}

func goClientForPlan(plan curlExportPlan, pkg string) string {
	endpoint, _ := url.Parse(plan.Operation.Endpoint)
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	function := goFunctionName(plan)
	defaults := goDefaultParamLiteral(plan.PublicParams)
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"io\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"net/url\"\n")
	b.WriteString("\t\"os\"\n")
	b.WriteString("\t\"strings\"\n")
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "const defaultBaseURL = %q\n", endpoint.String())
	fmt.Fprintf(&b, "const defaultServiceKeyEnv = %q\n\n", plan.EnvVar)
	b.WriteString("// HTTPDoer is satisfied by *http.Client and test clients.\n")
	b.WriteString("type HTTPDoer interface {\n\tDo(*http.Request) (*http.Response, error)\n}\n\n")
	b.WriteString("// Client calls one Datapan-planned public data operation.\n")
	b.WriteString("type Client struct {\n\tHTTPClient HTTPDoer\n\tServiceKey string\n\tBaseURL string\n}\n\n")
	b.WriteString("// New creates a client with an explicit service key.\n")
	b.WriteString("func New(serviceKey string) *Client {\n")
	b.WriteString("\treturn &Client{HTTPClient: http.DefaultClient, ServiceKey: strings.TrimSpace(serviceKey), BaseURL: defaultBaseURL}\n")
	b.WriteString("}\n\n")
	b.WriteString("// NewFromEnv creates a client from the Datapan-planned service-key environment variable.\n")
	b.WriteString("func NewFromEnv() (*Client, error) {\n")
	b.WriteString("\tkey := strings.TrimSpace(os.Getenv(defaultServiceKeyEnv))\n")
	b.WriteString("\tif key == \"\" {\n")
	b.WriteString("\t\treturn nil, fmt.Errorf(\"missing %s\", defaultServiceKeyEnv)\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn New(key), nil\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// %s calls %s (%s).\n", function, goCommentText(plan.Operation.Name), plan.Spec.ID)
	fmt.Fprintf(&b, "func (c *Client) %s(ctx context.Context, params map[string]string) ([]byte, error) {\n", function)
	fmt.Fprintf(&b, "\treq, err := c.New%sRequest(ctx, params)\n", function)
	b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\thttpClient := c.HTTPClient\n\tif httpClient == nil {\n\t\thttpClient = http.DefaultClient\n\t}\n")
	b.WriteString("\tresp, err := httpClient.Do(req)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\tdefer resp.Body.Close()\n")
	b.WriteString("\tbody, err := io.ReadAll(resp.Body)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\tif resp.StatusCode < 200 || resp.StatusCode >= 300 {\n\t\treturn nil, fmt.Errorf(\"provider returned HTTP %d: %s\", resp.StatusCode, strings.TrimSpace(string(body)))\n\t}\n")
	b.WriteString("\treturn body, nil\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// New%sRequest builds the HTTP request without executing it.\n", function)
	fmt.Fprintf(&b, "func (c *Client) New%sRequest(ctx context.Context, params map[string]string) (*http.Request, error) {\n", function)
	b.WriteString("\tif strings.TrimSpace(c.ServiceKey) == \"\" {\n\t\treturn nil, fmt.Errorf(\"missing service key\")\n\t}\n")
	b.WriteString("\tbaseURL := strings.TrimSpace(c.BaseURL)\n\tif baseURL == \"\" {\n\t\tbaseURL = defaultBaseURL\n\t}\n")
	b.WriteString("\tu, err := url.Parse(baseURL)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\tq := u.Query()\n")
	fmt.Fprintf(&b, "\tfor key, value := range %s {\n\t\tq.Set(key, value)\n\t}\n", defaults)
	b.WriteString("\tfor key, value := range params {\n\t\tif strings.TrimSpace(key) != \"\" && key != \"serviceKey\" {\n\t\t\tq.Set(key, value)\n\t\t}\n\t}\n")
	b.WriteString("\tq.Set(\"serviceKey\", c.ServiceKey)\n\tu.RawQuery = q.Encode()\n")
	b.WriteString("\treturn http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)\n")
	b.WriteString("}\n")
	return b.String()
}

func goDefaultParamLiteral(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		if strings.TrimSpace(key) != "" && !isAuthParam(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "map[string]string{}"
	}
	var b strings.Builder
	b.WriteString("map[string]string{\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "\t\t%q: %q,\n", key, params[key])
	}
	b.WriteString("\t}")
	return b.String()
}

func goFunctionName(plan curlExportPlan) string {
	name := goExportedIdentifier(plan.Operation.Name)
	if name == "" {
		name = "Call" + goExportedIdentifier(plan.Spec.ID)
	}
	if name == "Call" {
		name = "CallOperation"
	}
	return name
}

func goExportedIdentifier(value string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range value {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isUpper && !isDigit {
			upperNext = true
			continue
		}
		if b.Len() == 0 && isDigit {
			b.WriteString("Call")
		}
		if upperNext && isLower {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
		upperNext = false
	}
	return b.String()
}

func validGoPackageName(value string) bool {
	if value == "" {
		return false
	}
	for idx, r := range value {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if idx == 0 && isDigit {
			return false
		}
		if !isLower && !isUpper && !isDigit && r != '_' {
			return false
		}
	}
	return true
}

func goCommentText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value == "" {
		return "the planned operation"
	}
	return value
}

func (a app) requestPlan(ref, operation string, params map[string]string) (requestPlan, string, error) {
	result := a.reg.Resolve(ref, 10)
	if result.Status != datago.ResolveFound {
		if result.Status == datago.ResolveAmbiguous {
			return requestPlan{}, "", errAmbiguous{ref: ref, candidates: result.Candidates}
		}
		return requestPlan{}, "", errNotFound{ref}
	}
	spec := result.Spec
	op, ok := spec.Operation(operation)
	if !ok {
		if operation == "" {
			return requestPlan{}, "", fmt.Errorf("spec %s has no callable operation endpoint yet", spec.ID)
		}
		return requestPlan{}, "", fmt.Errorf("unknown operation %q for %s", operation, spec.ID)
	}
	return a.requestPlanForOperation(spec, op, params)
}

func (a app) requestPlanForOperation(spec datago.Spec, op datago.Operation, params map[string]string) (requestPlan, string, error) {
	keyName, key, ok := a.resolveKeyValue()
	if !ok {
		return requestPlan{}, "", errAuth{}
	}
	u, err := url.Parse(op.Endpoint)
	if err != nil {
		return requestPlan{}, "", err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	for k, v := range op.DefaultParams {
		if q.Get(k) == "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = datago.QueryWithServiceKey(q, key)
	redacted := *u
	rq := redacted.Query()
	rq.Set("serviceKey", "REDACTED")
	redacted.RawQuery = rq.Encode()
	publicParams := map[string]string{}
	for k, values := range rq {
		if len(values) > 0 {
			publicParams[k] = values[0]
		}
	}
	return requestPlan{Spec: spec, Operation: op, URL: u.String(), RedactedURL: redacted.String(), PublicParams: publicParams}, keyName, nil
}

func (a app) execute(plan requestPlan) (datago.ResponseEnvelope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.URL, nil)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return datago.ResponseEnvelope{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	ok, semanticStatus, message, providerStatus := datago.ClassifyResponse(resp.StatusCode, contentType, body)
	return datago.ResponseEnvelope{
		OK:             ok,
		Provider:       "data.go.kr",
		Dataset:        plan.Spec.ID,
		Operation:      plan.Operation.Name,
		StatusCode:     resp.StatusCode,
		ContentType:    contentType,
		SemanticStatus: semanticStatus,
		Message:        message,
		ProviderStatus: providerStatus,
		URL:            plan.RedactedURL,
		Body:           string(body),
	}, nil
}

func (a app) resolveKey() (string, bool) {
	name, _, ok := a.resolveKeyValue()
	return name, ok
}

func (a app) resolveKeyValue() (string, string, bool) {
	for _, name := range datago.KeyEnvNames {
		if value, ok := a.env.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			return name, value, true
		}
	}
	return "", "", false
}

func (a app) mapError(err error, jsonOut bool) int {
	var usage errUsage
	if errors.As(err, &usage) {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":      false,
				"error":   "usage",
				"message": usage.message,
			}); code != exitOK {
				return code
			}
			return exitUsage
		}
		return a.fail(exitUsage, "%s", usage.message)
	}
	var ambiguous errAmbiguous
	if errors.As(err, &ambiguous) {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":         false,
				"error":      "ambiguous_ref",
				"ref":        ambiguous.ref,
				"candidates": specSummaries(ambiguous.candidates),
			}); code != exitOK {
				return code
			}
			return exitAmbiguous
		}
		fmt.Fprintf(a.stderr, "ambiguous data.go.kr ref %q; candidates:\n", ambiguous.ref)
		for _, spec := range ambiguous.candidates {
			fmt.Fprintf(a.stderr, "  %s  %s\n", spec.ID, spec.Title)
		}
		return exitAmbiguous
	}
	var nf errNotFound
	if errors.As(err, &nf) {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":    false,
				"error": "not_found",
				"ref":   nf.id,
			}); code != exitOK {
				return code
			}
			return exitNotFound
		}
		return a.fail(exitNotFound, "unknown data.go.kr ref %q", nf.id)
	}
	var auth errAuth
	if errors.As(err, &auth) {
		if jsonOut {
			if code := a.writeJSON(map[string]any{
				"ok":                false,
				"error":             "missing_auth",
				"accepted_env_vars": datago.KeyEnvNames,
			}); code != exitOK {
				return code
			}
			return exitAuth
		}
		return a.fail(exitAuth, "missing data.go.kr API key; set one of: %s", strings.Join(datago.KeyEnvNames, ", "))
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{
			"ok":      false,
			"error":   "request_failed",
			"message": err.Error(),
		}); code != exitOK {
			return code
		}
		return exitRequest
	}
	return a.fail(exitRequest, "%v", err)
}

func (a app) writeJSON(payload any) int {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	return exitOK
}

func (a app) fail(code int, format string, args ...any) int {
	fmt.Fprintf(a.stderr, format, args...)
	fmt.Fprintln(a.stderr)
	return code
}

func (a app) printHelp() {
	fmt.Fprintln(a.stdout, `datapan is an agent-friendly CLI for Korean public data.

Usage:
  datapan search [query] [--org NAME] [--category NAME] [--priority P0] [--provider NAME] [--json] [--limit N]
  datapan catalog import data-go-kr [--output PATH|-] [--page N] [--per-page N] [--pages N|--all] [--max-pages N] [--retries N] [--retry-delay-ms N] [--query TEXT] [--org NAME] [--category NAME] [--json]
  datapan catalog update data-go-kr [--registry PATH] [--apply] [--backup] [--diff-limit N] [--retries N] [--retry-delay-ms N] [--json]
  datapan catalog diff --old OLD --new NEW [--limit N] [--output PATH|-] [--json]
  datapan catalog audit [--registry PATH] [--sample N] [--output PATH|-] [--json]
  datapan catalog errors [--registry PATH] [--limit N] [--output PATH|-] [--json]
  datapan catalog providers [--registry PATH] [--limit N] [--sample N] [--status STATUS] [--kind KIND] [--provider NAME] [--output PATH|-] [--json]
  datapan catalog dependencies [--registry PATH] [--limit N] [--kind KIND] [--status STATUS] [--provider NAME] [--host HOST] [--output PATH|-] [--json]
  datapan catalog adapter-targets [--registry PATH] [--limit N] [--sample N] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json]
  datapan catalog verify [--registry PATH] [--ref REF] [--operation NAME] [--limit N] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json]
  datapan catalog verify --input REPORT [--status verified|failed|skipped|unknown] [--limit N] [--output PATH|-] [--json]
  datapan catalog verify summary --input REPORT [--limit N] [--output PATH|-] [--json]
  datapan catalog release draft --registry PATH [--previous-registry PATH] [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]
  datapan catalog release verify --manifest PATH [--output PATH|-] [--json]
  datapan catalog release readiness --manifest PATH [--output PATH|-] [--json]
  datapan show <ref> [--json]
  datapan auth check [--json]
  datapan access <ref> [--open] [--copy-purpose] [--start] [--purpose] [--json]
  datapan access login [--headed] [--manual-login-wait-ms N] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan access <ref> [--dry-run|--apply] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan get <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]
  datapan curl <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--json]
  datapan save <ref> [KEY=VALUE ...] [--format csv|json] [--output PATH|-] [--json]
  datapan call <ref> [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]
  datapan export --input PATH|- [--format csv|json]
  datapan export --format curl <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-]
  datapan export --format postman <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--output PATH|-]
  datapan export --format openapi <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--output PATH|-]
  datapan codegen go <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--package NAME] [--output PATH|-]
  datapan version [--json]

Accepted data.go.kr key env vars:
  DATAPAN_DATA_GO_KR_KEY, DATA_PORTAL_API_KEY, DATA_GO_KR_SERVICE_KEY`)
}

type errNotFound struct{ id string }

func (e errNotFound) Error() string { return "not found: " + e.id }

type errAmbiguous struct {
	ref        string
	candidates []datago.Spec
}

func (e errAmbiguous) Error() string { return "ambiguous: " + e.ref }

type errUsage struct{ message string }

func (e errUsage) Error() string { return e.message }

type errAuth struct{}

func (errAuth) Error() string { return "missing auth" }

func consumeBool(args []string, name string) (bool, []string) {
	out := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return found, out
}

func hasAnyArg(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
		}
	}
	return false
}

func consumeString(args []string, name, fallback string) (string, []string, error) {
	out := make([]string, 0, len(args))
	value := fallback
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == name {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, name+"=") {
			value = strings.TrimPrefix(arg, name+"=")
			continue
		}
		out = append(out, arg)
	}
	return value, out, nil
}

func consumeInt(args []string, name string, fallback int) (int, []string, error) {
	raw, out, err := consumeString(args, name, strconv.Itoa(fallback))
	if err != nil {
		return 0, nil, err
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, nil, fmt.Errorf("%s requires a non-negative integer", name)
	}
	return value, out, nil
}

func consumeParams(args []string) (map[string]string, []string, error) {
	params := map[string]string{}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg != "--param" {
			out = append(out, arg)
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("--param requires k=v")
		}
		key, value, ok := strings.Cut(args[i+1], "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, nil, fmt.Errorf("--param requires k=v")
		}
		params[key] = value
		i++
	}
	return params, out, nil
}

func parseKeyValueArgs(args []string) (map[string]string, error) {
	params := map[string]string{}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("expected KEY=VALUE argument, got %q", arg)
		}
		params[key] = value
	}
	return params, nil
}

func readParamsFile(path string, stdin io.Reader) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	data, err := readAllInput(path, stdin)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("params file must be a JSON object: %w", err)
	}
	params := map[string]string{}
	for k, v := range raw {
		params[k] = fmt.Sprint(v)
	}
	return params, nil
}

func readAllInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func loadRegistryOrEmpty(path string) (datago.Registry, bool, error) {
	reg, err := datago.LoadRegistry(path)
	if err == nil {
		return reg, true, nil
	}
	if os.IsNotExist(err) {
		return datago.NewRegistry(nil), false, nil
	}
	return datago.Registry{}, false, err
}

func writeRegistryOutput(path string, specs []datago.Spec, stdout io.Writer) error {
	if path == "-" {
		return datago.EncodeRegistry(stdout, specs)
	}
	if path == "" {
		path = defaultRegistryPath
	}
	if dir := strings.TrimSpace(filepathDir(path)); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return datago.EncodeRegistry(f, specs)
}

func writeRegistryAtomic(path string, specs []datago.Spec) error {
	if path == "" {
		path = defaultRegistryPath
	}
	dir := filepathDir(path)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".datapan-registry-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := datago.EncodeRegistry(tmp, specs); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		if retryErr := os.Rename(tmpPath, path); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if dir := strings.TrimSpace(filepathDir(dst)); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(dst, data, 0o600)
}

func writeOutput(path string, data []byte, stdout io.Writer) error {
	if path == "-" || path == "" {
		_, err := stdout.Write(data)
		return err
	}
	if dir := strings.TrimSpace(filepathDir(path)); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o600)
}

type releasePaths struct {
	OutputDir               string
	SchemaDir               string
	SchemaIndexPath         string
	DataDir                 string
	ReportsDir              string
	ProvenanceDir           string
	RegistryPath            string
	ProviderIndexPath       string
	CatalogDiffPath         string
	ProviderBacklogPath     string
	DependencyInventoryPath string
	AdapterTargetsPath      string
	CatalogAuditPath        string
	ErrorCatalogPath        string
	VerificationPath        string
	VerificationSummaryPath string
	ProvenancePath          string
	ManifestPath            string
}

func releaseDraftPaths(outputDir string) releasePaths {
	return releasePaths{
		OutputDir:               outputDir,
		SchemaDir:               joinPath(outputDir, "schemas"),
		SchemaIndexPath:         joinPath(outputDir, "schemas/index.json"),
		DataDir:                 joinPath(outputDir, "data"),
		ReportsDir:              joinPath(outputDir, "reports"),
		ProvenanceDir:           joinPath(outputDir, "provenance"),
		RegistryPath:            joinPath(outputDir, "data/data-go-kr.registry.json"),
		ProviderIndexPath:       joinPath(outputDir, "data/provider-index.json"),
		CatalogDiffPath:         joinPath(outputDir, "reports/catalog-diff.json"),
		ProviderBacklogPath:     joinPath(outputDir, "reports/provider-backlog.json"),
		DependencyInventoryPath: joinPath(outputDir, "reports/dependencies.json"),
		AdapterTargetsPath:      joinPath(outputDir, "reports/adapter-targets.json"),
		CatalogAuditPath:        joinPath(outputDir, "reports/catalog-audit.json"),
		ErrorCatalogPath:        joinPath(outputDir, "reports/error-catalog.json"),
		VerificationPath:        joinPath(outputDir, "reports/latest-verification.json"),
		VerificationSummaryPath: joinPath(outputDir, "reports/latest-verification-summary.json"),
		ProvenancePath:          joinPath(outputDir, "provenance/data-go-kr.md"),
		ManifestPath:            joinPath(outputDir, "manifest.json"),
	}
}

func datapanSchemaFiles() []string {
	return []string{
		"schemas/datapan.specs.v1.schema.json",
		"schemas/datapan.dependencies.v1.schema.json",
		"schemas/datapan.adapter-targets.v1.schema.json",
		"schemas/datapan.providers.v1.schema.json",
		"schemas/datapan.verification.v1.schema.json",
		"schemas/datapan.verification-summary.v1.schema.json",
		"schemas/datapan.release-manifest.v1.schema.json",
		"schemas/datapan.release-verification.v1.schema.json",
		"schemas/datapan.release-readiness.v1.schema.json",
		"schemas/datapan.schema-index.v1.schema.json",
		"schemas/datapan.catalog-diff.v1.schema.json",
		"schemas/datapan.error-catalog.v1.schema.json",
		"schemas/datapan.catalog-audit.v1.schema.json",
		"schemas/datapan.provider-index.v1.schema.json",
	}
}

func datapanSchemaFileNames() []string {
	files := datapanSchemaFiles()
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, schemaFileName(file))
	}
	return names
}

func schemaFileName(path string) string {
	path = strings.TrimRight(path, `/\`)
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

func datapanSchemaSources() ([]string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return nil, err
	}
	files := datapanSchemaFiles()
	sources := make([]string, 0, len(files))
	for _, file := range files {
		sources = append(sources, filepath.Join(root, filepath.FromSlash(file)))
	}
	return sources, nil
}

func findProjectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "schemas")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", fmt.Errorf("could not find datapan project root from %s", wd)
}

func joinPath(base, elem string) string {
	base = strings.TrimRight(base, `/\`)
	elem = strings.TrimLeft(elem, `/\`)
	if base == "" {
		return elem
	}
	if elem == "" {
		return base
	}
	return base + string(os.PathSeparator) + strings.ReplaceAll(elem, "/", string(os.PathSeparator))
}

func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeOutput(path, data, io.Discard)
}

type releaseManifest struct {
	SchemaVersion  string                    `json:"schema_version"`
	GeneratedAt    string                    `json:"generated_at"`
	DatapanVersion string                    `json:"datapan_version"`
	Provider       string                    `json:"provider"`
	SourceRegistry string                    `json:"source_registry"`
	OutputDir      string                    `json:"output_dir"`
	ArtifactCount  int                       `json:"artifact_count"`
	Artifacts      []releaseManifestArtifact `json:"artifacts"`
}

type releaseManifestArtifact struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Schema string `json:"schema,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type schemaIndex struct {
	SchemaVersion  string             `json:"schema_version"`
	GeneratedAt    string             `json:"generated_at"`
	DatapanVersion string             `json:"datapan_version"`
	Count          int                `json:"count"`
	Schemas        []schemaIndexEntry `json:"schemas"`
}

type schemaIndexEntry struct {
	Path     string `json:"path"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Contract string `json:"contract"`
	Version  string `json:"version"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}

type releaseManifestVerificationReport struct {
	Manifest              string                              `json:"manifest"`
	Root                  string                              `json:"root"`
	SchemaVersion         string                              `json:"schema_version"`
	ManifestSchemaVersion string                              `json:"manifest_schema_version,omitempty"`
	ArtifactCount         int                                 `json:"artifact_count"`
	Checked               int                                 `json:"checked"`
	Failed                int                                 `json:"failed"`
	OK                    bool                                `json:"ok"`
	Results               []releaseManifestVerificationResult `json:"results"`
}

type releaseManifestVerificationResult struct {
	Path           string `json:"path"`
	Kind           string `json:"kind,omitempty"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	ExpectedBytes  int64  `json:"expected_bytes,omitempty"`
	ActualBytes    int64  `json:"actual_bytes,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
}

type releaseReadinessReport struct {
	Manifest       string                  `json:"manifest"`
	Root           string                  `json:"root"`
	SchemaVersion  string                  `json:"schema_version"`
	GeneratedAt    string                  `json:"generated_at"`
	DatapanVersion string                  `json:"datapan_version"`
	Provider       string                  `json:"provider"`
	Ready          bool                    `json:"ready"`
	Summary        releaseReadinessSummary `json:"summary"`
	Gates          []releaseReadinessGate  `json:"gates"`
}

type releaseReadinessSummary struct {
	GatesTotal               int `json:"gates_total"`
	Passed                   int `json:"passed"`
	Warned                   int `json:"warned"`
	Failed                   int `json:"failed"`
	RequiredArtifacts        int `json:"required_artifacts"`
	MissingRequiredArtifacts int `json:"missing_required_artifacts"`
	RecommendedArtifacts     int `json:"recommended_artifacts"`
	MissingRecommended       int `json:"missing_recommended_artifacts"`
	SchemaArtifacts          int `json:"schema_artifacts"`
	RegistrySpecs            int `json:"registry_specs"`
}

type releaseReadinessGate struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	ArtifactKind string `json:"artifact_kind,omitempty"`
	ArtifactPath string `json:"artifact_path,omitempty"`
	Expected     int    `json:"expected,omitempty"`
	Actual       int    `json:"actual,omitempty"`
}

type releaseSchemaValidator struct {
	schemas map[string]*jsonschema.Schema
}

func buildSchemaIndex(generatedAt string, paths releasePaths) (schemaIndex, error) {
	entries := make([]schemaIndexEntry, 0, len(datapanSchemaFiles()))
	for _, schema := range datapanSchemaFiles() {
		name := schemaFileName(schema)
		path := joinPath(paths.SchemaDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return schemaIndex{}, err
		}
		var payload struct {
			ID    string `json:"$id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return schemaIndex{}, err
		}
		contract, schemaVersion := schemaContractVersion(name)
		sum := sha256.Sum256(data)
		entries = append(entries, schemaIndexEntry{
			Path:     "schemas/" + name,
			ID:       payload.ID,
			Title:    payload.Title,
			Contract: contract,
			Version:  schemaVersion,
			Bytes:    int64(len(data)),
			SHA256:   fmt.Sprintf("%x", sum),
		})
	}
	return schemaIndex{
		SchemaVersion:  "datapan.schema-index.v1",
		GeneratedAt:    generatedAt,
		DatapanVersion: version,
		Count:          len(entries),
		Schemas:        entries,
	}, nil
}

func schemaContractVersion(name string) (string, string) {
	name = strings.TrimPrefix(name, "datapan.")
	name = strings.TrimSuffix(name, ".schema.json")
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return name, ""
	}
	return strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1]
}

func writeReleaseManifest(generatedAt, sourceRegistry string, includeCatalogDiff, includeVerification bool, paths releasePaths) (releaseManifest, error) {
	artifacts := releaseManifestArtifacts(paths, includeCatalogDiff, includeVerification)
	manifest := releaseManifest{
		SchemaVersion:  "datapan.release-manifest.v1",
		GeneratedAt:    generatedAt,
		DatapanVersion: version,
		Provider:       "data.go.kr",
		SourceRegistry: sourceRegistry,
		OutputDir:      paths.OutputDir,
		ArtifactCount:  len(artifacts),
		Artifacts:      artifacts,
	}
	for idx := range manifest.Artifacts {
		metadata, err := releaseArtifactMetadata(paths.OutputDir, manifest.Artifacts[idx])
		if err != nil {
			return releaseManifest{}, err
		}
		manifest.Artifacts[idx] = metadata
	}
	if err := writeJSONFile(paths.ManifestPath, manifest); err != nil {
		return releaseManifest{}, err
	}
	return manifest, nil
}

func verifyReleaseManifest(manifestPath string) (releaseManifestVerificationReport, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return releaseManifestVerificationReport{}, err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return releaseManifestVerificationReport{}, err
	}
	root := filepathDir(manifestPath)
	if root == "" {
		root = "."
	}
	report := releaseManifestVerificationReport{
		Manifest:              manifestPath,
		Root:                  root,
		SchemaVersion:         "datapan.release-verification.v1",
		ManifestSchemaVersion: manifest.SchemaVersion,
		ArtifactCount:         manifest.ArtifactCount,
		Checked:               len(manifest.Artifacts),
		OK:                    true,
		Results:               make([]releaseManifestVerificationResult, 0, len(manifest.Artifacts)+1),
	}
	if manifest.ArtifactCount != len(manifest.Artifacts) {
		report.addManifestFailure(manifestPath, "artifact_count_mismatch")
	}
	if manifest.SchemaVersion != "datapan.release-manifest.v1" {
		report.addManifestFailure(manifestPath, "unsupported_schema_version")
	}
	validator, schemasAvailable, err := loadReleaseSchemaValidator(root)
	if err != nil {
		report.addManifestFailure("schemas", "schema_compile_failed")
	} else if schemasAvailable {
		if err := validator.validate("https://schemas.datapan.dev/datapan.release-manifest.v1.schema.json", data); err != nil {
			report.addManifestFailure(manifestPath, "schema_validation_failed")
		}
	}
	seen := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		if seen[artifact.Path] {
			report.addManifestFailure(artifact.Path, "duplicate_artifact_path")
			continue
		}
		seen[artifact.Path] = true
		if filepath.ToSlash(filepath.Clean(filepath.FromSlash(artifact.Path))) == "manifest.json" {
			report.addManifestFailure(artifact.Path, "manifest_self_reference")
			continue
		}
		result := verifyReleaseManifestArtifact(root, artifact, validator)
		if result.Status == "failed" {
			report.OK = false
			report.Failed++
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func releaseReadinessReportForManifest(manifestPath, generatedAt string) (releaseReadinessReport, error) {
	manifest, err := readReleaseManifest(manifestPath)
	if err != nil {
		return releaseReadinessReport{}, err
	}
	root := filepathDir(manifestPath)
	if root == "" {
		root = "."
	}
	report := releaseReadinessReport{
		Manifest:       manifestPath,
		Root:           root,
		SchemaVersion:  "datapan.release-readiness.v1",
		GeneratedAt:    generatedAt,
		DatapanVersion: version,
		Provider:       manifest.Provider,
		Ready:          true,
		Gates:          make([]releaseReadinessGate, 0),
	}
	verification, err := verifyReleaseManifest(manifestPath)
	if err != nil {
		return releaseReadinessReport{}, err
	}
	if verification.OK {
		report.addReadinessGate(releaseReadinessGate{
			ID:       "manifest_verified",
			Status:   "pass",
			Severity: "required",
			Message:  "release manifest checksums and schema-bound artifacts verified",
			Expected: verification.Checked,
			Actual:   verification.Checked,
		})
	} else {
		report.addReadinessGate(releaseReadinessGate{
			ID:       "manifest_verified",
			Status:   "fail",
			Severity: "required",
			Message:  "release manifest verification failed",
			Expected: verification.Checked,
			Actual:   verification.Checked - verification.Failed,
		})
	}
	byKind := releaseArtifactsByKind(manifest.Artifacts)
	required := []string{
		"schema_index",
		"registry",
		"provider_index",
		"catalog_audit",
		"error_catalog",
		"dependencies",
		"adapter_targets",
		"provider_backlog",
		"provenance",
	}
	for _, kind := range required {
		report.Summary.RequiredArtifacts++
		artifacts := byKind[kind]
		if len(artifacts) == 0 {
			report.Summary.MissingRequiredArtifacts++
			report.addReadinessGate(releaseReadinessGate{
				ID:           "required_artifact_" + kind,
				Status:       "fail",
				Severity:     "required",
				Message:      "required release artifact is missing",
				ArtifactKind: kind,
				Expected:     1,
			})
			continue
		}
		report.addReadinessGate(releaseReadinessGate{
			ID:           "required_artifact_" + kind,
			Status:       "pass",
			Severity:     "required",
			Message:      "required release artifact is present",
			ArtifactKind: kind,
			ArtifactPath: artifacts[0].Path,
			Expected:     1,
			Actual:       len(artifacts),
		})
	}
	recommended := []string{"catalog_diff", "verification", "verification_summary"}
	for _, kind := range recommended {
		report.Summary.RecommendedArtifacts++
		artifacts := byKind[kind]
		if len(artifacts) == 0 {
			report.Summary.MissingRecommended++
			report.addReadinessGate(releaseReadinessGate{
				ID:           "recommended_artifact_" + kind,
				Status:       "warn",
				Severity:     "recommended",
				Message:      "recommended release artifact is not present yet",
				ArtifactKind: kind,
				Expected:     1,
			})
			continue
		}
		report.addReadinessGate(releaseReadinessGate{
			ID:           "recommended_artifact_" + kind,
			Status:       "pass",
			Severity:     "recommended",
			Message:      "recommended release artifact is present",
			ArtifactKind: kind,
			ArtifactPath: artifacts[0].Path,
			Expected:     1,
			Actual:       len(artifacts),
		})
	}
	schemaCount := len(byKind["schema"])
	report.Summary.SchemaArtifacts = schemaCount
	if schemaCount >= len(datapanSchemaFiles()) {
		report.addReadinessGate(releaseReadinessGate{
			ID:       "schema_set_complete",
			Status:   "pass",
			Severity: "required",
			Message:  "release includes all Datapan schema files known to this CLI",
			Expected: len(datapanSchemaFiles()),
			Actual:   schemaCount,
		})
	} else {
		report.addReadinessGate(releaseReadinessGate{
			ID:       "schema_set_complete",
			Status:   "fail",
			Severity: "required",
			Message:  "release is missing one or more Datapan schema files known to this CLI",
			Expected: len(datapanSchemaFiles()),
			Actual:   schemaCount,
		})
	}
	registrySpecs, registryGate := releaseRegistryReadinessGate(root, byKind["registry"])
	report.Summary.RegistrySpecs = registrySpecs
	report.addReadinessGate(registryGate)
	report.Ready = report.Summary.Failed == 0
	return report, nil
}

func readReleaseManifest(path string) (releaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseManifest{}, err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return releaseManifest{}, err
	}
	return manifest, nil
}

func releaseArtifactsByKind(artifacts []releaseManifestArtifact) map[string][]releaseManifestArtifact {
	out := map[string][]releaseManifestArtifact{}
	for _, artifact := range artifacts {
		out[artifact.Kind] = append(out[artifact.Kind], artifact)
	}
	return out
}

func releaseRegistryReadinessGate(root string, artifacts []releaseManifestArtifact) (int, releaseReadinessGate) {
	if len(artifacts) == 0 {
		return 0, releaseReadinessGate{
			ID:           "registry_has_specs",
			Status:       "fail",
			Severity:     "required",
			Message:      "registry artifact is missing",
			ArtifactKind: "registry",
			Expected:     1,
		}
	}
	path, ok := releaseArtifactPath(root, artifacts[0].Path)
	if !ok {
		return 0, releaseReadinessGate{
			ID:           "registry_has_specs",
			Status:       "fail",
			Severity:     "required",
			Message:      "registry artifact path is invalid",
			ArtifactKind: "registry",
			ArtifactPath: artifacts[0].Path,
		}
	}
	reg, err := datago.LoadRegistry(path)
	if err != nil {
		return 0, releaseReadinessGate{
			ID:           "registry_has_specs",
			Status:       "fail",
			Severity:     "required",
			Message:      "registry artifact cannot be decoded",
			ArtifactKind: "registry",
			ArtifactPath: artifacts[0].Path,
		}
	}
	specs := len(reg.Specs())
	if specs == 0 {
		return 0, releaseReadinessGate{
			ID:           "registry_has_specs",
			Status:       "fail",
			Severity:     "required",
			Message:      "registry contains no specs",
			ArtifactKind: "registry",
			ArtifactPath: artifacts[0].Path,
			Expected:     1,
		}
	}
	return specs, releaseReadinessGate{
		ID:           "registry_has_specs",
		Status:       "pass",
		Severity:     "required",
		Message:      "registry contains normalized specs",
		ArtifactKind: "registry",
		ArtifactPath: artifacts[0].Path,
		Expected:     1,
		Actual:       specs,
	}
}

func (r *releaseReadinessReport) addReadinessGate(gate releaseReadinessGate) {
	r.Summary.GatesTotal++
	switch gate.Status {
	case "pass":
		r.Summary.Passed++
	case "warn":
		r.Summary.Warned++
	case "fail":
		r.Summary.Failed++
	}
	r.Gates = append(r.Gates, gate)
}

func (r *releaseManifestVerificationReport) addManifestFailure(path, reason string) {
	r.OK = false
	r.Failed++
	r.Results = append(r.Results, releaseManifestVerificationResult{
		Path:   path,
		Kind:   "manifest",
		Status: "failed",
		Reason: reason,
	})
}

func verifyReleaseManifestArtifact(root string, artifact releaseManifestArtifact, validator *releaseSchemaValidator) releaseManifestVerificationResult {
	result := releaseManifestVerificationResult{
		Path:           artifact.Path,
		Kind:           artifact.Kind,
		Status:         "verified",
		ExpectedBytes:  artifact.Bytes,
		ExpectedSHA256: artifact.SHA256,
	}
	path, ok := releaseArtifactPath(root, artifact.Path)
	if !ok {
		result.Status = "failed"
		result.Reason = "invalid_path"
		return result
	}
	if artifact.Bytes < 0 {
		result.Status = "failed"
		result.Reason = "invalid_size"
		return result
	}
	if !isSHA256Hex(artifact.SHA256) {
		result.Status = "failed"
		result.Reason = "invalid_checksum"
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.Status = "failed"
		result.Reason = "missing_artifact"
		return result
	}
	result.ActualBytes = int64(len(data))
	sum := sha256.Sum256(data)
	result.ActualSHA256 = fmt.Sprintf("%x", sum)
	if artifact.Bytes != result.ActualBytes {
		result.Status = "failed"
		result.Reason = "size_mismatch"
		return result
	}
	if !strings.EqualFold(artifact.SHA256, result.ActualSHA256) {
		result.Status = "failed"
		result.Reason = "checksum_mismatch"
		return result
	}
	if artifact.Schema != "" {
		if validator == nil {
			result.Status = "failed"
			result.Reason = "schema_unavailable"
			return result
		}
		if err := validator.validate(artifact.Schema, data); err != nil {
			result.Status = "failed"
			result.Reason = "schema_validation_failed"
			return result
		}
	}
	return result
}

func loadReleaseSchemaValidator(root string) (*releaseSchemaValidator, bool, error) {
	dir := filepath.Join(root, "schemas")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	compiler := jsonschema.NewCompiler()
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, true, err
		}
		var meta struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, true, err
		}
		if strings.TrimSpace(meta.ID) == "" {
			return nil, true, fmt.Errorf("schema %s missing $id", path)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, true, err
		}
		if err := compiler.AddResource(meta.ID, doc); err != nil {
			return nil, true, err
		}
		ids = append(ids, meta.ID)
	}
	if len(ids) == 0 {
		return nil, false, nil
	}
	schemas := make(map[string]*jsonschema.Schema, len(ids))
	for _, id := range ids {
		schema, err := compiler.Compile(id)
		if err != nil {
			return nil, true, err
		}
		schemas[id] = schema
	}
	return &releaseSchemaValidator{schemas: schemas}, true, nil
}

func (v *releaseSchemaValidator) validate(schemaID string, data []byte) error {
	schema, ok := v.schemas[schemaID]
	if !ok {
		return fmt.Errorf("schema not found: %s", schemaID)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return schema.Validate(instance)
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' {
			continue
		}
		return false
	}
	return true
}

func releaseArtifactPath(root, artifactPath string) (string, bool) {
	if strings.TrimSpace(artifactPath) == "" || strings.Contains(artifactPath, `\`) {
		return "", false
	}
	rel := filepath.Clean(filepath.FromSlash(artifactPath))
	if filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	rootClean := filepath.Clean(root)
	full := filepath.Join(rootClean, rel)
	check, err := filepath.Rel(rootClean, full)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

func releaseManifestArtifacts(paths releasePaths, includeCatalogDiff, includeVerification bool) []releaseManifestArtifact {
	artifacts := make([]releaseManifestArtifact, 0, len(datapanSchemaFiles())+5)
	for _, schema := range datapanSchemaFiles() {
		artifacts = append(artifacts, releaseManifestArtifact{
			Path: "schemas/" + schemaFileName(schema),
			Kind: "schema",
		})
	}
	artifacts = append(artifacts,
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.SchemaIndexPath), Kind: "schema_index", Schema: "https://schemas.datapan.dev/datapan.schema-index.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.RegistryPath), Kind: "registry", Schema: "https://schemas.datapan.dev/datapan.specs.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.ProviderIndexPath), Kind: "provider_index", Schema: "https://schemas.datapan.dev/datapan.provider-index.v1.schema.json"},
	)
	if includeCatalogDiff {
		artifacts = append(artifacts,
			releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.CatalogDiffPath), Kind: "catalog_diff", Schema: "https://schemas.datapan.dev/datapan.catalog-diff.v1.schema.json"},
		)
	}
	artifacts = append(artifacts,
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.CatalogAuditPath), Kind: "catalog_audit", Schema: "https://schemas.datapan.dev/datapan.catalog-audit.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.ErrorCatalogPath), Kind: "error_catalog", Schema: "https://schemas.datapan.dev/datapan.error-catalog.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.DependencyInventoryPath), Kind: "dependencies", Schema: "https://schemas.datapan.dev/datapan.dependencies.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.AdapterTargetsPath), Kind: "adapter_targets", Schema: "https://schemas.datapan.dev/datapan.adapter-targets.v1.schema.json"},
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.ProviderBacklogPath), Kind: "provider_backlog", Schema: "https://schemas.datapan.dev/datapan.providers.v1.schema.json"},
	)
	if includeVerification {
		artifacts = append(artifacts,
			releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.VerificationPath), Kind: "verification", Schema: "https://schemas.datapan.dev/datapan.verification.v1.schema.json"},
			releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.VerificationSummaryPath), Kind: "verification_summary", Schema: "https://schemas.datapan.dev/datapan.verification-summary.v1.schema.json"},
		)
	}
	artifacts = append(artifacts, releaseManifestArtifact{
		Path: releaseRelativePath(paths.OutputDir, paths.ProvenancePath),
		Kind: "provenance",
	})
	return artifacts
}

func releaseArtifactMetadata(outputDir string, artifact releaseManifestArtifact) (releaseManifestArtifact, error) {
	path := joinPath(outputDir, artifact.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseManifestArtifact{}, err
	}
	sum := sha256.Sum256(data)
	artifact.Bytes = int64(len(data))
	artifact.SHA256 = fmt.Sprintf("%x", sum)
	return artifact, nil
}

func releaseRelativePath(outputDir, path string) string {
	rel, err := filepath.Rel(filepath.Clean(outputDir), filepath.Clean(path))
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func readVerificationReport(path string) (datago.VerificationReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return datago.VerificationReport{}, err
	}
	var report datago.VerificationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return datago.VerificationReport{}, err
	}
	return report, nil
}

func emptyIfFalse(value string, ok bool) string {
	if !ok {
		return ""
	}
	return value
}

func releaseProvenance(generatedAt, registryPath, previousRegistryPath, verificationPath string, providerLimit int, paths releasePaths) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# data.go.kr Release Provenance")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- generated_at: %s\n", generatedAt)
	fmt.Fprintf(&b, "- datapan_version: %s\n", version)
	fmt.Fprintf(&b, "- source_provider: data.go.kr\n")
	fmt.Fprintf(&b, "- source_registry: %s\n", registryPath)
	if previousRegistryPath != "" {
		fmt.Fprintf(&b, "- previous_registry: %s\n", previousRegistryPath)
	}
	fmt.Fprintf(&b, "- release_registry: %s\n", paths.RegistryPath)
	fmt.Fprintf(&b, "- provider_limit: %d\n", providerLimit)
	if verificationPath != "" {
		fmt.Fprintf(&b, "- verification_source: %s\n", verificationPath)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Commands")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "```bash\n")
	fmt.Fprintf(&b, "datapan catalog release draft --registry %s --output-dir %s --provider-limit %d", registryPath, paths.OutputDir, providerLimit)
	if previousRegistryPath != "" {
		fmt.Fprintf(&b, " --previous-registry %s", previousRegistryPath)
	}
	if verificationPath != "" {
		fmt.Fprintf(&b, " --verification %s", verificationPath)
	}
	fmt.Fprintf(&b, " --json\n")
	fmt.Fprintf(&b, "# provider index: %s\n", paths.ProviderIndexPath)
	if previousRegistryPath != "" {
		fmt.Fprintf(&b, "datapan catalog diff --old %s --new %s --limit 0 --output %s --json\n", previousRegistryPath, paths.RegistryPath, paths.CatalogDiffPath)
	}
	fmt.Fprintf(&b, "datapan catalog audit --registry %s --output %s --json\n", paths.RegistryPath, paths.CatalogAuditPath)
	fmt.Fprintf(&b, "datapan catalog errors --registry %s --output %s --json\n", paths.RegistryPath, paths.ErrorCatalogPath)
	fmt.Fprintf(&b, "datapan catalog dependencies --registry %s --limit 0 --output %s --json\n", paths.RegistryPath, paths.DependencyInventoryPath)
	fmt.Fprintf(&b, "datapan catalog adapter-targets --registry %s --limit 0 --output %s --json\n", paths.RegistryPath, paths.AdapterTargetsPath)
	fmt.Fprintf(&b, "datapan catalog providers --registry %s --limit %d --output %s --json\n", paths.RegistryPath, providerLimit, paths.ProviderBacklogPath)
	if verificationPath != "" {
		fmt.Fprintf(&b, "datapan catalog verify --input %s --json\n", paths.VerificationPath)
		fmt.Fprintf(&b, "datapan catalog verify summary --input %s --output %s --json\n", paths.VerificationPath, paths.VerificationSummaryPath)
	}
	fmt.Fprintf(&b, "```\n")
	return b.String()
}

func filepathDir(path string) string {
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return ""
	}
	return path[:idx]
}

func writeCSV(w io.Writer, rows []map[string]any) int {
	cw := csv.NewWriter(w)
	headers := make([]string, 0)
	seen := map[string]bool{}
	for _, row := range rows {
		for key := range row {
			if !seen[key] {
				seen[key] = true
				headers = append(headers, key)
			}
		}
	}
	sort.Strings(headers)
	if err := cw.Write(headers); err != nil {
		return exitRequest
	}
	for _, row := range rows {
		record := make([]string, len(headers))
		for i, key := range headers {
			record[i] = fmt.Sprint(row[key])
		}
		if err := cw.Write(record); err != nil {
			return exitRequest
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return exitRequest
	}
	return exitOK
}

func applyNextSteps(spec datago.Spec, smokeCommand string) []string {
	steps := []string{
		"Log in to data.go.kr with the account that owns your service key.",
		"Open the application page and click 활용신청 if the service is not already approved.",
		"Paste the purpose text into the usage-purpose field.",
		"Submit the application, then wait for approval if the portal marks it pending.",
		"Keep the API key in an environment variable; do not paste it into docs, issues, logs, or chat.",
	}
	if smokeCommand != "" {
		steps = append(steps, "After approval, run: "+smokeCommand)
	}
	return steps
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard command found; install wl-copy, xclip, or xsel")
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}

func openURL(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
