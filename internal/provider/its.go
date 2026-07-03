package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ITSAdapter struct {
	StaticHostMatcher
}

func NewITSAdapter() ITSAdapter {
	return ITSAdapter{StaticHostMatcher{Hosts: ITSHosts()}}
}

func ITSHosts() []string {
	return []string{
		"its.go.kr",
		"www.its.go.kr",
	}
}

func (a ITSAdapter) Name() string { return "its" }

func (a ITSAdapter) Hosts() []string { return ITSHosts() }

func (a ITSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ITSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "its", a.DependencyClass(req.Spec, req.Operation))
}

func (a ITSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("its adapter call support is not enabled yet")
}
