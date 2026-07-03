package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SmartFarmKoreaAdapter struct {
	StaticHostMatcher
}

func NewSmartFarmKoreaAdapter() SmartFarmKoreaAdapter {
	return SmartFarmKoreaAdapter{StaticHostMatcher{Hosts: SmartFarmKoreaHosts()}}
}

func SmartFarmKoreaHosts() []string {
	return []string{"www.smartfarmkorea.net"}
}

func (a SmartFarmKoreaAdapter) Name() string { return "smartfarm-korea" }

func (a SmartFarmKoreaAdapter) Hosts() []string { return SmartFarmKoreaHosts() }

func (a SmartFarmKoreaAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SmartFarmKoreaAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "smartfarm-korea", a.DependencyClass(req.Spec, req.Operation))
}

func (a SmartFarmKoreaAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("smartfarm-korea adapter call support is not enabled yet")
}
