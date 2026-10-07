package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteHealthHTTPRequestBuildsDeclaredRESTRequest(t *testing.T) {
	const credential = "rest-credential-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/records" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("api_key") != credential || r.Header.Get("X-Request-Mode") != "health" || r.Header.Get("Authorization") != "Bearer "+credential {
			t.Errorf("unexpected REST request: method=%q path=%q query=%v", r.Method, r.URL.Path, r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	shape := validHealthHTTPRequestShape(server.URL+"/v1/records?existing=kept", healthHTTPREST)
	shape.Query = url.Values{"page": {"1"}, "api_key": {credential}}
	shape.Headers = http.Header{"X-Request-Mode": {"health"}, "Authorization": {"Bearer " + credential}}
	response, err := executeHealthHTTPRequest(context.Background(), &http.Client{Timeout: time.Second}, shape)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.ContentType != "application/json" || string(response.Body) != `{"ok":true}` {
		t.Fatalf("unexpected bounded response: %#v", response)
	}
}

func TestExecuteHealthHTTPRequestBuildsDeclaredSOAPRequest(t *testing.T) {
	const credential = "soap-credential-secret"
	const action = "urn:registry:LookupRecords"
	const requestBody = `<soap:Envelope><soap:Body><Lookup><page>1</page><apiKey>` + credential + `</apiKey></Lookup></soap:Body></soap:Envelope>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error("could not read synthetic SOAP request body")
		}
		if r.Method != http.MethodPost || r.URL.Path != "/soap" || r.Header.Get("SOAPAction") != action || r.Header.Get("X-API-Key") != credential || string(body) != requestBody {
			t.Errorf("unexpected SOAP request: method=%q path=%q action=%q", r.Method, r.URL.Path, r.Header.Get("SOAPAction"))
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = io.WriteString(w, `<Envelope><Body><resultCode>00</resultCode></Body></Envelope>`)
	}))
	defer server.Close()

	shape := validHealthHTTPRequestShape(server.URL+"/soap", healthHTTPSOAP)
	shape.Method = http.MethodPost
	shape.SOAPAction = action
	shape.Headers = http.Header{"Content-Type": {"text/xml; charset=utf-8"}, "X-API-Key": {credential}}
	shape.Body = []byte(requestBody)
	response, err := executeHealthHTTPRequest(context.Background(), &http.Client{Timeout: time.Second}, shape)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.ContentType != "application/soap+xml" || !strings.Contains(string(response.Body), "resultCode") {
		t.Fatalf("unexpected bounded SOAP response: %#v", response)
	}
}

func TestBuildHealthHTTPRequestRejectsUnsupportedPlansBeforeNetwork(t *testing.T) {
	tests := []struct {
		name   string
		change func(*healthHTTPRequestShape)
	}{
		{name: "not read only", change: func(s *healthHTTPRequestShape) { s.ReadOnly = false }},
		{name: "unsupported method", change: func(s *healthHTTPRequestShape) { s.Method = http.MethodDelete }},
		{name: "multiple requests", change: func(s *healthHTTPRequestShape) { s.RequestBudget = 2 }},
		{name: "zero timeout", change: func(s *healthHTTPRequestShape) { s.Timeout = 0 }},
		{name: "missing response bound", change: func(s *healthHTTPRequestShape) { s.MaxResponseBytes = 0 }},
		{name: "response bound exceeds client ceiling", change: func(s *healthHTTPRequestShape) { s.MaxResponseBytes = healthTransportMaxBytes + 1 }},
		{name: "request bound exceeds client ceiling", change: func(s *healthHTTPRequestShape) { s.MaxRequestBytes = healthTransportMaxBytes + 1 }},
		{name: "GET body", change: func(s *healthHTTPRequestShape) { s.Body = []byte("unexpected") }},
		{name: "endpoint credentials", change: func(s *healthHTTPRequestShape) { s.Endpoint = "https://user:pass@example.test/data" }},
		{name: "endpoint fragment", change: func(s *healthHTTPRequestShape) { s.Endpoint = "https://example.test/data#fragment" }},
		{name: "unsupported scheme", change: func(s *healthHTTPRequestShape) { s.Endpoint = "ftp://example.test/data" }},
		{name: "header injection", change: func(s *healthHTTPRequestShape) { s.Headers = http.Header{"X-Input": {"ok\r\nAuthorization: secret"}} }},
		{name: "hop by hop header", change: func(s *healthHTTPRequestShape) { s.Headers = http.Header{"Connection": {"keep-alive"}} }},
		{name: "host override", change: func(s *healthHTTPRequestShape) { s.Headers = http.Header{"Host": {"other.example"}} }},
		{name: "content length override", change: func(s *healthHTTPRequestShape) { s.Headers = http.Header{"Content-Length": {"4"}} }},
		{name: "blank query key", change: func(s *healthHTTPRequestShape) { s.Query = url.Values{"": {"value"}} }},
		{name: "SOAP missing action", change: func(s *healthHTTPRequestShape) {
			s.Protocol = healthHTTPSOAP
			s.Method = http.MethodPost
			s.Headers = http.Header{"Content-Type": {"text/xml"}}
		}},
		{name: "SOAP action header bypass", change: func(s *healthHTTPRequestShape) {
			s.Protocol = healthHTTPSOAP
			s.Method = http.MethodPost
			s.SOAPAction = "urn:lookup"
			s.Headers = http.Header{"Content-Type": {"text/xml"}, "SOAPAction": {"urn:other"}}
		}},
		{name: "SOAP method mismatch", change: func(s *healthHTTPRequestShape) {
			s.Protocol = healthHTTPSOAP
			s.SOAPAction = "urn:lookup"
			s.Headers = http.Header{"Content-Type": {"text/xml"}}
		}},
		{name: "REST SOAP action", change: func(s *healthHTTPRequestShape) { s.SOAPAction = "urn:lookup" }},
	}
	var requests atomic.Int32
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
			test.change(&shape)
			_, err := executeHealthHTTPRequest(context.Background(), client, shape)
			var transportErr healthTransportError
			if !errors.As(err, &transportErr) || transportErr.code != healthTransportInvalidPlan {
				t.Fatalf("error=%v, want invalid-plan transport error", err)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid plans issued %d requests", got)
	}
}

func TestExecuteHealthHTTPRequestEnforcesPlanRequestByteBudgetBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.MaxRequestBytes = 1
	_, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportRequestLimit) || requests.Load() != 0 {
		t.Fatalf("error=%v requests=%d", err, requests.Load())
	}
}

func TestHealthRequestByteCountIncludesHostAndInferredContentLength(t *testing.T) {
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.Method = http.MethodPost
	shape.Body = []byte("body")
	request, err := buildHealthHTTPRequest(shape)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len("POST /data HTTP/1.1\r\n") + len("Host: example.test\r\n") + len("Content-Length: 4\r\n") + len("\r\n") + len("body"))
	if got := healthRequestByteCount(request); got != want {
		t.Fatalf("request byte count=%d, want %d", got, want)
	}

	shape.MaxRequestBytes = want
	if _, err := buildHealthHTTPRequest(shape); err != nil {
		t.Fatalf("request at the byte limit was rejected: %v", err)
	}
	shape.MaxRequestBytes = want - 1
	if _, err := buildHealthHTTPRequest(shape); !hasHealthTransportError(err, healthTransportRequestLimit) {
		t.Fatalf("request over the byte limit returned %v", err)
	}
}

func TestExecuteHealthHTTPRequestMakesOneRequestAndDoesNotFollowRedirect(t *testing.T) {
	var firstCalls, redirectedCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			firstCalls.Add(1)
			w.Header().Set("Location", "/second?token=must-not-be-forwarded")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		redirectedCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	shape := validHealthHTTPRequestShape(server.URL+"/first?token=request-secret", healthHTTPREST)
	response, err := executeHealthHTTPRequest(context.Background(), &http.Client{Timeout: time.Second}, shape)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusTemporaryRedirect || firstCalls.Load() != 1 || redirectedCalls.Load() != 0 {
		t.Fatalf("status=%d first=%d redirected=%d", response.StatusCode, firstCalls.Load(), redirectedCalls.Load())
	}
}

func TestExecuteHealthHTTPRequestBoundsAndClosesOversizedResponse(t *testing.T) {
	body := &trackedHTTPBody{reader: strings.NewReader("12345")}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: body, ContentLength: -1}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.MaxResponseBytes = 4
	response, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportResponseLimit) || response.StatusCode != http.StatusOK || len(response.Body) != 0 || !body.closed.Load() {
		t.Fatalf("response=%#v error=%v closed=%v", response, err, body.closed.Load())
	}
}

func TestExecuteHealthHTTPRequestAcceptsResponseAtExactByteLimit(t *testing.T) {
	body := &trackedHTTPBody{reader: strings.NewReader("1234")}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: -1}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.MaxResponseBytes = 4
	response, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if err != nil || string(response.Body) != "1234" || !body.closed.Load() {
		t.Fatalf("response=%#v error=%v closed=%v", response, err, body.closed.Load())
	}
}

func TestExecuteHealthHTTPRequestClosesOversizedContentLengthWithoutReading(t *testing.T) {
	body := &trackedHTTPBody{reader: strings.NewReader("must-not-read")}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: 100}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.MaxResponseBytes = 10
	_, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportResponseLimit) || !body.closed.Load() || body.reads.Load() != 0 {
		t.Fatalf("error=%v closed=%v reads=%d", err, body.closed.Load(), body.reads.Load())
	}
}

func TestExecuteHealthHTTPRequestDeadlineCoversResponseRead(t *testing.T) {
	var body *deadlineHTTPBody
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body = &deadlineHTTPBody{ctx: req.Context()}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: body, ContentLength: -1}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.Timeout = 25 * time.Millisecond
	_, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportDeadline) || body == nil || !body.closed.Load() {
		t.Fatalf("error=%v body=%v", err, body)
	}
}

func TestExecuteHealthHTTPRequestDeadlineCoversRequestHeaders(t *testing.T) {
	client := &http.Client{Transport: deadlineRoundTripper{}}
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	shape.Timeout = 25 * time.Millisecond
	_, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportDeadline) {
		t.Fatalf("error=%v, want deadline error", err)
	}
}

func TestExecuteHealthHTTPRequestCancelsRequestContextAfterResponseCleanup(t *testing.T) {
	var requestContext context.Context
	var body *trackedHTTPBody
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestContext = req.Context()
		body = &trackedHTTPBody{reader: strings.NewReader("ok")}
		return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: 2}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	if _, err := executeHealthHTTPRequest(context.Background(), client, shape); err != nil {
		t.Fatal(err)
	}
	if requestContext == nil || body == nil || !body.closed.Load() {
		t.Fatal("response body was not closed")
	}
	select {
	case <-requestContext.Done():
	default:
		t.Fatal("request context was not canceled after completion")
	}
}

func TestExecuteHealthHTTPRequestRedactsTransportErrors(t *testing.T) {
	const credential = "do-not-publish-this-token"
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q failed with %s", req.URL.String(), credential)
	})
	shape := validHealthHTTPRequestShape("https://example.test/resource", healthHTTPREST)
	shape.Query = url.Values{"token": {credential}}
	shape.Headers = http.Header{"Authorization": {"Bearer " + credential}}
	_, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportRequestFailed) || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "example.test") || strings.Contains(err.Error(), "token=") {
		t.Fatalf("transport error is not redacted: %v", err)
	}
}

func TestExecuteHealthHTTPRequestRejectsPartialResponseOnReadFailure(t *testing.T) {
	body := &readFailureHTTPBody{}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: body, ContentLength: -1}, nil
	})
	shape := validHealthHTTPRequestShape("https://example.test/data", healthHTTPREST)
	response, err := executeHealthHTTPRequest(context.Background(), client, shape)
	if !hasHealthTransportError(err, healthTransportResponseRead) || response.StatusCode != http.StatusOK || len(response.Body) != 0 || !body.closed.Load() {
		t.Fatalf("partial response was retained: response=%#v err=%v closed=%v", response, err, body.closed.Load())
	}
}

func validHealthHTTPRequestShape(endpoint string, protocol healthHTTPProtocol) healthHTTPRequestShape {
	return healthHTTPRequestShape{
		Protocol:         protocol,
		ReadOnly:         true,
		Method:           http.MethodGet,
		Endpoint:         endpoint,
		RequestBudget:    1,
		Timeout:          time.Second,
		MaxRequestBytes:  healthTransportMaxBytes,
		MaxResponseBytes: healthTransportMaxBytes,
	}
}

type trackedHTTPBody struct {
	reader io.Reader
	closed atomic.Bool
	reads  atomic.Int32
}

func (b *trackedHTTPBody) Read(data []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(data)
}

func (b *trackedHTTPBody) Close() error {
	b.closed.Store(true)
	return nil
}

type deadlineHTTPBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *deadlineHTTPBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *deadlineHTTPBody) Close() error {
	b.closed.Store(true)
	return nil
}

type readFailureHTTPBody struct {
	closed atomic.Bool
}

type deadlineRoundTripper struct{}

func (deadlineRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func (b *readFailureHTTPBody) Read([]byte) (int, error) {
	return 0, errors.New("synthetic read failed")
}

func (b *readFailureHTTPBody) Close() error {
	b.closed.Store(true)
	return nil
}

func hasHealthTransportError(err error, code healthTransportErrorCode) bool {
	var transportErr healthTransportError
	return errors.As(err, &transportErr) && transportErr.code == code
}
