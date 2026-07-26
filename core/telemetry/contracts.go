package telemetry

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Tracer]((*noopTracer)(nil))
	_ = contracts.Assert[Span]((*noopSpan)(nil))
	_ = contracts.Assert[Meter]((*noopMeter)(nil))
	_ = contracts.Assert[Counter]((*noopCounter)(nil))
	_ = contracts.Assert[Histogram]((*noopHistogram)(nil))
	_ = contracts.Assert[Gauge]((*noopGauge)(nil))
	_ = contracts.Assert[Tracer]((*SimpleTracer)(nil))
	_ = contracts.Assert[Span]((*simpleSpan)(nil))
	_ = contracts.Assert[Meter]((*SimpleMeter)(nil))
	_ = contracts.Assert[Counter]((*simpleCounter)(nil))
	_ = contracts.Assert[Histogram]((*simpleHistogram)(nil))
	_ = contracts.Assert[Gauge]((*simpleGauge)(nil))
)
