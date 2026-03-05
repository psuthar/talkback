package obsworker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Region is US or EU for New Relic API endpoint.
type Region string

const (
	RegionUS Region = "US"
	RegionEU Region = "EU"
)

// Config holds env-derived settings for the observability worker.
type Config struct {
	APIKey               string
	AccountID            int
	Region               Region
	WindowMins            int
	AppName              string
	RequireAppNameFilter bool
	// Auth error gating
	AuthUserThreshold  int
	AuthWindowMins     int
	AuthErrorMessage   string
	AuthErrorClass     string
}

// NerdGraphEndpoint returns the GraphQL URL for the configured region.
func (c Config) NerdGraphEndpoint() string {
	if c.Region == RegionEU {
		return "https://api.eu.newrelic.com/graphql"
	}
	return "https://api.newrelic.com/graphql"
}

// LoadConfig reads and validates config from environment.
// Returns an error with actionable message if required vars are missing.
func LoadConfig() (Config, error) {
	apiKey := strings.TrimSpace(os.Getenv("NEW_RELIC_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("NEW_RELIC_API_KEY is required (create a NerdGraph/user API key in New Relic)")
	}

	accountStr := strings.TrimSpace(os.Getenv("NEW_RELIC_ACCOUNT_ID"))
	if accountStr == "" {
		return Config{}, fmt.Errorf("NEW_RELIC_ACCOUNT_ID is required (integer, from New Relic URL or account settings)")
	}
	accountID, err := strconv.Atoi(accountStr)
	if err != nil || accountID <= 0 {
		return Config{}, fmt.Errorf("NEW_RELIC_ACCOUNT_ID must be a positive integer, got %q", accountStr)
	}

	region := Region(strings.ToUpper(strings.TrimSpace(os.Getenv("NEW_RELIC_REGION"))))
	if region == "" {
		region = RegionUS
	}
	if region != RegionUS && region != RegionEU {
		return Config{}, fmt.Errorf("NEW_RELIC_REGION must be US or EU, got %q", region)
	}

	windowMins := 30
	if w := strings.TrimSpace(os.Getenv("OBS_WINDOW_MINUTES")); w != "" {
		if parsed, err := strconv.Atoi(w); err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("OBS_WINDOW_MINUTES must be a positive integer, got %q", w)
		} else {
			windowMins = parsed
		}
	}

	appName := strings.TrimSpace(os.Getenv("OBS_APP_NAME"))
	requireAppNameFilter := isTruthy(os.Getenv("OBS_REQUIRE_APPNAME_FILTER"))

	authUserThreshold := 5
	if s := strings.TrimSpace(os.Getenv("OBS_AUTH_USER_THRESHOLD")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			authUserThreshold = n
		}
	}
	authWindowMins := windowMins
	if s := strings.TrimSpace(os.Getenv("OBS_AUTH_WINDOW_MINUTES")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			authWindowMins = n
		}
	}
	authErrorMessage := strings.TrimSpace(os.Getenv("OBS_AUTH_ERROR_MESSAGE"))
	if authErrorMessage == "" {
		authErrorMessage = "Unauthorized"
	}
	authErrorClass := strings.TrimSpace(os.Getenv("OBS_AUTH_ERROR_CLASS"))
	if authErrorClass == "" {
		authErrorClass = "401"
	}

	return Config{
		APIKey:               apiKey,
		AccountID:            accountID,
		Region:               region,
		WindowMins:            windowMins,
		AppName:              appName,
		RequireAppNameFilter:  requireAppNameFilter,
		AuthUserThreshold:     authUserThreshold,
		AuthWindowMins:        authWindowMins,
		AuthErrorMessage:      authErrorMessage,
		AuthErrorClass:        authErrorClass,
	}, nil
}

func isTruthy(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}
