package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type Credential struct {
	Name  string
	Value string
}

type CallRequest struct {
	Spec          datago.Spec
	Operation     datago.Operation
	Params        map[string]string
	MissingParams []string
	Credential    Credential
	HTTP          HTTPDoer
}

type VerificationRequest struct {
	Spec          datago.Spec
	Operation     datago.Operation
	Params        map[string]string
	MissingParams []string
	Credential    Credential
	HTTP          HTTPDoer
	VerifiedAt    string
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Adapter interface {
	Name() string
	Hosts() []string
	MatchHost(host string) bool
	DependencyClass(spec datago.Spec, op datago.Operation) string
	Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult
	Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error)
}

type CapabilityReporter interface {
	Capabilities() []string
}

type CallParamPreparer interface {
	PrepareCallParams(params map[string]string, missing []string) (map[string]string, []string)
}

type CallPlan struct {
	URL          string
	RedactedURL  string
	PublicParams map[string]string
}

type CallPlanner interface {
	PlanCall(req CallRequest) (CallPlan, error)
}

type CatalogImporter interface {
	ImportCatalog(ctx context.Context) ([]datago.Spec, error)
}

type StaticHostMatcher struct {
	Hosts []string
}

func (m StaticHostMatcher) MatchHost(host string) bool {
	host = normalizeHost(host)
	for _, candidate := range m.Hosts {
		if host == normalizeHost(candidate) {
			return true
		}
	}
	return false
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func redactProviderError(err error, plan providerRequestPlan, credential string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if plan.url != "" && plan.redacted != "" {
		message = strings.ReplaceAll(message, plan.url, plan.redacted)
	}
	if strings.TrimSpace(credential) != "" {
		message = strings.ReplaceAll(message, credential, "REDACTED")
	}
	return message
}
