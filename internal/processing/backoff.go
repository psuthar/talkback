package processing

import "time"

// BackoffTransient returns the delay for failed_transient retries.
// attempt 1: 1m, 2: 3m, 3: 10m, 4: 30m, 5+: 2h (cap).
func BackoffTransient(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 1 * time.Minute
	case attempt == 2:
		return 3 * time.Minute
	case attempt == 3:
		return 10 * time.Minute
	case attempt == 4:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

// BackoffWaiting returns the delay for "not ready yet" (e.g. transcript still processing).
// 10m, 30m, 2h cap.
func BackoffWaiting(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 10 * time.Minute
	case attempt == 2:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}
