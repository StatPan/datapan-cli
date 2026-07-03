package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NABICAdapter struct {
	StaticHostMatcher
}

func NewNABICAdapter() NABICAdapter {
	return NABICAdapter{StaticHostMatcher{Hosts: NABICHosts()}}
}

func NABICHosts() []string {
	return []string{"nabic.rda.go.kr"}
}

func (a NABICAdapter) Name() string { return "nabic" }

func (a NABICAdapter) Hosts() []string { return NABICHosts() }

func (a NABICAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NABICAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nabic", a.DependencyClass(req.Spec, req.Operation))
}

func (a NABICAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nabic adapter call support is not enabled yet")
}
