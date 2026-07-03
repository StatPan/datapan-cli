package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ConsumerAdapter struct {
	StaticHostMatcher
}

func NewConsumerAdapter() ConsumerAdapter {
	return ConsumerAdapter{StaticHostMatcher{Hosts: ConsumerHosts()}}
}

func ConsumerHosts() []string {
	return []string{"www.consumer.go.kr"}
}

func (a ConsumerAdapter) Name() string { return "consumer" }

func (a ConsumerAdapter) Hosts() []string { return ConsumerHosts() }

func (a ConsumerAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ConsumerAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "consumer", a.DependencyClass(req.Spec, req.Operation))
}

func (a ConsumerAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("consumer adapter call support is not enabled yet")
}
