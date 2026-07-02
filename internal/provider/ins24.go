package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type Ins24Adapter struct {
	StaticHostMatcher
}

func NewIns24Adapter() Ins24Adapter {
	return Ins24Adapter{StaticHostMatcher{Hosts: Ins24Hosts()}}
}

func Ins24Hosts() []string {
	return []string{"www.ins24.go.kr"}
}

func (a Ins24Adapter) Name() string { return "ins24" }

func (a Ins24Adapter) Hosts() []string { return Ins24Hosts() }

func (a Ins24Adapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a Ins24Adapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ins24", a.DependencyClass(req.Spec, req.Operation))
}

func (a Ins24Adapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ins24 adapter call support is not enabled yet")
}
