package dir

import (
	"github.com/mirkobrombin/go-foundation/v2/core/configuration"
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

var _ = contracts.Assert[configuration.Provider]((*Provider)(nil))
