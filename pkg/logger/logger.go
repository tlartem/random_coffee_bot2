package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Config struct {
	Level         string `default:"error"         envconfig:"LOGGER_LEVEL"`
	PrettyConsole bool   `default:"false"         envconfig:"LOGGER_PRETTY_CONSOLE"`
}

func Init(c Config) {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	level, err := zerolog.ParseLevel(c.Level)
	if err != nil {
		zerolog.SetGlobalLevel(level)
	}

	log.Logger = log.With().Logger()

	if c.PrettyConsole {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	}

	log.Info().Msg("Logger initialized")
}
