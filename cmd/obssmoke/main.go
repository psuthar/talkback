// obssmoke generates a small amount of representative traffic so the observability
// window includes more than /health. Use before running obsworker for local or CI smoke.
//
// Env: TALKBACK_BASE_URL (default http://localhost:8080)
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	base := os.Getenv("TALKBACK_BASE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	base = strings.TrimSuffix(base, "/")

	client := &http.Client{Timeout: 10 * time.Second}
	var ok, fail int

	// Health x5
	for i := 0; i < 5; i++ {
		resp, err := client.Get(base + "/health")
		if err != nil {
			log.Printf("GET /health: %v", err)
			fail++
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ok++
		} else {
			fail++
		}
	}

	// Optional: GET /api/sessions (may require auth)
	resp, err := client.Get(base + "/api/sessions")
	if err != nil {
		log.Printf("GET /api/sessions: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			fmt.Println("(GET /api/sessions requires auth; only /health was used for traffic.)")
		} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ok++
		}
	}

	fmt.Printf("obssmoke: %d ok, %d fail (base=%s)\n", ok, fail, base)
	if fail > 0 && ok == 0 {
		os.Exit(1)
	}
}
