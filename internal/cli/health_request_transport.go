package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Health request shapes are internal, validated execution inputs. They are not
// a serialized Registry contract. The plan consumer must bind every field to
// verified Registry policy before constructing one. The per-request one MiB
// transport ceiling matches the existing provider adapter response cap; a
// larger declared plan is rejected instead of silently truncated.
const healthTransportMaxBytes int64 = 1 << 20

type healthHTTPProtocol string

const (
	healthHTTPREST healthHTTPProtocol = "rest"
	healthHTTPSOAP healthHTTPProtocol = "soap"
)

type healthHTTPRequestShape struct {
	Protocol         healthHTTPProtocol
	PublicTargetOnly bool
	ReadOnly         bool
	Method           string
	Endpoint         string
	Query            url.Values
	Headers          http.Header
	SOAPAction       string
	SOAPVersion      string
	Body             []byte
	RequestBudget    int
	Timeout          time.Duration
	MaxRequestBytes  int64
	MaxResponseBytes int64
}

// healthHTTPResponse deliberately returns only response metadata needed by an
// internal classifier. It never returns the request URL or request headers.
// Body is response content for internal assertion/classification only and must
// never be written to a public receipt or log.
type healthHTTPResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

type healthTransportErrorCode string

const (
	healthTransportInvalidPlan     healthTransportErrorCode = "invalid_plan"
	healthTransportRequestFailed   healthTransportErrorCode = "request_failed"
	healthTransportDeadline        healthTransportErrorCode = "deadline_exceeded"
	healthTransportInvalidResponse healthTransportErrorCode = "invalid_response"
	healthTransportRequestLimit    healthTransportErrorCode = "request_limit_exceeded"
	healthTransportResponseLimit   healthTransportErrorCode = "response_limit_exceeded"
	healthTransportResponseRead    healthTransportErrorCode = "response_read_failed"
)

type healthTransportError struct {
	code healthTransportErrorCode
}

func (e healthTransportError) Error() string {
	return "health transport " + string(e.code)
}

func newHealthTransportError(code healthTransportErrorCode) error {
	return healthTransportError{code: code}
}

func executeHealthHTTPRequest(parent context.Context, client HTTPClient, shape healthHTTPRequestShape) (healthHTTPResponse, error) {
	req, err := buildHealthHTTPRequest(shape)
	if err != nil {
		var transportErr healthTransportError
		if errors.As(err, &transportErr) {
			return healthHTTPResponse{}, transportErr
		}
		return healthHTTPResponse{}, newHealthTransportError(healthTransportInvalidPlan)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, shape.Timeout)
	defer cancel()
	req = req.WithContext(ctx)

	client = healthSingleRequestClient(client)
	if shape.PublicTargetOnly {
		client = healthOperationPlanHTTPClient(client)
	}
	resp, err := client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return healthHTTPResponse{}, newHealthTransportError(healthTransportDeadline)
		}
		return healthHTTPResponse{}, newHealthTransportError(healthTransportRequestFailed)
	}
	if resp == nil || resp.Body == nil || resp.StatusCode < 100 || resp.StatusCode > 599 {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return healthHTTPResponse{}, newHealthTransportError(healthTransportInvalidResponse)
	}
	defer resp.Body.Close()

	result := healthHTTPResponse{StatusCode: resp.StatusCode, ContentType: safeHealthContentType(resp.Header.Get("Content-Type"))}
	if resp.ContentLength > shape.MaxResponseBytes {
		return result, newHealthTransportError(healthTransportResponseLimit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, shape.MaxResponseBytes+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return result, newHealthTransportError(healthTransportDeadline)
		}
		return result, newHealthTransportError(healthTransportResponseRead)
	}
	if int64(len(body)) > shape.MaxResponseBytes {
		return result, newHealthTransportError(healthTransportResponseLimit)
	}
	result.Body = body
	return result, nil
}

