package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GimhaeAdapter struct {
	StaticHostMatcher
}

func NewGimhaeAdapter() GimhaeAdapter {
	return GimhaeAdapter{StaticHostMatcher{Hosts: GimhaeHosts()}}
}

func GimhaeHosts() []string {
	return []string{"www.gimhae.go.kr"}
}

func (a GimhaeAdapter) Name() string { return "gimhae" }

func (a GimhaeAdapter) Hosts() []string { return GimhaeHosts() }

func (a GimhaeAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GimhaeAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gimhae", a.DependencyClass(req.Spec, req.Operation))
}

func (a GimhaeAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gimhae adapter call support is not enabled yet")
}
