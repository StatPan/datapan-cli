package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type EShareAdapter struct {
	StaticHostMatcher
}

func NewEShareAdapter() EShareAdapter {
	return EShareAdapter{StaticHostMatcher{Hosts: EShareHosts()}}
}

func EShareHosts() []string {
	return []string{"www.eshare.go.kr"}
}

func (a EShareAdapter) Name() string { return "eshare" }

func (a EShareAdapter) Hosts() []string { return EShareHosts() }

func (a EShareAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a EShareAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "eshare", a.DependencyClass(req.Spec, req.Operation))
}

func (a EShareAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("eshare adapter call support is not enabled yet")
}
