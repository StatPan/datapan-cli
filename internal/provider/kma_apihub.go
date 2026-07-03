package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KMAAPIHubAdapter struct {
	StaticHostMatcher
}

func NewKMAAPIHubAdapter() KMAAPIHubAdapter {
	return KMAAPIHubAdapter{StaticHostMatcher{Hosts: KMAAPIHubHosts()}}
}

func KMAAPIHubHosts() []string {
	return []string{"apihub.kma.go.kr"}
}

func (a KMAAPIHubAdapter) Name() string { return "kma-apihub" }

func (a KMAAPIHubAdapter) Hosts() []string { return KMAAPIHubHosts() }

func (a KMAAPIHubAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KMAAPIHubAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kma-apihub", a.DependencyClass(req.Spec, req.Operation))
}

func (a KMAAPIHubAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kma-apihub adapter call support is not enabled yet")
}
