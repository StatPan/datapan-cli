package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
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
	case "export":
		return a.export(args[1:], jsonOut)
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
		return a.fail(exitUsage, "usage: datapan catalog import data-go-kr ... | datapan catalog update data-go-kr ... | datapan catalog diff --old OLD --new NEW [--json] | datapan catalog audit [--registry PATH] [--json] | datapan catalog providers [--registry PATH] [--status STATUS] [--kind KIND] [--output PATH] [--json] | datapan catalog verify [--registry PATH|--input REPORT|summary] [--json] | datapan catalog release draft --registry PATH [--json] | datapan catalog release verify --manifest PATH [--json]")
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
	case "providers":
		return a.catalogProviders(args[1:], jsonOut)
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
	if oldPath == "" && len(args) > 0 {
		oldPath = args[0]
		args = args[1:]
	}
	if newPath == "" && len(args) > 0 {
		newPath = args[0]
		args = args[1:]
	}
	if oldPath == "" || newPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog diff --old OLD --new NEW [--json]")
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
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":        true,
			"old":       oldPath,
			"new":       newPath,
			"summary":   diff.Summary,
			"limit":     limit,
			"truncated": diffTruncated(diff, limit),
			"added":     specSummaries(limitSpecs(diff.Added, limit)),
			"removed":   specSummaries(limitSpecs(diff.Removed, limit)),
			"changed":   limitChanges(diff.Changed, limit),
			"counts": map[string]int{
				"old": len(oldReg.Specs()),
				"new": len(newReg.Specs()),
			},
		})
	}
	fmt.Fprintf(a.stdout, "Catalog diff: %s -> %s\n", oldPath, newPath)
	fmt.Fprintf(a.stdout, "  added: %d\n", diff.Summary.Added)
	fmt.Fprintf(a.stdout, "  removed: %d\n", diff.Summary.Removed)
	fmt.Fprintf(a.stdout, "  changed: %d\n", diff.Summary.Changed)
	fmt.Fprintf(a.stdout, "  stable: %d\n", diff.Summary.Stable)
	for _, spec := range limitSpecs(diff.Added, limit) {
		fmt.Fprintf(a.stdout, "+ %s  %s\n", spec.ID, spec.Title)
	}
	for _, spec := range limitSpecs(diff.Removed, limit) {
		fmt.Fprintf(a.stdout, "- %s  %s\n", spec.ID, spec.Title)
	}
	for _, change := range limitChanges(diff.Changed, limit) {
		fmt.Fprintf(a.stdout, "~ %s  %s\n", change.ID, strings.Join(change.Fields, ","))
	}
	if diffTruncated(diff, limit) {
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
	if len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog audit [--registry PATH] [--sample N] [--json]")
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
	if jsonOut {
		return a.writeJSON(map[string]any{
			"ok":       true,
			"registry": registryPath,
			"audit":    audit,
		})
	}
	fmt.Fprintln(a.stdout, "Catalog audit")
	if registryPath != "" {
		fmt.Fprintf(a.stdout, "  registry: %s\n", registryPath)
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
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]\n       datapan catalog release verify --manifest PATH [--json]")
	}
	switch args[0] {
	case "draft":
		return a.catalogReleaseDraft(args[1:], jsonOut)
	case "verify":
		return a.catalogReleaseVerify(args[1:], jsonOut)
	default:
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]\n       datapan catalog release verify --manifest PATH [--json]")
	}
}

func (a app) catalogReleaseDraft(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	registryPath, args, err := consumeString(args, "--registry", "")
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
		return a.fail(exitUsage, "usage: datapan catalog release draft --registry PATH [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]")
	}
	reg, err := datago.LoadRegistry(registryPath)
	if err != nil {
		return a.catalogDiffFailure(jsonOut, "registry", registryPath, err)
	}
	return a.writeReleaseDraft(reg, registryPath, outputDir, verificationPath, providerLimit, jsonOut)
}

func (a app) catalogReleaseVerify(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	manifestPath, args, err := consumeString(args, "--manifest", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if manifestPath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan catalog release verify --manifest PATH [--json]")
	}
	report, err := verifyReleaseManifest(manifestPath)
	if err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		code := exitOK
		if !report.OK {
			code = exitRequest
		}
		if writeCode := a.writeJSON(map[string]any{
			"ok":     report.OK,
			"report": report,
		}); writeCode != exitOK {
			return writeCode
		}
		return code
	}
	fmt.Fprintln(a.stdout, "Release manifest verification")
	fmt.Fprintf(a.stdout, "  manifest: %s\n", report.Manifest)
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

