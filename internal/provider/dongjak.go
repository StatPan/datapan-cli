package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type DongjakAdapter struct {
	StaticHostMatcher
}

func NewDongjakAdapter() DongjakAdapter {
	return DongjakAdapter{StaticHostMatcher{Hosts: DongjakHosts()}}
}

func DongjakHosts() []string {
	return []string{"data.dongjak.go.kr"}
}

func (a DongjakAdapter) Name() string { return "dongjak" }

func (a DongjakAdapter) Hosts() []string { return DongjakHosts() }

func (a DongjakAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a DongjakAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "dongjak", a.DependencyClass(req.Spec, req.Operation))
}

func (a DongjakAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("dongjak adapter call support is not enabled yet")
}
