package auth

import (
	"os"
	"strconv"
	"strings"
)

// Config holds auth-related env (session cookie, TTL, bootstrap admin).
var Config = loadConfig()

type ConfigStruct struct {
	SessionCookieName       string
	SessionTTLHours         int
	CookieDomain            string
	CookieSecure            bool
	BootstrapAdminEmail     string // if set and matches first signup/login, set global_role=admin; also used for auto-create at startup
	BootstrapAdminPassword  string // if set with BootstrapAdminEmail, auto-create admin account at startup when missing
	AllowedOrigins          []string
	MaxQuestionsPerSession  int    // max questions allowed per session (default 10); set via TB_MAX_QUESTIONS_PER_SESSION
}

func loadConfig() ConfigStruct {
	c := ConfigStruct{
		SessionCookieName:      "tb_login",
		SessionTTLHours:        168, // 7 days
		CookieSecure:           false,
		MaxQuestionsPerSession: 10,
	}
	if v := os.Getenv("TB_SESSION_COOKIE_NAME"); v != "" {
		c.SessionCookieName = v
	}
	if v := os.Getenv("TB_SESSION_TTL_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			c.SessionTTLHours = h
		}
	}
	if v := os.Getenv("TB_COOKIE_DOMAIN"); v != "" {
		c.CookieDomain = v
	}
	if v := os.Getenv("TB_COOKIE_SECURE"); v != "" {
		c.CookieSecure = strings.EqualFold(v, "true") || v == "1"
	}
	// Render: ENV=production typically; default Secure true in prod
	if os.Getenv("ENV") == "production" && os.Getenv("TB_COOKIE_SECURE") == "" {
		c.CookieSecure = true
	}
	if v := os.Getenv("TALKBACK_BOOTSTRAP_ADMIN_EMAIL"); v != "" {
		c.BootstrapAdminEmail = strings.TrimSpace(strings.ToLower(v))
	}
	if v := os.Getenv("TALKBACK_BOOTSTRAP_ADMIN_PASSWORD"); v != "" {
		c.BootstrapAdminPassword = v
	}
	if v := os.Getenv("TB_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				c.AllowedOrigins = append(c.AllowedOrigins, o)
			}
		}
	}
	if v := os.Getenv("TB_MAX_QUESTIONS_PER_SESSION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxQuestionsPerSession = n
		}
	}
	// Fallback: use CORS_ALLOWED_ORIGINS so one env var works for both CORS and cookie SameSite on Render
	if len(c.AllowedOrigins) == 0 {
		if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
			for _, o := range strings.Split(v, ",") {
				o = strings.TrimSpace(o)
				if o != "" {
					c.AllowedOrigins = append(c.AllowedOrigins, o)
				}
			}
		}
	}
	return c
}