func (a app) writeReleaseDraft(reg datago.Registry, registryPath, outputDir, verificationPath string, providerLimit int, jsonOut bool) int {
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
	specs := reg.Specs()
	if err := writeRegistryAtomic(paths.RegistryPath, specs); err != nil {
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
	provenance := releaseProvenance(generatedAt, registryPath, verificationPath, providerLimit, paths)
	if err := writeOutput(paths.ProvenancePath, []byte(provenance), a.stdout); err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	manifest, err := writeReleaseManifest(generatedAt, registryPath, verificationCopied, paths)
	if err != nil {
		return a.releaseFailure(jsonOut, err)
	}
	payload := map[string]any{
		"ok":                           true,
		"output_dir":                   outputDir,
		"generated_at":                 generatedAt,
		"registry":                     paths.RegistryPath,
		"schemas":                      datapanSchemaFileNames(),
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
  datapan catalog diff --old OLD --new NEW [--limit N] [--json]
  datapan catalog audit [--registry PATH] [--sample N] [--json]
  datapan catalog providers [--registry PATH] [--limit N] [--sample N] [--status STATUS] [--kind KIND] [--provider NAME] [--output PATH|-] [--json]
  datapan catalog verify [--registry PATH] [--ref REF] [--operation NAME] [--limit N] [--provider NAME] [--host HOST] [--kind KIND] [--output PATH|-] [--json]
  datapan catalog verify --input REPORT [--status verified|failed|skipped|unknown] [--limit N] [--output PATH|-] [--json]
  datapan catalog verify summary --input REPORT [--limit N] [--output PATH|-] [--json]
  datapan catalog release draft --registry PATH [--output-dir DIR] [--verification PATH] [--provider-limit N] [--json]
  datapan catalog release verify --manifest PATH [--json]
  datapan show <ref> [--json]
  datapan auth check [--json]
  datapan access <ref> [--open] [--copy-purpose] [--start] [--purpose] [--json]
  datapan access login [--headed] [--manual-login-wait-ms N] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan access <ref> [--dry-run|--apply] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan get <ref> [KEY=VALUE ...] [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]
  datapan save <ref> [KEY=VALUE ...] [--format csv|json] [--output PATH|-] [--json]
  datapan call <ref> [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]
  datapan export --input PATH|- [--format csv|json]
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
	DataDir                 string
	ReportsDir              string
	ProvenanceDir           string
	RegistryPath            string
	ProviderBacklogPath     string
	VerificationPath        string
	VerificationSummaryPath string
	ProvenancePath          string
	ManifestPath            string
}

func releaseDraftPaths(outputDir string) releasePaths {
	return releasePaths{
		OutputDir:               outputDir,
		SchemaDir:               joinPath(outputDir, "schemas"),
		DataDir:                 joinPath(outputDir, "data"),
		ReportsDir:              joinPath(outputDir, "reports"),
		ProvenanceDir:           joinPath(outputDir, "provenance"),
		RegistryPath:            joinPath(outputDir, "data/data-go-kr.registry.json"),
		ProviderBacklogPath:     joinPath(outputDir, "reports/provider-backlog.json"),
		VerificationPath:        joinPath(outputDir, "reports/latest-verification.json"),
		VerificationSummaryPath: joinPath(outputDir, "reports/latest-verification-summary.json"),
		ProvenancePath:          joinPath(outputDir, "provenance/data-go-kr.md"),
		ManifestPath:            joinPath(outputDir, "manifest.json"),
	}
}

func datapanSchemaFiles() []string {
	return []string{
		"schemas/datapan.specs.v1.schema.json",
		"schemas/datapan.providers.v1.schema.json",
		"schemas/datapan.verification.v1.schema.json",
		"schemas/datapan.verification-summary.v1.schema.json",
		"schemas/datapan.release-manifest.v1.schema.json",
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

type releaseManifestVerificationReport struct {
	Manifest      string                              `json:"manifest"`
	Root          string                              `json:"root"`
	SchemaVersion string                              `json:"schema_version,omitempty"`
	ArtifactCount int                                 `json:"artifact_count"`
	Checked       int                                 `json:"checked"`
	Failed        int                                 `json:"failed"`
	OK            bool                                `json:"ok"`
	Results       []releaseManifestVerificationResult `json:"results"`
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

func writeReleaseManifest(generatedAt, sourceRegistry string, includeVerification bool, paths releasePaths) (releaseManifest, error) {
	artifacts := releaseManifestArtifacts(paths, includeVerification)
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
		Manifest:      manifestPath,
		Root:          root,
		SchemaVersion: manifest.SchemaVersion,
		ArtifactCount: manifest.ArtifactCount,
		Checked:       len(manifest.Artifacts),
		OK:            true,
		Results:       make([]releaseManifestVerificationResult, 0, len(manifest.Artifacts)+1),
	}
	if manifest.ArtifactCount != len(manifest.Artifacts) {
		report.OK = false
		report.Failed++
		report.Results = append(report.Results, releaseManifestVerificationResult{
			Path:   manifestPath,
			Kind:   "manifest",
			Status: "failed",
			Reason: "artifact_count_mismatch",
		})
	}
	for _, artifact := range manifest.Artifacts {
		result := verifyReleaseManifestArtifact(root, artifact)
		if result.Status == "failed" {
			report.OK = false
			report.Failed++
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func verifyReleaseManifestArtifact(root string, artifact releaseManifestArtifact) releaseManifestVerificationResult {
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
	return result
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

func releaseManifestArtifacts(paths releasePaths, includeVerification bool) []releaseManifestArtifact {
	artifacts := make([]releaseManifestArtifact, 0, len(datapanSchemaFiles())+5)
	for _, schema := range datapanSchemaFiles() {
		artifacts = append(artifacts, releaseManifestArtifact{
			Path: "schemas/" + schemaFileName(schema),
			Kind: "schema",
		})
	}
	artifacts = append(artifacts,
		releaseManifestArtifact{Path: releaseRelativePath(paths.OutputDir, paths.RegistryPath), Kind: "registry", Schema: "https://schemas.datapan.dev/datapan.specs.v1.schema.json"},
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

func releaseProvenance(generatedAt, registryPath, verificationPath string, providerLimit int, paths releasePaths) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# data.go.kr Release Provenance")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- generated_at: %s\n", generatedAt)
	fmt.Fprintf(&b, "- datapan_version: %s\n", version)
	fmt.Fprintf(&b, "- source_provider: data.go.kr\n")
	fmt.Fprintf(&b, "- source_registry: %s\n", registryPath)
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
	if verificationPath != "" {
		fmt.Fprintf(&b, " --verification %s", verificationPath)
	}
	fmt.Fprintf(&b, " --json\n")
	fmt.Fprintf(&b, "datapan catalog audit --registry %s --json\n", paths.RegistryPath)
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
