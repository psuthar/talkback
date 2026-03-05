package obsworker

import (
	"fmt"
	"strings"
)

// AppNameSource is how the app name was resolved.
const (
	AppNameSourceEnv        = "ENV"
	AppNameSourceDiscovered = "DISCOVERED"
	AppNameSourceAutoFirst  = "AUTO_FIRST"
)

// ResolveAppName determines the app name for filtering.
// Resolution order: 1) OBS_APP_NAME env (cfg.AppName), 2) discover_appnames query (1 = use, multiple = first + AUTO_FIRST, none = "").
// If requireFilter is true and appName is empty, returns an error.
func ResolveAppName(cfg Config, client *NerdGraphClient) (appName string, source string, warning string, err error) {
	if cfg.AppName != "" {
		appName = strings.TrimSpace(cfg.AppName)
		if strings.Contains(appName, "'") {
			return "", "", "", fmt.Errorf("OBS_APP_NAME must not contain single quotes")
		}
		return appName, AppNameSourceEnv, "", nil
	}

	// Run discover_appnames (window 60 minutes; no app filter)
	nrql := "SELECT uniques(appName) FROM Transaction SINCE 60 minutes ago LIMIT 50"
	results, err := client.RunNRQL(nrql)
	if err != nil {
		return "", "", "", fmt.Errorf("discover app names: %w", err)
	}
	if len(results) == 0 {
		if cfg.RequireAppNameFilter {
			return "", "", "", fmt.Errorf("OBS_REQUIRE_APPNAME_FILTER is true but no app names discovered; set OBS_APP_NAME or ensure traffic in last 60 minutes")
		}
		return "", "", "", nil
	}

	row := results[0]
	var names []interface{}
	if v := row["uniques"]; v != nil {
		if list, ok := v.([]interface{}); ok {
			names = list
		}
	}
	if names == nil {
		for _, v := range row {
			if list, ok := v.([]interface{}); ok {
				names = list
				break
			}
		}
	}
	if names == nil {
		if s, ok := row["uniques"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), AppNameSourceDiscovered, "", nil
		}
		if cfg.RequireAppNameFilter {
			return "", "", "", fmt.Errorf("OBS_REQUIRE_APPNAME_FILTER is true but discover_appnames returned no uniques; set OBS_APP_NAME")
		}
		return "", "", "", nil
	}

	var appNames []string
	for _, n := range names {
		if s, ok := n.(string); ok && strings.TrimSpace(s) != "" {
			appNames = append(appNames, strings.TrimSpace(s))
		}
	}
	if len(appNames) == 0 {
		if cfg.RequireAppNameFilter {
			return "", "", "", fmt.Errorf("OBS_REQUIRE_APPNAME_FILTER is true but no app names in result; set OBS_APP_NAME")
		}
		return "", "", "", nil
	}
	if len(appNames) == 1 {
		appName = appNames[0]
		if strings.Contains(appName, "'") {
			return "", "", "", fmt.Errorf("discovered app name must not contain single quotes")
		}
		return appName, AppNameSourceDiscovered, "", nil
	}
	// Multiple: use first, mark source and add warning
	appName = appNames[0]
	if strings.Contains(appName, "'") {
		return "", "", "", fmt.Errorf("discovered app name must not contain single quotes")
	}
	warning = fmt.Sprintf("Multiple app names discovered (%d); using first: %q", len(appNames), appName)
	return appName, AppNameSourceAutoFirst, warning, nil
}
