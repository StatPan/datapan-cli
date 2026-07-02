package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type VWorldAdapter struct {
	StaticHostMatcher
}

func NewVWorldAdapter() VWorldAdapter {
	return VWorldAdapter{StaticHostMatcher{Hosts: VWorldHosts()}}
}

func VWorldHosts() []string {
	return []string{"www.vworld.kr"}
}

func (a VWorldAdapter) Name() string { return "vworld" }

func (a VWorldAdapter) Hosts() []string { return VWorldHosts() }

func (a VWorldAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a VWorldAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "vworld", a.DependencyClass(req.Spec, req.Operation))
}

func (a VWorldAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("vworld adapter call support is not enabled yet")
}
