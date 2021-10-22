package logger

import (
	log "common/log/newlog"
	"strings"
)

// New -.
func Init(level string) {
	var l log.Level

	switch strings.ToLower(level) {
	case "error":
		l = log.DebugLevel
	case "warn":
		l = log.WarnLevel
	case "info":
		l = log.InfoLevel
	case "debug":
		l = log.DebugLevel
	default:
		l = log.InfoLevel
	}

	log.InitLogger(
		log.WithLevel(log.Level(l)),
		log.WithFields(log.Fields{
			0: {0: "AppServer"},
		}),
		log.WithOutput(
			log.NewOutput2(
				log.NewOutputOptions(log.OutputDir("./"), log.OutputName("all.log")),
				log.NewAsyncOptions(log.EnableAsync(false)),
			),
		),
	)
}
