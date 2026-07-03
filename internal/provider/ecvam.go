package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ECVAMAdapter struct {
	StaticHostMatcher
}

func NewECVAMAdapter() ECVAMAdapter {
	return ECVAMAdapter{StaticHostMatcher{Hosts: ECVAMHosts()}}
}

func ECVAMHosts() []string {
	return []string{"ecvam.neins.go.kr"}
}

func (a ECVAMAdapter) Name() string { return "ecvam" }

func (a ECVAMAdapter) Hosts() []string { return ECVAMHosts() }

func (a ECVAMAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ECVAMAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ecvam", a.DependencyClass(req.Spec, req.Operation))
}

func (a ECVAMAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ecvam adapter call support is not enabled yet")
}
