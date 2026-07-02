package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SafeMapAdapter struct {
	StaticHostMatcher
}

func NewSafeMapAdapter() SafeMapAdapter {
	return SafeMapAdapter{StaticHostMatcher{Hosts: SafeMapHosts()}}
}

func SafeMapHosts() []string {
	return []string{"www.safemap.go.kr"}
}

func (a SafeMapAdapter) Name() string { return "safemap" }

func (a SafeMapAdapter) Hosts() []string { return SafeMapHosts() }

func (a SafeMapAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SafeMapAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "safemap", a.DependencyClass(req.Spec, req.Operation))
}

func (a SafeMapAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("safemap adapter call support is not enabled yet")
}
