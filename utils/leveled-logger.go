package utils

import (
	cfc "github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog"
)

type leveledLogger struct {
	zlog *zerolog.Logger
}

func NewLeveledLogger(zlog *zerolog.Logger) cfc.LeveledLoggerInterface {
	return &leveledLogger{
		zlog: zlog,
	}
}

func (l leveledLogger) Debugf(format string, v ...interface{}) {
	l.zlog.Debug().Msgf(format, v...)
}

func (l leveledLogger) Errorf(format string, v ...interface{}) {
	l.zlog.Error().Msgf(format, v...)
}

func (l leveledLogger) Infof(format string, v ...interface{}) {
	l.zlog.Info().Msgf(format, v...)
}

func (l leveledLogger) Warnf(format string, v ...interface{}) {
	l.zlog.Warn().Msgf(format, v...)
}
