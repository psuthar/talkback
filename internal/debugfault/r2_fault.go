package debugfault

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/storage/r2"
)

// RunR2Fault creates a temporary R2/S3 client with intentionally bad config
// and attempts an operation that will fail. Does not affect the app's real R2 client.
// Supported modes: auth, notfound, endpoint, timeout
func RunR2Fault(ctx context.Context, mode string) error {
	base := r2.LoadConfig()
	switch mode {
	case "auth":
		// Invalid credentials
		cfg := r2.Config{
			Bucket:          base.Bucket,
			Endpoint:        base.Endpoint,
			Region:          base.Region,
			AccessKeyID:     "invalid-key-for-fault-test",
			SecretAccessKey: "invalid-secret-for-fault-test",
		}
		if cfg.Bucket == "" {
			cfg.Bucket = "fake-bucket"
		}
		if cfg.Endpoint == "" {
			cfg.Endpoint = "https://fake.r2.cloudflarestorage.com"
		}
		client, err := r2.New(cfg)
		if err != nil {
			return err
		}
		_, _, err = client.Put(ctx, "fault-test/synthetic-put", strings.NewReader("x"), "text/plain", 1)
		return err
	case "notfound":
		// Valid-looking config but non-existent bucket
		cfg := base
		cfg.Bucket = "nonexistent-bucket-fault-test-12345"
		client, err := r2.New(cfg)
		if err != nil {
			return err
		}
		_, _, err = client.Put(ctx, "fault-test/synthetic", strings.NewReader("x"), "text/plain", 1)
		return err
	case "endpoint":
		// Invalid endpoint
		cfg := r2.Config{
			Bucket:          "fake",
			Endpoint:        "https://invalid-endpoint-fault-test.example.invalid",
			Region:          "auto",
			AccessKeyID:     base.AccessKeyID,
			SecretAccessKey: base.SecretAccessKey,
		}
		if cfg.AccessKeyID == "" {
			cfg.AccessKeyID = "x"
			cfg.SecretAccessKey = "y"
		}
		client, err := r2.New(cfg)
		if err != nil {
			return err
		}
		_, _, err = client.Put(ctx, "x", strings.NewReader("x"), "text/plain", 1)
		return err
	case "timeout":
		// Use a very short context so the request times out
		shortCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
		defer cancel()
		cfg := base
		if cfg.Bucket == "" || cfg.Endpoint == "" {
			cfg = r2.Config{
				Bucket:          "fake",
				Endpoint:        "https://fake.r2.cloudflarestorage.com",
				Region:          "auto",
				AccessKeyID:     "x",
				SecretAccessKey: "y",
			}
		}
		client, err := r2.New(cfg)
		if err != nil {
			return err
		}
		_, _, err = client.Put(shortCtx, "fault-test/timeout", strings.NewReader("x"), "text/plain", 1)
		return err
	default:
		return fmt.Errorf("unsupported mode %q (use auth, notfound, endpoint, timeout)", mode)
	}
}
