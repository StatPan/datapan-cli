package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

func TestHealthPublicTargetIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "public IPv4", ip: "8.8.8.8", want: true},
		{name: "public IPv6", ip: "2001:4860:4860::8888", want: true},
		{name: "private IPv4", ip: "10.0.0.1"},
		{name: "loopback IPv4", ip: "127.0.0.1"},
		{name: "link local IPv4", ip: "169.254.1.2"},
		{name: "carrier grade NAT", ip: "100.64.0.1"},
		{name: "documentation IPv4", ip: "203.0.113.8"},
		{name: "private IPv6", ip: "fd00::1"},
		{name: "link local IPv6", ip: "fe80::1"},
		{name: "documentation IPv6", ip: "2001:db8::1"},
		{name: "IPv4 mapped private", ip: "::ffff:192.168.1.1"},
		{name: "invalid", ip: "not-an-ip"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, err := netip.ParseAddr(test.ip)
			if err != nil && test.name != "invalid" {
				t.Fatal(err)
			}
			if got := healthPublicTargetIP(address); got != test.want {
				t.Fatalf("healthPublicTargetIP(%q)=%t, want %t", test.ip, got, test.want)
			}
		})
	}
}

func TestDialHealthPublicTargetPinsValidatedAddresses(t *testing.T) {
	ctx := context.Background()
	var lookupHost, dialNetwork, dialAddress string
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookupHost = network + ":" + host
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2001:4860:4860::8888")}, nil
	}
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialNetwork, dialAddress = network, address
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	connection, err := dialHealthPublicTarget(ctx, "tcp", "api.example.test:443", lookup, dial)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if lookupHost != "ip:api.example.test" || dialNetwork != "tcp" || dialAddress != "8.8.8.8:443" {
		t.Fatalf("lookup or pinned dial differed: lookup=%q network=%q address=%q", lookupHost, dialNetwork, dialAddress)
	}
}

func TestDialHealthPublicTargetRejectsMixedPublicAndPrivateDNS(t *testing.T) {
	dialCalls := 0
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCalls++
		return nil, errors.New("unexpected dial")
	}
	if _, err := dialHealthPublicTarget(context.Background(), "tcp", "api.example.test:443", lookup, dial); err == nil {
		t.Fatal("mixed public/private DNS answer was accepted")
	}
	if dialCalls != 0 {
		t.Fatalf("dial was attempted %d times for a mixed DNS answer", dialCalls)
	}
}

func TestHealthOperationPlanHTTPClientEnforcesPublicTransport(t *testing.T) {
	client, ok := healthOperationPlanHTTPClient(nil).(RealHTTPClient)
	if !ok || !client.rejectRedirects || !client.publicTargetsOnly {
		t.Fatalf("default plan client does not enforce target and redirect policy: %#v", client)
	}
	if _, ok := healthOperationPlanHTTPClient(&http.Client{Transport: http.DefaultTransport}).(RealHTTPClient); !ok {
		t.Fatal("default standard-library transport did not select the guarded client")
	}
	custom := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("test transport")
	})}
	if _, ok := healthOperationPlanHTTPClient(custom).(healthPlanRejectedHTTPClient); !ok {
		t.Fatal("custom standard-library transport bypassed the public-target guard")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTrip(req)
}
