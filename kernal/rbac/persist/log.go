package persist

import "github.com/aid297/rbac/kernal/logging"

var log logging.Logger

// SetLogger installs structured logging for persist (pause, reconcile, flush failures).
// Pass logging.FromZap(zapLogger) from cmd/rbac; nil clears logging.
func SetLogger(l logging.Logger) {
	log = l
}

func logInfo(msg string, kvs ...any) {
	if log != nil {
		log.Info(msg, kvs...)
	}
}

func logWarn(msg string, kvs ...any) {
	if log != nil {
		log.Warn(msg, kvs...)
	}
}

func logError(msg string, kvs ...any) {
	if log != nil {
		log.Error(msg, kvs...)
	}
}
