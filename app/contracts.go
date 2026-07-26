package app

import (
	"github.com/mirkobrombin/go-foundation/v2/app/hosting"
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

var _ = contracts.Assert[hosting.HostedService]((*schedulerHost)(nil))
