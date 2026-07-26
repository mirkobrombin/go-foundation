package saga

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[SagaStore]((*MemoryStore)(nil))
	_ = contracts.Assert[SagaStore]((*FileStore)(nil))
)
