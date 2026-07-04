package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type IncheonAdapter struct {
	StaticHostMatcher
}

func NewIncheonAdapter() IncheonAdapter {
	return IncheonAdapter{StaticHostMatcher{Hosts: IncheonHosts()}}
}

func IncheonHosts() []string {
	return []string{"ifac.or.kr", "itour.incheon.go.kr", "www.incheon.go.kr"}
}

func (a IncheonAdapter) Name() string { return "incheon" }

func (a IncheonAdapter) Hosts() []string { return IncheonHosts() }

func (a IncheonAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a IncheonAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "incheon", a.DependencyClass(req.Spec, req.Operation))
}

func (a IncheonAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("incheon adapter call support is not enabled yet")
}
