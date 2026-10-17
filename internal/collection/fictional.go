package collection

import (
	"context"

	"buyersworkbench/internal/domain"
)

// Fictional replays only the trusted workspace's fictional source facts. It
// does not discover, authenticate to, or retrieve anything from a supplier.
type Fictional struct{}

func (Fictional) Describe() Descriptor {
	return Descriptor{Key: "fictional_supplier", Version: domain.SeedVersion, Method: MethodFictional, Auth: AuthNone, Mode: ModeFictional}
}

func (Fictional) Collect(ctx context.Context, req Request) (domain.Batch, error) {
	if err := ctx.Err(); err != nil {
		return domain.Batch{}, safeFailure(err)
	}
	if req.Incomplete {
		return domain.Batch{}, NewFailure(CodeIncomplete)
	}
	if req.ObservedAt.IsZero() {
		return domain.Batch{}, NewFailure(CodeSchema)
	}
	return domain.FictionalBatch(req.Records, req.Binding, req.ObservedAt), nil
}
