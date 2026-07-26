package lock

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Locker]((*InMemoryLocker)(nil))
	_ = contracts.Assert[Lease]((*memoryLease)(nil))
)
