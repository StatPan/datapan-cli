package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KISTEPAdapter struct {
	StaticHostMatcher
}

func NewKISTEPAdapter() KISTEPAdapter {
	return KISTEPAdapter{StaticHostMatcher{Hosts: KISTEPHosts()}}
}

func KISTEPHosts() []string {
	return []string{"www.kistep.re.kr"}
}

func (a KISTEPAdapter) Name() string { return "kistep" }

func (a KISTEPAdapter) Hosts() []string { return KISTEPHosts() }

func (a KISTEPAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KISTEPAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kistep", a.DependencyClass(req.Spec, req.Operation))
}

func (a KISTEPAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kistep adapter call support is not enabled yet")
}
