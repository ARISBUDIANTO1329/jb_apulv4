package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/jbapul/jb_apulv4/internal/models"
)

type contextKey string

const (
	UserKey       contextKey = "user"
	CSRFTokenKey  contextKey = "csrf_token"
)

func GetUser(r *http.Request) *models.User {
	user, ok := r.Context().Value(UserKey).(*models.User)
	if !ok {
		return nil
	}
	return user
}

func SetUser(r *http.Request, user *models.User) *http.Request {
	ctx := context.WithValue(r.Context(), UserKey, user)
	return r.WithContext(ctx)
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			// API routes return 401 JSON, page routes redirect to login
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil || user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFProtection implements double-submit cookie pattern for state-changing requests.
// Safe methods (GET, HEAD, OPTIONS) are skipped.
// The token is set as a cookie and must be echoed back in the X-CSRF-Token header.
func CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip safe methods
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			// Ensure cookie exists for GET requests so client can read it
			cookie, err := r.Cookie("csrf_token")
			if err != nil || cookie.Value == "" {
				token := generateCSRFToken()
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					HttpOnly: false, // JS needs to read this
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400,
				})
				ctx := context.WithValue(r.Context(), CSRFTokenKey, token)
				r = r.WithContext(ctx)
			} else {
				ctx := context.WithValue(r.Context(), CSRFTokenKey, cookie.Value)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
			return
		}

		// For state-changing methods, validate CSRF token
		cookie, err := r.Cookie("csrf_token")
		if err != nil || cookie.Value == "" {
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}

		headerToken := r.Header.Get("X-CSRF-Token")
		if headerToken == "" {
			// Also check form value as fallback for HTMX form submissions
			headerToken = r.FormValue("csrf_token")
		}

		if headerToken == "" || headerToken != cookie.Value {
			http.Error(w, "CSRF token invalid", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CSRFToken returns the CSRF token from the request context (for use in templates).
func CSRFToken(r *http.Request) string {
	token, _ := r.Context().Value(CSRFTokenKey).(string)
	return token
}
