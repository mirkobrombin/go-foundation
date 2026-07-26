package hosting

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[HostedService]((*BackgroundServiceAdapter)(nil))
	_ = contracts.Assert[BackgroundService]((*webService)(nil))
)
