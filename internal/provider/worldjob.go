package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type WorldJobAdapter struct {
	StaticHostMatcher
}

func NewWorldJobAdapter() WorldJobAdapter {
	return WorldJobAdapter{StaticHostMatcher{Hosts: WorldJobHosts()}}
}

func WorldJobHosts() []string {
	return []string{"www.worldjob.or.kr"}
}

func (a WorldJobAdapter) Name() string { return "worldjob" }

func (a WorldJobAdapter) Hosts() []string { return WorldJobHosts() }

func (a WorldJobAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a WorldJobAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "worldjob", a.DependencyClass(req.Spec, req.Operation))
}

func (a WorldJobAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("worldjob adapter call support is not enabled yet")
}
