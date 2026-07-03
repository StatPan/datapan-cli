package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MUCHAdapter struct {
	StaticHostMatcher
}

func NewMUCHAdapter() MUCHAdapter {
	return MUCHAdapter{StaticHostMatcher{Hosts: MUCHHosts()}}
}

func MUCHHosts() []string {
	return []string{"www.much.go.kr"}
}

func (a MUCHAdapter) Name() string { return "much" }

func (a MUCHAdapter) Hosts() []string { return MUCHHosts() }

func (a MUCHAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MUCHAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "much", a.DependencyClass(req.Spec, req.Operation))
}

func (a MUCHAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("much adapter call support is not enabled yet")
}
