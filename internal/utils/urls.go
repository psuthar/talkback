package utils

import (
	"fmt"
	"strings"
)

// RequireAbsoluteURL validates that value is an absolute URL (http:// or https://).
// Returns the trimmed value or an error suitable for production checks.
func RequireAbsoluteURL(name, value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", fmt.Errorf("%s must be set", name)
	}
	if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return "", fmt.Errorf("%s must be an absolute URL (http:// or https://), got: %q", name, v)
	}
	return v, nil
}
