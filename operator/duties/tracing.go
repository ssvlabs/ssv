package duties

import "context"

func withDutyTracingContext(ctx context.Context, id string) context.Context { // TODO add extra identifier? (ATTESTER, PROPOSER etc)
	// TODO compose the tracing enriched context
	return ctx
}

func withReorgTracingContext(ctx context.Context, id string) context.Context {
	// TODO compose the tracing enriched context
	return ctx
}
