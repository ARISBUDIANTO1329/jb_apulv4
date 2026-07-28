package middleware

import (
	"context"
	"net/http"

	"github.com/jbapul/jb_apulv4/internal/models"
)

type contextKey string

const (
	UserKey contextKey = "user"
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
