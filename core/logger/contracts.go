package logger

import "github.com/mirkobrombin/go-foundation/v2/core/contracts"

var (
	_ = contracts.Assert[Logger]((*stdLogger)(nil))
	_ = contracts.Assert[Sink]((*ConsoleSink)(nil))
	_ = contracts.Assert[Sink]((*CLEFSink)(nil))
)
