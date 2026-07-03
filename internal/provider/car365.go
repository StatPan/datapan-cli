package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type Car365Adapter struct {
	StaticHostMatcher
}

func NewCar365Adapter() Car365Adapter {
	return Car365Adapter{StaticHostMatcher{Hosts: Car365Hosts()}}
}

func Car365Hosts() []string {
	return []string{"www.car365.go.kr"}
}

func (a Car365Adapter) Name() string { return "car365" }

func (a Car365Adapter) Hosts() []string { return Car365Hosts() }

func (a Car365Adapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a Car365Adapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "car365", a.DependencyClass(req.Spec, req.Operation))
}

func (a Car365Adapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("car365 adapter call support is not enabled yet")
}
