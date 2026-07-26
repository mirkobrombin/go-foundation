package caching

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Cache[string]]((*InMemoryCache[string])(nil))
	_ = contracts.Assert[Cache[string]]((*DistributedBridge[string])(nil))
	_ = contracts.Assert[DistributedCache]((*DistributedInMemory)(nil))
)
