package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type IcheonAdapter struct {
	StaticHostMatcher
}

func NewIcheonAdapter() IcheonAdapter {
	return IcheonAdapter{StaticHostMatcher{Hosts: IcheonHosts()}}
}

func IcheonHosts() []string {
	return []string{"www.icheon.go.kr"}
}

func (a IcheonAdapter) Name() string { return "icheon" }

func (a IcheonAdapter) Hosts() []string { return IcheonHosts() }

func (a IcheonAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a IcheonAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "icheon", a.DependencyClass(req.Spec, req.Operation))
}

func (a IcheonAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("icheon adapter call support is not enabled yet")
}
