// Package env ensures .env is loaded before any other package reads os.Getenv.
// Import it with _ "github.com/psuthar/talkback/internal/env" in packages that
// read config at init time (e.g. auth).
package env

import (
	"os"

	"github.com/joho/godotenv"
)

func init() {
	if os.Getenv("ENV") == "production" {
		return
	}
	_ = godotenv.Load()
}
