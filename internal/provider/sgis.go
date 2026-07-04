package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SGISAdapter struct {
	StaticHostMatcher
}

func NewSGISAdapter() SGISAdapter {
	return SGISAdapter{StaticHostMatcher{Hosts: SGISHosts()}}
}

func SGISHosts() []string {
	return []string{"sgis.kostat.go.kr"}
}

func (a SGISAdapter) Name() string { return "sgis" }

func (a SGISAdapter) Hosts() []string { return SGISHosts() }

func (a SGISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SGISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "sgis", a.DependencyClass(req.Spec, req.Operation))
}

func (a SGISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("sgis adapter call support is not enabled yet")
}
