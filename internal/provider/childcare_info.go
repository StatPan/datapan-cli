package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ChildcareInfoAdapter struct {
	StaticHostMatcher
}

func NewChildcareInfoAdapter() ChildcareInfoAdapter {
	return ChildcareInfoAdapter{StaticHostMatcher{Hosts: ChildcareInfoHosts()}}
}

func ChildcareInfoHosts() []string {
	return []string{"info.childcare.go.kr"}
}

func (a ChildcareInfoAdapter) Name() string { return "childcare-info" }

func (a ChildcareInfoAdapter) Hosts() []string { return ChildcareInfoHosts() }

func (a ChildcareInfoAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ChildcareInfoAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "childcare-info", a.DependencyClass(req.Spec, req.Operation))
}

func (a ChildcareInfoAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("childcare-info adapter call support is not enabled yet")
}
