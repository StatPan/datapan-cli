package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sourceProfileVerification is deliberately metadata-only: it never persists
// request URLs, response bodies, credential values, or credential hashes.
type sourceProfileVerification struct {
	SchemaVersion        string                             `json:"schema_version"`
	GeneratedAt          string                             `json:"generated_at"`
	SourceID             string                             `json:"source_id"`
	Provider             string                             `json:"provider"`
	SourceProfile        string                             `json:"source_profile"`
	CandidateBatch       string                             `json:"candidate_batch"`
	Bounded              bool                               `json:"bounded"`
	CredentialConfigured bool                               `json:"credential_configured"`
	CredentialEnvNames   []string                           `json:"credential_env_names"`
	Summary              sourceProfileVerificationSummary   `json:"summary"`
	Results              []sourceProfileVerificationResult  `json:"results"`
	Redaction            sourceProfileVerificationRedaction `json:"redaction"`
}

type sourceProfileVerificationSummary struct {
	Candidates int `json:"candidates"`
	Verified   int `json:"verified"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
}

type sourceProfileVerificationResult struct {
	CandidateID   string `json:"candidate_id"`
	Outcome       string `json:"outcome"`
	ErrorClass    string `json:"error_class"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	ResponseBytes int64  `json:"response_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

type sourceProfileVerificationRedaction struct {
	SecretValuesPresent   bool `json:"secret_values_present"`
	SecretHashesPresent   bool `json:"secret_hashes_present"`
	RequestURLsPresent    bool `json:"request_urls_present"`
	ResponseBodiesPresent bool `json:"response_bodies_present"`
}

type sourceProfileInput struct {
	SourceID string `json:"source_id"`
	Provider string `json:"provider"`
}

type sourceCandidateBatch struct {
	SourceID   string            `json:"source_id"`
	Provider   string            `json:"provider"`
	Candidates []sourceCandidate `json:"candidates"`
}

type sourceCandidate struct {
	CandidateID      string            `json:"candidate_id"`
	Method           string            `json:"method"`
	EndpointTemplate string            `json:"endpoint_template"`
	Format           string            `json:"format"`
	SampleParameters map[string]string `json:"sample_parameters"`
	CredentialPolicy struct {
		Required          bool   `json:"required"`
		InjectionLocation string `json:"injection_location"`
		Placeholder       string `json:"placeholder"`
	} `json:"credential_policy"`
}

func (a app) verifySourceProfile(args []string, jsonOut bool) int {
	localJSON, args := consumeBool(args, "--json")
	jsonOut = jsonOut || localJSON
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
	limit, args, err := consumeInt(args, "--limit", 1)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	timeout, args, err := consumeDuration(args, "--timeout", defaultCallTimeout)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	output, args, err := consumeString(args, "--output", "")
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if profilePath == "" || candidatePath == "" || credentialEnv == "" || output == "" || limit <= 0 || len(args) != 0 {
		return a.fail(exitUsage, "usage: datapan verify --source-profile PATH --candidates PATH --credential-env NAME --limit N --output PATH [--timeout DURATION] [--json]")
	}

	profile, err := readSourceProfile(profilePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	batch, err := readSourceCandidateBatch(candidatePath)
	if err != nil {
		return a.fail(exitUsage, "%v", err)
	}
	if profile.SourceID == "" || profile.Provider == "" || batch.SourceID != profile.SourceID || batch.Provider != profile.Provider {
		return a.fail(exitUsage, "source profile and candidate batch must identify the same non-empty source and provider")
	}
	credential, configured := a.env.LookupEnv(credentialEnv)
	credential = strings.TrimSpace(credential)
	configured = configured && credential != ""
	report := sourceProfileVerification{
		SchemaVersion:        "datapan.source-candidate-verification.v1",
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		SourceID:             profile.SourceID,
		Provider:             profile.Provider,
		SourceProfile:        profilePath,
		CandidateBatch:       candidatePath,
		Bounded:              true,
		CredentialConfigured: configured,
		CredentialEnvNames:   []string{credentialEnv},
		Results:              make([]sourceProfileVerificationResult, 0),
		Redaction:            sourceProfileVerificationRedaction{},
	}
	for _, candidate := range batch.Candidates {
		if len(report.Results) >= limit {
			break
		}
		result := a.verifySourceCandidate(candidate, credential, configured, timeout)
		report.Results = append(report.Results, result)
		report.Summary.Candidates++
		switch result.Outcome {
		case "verified":
			report.Summary.Verified++
		case "failed":
			report.Summary.Failed++
		default:
			report.Summary.Skipped++
		}
	}
	if err := writeJSONFile(output, report); err != nil {
		return a.fail(exitRequest, "%v", err)
	}
	if jsonOut {
		if code := a.writeJSON(map[string]any{"ok": report.Summary.Failed == 0, "output": output, "report": report}); code != exitOK {
			return code
		}
		return verificationExitCodeForSource(report.Summary)
	}
	fmt.Fprintf(a.stdout, "Source profile verification: %s (verified=%d failed=%d skipped=%d)\n", profile.SourceID, report.Summary.Verified, report.Summary.Failed, report.Summary.Skipped)
	return verificationExitCodeForSource(report.Summary)
}

func readSourceProfile(path string) (sourceProfileInput, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return sourceProfileInput{}, fmt.Errorf("read source profile: %w", err)
	}
	var profile sourceProfileInput
	if err := json.Unmarshal(data, &profile); err != nil {
		return sourceProfileInput{}, fmt.Errorf("decode source profile: %w", err)
	}
	return profile, nil
}

func readSourceCandidateBatch(path string) (sourceCandidateBatch, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return sourceCandidateBatch{}, fmt.Errorf("read source candidates: %w", err)
	}
	var batch sourceCandidateBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		return sourceCandidateBatch{}, fmt.Errorf("decode source candidates: %w", err)
	}
	if len(batch.Candidates) == 0 {
		return sourceCandidateBatch{}, fmt.Errorf("source candidates must not be empty")
	}
	return batch, nil
}

func (a app) verifySourceCandidate(candidate sourceCandidate, credential string, configured bool, timeout time.Duration) sourceProfileVerificationResult {
	started := time.Now()
	result := sourceProfileVerificationResult{CandidateID: candidate.CandidateID, Outcome: "failed", ErrorClass: "unknown"}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	if candidate.CandidateID == "" || candidate.EndpointTemplate == "" || candidate.Method == "" {
		result.ErrorClass = "parameter"
		return result
	}
	if candidate.CredentialPolicy.Required && !configured {
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
	response, err := a.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			result.ErrorClass = "timeout"
		} else {
			result.ErrorClass = "network"
		}
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	result.ContentType = response.Header.Get("Content-Type")
	result.ResponseBytes, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if strings.EqualFold(candidate.Format, "json") && !strings.Contains(strings.ToLower(result.ContentType), "json") {
			result.ErrorClass = "parser"
			return result
		}
		result.Outcome, result.ErrorClass = "verified", "none"
		return result
	}
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.ErrorClass = "credential"
	case http.StatusTooManyRequests:
		result.ErrorClass = "rate_limit"
	default:
		result.ErrorClass = "provider"
	}
	return result
}

func sourceCandidateURL(candidate sourceCandidate, credential string) (string, error) {
	values := make(map[string]string, len(candidate.SampleParameters))
	for key, value := range candidate.SampleParameters {
		values[key] = value
	}
	if candidate.CredentialPolicy.Required {
		for key, value := range values {
			if value == candidate.CredentialPolicy.Placeholder {
				values[key] = credential
			}
		}
	}
	endpoint := candidate.EndpointTemplate
	for key, value := range values {
		endpoint = strings.ReplaceAll(endpoint, "{"+key+"}", url.PathEscape(value))
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid endpoint template")
	}
	query := parsed.Query()
	for key, value := range values {
		if strings.Contains(candidate.EndpointTemplate, "{"+key+"}") {
			continue
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func verificationExitCodeForSource(summary sourceProfileVerificationSummary) int {
	if summary.Failed > 0 {
		return exitRequest
	}
	if summary.Skipped > 0 {
		return exitAuth
	}
	return exitOK
}
