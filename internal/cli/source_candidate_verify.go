package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type sourceCandidateProfile struct {
	SchemaVersion string `json:"schema_version"`
	SourceID      string `json:"source_id"`
	Provider      string `json:"provider"`
}

type sourceCandidateBatch struct {
	SchemaVersion string                   `json:"schema_version"`
	SourceID      string                   `json:"source_id"`
	Provider      string                   `json:"provider"`
	Candidates    []sourceCandidateRequest `json:"candidates"`
}

type sourceCandidateRequest struct {
	CandidateID      string            `json:"candidate_id"`
	Method           string            `json:"method"`
	EndpointTemplate string            `json:"endpoint_template"`
	Format           string            `json:"format"`
	SampleParameters map[string]string `json:"sample_parameters"`
	CredentialPolicy struct {
		Required          bool     `json:"required"`
		KeyNames          []string `json:"key_names"`
		InjectionLocation string   `json:"injection_location"`
		Placeholder       string   `json:"placeholder"`
	} `json:"credential_policy"`
	ExpectedResponse struct {
		EnvelopePath string `json:"envelope_path"`
		ItemsPath    string `json:"items_path"`
		SuccessCode  string `json:"success_code"`
	} `json:"expected_response"`
}

type sourceCandidateResult struct {
	CandidateID   string `json:"candidate_id"`
	Outcome       string `json:"outcome"`
	ErrorClass    string `json:"error_class"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	ResponseBytes int64  `json:"response_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

type sourceCandidateVerification struct {
	SchemaVersion        string                  `json:"schema_version"`
	GeneratedAt          string                  `json:"generated_at"`
	SourceID             string                  `json:"source_id"`
	Provider             string                  `json:"provider"`
	SourceProfile        string                  `json:"source_profile"`
	CandidateBatch       string                  `json:"candidate_batch"`
	Bounded              bool                    `json:"bounded"`
	CredentialConfigured bool                    `json:"credential_configured"`
	CredentialEnvNames   []string                `json:"credential_env_names"`
	CredentialGroup      string                  `json:"credential_group"`
	Summary              map[string]int          `json:"summary"`
	Results              []sourceCandidateResult `json:"results"`
	Redaction            map[string]bool         `json:"redaction"`
}

func readSourceCandidateJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}

func (a app) sourceCandidateVerify(args []string, jsonOut bool) int {
	profilePath, args, err := consumeString(args, "--source-profile", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	candidatePath, args, err := consumeString(args, "--candidates", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	credentialEnv, args, err := consumeString(args, "--credential-env", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	limit, args, err := consumeInt(args, "--limit", 1)
	if err != nil || limit < 1 {
		return a.fail(exitUsage, "--limit requires a positive integer")
	}
	timeout, args, err := consumeDuration(args, "--timeout", defaultCallTimeout)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if profilePath == "" || candidatePath == "" || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan verify --source-profile PATH --candidates PATH [--credential-env NAME] [--limit N] [--timeout DURATION] [--output PATH] [--json]")
	}
	var profile sourceCandidateProfile
	var batch sourceCandidateBatch
	if err := readSourceCandidateJSON(profilePath, &profile); err != nil {
		return a.fail(exitUsage, "read source profile: %v", err)
	}
	if err := readSourceCandidateJSON(candidatePath, &batch); err != nil {
		return a.fail(exitUsage, "read candidate batch: %v", err)
	}
	if profile.SourceID == "" || profile.SourceID != batch.SourceID {
		return a.fail(exitUsage, "source profile and candidate batch source_id must match")
	}
	credential := ""
	if credentialEnv != "" {
		credential, _ = a.env.LookupEnv(credentialEnv)
	}
	count := limit
	if count > len(batch.Candidates) {
		count = len(batch.Candidates)
	}
	results := make([]sourceCandidateResult, 0, count)
	for _, candidate := range batch.Candidates[:count] {
		results = append(results, a.verifySourceCandidate(candidate, credential, timeout))
	}
	summary := map[string]int{"candidates": len(results), "verified": 0, "failed": 0, "skipped": 0}
	for _, result := range results {
		summary[result.Outcome]++
	}
	envs := []string{}
	if credentialEnv != "" {
		envs = append(envs, credentialEnv)
	}
	report := sourceCandidateVerification{
		SchemaVersion: "datapan.source-candidate-verification.v1", GeneratedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		SourceID: profile.SourceID, Provider: profile.Provider, SourceProfile: profilePath, CandidateBatch: candidatePath,
		Bounded: true, CredentialConfigured: credential != "", CredentialEnvNames: envs, CredentialGroup: "explicit_generic_source", Summary: summary, Results: results,
		Redaction: map[string]bool{"secret_values_present": false, "secret_hashes_present": false, "request_urls_present": false, "response_bodies_present": false},
	}
	if output != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
			return a.fail(exitRequest, "write verification output: %v", err)
		}
	}
	if jsonOut {
		return a.writeJSON(report)
	}
	fmt.Fprintf(a.stdout, "Source candidate verification: source=%s verified=%d failed=%d skipped=%d\n", report.SourceID, summary["verified"], summary["failed"], summary["skipped"])
	return exitOK
}

