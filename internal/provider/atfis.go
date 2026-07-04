package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ATFISAdapter struct {
	StaticHostMatcher
}

func NewATFISAdapter() ATFISAdapter {
	return ATFISAdapter{StaticHostMatcher{Hosts: ATFISHosts()}}
}

func ATFISHosts() []string {
	return []string{"www.atfis.or.kr"}
}

func (a ATFISAdapter) Name() string { return "atfis" }

func (a ATFISAdapter) Hosts() []string { return ATFISHosts() }

func (a ATFISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ATFISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "atfis", a.DependencyClass(req.Spec, req.Operation))
}

func (a ATFISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("atfis adapter call support is not enabled yet")
}
