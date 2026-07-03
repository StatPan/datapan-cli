package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NierNescAdapter struct {
	StaticHostMatcher
}

func NewNierNescAdapter() NierNescAdapter {
	return NierNescAdapter{StaticHostMatcher{Hosts: NierNescHosts()}}
}

func NierNescHosts() []string {
	return []string{"nesc.nier.go.kr"}
}

func (a NierNescAdapter) Name() string { return "nier-nesc" }

func (a NierNescAdapter) Hosts() []string { return NierNescHosts() }

func (a NierNescAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NierNescAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nier-nesc", a.DependencyClass(req.Spec, req.Operation))
}

func (a NierNescAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nier-nesc adapter call support is not enabled yet")
}
