package debugfault

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	synthetic500Prefix = "synthetic test fault: forced 500"
	syntheticLatencyPrefix = "synthetic latency injected: "
	maxLatencyMs       = 10000
	defaultLatencyMs  = 1500
)

// HandleError returns HTTP 500 with a recognizable synthetic message.
// POST /debug/fault/error
func HandleError(w http.ResponseWriter, r *http.Request) {
	msg := synthetic500Prefix
	if m := r.URL.Query().Get("message"); m != "" {
		msg = m
	}
	log.Printf("[debugfault] forced 500: %s", msg)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(msg))
}

// HandleLatency sleeps for ms (query param, default 1500, max 10000) then returns 200.
// POST /debug/fault/latency?ms=1500
func HandleLatency(w http.ResponseWriter, r *http.Request) {
	ms := defaultLatencyMs
	if s := r.URL.Query().Get("ms"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			ms = n
			if ms > maxLatencyMs {
				ms = maxLatencyMs
			}
		} else {
			http.Error(w, "invalid ms: must be non-negative integer", http.StatusBadRequest)
			return
		}
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	body := syntheticLatencyPrefix + strconv.Itoa(ms) + " ms"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(body))
}

// HandleR2 invokes the R2 fault logic and returns the result.
// POST /debug/fault/r2?mode=auth|notfound|endpoint|timeout
func HandleR2(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = "auth"
	}
	err := RunR2Fault(r.Context(), mode)
	msg := "synthetic r2 fault: " + err.Error()
	log.Printf("[debugfault] r2 fault mode=%s: %v", mode, err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(msg))
}
