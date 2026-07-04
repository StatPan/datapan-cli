package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KoreaPostAdapter struct {
	StaticHostMatcher
}

func NewKoreaPostAdapter() KoreaPostAdapter {
	return KoreaPostAdapter{StaticHostMatcher{Hosts: KoreaPostHosts()}}
}

func KoreaPostHosts() []string {
	return []string{"koreapost.go.kr"}
}

func (a KoreaPostAdapter) Name() string { return "koreapost" }

func (a KoreaPostAdapter) Hosts() []string { return KoreaPostHosts() }

func (a KoreaPostAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KoreaPostAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "koreapost", a.DependencyClass(req.Spec, req.Operation))
}

func (a KoreaPostAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("koreapost adapter call support is not enabled yet")
}
