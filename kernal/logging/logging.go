package logging

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init creates a *zap.Logger from the log configuration section.
//
// Logs are always written to the configured file in JSON format.
// When debug is true, an additional console core (human-readable) is added.
//
// The returned Logger must be Sync'd (typically via defer) to flush buffers.
func Init(level, file string, debug bool, configDir string) (*zap.Logger, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}

	// Resolve log file path relative to config directory when not absolute.
	if file != "" && !filepath.IsAbs(file) && configDir != "" {
		file = filepath.Join(configDir, filepath.Clean(file))
	}

	if file == "" {
		file = "logs/rbac.log"
		if configDir != "" {
			file = filepath.Join(configDir, file)
		}
	}
	file = filepath.Clean(file)

	if dir := filepath.Dir(file); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("logging: create log dir %s: %w", dir, err)
		}
	}

	logFile, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logging: open %s: %w", file, err)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.LowercaseLevelEncoder

	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(logFile),
		lvl,
	)

	if debug {
		consoleCfg := zap.NewDevelopmentEncoderConfig()
		consoleCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		consoleCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder

		consoleCore := zapcore.NewCore(
			zapcore.NewConsoleEncoder(consoleCfg),
			zapcore.AddSync(os.Stdout),
			lvl,
		)

		core := zapcore.NewTee(fileCore, consoleCore)
		return zap.New(core, zap.AddCaller()), nil
	}

	return zap.New(fileCore, zap.AddCaller()), nil
}
