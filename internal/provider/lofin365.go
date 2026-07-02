package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type Lofin365Adapter struct {
	StaticHostMatcher
}

func NewLofin365Adapter() Lofin365Adapter {
	return Lofin365Adapter{StaticHostMatcher{Hosts: Lofin365Hosts()}}
}

func Lofin365Hosts() []string {
	return []string{"www.lofin365.go.kr"}
}

func (a Lofin365Adapter) Name() string { return "lofin365" }

func (a Lofin365Adapter) Hosts() []string { return Lofin365Hosts() }

func (a Lofin365Adapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a Lofin365Adapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "lofin365", a.DependencyClass(req.Spec, req.Operation))
}

func (a Lofin365Adapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("lofin365 adapter call support is not enabled yet")
}
