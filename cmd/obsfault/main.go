// obsfault is a CLI that repeatedly hits fault-injection routes for observability testing.
// Requires OBS_TEST_MODE=true (and optionally OBS_TEST_TOKEN) on the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "Base URL of the API")
	token := flag.String("token", "", "X-Obs-Test-Token header value (required if server has OBS_TEST_TOKEN)")
	scenario := flag.String("scenario", "", "error|latency|r2")
	count := flag.Int("count", 10, "Number of requests")
	concurrency := flag.Int("concurrency", 1, "Concurrent requests")
	latencyMs := flag.Int("latency-ms", 1500, "For latency scenario: sleep duration in ms")
	r2Mode := flag.String("r2-mode", "auth", "For r2 scenario: auth|notfound|endpoint|timeout")
	method := flag.String("method", "POST", "HTTP method")
	flag.Parse()

	if *scenario == "" {
		fmt.Println("Usage: obsfault --scenario error|latency|r2 [options]")
		fmt.Println("  --base-url     Base URL (default http://localhost:8080)")
		fmt.Println("  --token        X-Obs-Test-Token (required if server has OBS_TEST_TOKEN)")
		fmt.Println("  --count        Number of requests (default 10)")
		fmt.Println("  --concurrency  Concurrent requests (default 1)")
		fmt.Println("  --latency-ms   For latency scenario (default 1500)")
		fmt.Println("  --r2-mode      For r2 scenario: auth|notfound|endpoint|timeout (default auth)")
		flag.PrintDefaults()
		return
	}

	var url string
	switch *scenario {
	case "error":
		url = *baseURL + "/debug/fault/error"
	case "latency":
		url = fmt.Sprintf("%s/debug/fault/latency?ms=%d", *baseURL, *latencyMs)
	case "r2":
		url = fmt.Sprintf("%s/debug/fault/r2?mode=%s", *baseURL, *r2Mode)
	default:
		fmt.Printf("Unknown scenario %q\n", *scenario)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(*method, url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}
	if *token != "" {
		req.Header.Set("X-Obs-Test-Token", *token)
	}

	var totalOK, total5xx, totalOther int64
	var totalDuration int64
	var codeMu sync.Mutex
	codeCount := make(map[int]int64)

	start := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for i := 0; i < *count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			t0 := time.Now()
			resp, err := client.Do(req.Clone(context.Background()))
			dur := time.Since(t0).Milliseconds()
			atomic.AddInt64(&totalDuration, dur)
			if err != nil {
				atomic.AddInt64(&totalOther, 1)
				return
			}
			defer resp.Body.Close()
			codeMu.Lock()
			codeCount[resp.StatusCode]++
			codeMu.Unlock()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				atomic.AddInt64(&totalOK, 1)
			} else if resp.StatusCode >= 500 {
				atomic.AddInt64(&total5xx, 1)
			} else {
				atomic.AddInt64(&totalOther, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	ok := atomic.LoadInt64(&totalOK)
	fivexx := atomic.LoadInt64(&total5xx)
	other := atomic.LoadInt64(&totalOther)
	durTotal := atomic.LoadInt64(&totalDuration)

	fmt.Printf("\n--- obsfault summary ---\n")
	fmt.Printf("Scenario: %s | URL: %s\n", *scenario, url)
	fmt.Printf("Total requests: %d | Concurrency: %d\n", *count, *concurrency)
	fmt.Printf("Elapsed: %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("2xx: %d | 5xx: %d | other: %d\n", ok, fivexx, other)
	if ok+fivexx+other > 0 {
		avg := durTotal / (ok + fivexx + other)
		fmt.Printf("Avg request duration: %d ms\n", avg)
	}
	fmt.Printf("Status code distribution:\n")
	codeMu.Lock()
	for code, n := range codeCount {
		fmt.Printf("  %d: %d\n", code, n)
	}
	codeMu.Unlock()
}
