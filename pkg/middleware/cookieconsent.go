// Package middleware provides HTTP middleware for the media server.
package middleware

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

const (
	consentValueAll       = "all"
	consentValueEssential = "essential"
)

// CookieConsent manages GDPR/CCPA cookie consent for the site.
//
// Consent is stored as a persistent cookie with one of two values:
//   - "all"       – user accepted essential + analytics cookies
//   - "essential" – user accepted essential-only cookies
//
// No server-side state is kept; the cookie is the sole source of truth.
type CookieConsent struct {
	cfg config.CookieConsentConfig
	log *logger.Logger
}

// NewCookieConsent creates a CookieConsent handler from the provided config.
func NewCookieConsent(cfg config.CookieConsentConfig) *CookieConsent {
	return &CookieConsent{
		cfg: cfg,
		log: logger.New("cookieconsent"),
	}
}

// consentStatus returns whether consent has been given and whether analytics was accepted.
func (cc *CookieConsent) consentStatus(r *http.Request) (given bool, analyticsAccepted bool) {
	cookie, err := r.Cookie(cc.cfg.CookieName)
	if err != nil || cookie.Value == "" {
		return false, false
	}
	return true, cookie.Value == consentValueAll
}

// isSameOrigin rejects cross-origin POSTs (CSRF guard).
func (cc *CookieConsent) isSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// GinStatusHandler handles GET /api/cookie-consent/status.
// Returns whether consent is required and whether it has already been given.
func (cc *CookieConsent) GinStatusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		given, analyticsAccepted := cc.consentStatus(c.Request)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"required":           cc.cfg.Enabled,
				"given":              given,
				"analytics_accepted": analyticsAccepted,
			},
		})
	}
}

// GinAcceptHandler handles POST /api/cookie-consent.
// Body: {"analytics": true|false}
// Sets a persistent cookie recording the user's choice.
func (cc *CookieConsent) GinAcceptHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cc.isSameOrigin(c.Request) {
			cc.log.Warn("Cookie consent rejected: cross-origin request Origin=%q Host=%q",
				c.Request.Header.Get("Origin"), c.Request.Host)
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "forbidden"})
			return
		}

		var body struct {
			Analytics bool `json:"analytics"`
		}
		// Body is optional; default is essential-only on parse failure.
		_ = c.ShouldBindJSON(&body)

		value := consentValueEssential
		if body.Analytics {
			value = consentValueAll
		}

		http.SetCookie(c.Writer, &http.Cookie{
			Name:     cc.cfg.CookieName,
			Value:    value,
			MaxAge:   cc.cfg.CookieMaxAge,
			Path:     "/",
			HttpOnly: false, // must be readable by JS so the banner can check it client-side
			SameSite: http.SameSiteStrictMode,
			Secure:   ageGateSecure(c.Request), // reuse HTTPS detection from agegate.go
		})

		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"analytics_accepted": body.Analytics}})
	}
}
