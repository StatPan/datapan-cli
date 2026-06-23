package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type QNetAdapter struct {
	StaticHostMatcher
}

func NewQNetAdapter() QNetAdapter {
	return QNetAdapter{StaticHostMatcher{Hosts: QNetHosts()}}
}

func QNetHosts() []string {
	return []string{
		"openapi.q-net.or.kr",
		"c.q-net.or.kr",
		"open.api.q-net.or.kr",
	}
}

func (a QNetAdapter) Name() string { return "q-net" }

func (a QNetAdapter) Hosts() []string { return QNetHosts() }

func (a QNetAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a QNetAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return datago.VerificationResult{
		DatasetID:       req.Spec.ID,
		Title:           req.Spec.Title,
		Operation:       req.Operation.Name,
		Provider:        "q-net",
		EndpointHost:    endpointHost(req.Operation.Endpoint),
		DependencyClass: a.DependencyClass(req.Spec, req.Operation),
		Status:          "skipped",
		Reason:          "qnet_adapter_observation_required",
		Params:          publicParams(req.Params),
	}
}

func (a QNetAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("q-net adapter call support is not enabled yet")
}

func DefaultRegistry() (Registry, error) {
	return NewRegistry(NewQNetAdapter())
}

func endpointHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

func publicParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range params {
		if strings.EqualFold(key, "serviceKey") || strings.EqualFold(key, "service_key") {
			continue
		}
		out[key] = value
	}
	return out
}
