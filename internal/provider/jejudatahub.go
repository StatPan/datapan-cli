package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JejuDataHubAdapter struct {
	StaticHostMatcher
}

func NewJejuDataHubAdapter() JejuDataHubAdapter {
	return JejuDataHubAdapter{StaticHostMatcher{Hosts: JejuDataHubHosts()}}
}

func JejuDataHubHosts() []string {
	return []string{"www.jejudatahub.net"}
}

func (a JejuDataHubAdapter) Name() string { return "jejudatahub" }

func (a JejuDataHubAdapter) Hosts() []string { return JejuDataHubHosts() }

func (a JejuDataHubAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JejuDataHubAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jejudatahub", a.DependencyClass(req.Spec, req.Operation))
}

func (a JejuDataHubAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jejudatahub adapter call support is not enabled yet")
}
