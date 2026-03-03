// reset-r2-sessions deletes all objects under the sessions/ prefix in the configured R2 bucket.
// Use when you want to clear session uploads/videos/transcripts (e.g. after a DB reset).
//
// Usage: Set R2_* env vars (same as the app), then run:
//
//	go run ./cmd/reset-r2-sessions
//
// Or with .env: ensure R2_BUCKET, R2_ENDPOINT, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY are set.
package main

import (
	"context"
	"log"

	"github.com/joho/godotenv"
	"github.com/psuthar/talkback/internal/storage/r2"
)

func main() {
	_ = godotenv.Load()

	cfg := r2.LoadConfig()
	if cfg.Bucket == "" || cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		log.Fatal("R2_BUCKET, R2_ENDPOINT, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY must be set (e.g. from .env or Render env)")
	}

	client, err := r2.New(cfg)
	if err != nil {
		log.Fatalf("r2 client: %v", err)
	}

	log.Println("Deleting all objects under sessions/ in R2 (this will remove all session uploads, videos, transcripts)...")
	ctx := context.Background()
	// Try with configured prefix first (e.g. talkback/sessions/), then literal sessions/ if nothing found
	deleted, err := client.DeletePrefix(ctx, "sessions/")
	if err != nil {
		log.Fatalf("delete prefix: %v", err)
	}
	if deleted == 0 {
		deleted, err = client.DeletePrefixLiteral(ctx, "sessions/")
		if err != nil {
			log.Fatalf("delete prefix (literal): %v", err)
		}
		if deleted > 0 {
			log.Println("(Objects were under literal 'sessions/' prefix; consider setting R2_PREFIX to match the app for future runs.)")
		}
	}
	log.Printf("Done. Deleted %d objects under sessions/.", deleted)
}