func buildHealthHTTPRequest(shape healthHTTPRequestShape) (*http.Request, error) {
	if !shape.ReadOnly || shape.RequestBudget != 1 || shape.Timeout <= 0 ||
		!validHealthTransportByteLimit(shape.MaxRequestBytes) || !validHealthTransportByteLimit(shape.MaxResponseBytes) {
		return nil, errors.New("health request policy is invalid")
	}
	if shape.Method != http.MethodGet && shape.Method != http.MethodHead && shape.Method != http.MethodPost {
		return nil, errors.New("health request method is unsupported")
	}
	if (shape.Method == http.MethodGet || shape.Method == http.MethodHead) && len(shape.Body) != 0 {
		return nil, errors.New("health GET or HEAD request body is unsupported")
	}

	u, err := url.Parse(shape.Endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("health request endpoint is invalid")
	}
	if strings.ContainsAny(shape.Endpoint, "\r\n\x00") {
		return nil, errors.New("health request endpoint is invalid")
	}
	query := u.Query()
	for name, values := range shape.Query {
		if strings.TrimSpace(name) == "" || len(values) == 0 {
			return nil, errors.New("health request query is invalid")
		}
		query[name] = append([]string(nil), values...)
	}
	u.RawQuery = query.Encode()

	if shape.Protocol != healthHTTPREST && shape.Protocol != healthHTTPSOAP {
		return nil, errors.New("health request protocol is unsupported")
	}
	if shape.Protocol == healthHTTPSOAP {
		if shape.Method != http.MethodPost || strings.TrimSpace(shape.SOAPAction) == "" || hasHeaderFold(shape.Headers, "SOAPAction") || !hasHeaderFold(shape.Headers, "Content-Type") {
			return nil, errors.New("health SOAP request shape is invalid")
		}
	} else if shape.SOAPAction != "" || hasHeaderFold(shape.Headers, "SOAPAction") {
		return nil, errors.New("health REST request contains a SOAP action")
	}
	if err := validateHealthRequestHeaders(shape.Headers); err != nil {
		return nil, err
	}
	if shape.Protocol == healthHTTPSOAP && !validHeaderValue(shape.SOAPAction) {
		return nil, errors.New("health SOAP action is invalid")
	}
	if shape.Protocol == healthHTTPSOAP {
		switch shape.SOAPVersion {
		case "": // Retain the v1 transport helper's direct shape for existing callers.
		case "1.1":
			if !strings.HasPrefix(strings.ToLower(shape.Headers.Get("Content-Type")), "text/xml") {
				return nil, errors.New("health SOAP 1.1 content type is invalid")
			}
		case "1.2":
			mediaType, parameters, err := mime.ParseMediaType(shape.Headers.Get("Content-Type"))
			if err != nil || !strings.EqualFold(mediaType, "application/soap+xml") || parameters["action"] != shape.SOAPAction {
				return nil, errors.New("health SOAP 1.2 content type is invalid")
			}
		default:
			return nil, errors.New("health SOAP version is unsupported")
		}
	}

	body := bytes.NewReader(shape.Body)
	req, err := http.NewRequest(shape.Method, u.String(), body)
	if err != nil {
		return nil, errors.New("health request could not be constructed")
	}
	if (shape.Method == http.MethodGet || shape.Method == http.MethodHead) && len(shape.Body) == 0 {
		req.Body = nil
		req.GetBody = nil
		req.ContentLength = 0
	}
	req.Close = true
	req.Header = shape.Headers.Clone()
	if shape.Protocol == healthHTTPSOAP && shape.SOAPVersion != "1.2" {
		req.Header.Set("SOAPAction", shape.SOAPAction)
	}
	if healthRequestByteCount(req) > shape.MaxRequestBytes {
		return nil, healthTransportError{code: healthTransportRequestLimit}
	}
	return req, nil
}

func validHealthTransportByteLimit(value int64) bool {
	return value > 0 && value <= healthTransportMaxBytes
}

func healthRequestByteCount(req *http.Request) int64 {
	if req == nil || req.URL == nil {
		return healthTransportMaxBytes + 1
	}
	count := int64(len(req.Method) + 1 + len(req.URL.RequestURI()) + len(" HTTP/1.1\r\n") + 2)
	count += int64(len("Host: ") + len(req.URL.Host) + 2)
	if req.ContentLength > 0 {
		count += int64(len("Content-Length: ") + len(strconv.FormatInt(req.ContentLength, 10)) + 2)
	}
	for name, values := range req.Header {
		lineBytes := int64(len(name) + 2)
		for _, value := range values {
			lineBytes += int64(len(value) + 2)
		}
		count += lineBytes
	}
	if req.ContentLength > 0 {
		count += req.ContentLength
	}
	return count
}

func validateHealthRequestHeaders(headers http.Header) error {
	for name, values := range headers {
		if !validHeaderName(name) || len(values) == 0 || isHopByHopHeader(name) || strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "SOAPAction") {
			return errors.New("health request header is unsupported")
		}
		for _, value := range values {
			if !validHeaderValue(value) {
				return errors.New("health request header value is invalid")
			}
		}
	}
	return nil
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func validHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\r' || c == '\n' || c == 0 || c == 0x7f || (c < 0x20 && c != '\t') {
			return false
		}
	}
	return true
}

func isHopByHopHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func hasHeaderFold(headers http.Header, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func safeHealthContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || len(mediaType) > 127 {
		return ""
	}
	return strings.ToLower(mediaType)
}
