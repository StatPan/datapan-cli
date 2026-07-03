package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SexOffenderAdapter struct {
	StaticHostMatcher
}

func NewSexOffenderAdapter() SexOffenderAdapter {
	return SexOffenderAdapter{StaticHostMatcher{Hosts: SexOffenderHosts()}}
}

func SexOffenderHosts() []string {
	return []string{"api.sexoffender.go.kr"}
}

func (a SexOffenderAdapter) Name() string { return "sexoffender" }

func (a SexOffenderAdapter) Hosts() []string { return SexOffenderHosts() }

func (a SexOffenderAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SexOffenderAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "sexoffender", a.DependencyClass(req.Spec, req.Operation))
}

func (a SexOffenderAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("sexoffender adapter call support is not enabled yet")
}
