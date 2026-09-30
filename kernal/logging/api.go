package logging

import "go.uber.org/zap"

// Logger is an optional structured logger for kernal libraries (persist, HTTP, gRPC).
// When unset, libraries stay silent so embedders are not forced to configure logging.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// FromZap adapts a *zap.Logger for persist.SetLogger and transport SetLogger calls.
func FromZap(l *zap.Logger) Logger {
	if l == nil {
		return nil
	}
	return zapLogger{l: l}
}

type zapLogger struct {
	l *zap.Logger
}

func (z zapLogger) Info(msg string, kvs ...any)  { z.l.Sugar().Infow(msg, kvs...) }
func (z zapLogger) Warn(msg string, kvs ...any)  { z.l.Sugar().Warnw(msg, kvs...) }
func (z zapLogger) Error(msg string, kvs ...any) { z.l.Sugar().Errorw(msg, kvs...) }
