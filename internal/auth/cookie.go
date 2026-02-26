package auth

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SetSessionCookie sets the session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, sessionID uuid.UUID, expiresAt time.Time) {
	sameSite := http.SameSiteLaxMode
	if Config.CookieSecure && len(Config.AllowedOrigins) > 0 {
		// Cross-origin (frontend on different domain): use None so cookie is sent with credentials
		sameSite = http.SameSiteNoneMode
	}
	cookie := &http.Cookie{
		Name:     Config.SessionCookieName,
		Value:    sessionID.String(),
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   Config.CookieSecure,
		SameSite: sameSite,
	}
	if Config.CookieDomain != "" {
		cookie.Domain = Config.CookieDomain
	}
	http.SetCookie(w, cookie)
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	sameSite := http.SameSiteLaxMode
	if Config.CookieSecure && len(Config.AllowedOrigins) > 0 {
		sameSite = http.SameSiteNoneMode
	}
	cookie := &http.Cookie{
		Name:     Config.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   Config.CookieSecure,
		SameSite: sameSite,
	}
	if Config.CookieDomain != "" {
		cookie.Domain = Config.CookieDomain
	}
	http.SetCookie(w, cookie)
}

// SessionIDFromRequest returns the session ID from the request cookie, or nil if missing/invalid.
func SessionIDFromRequest(r *http.Request) *uuid.UUID {
	c, err := r.Cookie(Config.SessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	id, err := uuid.Parse(c.Value)
	if err != nil {
		return nil
	}
	return &id
}
