package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

const (
	exitOK       = 0
	exitUsage    = 1
	exitNotFound = 2
	exitAuth     = 3
	exitRequest  = 4
)

const version = "0.1.0-dev"

const defaultStorageStatePath = ".datapan/data-go-kr-browser-state.json"
const defaultBrowserProfilePath = ".datapan/browser-profile"

type Env interface {
	LookupEnv(string) (string, bool)
}

type RealEnv struct{}

func (RealEnv) LookupEnv(name string) (string, bool) { return os.LookupEnv(name) }

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type RealHTTPClient struct{}

func (RealHTTPClient) Do(req *http.Request) (*http.Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

type app struct {
	args   []string
	stdout io.Writer
	stderr io.Writer
	env    Env
	http   HTTPClient
	reg    datago.Registry
}

func Run(args []string, stdout, stderr io.Writer, env Env, httpClient HTTPClient) int {
	a := app{
		args:   args,
		stdout: stdout,
		stderr: stderr,
		env:    env,
		http:   httpClient,
		reg:    datago.DefaultRegistry(),
	}
	return a.run()
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
	case "auth":
		return a.auth(args[1:], jsonOut)
	case "access":
		return a.access(args[1:], jsonOut)
	case "apply":
		return a.access(args[1:], jsonOut)
	case "call":
		return a.call(args[1:], jsonOut, false)
	case "export":
		return a.export(args[1:], jsonOut)
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
			"results": results,
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

func (a app) info(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
	if len(args) != 1 {
		return a.fail(exitUsage, "usage: datapan info <list-id> [--json]")
	}
	spec, ok := a.reg.ByID(args[0])
	if !ok {
		return a.fail(exitNotFound, "unknown data.go.kr list id %q", args[0])
	}
	if jsonOut {
		return a.writeJSON(map[string]any{"ok": true, "spec": spec})
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
	if len(spec.Operations) > 0 {
		fmt.Fprintln(a.stdout, "  operations:")
		for _, op := range spec.Operations {
			fmt.Fprintf(a.stdout, "    - %s", op.Name)
			if op.Endpoint != "" {
				fmt.Fprintf(a.stdout, " (%s)", op.Endpoint)
			}
			fmt.Fprintln(a.stdout)
		}
	}
	return exitOK
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
		return a.fail(exitUsage, "usage: datapan access <list-id> [--open] [--copy-purpose] [--start] [--purpose] [--json]")
	}
	spec, ok := a.reg.ByID(args[0])
	if !ok {
		return a.fail(exitNotFound, "unknown data.go.kr list id %q", args[0])
	}
	opened := false
	if openBrowser {
		if err := openURL(spec.ApplicationURL()); err != nil {
			return a.fail(exitRequest, "failed to open browser: %v", err)
		}
		opened = true
	}
	copied := false
	copyError := ""
	if copyPurpose {
		if err := copyToClipboard(datago.PurposeTextKO); err != nil {
			copyError = err.Error()
			if jsonOut {
				return a.fail(exitRequest, "failed to copy purpose text: %v", err)
			}
		} else {
			copied = true
		}
	}
	smokeCommand := spec.SmokeCommand()
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
		return a.fail(exitUsage, "usage: datapan access <list-id> [--dry-run|--apply] [--profile-dir PATH] [--browser-path PATH] [--output PATH] [--json]")
	}
	spec, ok := a.reg.ByID(args[0])
	if !ok {
		return a.fail(exitNotFound, "unknown data.go.kr list id %q", args[0])
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
	if len(args) != 1 {
		return a.fail(exitUsage, "usage: datapan call <list-id> [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]")
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
		return a.mapError(err)
	}
	if dryRun {
		payload := map[string]any{
			"ok":           true,
			"dry_run":      true,
			"dataset":      args[0],
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
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut || exportMode {
		return a.writeJSON(respPayload)
	}
	fmt.Fprintln(a.stdout, respPayload.Body)
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

func (a app) exportFromCall(args []string, jsonOut bool, format string) int {
	capture := bytes.Buffer{}
	code := app{args: a.args, stdout: &capture, stderr: a.stderr, env: a.env, http: a.http, reg: a.reg}.call(args, true, true)
	if code != exitOK {
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

func (a app) requestPlan(id, operation string, params map[string]string) (requestPlan, string, error) {
	spec, ok := a.reg.ByID(id)
	if !ok {
		return requestPlan{}, "", errNotFound{id}
	}
	op, ok := spec.Operation(operation)
	if !ok {
		if operation == "" {
			return requestPlan{}, "", fmt.Errorf("spec %s has no callable operation endpoint yet", id)
		}
		return requestPlan{}, "", fmt.Errorf("unknown operation %q for %s", operation, id)
	}
	keyName, key, ok := a.resolveKeyValue()
	if !ok {
		return requestPlan{}, "", errAuth{}
	}
	u, err := url.Parse(op.Endpoint)
	if err != nil {
		return requestPlan{}, "", err
	}
	q := u.Query()
	q.Set("serviceKey", key)
	for k, v := range params {
		q.Set(k, v)
	}
	for k, v := range op.DefaultParams {
		if q.Get(k) == "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
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
	return datago.ResponseEnvelope{
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
		Provider:   "data.go.kr",
		Dataset:    plan.Spec.ID,
		Operation:  plan.Operation.Name,
		StatusCode: resp.StatusCode,
		URL:        plan.RedactedURL,
		Body:       string(body),
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

func (a app) mapError(err error) int {
	var nf errNotFound
	if errors.As(err, &nf) {
		return a.fail(exitNotFound, "unknown data.go.kr list id %q", nf.id)
	}
	var auth errAuth
	if errors.As(err, &auth) {
		return a.fail(exitAuth, "missing data.go.kr API key; set one of: %s", strings.Join(datago.KeyEnvNames, ", "))
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
  datapan info <list-id> [--json]
  datapan auth check [--json]
  datapan access <list-id> [--open] [--copy-purpose] [--start] [--purpose] [--json]
  datapan access login [--headed] [--manual-login-wait-ms N] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan access <list-id> [--dry-run|--apply] [--profile-dir PATH] [--browser-path PATH] [--json]
  datapan call <list-id> [--operation NAME] [--param k=v] [--params-file PATH|-] [--dry-run] [--json]
  datapan export --input PATH|- [--format csv|json]
  datapan version [--json]

Accepted data.go.kr key env vars:
  DATAPAN_DATA_GO_KR_KEY, DATA_PORTAL_API_KEY, DATA_GO_KR_SERVICE_KEY`)
}

type errNotFound struct{ id string }

func (e errNotFound) Error() string { return "not found: " + e.id }

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
