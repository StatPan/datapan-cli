package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type WAMISAdapter struct {
	StaticHostMatcher
}

func NewWAMISAdapter() WAMISAdapter {
	return WAMISAdapter{StaticHostMatcher{Hosts: WAMISHosts()}}
}

func WAMISHosts() []string {
	return []string{"www.wamis.go.kr"}
}

func (a WAMISAdapter) Name() string { return "wamis" }

func (a WAMISAdapter) Hosts() []string { return WAMISHosts() }

func (a WAMISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a WAMISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "wamis", a.DependencyClass(req.Spec, req.Operation))
}

func (a WAMISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("wamis adapter call support is not enabled yet")
}
