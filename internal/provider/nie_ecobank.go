package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NIEEcobankAdapter struct {
	StaticHostMatcher
}

func NewNIEEcobankAdapter() NIEEcobankAdapter {
	return NIEEcobankAdapter{StaticHostMatcher{Hosts: NIEEcobankHosts()}}
}

func NIEEcobankHosts() []string {
	return []string{"www.nie-ecobank.kr"}
}

func (a NIEEcobankAdapter) Name() string { return "nie-ecobank" }

func (a NIEEcobankAdapter) Hosts() []string { return NIEEcobankHosts() }

func (a NIEEcobankAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NIEEcobankAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nie-ecobank", a.DependencyClass(req.Spec, req.Operation))
}

func (a NIEEcobankAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nie-ecobank adapter call support is not enabled yet")
}
