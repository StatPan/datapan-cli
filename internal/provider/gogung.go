package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GogungAdapter struct {
	StaticHostMatcher
}

func NewGogungAdapter() GogungAdapter {
	return GogungAdapter{StaticHostMatcher{Hosts: GogungHosts()}}
}

func GogungHosts() []string {
	return []string{"www.gogung.go.kr"}
}

func (a GogungAdapter) Name() string { return "gogung" }

func (a GogungAdapter) Hosts() []string { return GogungHosts() }

func (a GogungAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GogungAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gogung", a.DependencyClass(req.Spec, req.Operation))
}

func (a GogungAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gogung adapter call support is not enabled yet")
}
