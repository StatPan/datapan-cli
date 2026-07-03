package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KIPRISPlusAdapter struct {
	StaticHostMatcher
}

func NewKIPRISPlusAdapter() KIPRISPlusAdapter {
	return KIPRISPlusAdapter{StaticHostMatcher{Hosts: KIPRISPlusHosts()}}
}

func KIPRISPlusHosts() []string {
	return []string{"plus.kipris.or.kr"}
}

func (a KIPRISPlusAdapter) Name() string { return "kipris-plus" }

func (a KIPRISPlusAdapter) Hosts() []string { return KIPRISPlusHosts() }

func (a KIPRISPlusAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KIPRISPlusAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kipris-plus", a.DependencyClass(req.Spec, req.Operation))
}

func (a KIPRISPlusAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kipris-plus adapter call support is not enabled yet")
}
