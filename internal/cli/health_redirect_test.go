package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type healthRedirectTransport func(*http.Request) (*http.Response, error)

func (f healthRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHealthClientDoesNotFollowProviderRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var firstCalls, redirectedCalls atomic.Int32
			redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirectedCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer redirected.Close()
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls.Add(1)
				w.Header().Set("Location", redirected.URL+"/?serviceKey=credential-secret")
				w.WriteHeader(status)
			}))
			defer first.Close()
			for _, client := range []HTTPClient{nil, RealHTTPClient{}, &RealHTTPClient{}, &http.Client{Timeout: time.Second}} {
				req, err := http.NewRequest(http.MethodGet, first.URL+"/?serviceKey=credential-secret", nil)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := healthSingleRequestClient(client).Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != status {
					t.Fatalf("redirect status changed: got %d, want %d", resp.StatusCode, status)
				}
			}
			if firstCalls.Load() != 4 || redirectedCalls.Load() != 0 {
				t.Fatalf("provider requests=%d redirected requests=%d", firstCalls.Load(), redirectedCalls.Load())
			}
		})
	}
}

func TestHealthVerifyRejectsRedirectWithOneRedactedReceipt(t *testing.T) {
	root := setupHealthProbeRegistry(t, gatewayRegistryJSON())
	var firstCalls, redirectedCalls, callerRedirectCalls atomic.Int32
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":{"header":{"resultCode":"00"},"body":{"items":[{"private":"row-hidden"}]}}}`))
	}))
	defer redirected.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		if r.URL.Query().Get("serviceKey") != "credential-secret" {
			t.Error("first request did not receive the synthetic credential")
		}
		w.Header().Set("Location", redirected.URL+"/?serviceKey=credential-secret")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	firstURL, err := url.Parse(first.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		callerRedirectCalls.Add(1)
		return nil
	}, Transport: healthRedirectTransport(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		if req.URL.Host == "apis.data.go.kr" {
			copy.URL.Scheme, copy.URL.Host = firstURL.Scheme, firstURL.Host
		}
		return transport.RoundTrip(copy)
	})}
	output := filepath.Join(root, "receipt.json")
	code, stdout, stderr := runTest([]string{"verify", "--ref", "100", "--operation", "list", "--health", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitRequest || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if firstCalls.Load() != 1 || redirectedCalls.Load() != 0 || callerRedirectCalls.Load() != 0 {
		t.Fatalf("requests=%d redirects=%d caller callbacks=%d", firstCalls.Load(), redirectedCalls.Load(), callerRedirectCalls.Load())
	}
	assertHealthReceipt(t, output, "semantic_failure", "indeterminate")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var receipt healthProbeReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Execution.RequestBudget != 1 {
		t.Fatalf("wrong request budget: %d", receipt.Execution.RequestBudget)
	}
	if receipt.Observation.HTTPStatus != http.StatusTemporaryRedirect || receipt.Assessment.Outcome != "unhealthy" {
		t.Fatal("redirect was not recorded as the original unhealthy HTTP response")
	}
	for _, forbidden := range []string{"credential-secret", "serviceKey=", "row-hidden", first.URL, redirected.URL} {
		if strings.Contains(string(data)+stdout+stderr, forbidden) {
			t.Fatalf("public output contains forbidden diagnostic value")
		}
	}
	// The supplied client retains its original redirect policy after Health runs.
	resp, err := client.Get(first.URL + "/?serviceKey=credential-secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || redirectedCalls.Load() != 1 || callerRedirectCalls.Load() != 1 {
		t.Fatal("Health changed the caller's redirect policy")
	}
}

func TestOrdinaryRealHTTPClientRetainsDownloadRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/download" {
			w.Header().Set("Location", "/asset")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (RealHTTPClient{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatal("ordinary artifact download no longer follows its redirect")
	}
}
