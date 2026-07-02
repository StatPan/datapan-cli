package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type I815Adapter struct {
	StaticHostMatcher
}

func NewI815Adapter() I815Adapter {
	return I815Adapter{StaticHostMatcher{Hosts: I815Hosts()}}
}

func I815Hosts() []string {
	return []string{"search.i815.or.kr"}
}

func (a I815Adapter) Name() string { return "i815" }

func (a I815Adapter) Hosts() []string { return I815Hosts() }

func (a I815Adapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a I815Adapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "i815", a.DependencyClass(req.Spec, req.Operation))
}

func (a I815Adapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("i815 adapter call support is not enabled yet")
}
