package secrets

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Store]((*MemoryStore)(nil))
	_ = contracts.Assert[Store]((*EnvStore)(nil))
	_ = contracts.Assert[Store]((*VaultStore)(nil))
	_ = contracts.Assert[Store]((*CipherStore)(nil))
	_ = contracts.Assert[Store]((*PrefixStore)(nil))
	_ = contracts.Assert[Store]((*FallbackStore)(nil))
)
