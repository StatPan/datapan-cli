package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type IWestAdapter struct {
	StaticHostMatcher
}

func NewIWestAdapter() IWestAdapter {
	return IWestAdapter{StaticHostMatcher{Hosts: IWestHosts()}}
}

func IWestHosts() []string {
	return []string{"www.iwest.co.kr"}
}

func (a IWestAdapter) Name() string { return "iwest" }

func (a IWestAdapter) Hosts() []string { return IWestHosts() }

func (a IWestAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a IWestAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "iwest", a.DependencyClass(req.Spec, req.Operation))
}

func (a IWestAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("iwest adapter call support is not enabled yet")
}
