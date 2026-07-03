package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JeonnamRedtableAdapter struct {
	StaticHostMatcher
}

func NewJeonnamRedtableAdapter() JeonnamRedtableAdapter {
	return JeonnamRedtableAdapter{StaticHostMatcher{Hosts: JeonnamRedtableHosts()}}
}

func JeonnamRedtableHosts() []string {
	return []string{"jeonnam.openapi.redtable.global"}
}

func (a JeonnamRedtableAdapter) Name() string { return "jeonnam-redtable" }

func (a JeonnamRedtableAdapter) Hosts() []string { return JeonnamRedtableHosts() }

func (a JeonnamRedtableAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JeonnamRedtableAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jeonnam-redtable", a.DependencyClass(req.Spec, req.Operation))
}

func (a JeonnamRedtableAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jeonnam-redtable adapter call support is not enabled yet")
}