func (a app) verifySourceCandidate(candidate sourceCandidateRequest, credential string, timeout time.Duration) sourceCandidateResult {
	started := time.Now()
	result := sourceCandidateResult{CandidateID: candidate.CandidateID, Outcome: "failed", ErrorClass: "unknown"}
	if candidate.CredentialPolicy.Required && strings.TrimSpace(credential) == "" {
		result.Outcome, result.ErrorClass = "skipped", "credential"
		return result
	}
	requestURL, err := sourceCandidateURL(candidate, credential)
	if err != nil {
		result.ErrorClass = "parameter"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, candidate.Method, requestURL, nil)
	if err != nil {
		result.ErrorClass = "parameter"
		return result
	}
	req.Header.Set("User-Agent", "datapan-cli/source-candidate-verifier")
	resp, err := a.http.Do(req)
	result.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		if ctx.Err() != nil {
			result.ErrorClass = "timeout"
		} else {
			result.ErrorClass = "network"
		}
		return result
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	result.ContentType = resp.Header.Get("Content-Type")
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	result.ResponseBytes = int64(len(body))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		result.ErrorClass = "credential"
	case resp.StatusCode == http.StatusTooManyRequests:
		result.ErrorClass = "rate_limit"
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if readErr != nil {
			result.ErrorClass = "network"
		} else if strings.EqualFold(candidate.Format, "json") && !json.Valid(body) {
			result.ErrorClass = "parser"
		} else if candidate.ExpectedResponse.SuccessCode != "" && !candidateSuccessCodeMatches(candidate.ExpectedResponse.SuccessCode, resp.StatusCode, body) {
			result.ErrorClass = "provider"
		} else {
			result.Outcome, result.ErrorClass = "verified", "none"
		}
	case resp.StatusCode >= 500:
		result.ErrorClass = "provider"
	default:
		result.ErrorClass = "bad_request"
	}
	return result
}

func candidateSuccessCodeMatches(expected string, status int, body []byte) bool {
	if parsed, err := strconv.Atoi(expected); err == nil {
		return parsed == status
	}
	return jsonContainsString(body, expected)
}

func jsonContainsString(data []byte, expected string) bool {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(current any) bool {
		switch typed := current.(type) {
		case string:
			return typed == expected
		case []any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		case map[string]any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func sourceCandidateURL(candidate sourceCandidateRequest, credential string) (string, error) {
	raw := candidate.EndpointTemplate
	params := make(map[string]string, len(candidate.SampleParameters))
	keys := make([]string, 0, len(candidate.SampleParameters))
	for key, value := range candidate.SampleParameters {
		params[key] = value
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := params[key]
		if value == candidate.CredentialPolicy.Placeholder {
			value = credential
		}
		placeholder := "{" + key + "}"
		if strings.Contains(raw, placeholder) {
			raw = strings.ReplaceAll(raw, placeholder, url.PathEscape(value))
			delete(params, key)
		} else {
			params[key] = value
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for _, key := range keys {
		if value, ok := params[key]; ok {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
