package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type STCISAdapter struct {
	StaticHostMatcher
}

func NewSTCISAdapter() STCISAdapter {
	return STCISAdapter{StaticHostMatcher{Hosts: STCISHosts()}}
}

func STCISHosts() []string {
	return []string{"stcis.go.kr"}
}

func (a STCISAdapter) Name() string { return "stcis" }

func (a STCISAdapter) Hosts() []string { return STCISHosts() }

func (a STCISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a STCISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "stcis", a.DependencyClass(req.Spec, req.Operation))
}

func (a STCISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("stcis adapter call support is not enabled yet")
}
