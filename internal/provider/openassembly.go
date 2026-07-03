package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type OpenAssemblyAdapter struct {
	StaticHostMatcher
}

func NewOpenAssemblyAdapter() OpenAssemblyAdapter {
	return OpenAssemblyAdapter{StaticHostMatcher{Hosts: OpenAssemblyHosts()}}
}

func OpenAssemblyHosts() []string {
	return []string{"open.assembly.go.kr"}
}

func (a OpenAssemblyAdapter) Name() string { return "open-assembly" }

func (a OpenAssemblyAdapter) Hosts() []string { return OpenAssemblyHosts() }

func (a OpenAssemblyAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a OpenAssemblyAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "open-assembly", a.DependencyClass(req.Spec, req.Operation))
}

func (a OpenAssemblyAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("open-assembly adapter call support is not enabled yet")
}
