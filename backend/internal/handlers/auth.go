package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB  *pgxpool.Pool
	Cfg *config.Config
}

var cfgRef *config.Config

func NewAuthHandler(db *pgxpool.Pool, cfg *config.Config) *AuthHandler {
	cfgRef = cfg
	return &AuthHandler{DB: db, Cfg: cfg}
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, r, "login", map[string]interface{}{
		"Title":   "Login",
		"AppName": h.Cfg.AppName,
		"Active":  "login",
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderTemplate(w, r, "login", map[string]string{"Error": "Invalid form"})
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		renderTemplate(w, r, "login", map[string]string{"Error": "Email and password required"})
		return
	}

	var user models.User
	var passwordHash string
	err := h.DB.QueryRow(r.Context(),
		"SELECT id, email, name, avatar_url, password_hash, role, created_at, updated_at FROM users WHERE email = $1",
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &user.AvatarURL, &passwordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		renderTemplate(w, r, "login", map[string]string{"Error": "Invalid credentials"})
		return
	}

	if passwordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
			renderTemplate(w, r, "login", map[string]string{"Error": "Invalid credentials"})
			return
		}
	}

	r = middleware.SetUser(r, &user)
	setSession(w, r, &user)

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	url := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=email%%20profile%%20https://www.googleapis.com/auth/youtube&state=%s",
		h.Cfg.GoogleClientID, h.Cfg.GoogleRedirectURI, state,
	)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code", http.StatusBadRequest)
		return
	}

	token, err := h.exchangeGoogleCode(r.Context(), code)
	if err != nil {
		log.Printf("[Auth] Google token exchange failed: %v", err)
		http.Error(w, "Auth failed", http.StatusInternalServerError)
		return
	}

	userInfo, err := h.getGoogleUserInfo(r.Context(), token.AccessToken)
	if err != nil {
		log.Printf("[Auth] Google user info failed: %v", err)
		http.Error(w, "Auth failed", http.StatusInternalServerError)
		return
	}

	user, err := h.upsertGoogleUser(r.Context(), userInfo)
	if err != nil {
		log.Printf("[Auth] Upsert user failed: %v", err)
		http.Error(w, "Auth failed", http.StatusInternalServerError)
		return
	}

	r = middleware.SetUser(r, user)
	setSession(w, r, user)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

type googleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"verified_email"`
}

func (h *AuthHandler) exchangeGoogleCode(ctx context.Context, code string) (*googleTokenResponse, error) {
	data := url.Values{}
	data.Set("code", code)
	data.Set("client_id", h.Cfg.GoogleClientID)
	data.Set("client_secret", h.Cfg.GoogleClientSecret)
	data.Set("redirect_uri", h.Cfg.GoogleRedirectURI)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp googleTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return &tokenResp, nil
}

func (h *AuthHandler) getGoogleUserInfo(ctx context.Context, accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo failed (%d): %s", resp.StatusCode, string(body))
	}

	var info googleUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse userinfo: %w", err)
	}
	return &info, nil
}

func (h *AuthHandler) upsertGoogleUser(ctx context.Context, info *googleUserInfo) (*models.User, error) {
	var user models.User
	err := h.DB.QueryRow(ctx,
		`SELECT id, email, name, avatar_url, role, created_at, updated_at
		 FROM users WHERE google_id = $1`, info.ID,
	).Scan(&user.ID, &user.Email, &user.Name, &user.AvatarURL, &user.Role, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		// Not found by google_id, try by email
		err = h.DB.QueryRow(ctx,
			`SELECT id, email, name, avatar_url, role, created_at, updated_at
			 FROM users WHERE email = $1`, info.Email,
		).Scan(&user.ID, &user.Email, &user.Name, &user.AvatarURL, &user.Role, &user.CreatedAt, &user.UpdatedAt)

		if err != nil {
			// Create new user
			err = h.DB.QueryRow(ctx,
				`INSERT INTO users (email, name, avatar_url, google_id, role, is_active, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, 'user', true, NOW(), NOW())
				 RETURNING id, email, name, avatar_url, role, created_at, updated_at`,
				info.Email, info.Name, info.Picture, info.ID,
			).Scan(&user.ID, &user.Email, &user.Name, &user.AvatarURL, &user.Role, &user.CreatedAt, &user.UpdatedAt)
			if err != nil {
				return nil, fmt.Errorf("create user: %w", err)
			}
			return &user, nil
		}

		// Link google_id to existing email account
		_, err = h.DB.Exec(ctx,
			`UPDATE users SET google_id = $1, avatar_url = COALESCE(NULLIF($2,''), avatar_url), updated_at = NOW() WHERE id = $3`,
			info.ID, info.Picture, user.ID)
		if err != nil {
			return nil, fmt.Errorf("link google_id: %w", err)
		}
		user.AvatarURL = info.Picture
		return &user, nil
	}

	// Update avatar if changed
	if info.Picture != "" && info.Picture != user.AvatarURL {
		h.DB.Exec(ctx, `UPDATE users SET avatar_url = $1, updated_at = NOW() WHERE id = $2`, info.Picture, user.ID)
		user.AvatarURL = info.Picture
	}
	return &user, nil
}

func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func setSession(w http.ResponseWriter, r *http.Request, user *models.User) {
	// Generate session ID
	sessionID := generateSessionID()

	// Store in DB (expires in 24h)
	_, err := dbPool.Exec(r.Context(),
		"INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, NOW() + INTERVAL '24 hours') ON CONFLICT (id) DO NOTHING",
		sessionID, user.ID,
	)
	if err != nil {
		log.Printf("[Auth] Session insert error: %v", err)
		return
	}

	isSecure := false
	if cfgRef != nil && cfgRef.Env == "production" {
		isSecure = true
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil && cookie.Value != "" {
		_, _ = dbPool.Exec(r.Context(),
			"DELETE FROM sessions WHERE id = $1", cookie.Value,
		)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}