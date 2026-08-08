package httpx

import (
	"net/http"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

var _ = contracts.Assert[http.Handler]((*VHostMux)(nil))
