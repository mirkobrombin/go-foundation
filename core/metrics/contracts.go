package metrics

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Counter]((*SimpleCounter)(nil))
	_ = contracts.Assert[Gauge]((*SimpleGauge)(nil))
	_ = contracts.Assert[Histogram]((*SimpleHistogram)(nil))
	_ = contracts.Assert[Timer]((*SimpleTimer)(nil))
)
