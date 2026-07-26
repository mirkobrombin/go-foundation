package tracing

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Span](noopSpan{})
	_ = contracts.Assert[Tracer](noopTracer{})
)
