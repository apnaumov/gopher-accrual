package logger

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logDirectory string = ""

func SetLogDirectory(logDir string) error {
	logDirectory = logDir

	if len(logDirectory) != 0 {
		err := os.MkdirAll(logDir, 0755)
		return err
	}
	return nil
}

// initialize logger with loggerName
func InitializeRootLogger(loggerName string, level string) (*zap.Logger, error) {
	lvl, err := zap.ParseAtomicLevel(level)

	if err != nil {
		return nil, err
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = lvl

	if len(logDirectory) != 0 {
		cfg.OutputPaths = []string{
			filepath.Join(logDirectory, fmt.Sprintf("%s.log", loggerName)),
		}
	}

	cfg.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
	zl, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return zl, nil
}
