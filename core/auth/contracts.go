package auth

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Claims](Payload{})
	_ = contracts.Assert[Claims](StandardClaims{})
	_ = contracts.Assert[TokenService]((*multiKeyService)(nil))
)
