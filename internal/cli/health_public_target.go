package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

var healthGlobalIPv6Prefix = netip.MustParsePrefix("2000::/3")

const healthTransportMaxResponseHeaderBytes = 64 << 10

var healthNonPublicTargetPrefixes = mustHealthPrefixes(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"3fff::/20",
)

func mustHealthPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

func healthPublicTargetIP(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	if address.Is4In6() {
		address = address.Unmap()
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if address.Is6() && !healthGlobalIPv6Prefix.Contains(address) {
		return false
	}
	for _, prefix := range healthNonPublicTargetPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func healthPublicTargetDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return dialHealthPublicTarget(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}).DialContext)
}

// healthOperationPlanHTTPClient applies endpoint policy only to the new
// Registry-plan execution path. Existing v1 clients retain their prior target
// handling. A custom standard-library transport cannot be inspected or
// constrained here, so reject it; package-local fake HTTPClient implementations
// remain available for deterministic synthetic tests.
func healthOperationPlanHTTPClient(client HTTPClient) HTTPClient {
	switch c := client.(type) {
	case nil:
		return RealHTTPClient{rejectRedirects: true, publicTargetsOnly: true}
	case RealHTTPClient:
		c.rejectRedirects = true
		c.publicTargetsOnly = true
		return c
	case *RealHTTPClient:
		return RealHTTPClient{rejectRedirects: true, publicTargetsOnly: true}
	case *http.Client:
		if c == nil || c.Transport == nil || c.Transport == http.DefaultTransport {
			return RealHTTPClient{rejectRedirects: true, publicTargetsOnly: true}
		}
		return healthPlanRejectedHTTPClient{}
	default:
		return client
	}
}

type healthPlanRejectedHTTPClient struct{}

func (healthPlanRejectedHTTPClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("health operation plan client cannot enforce target policy")
}

func dialHealthPublicTarget(
	ctx context.Context,
	network, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	if lookup == nil || dial == nil || !(network == "tcp" || network == "tcp4" || network == "tcp6") {
		return nil, errors.New("health target is not public")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, errors.New("health target is not public")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("health target is not public")
	}
	addresses, err := lookup(ctx, "ip", host)
	if err != nil || len(addresses) == 0 || len(addresses) > 64 {
		return nil, errors.New("health target is not public")
	}
	for _, candidate := range addresses {
		if !healthPublicTargetIP(candidate) {
			return nil, errors.New("health target is not public")
		}
	}
	var lastErr error
	for _, candidate := range addresses {
		destination := net.JoinHostPort(candidate.String(), port)
		connection, err := dial(ctx, network, destination)
		if err == nil {
			return connection, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, errors.New("health target connection deadline exceeded")
		}
	}
	if lastErr == nil {
		return nil, errors.New("health target is not public")
	}
	return nil, errors.New("health target connection failed")
}

func newHealthPublicHTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            healthPublicTargetDialContext,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  0,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: healthTransportMaxResponseHeaderBytes,
		MaxConnsPerHost:        1,
		DisableKeepAlives:      true,
	}
}
