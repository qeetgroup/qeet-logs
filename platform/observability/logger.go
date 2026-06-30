package observability

import (
	"os"

	"github.com/rs/zerolog"
)

// New returns a structured logger. In development it uses a human-readable
// console writer; otherwise it emits JSON (ready for qeet-logs to ingest itself).
func New(env string) zerolog.Logger {
	if env == "development" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			With().Timestamp().Logger()
	}
	return zerolog.New(os.Stderr).With().Timestamp().Logger()
}
