package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type IPNaviAdapter struct {
	StaticHostMatcher
}

func NewIPNaviAdapter() IPNaviAdapter {
	return IPNaviAdapter{StaticHostMatcher{Hosts: IPNaviHosts()}}
}

func IPNaviHosts() []string {
	return []string{"api.ip-navi.or.kr", "api.ip-navi.or.kr:8000"}
}

func (a IPNaviAdapter) Name() string { return "ip-navi" }

func (a IPNaviAdapter) Hosts() []string { return IPNaviHosts() }

func (a IPNaviAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a IPNaviAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ip-navi", a.DependencyClass(req.Spec, req.Operation))
}

func (a IPNaviAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ip-navi adapter call support is not enabled yet")
}
