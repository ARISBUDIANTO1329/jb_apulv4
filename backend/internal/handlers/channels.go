package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type ChannelHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewChannelHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *ChannelHandler {
	return &ChannelHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *ChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	rows, err := h.DB.Query(r.Context(),
		`SELECT id, user_id, name, youtube_channel_id, youtube_channel_url, niche,
		        description, email, status, token_status, subscriber_count, total_views,
		        video_count, stream_key, notes, last_upload, last_livestream, created_at, updated_at
		 FROM channels WHERE user_id = $1 ORDER BY id ASC`, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	channels := []models.Channel{}
	for rows.Next() {
		var c models.Channel
		rows.Scan(&c.ID, &c.UserID, &c.Name, &c.YoutubeID, &c.YoutubeURL, &c.Niche,
			&c.Description, &c.Email, &c.Status, &c.TokenStatus, &c.SubscriberCount, &c.TotalViews,
			&c.VideoCount, &c.StreamKey, &c.Notes, &c.LastUpload, &c.LastLive, &c.CreatedAt, &c.UpdatedAt)
		channels = append(channels, c)
	}
	json.NewEncoder(w).Encode(channels)
}

func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		Name   string `json:"name"`
		Niche  string `json:"niche"`
		Email  string `json:"email"`
		YoutubeID string `json:"youtube_channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var id int
	err := h.DB.QueryRow(r.Context(),
		`INSERT INTO channels (user_id, name, youtube_channel_id, youtube_channel_url, niche, email, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,'active',NOW(),NOW()) RETURNING id`,
		user.ID, input.Name, input.YoutubeID, fmt.Sprintf("https://youtube.com/channel/%s", input.YoutubeID), input.Niche, input.Email).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func (h *ChannelHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	var c models.Channel
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, user_id, name, youtube_channel_id, youtube_channel_url, niche,
		        description, email, status, token_status, subscriber_count, total_views,
		        video_count, stream_key, notes, last_upload, last_livestream, created_at, updated_at
		 FROM channels WHERE id = $1`, id).Scan(
		&c.ID, &c.UserID, &c.Name, &c.YoutubeID, &c.YoutubeURL, &c.Niche,
		&c.Description, &c.Email, &c.Status, &c.TokenStatus, &c.SubscriberCount, &c.TotalViews,
		&c.VideoCount, &c.StreamKey, &c.Notes, &c.LastUpload, &c.LastLive, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(c)
}

// allowedChannelColumns is the allowlist of columns that can be updated via the API.
var allowedChannelColumns = map[string]bool{
	"name":              true,
	"youtube_channel_id": true,
	"youtube_channel_url": true,
	"niche":             true,
	"description":       true,
	"email":             true,
	"status":            true,
	"token_status":      true,
	"token_error":       true,
	"token_checked_at":  true,
	"token_expires_at":  true,
	"stream_key":        true,
	"proxy_host":        true,
	"proxy_port":        true,
	"proxy_type":        true,
	"subscriber_count":  true,
	"total_views":       true,
	"video_count":       true,
	"notes":             true,
	"last_upload":       true,
	"last_livestream":   true,
}

func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Build SET clause with allowlisted columns only
	setClause := ""
	args := []interface{}{}
	i := 1
	for k, v := range input {
		if !allowedChannelColumns[k] {
			continue
		}
		if setClause != "" {
			setClause += ", "
		}
		setClause += fmt.Sprintf("%s = $%d", k, i)
		args = append(args, v)
		i++
	}
	if setClause == "" {
		http.Error(w, "No valid fields to update", http.StatusBadRequest)
		return
	}
	args = append(args, id)
	_, err := h.DB.Exec(r.Context(),
		fmt.Sprintf("UPDATE channels SET %s, updated_at = NOW() WHERE id = $%d", setClause, i), args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	// Get file paths to clean up
	rows, _ := h.DB.Query(r.Context(), "SELECT file_path FROM media_items WHERE channel_id = $1", id)
	defer rows.Close()
	for rows.Next() {
		var fp string
		rows.Scan(&fp)
		if fp != "" {
			os.Remove(fp)
		}
	}
	// Cascade delete
	h.DB.Exec(r.Context(), "DELETE FROM channels WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChannelHandler) TokenHealth(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	rows, err := h.DB.Query(r.Context(),
		`SELECT id, name, token_status, token_checked_at, token_expires_at
		 FROM channels WHERE user_id = $1 ORDER BY name`, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TokenInfo struct {
		ID          int        `json:"id"`
		Name        string     `json:"name"`
		Status      string     `json:"token_status"`
		CheckedAt   *time.Time `json:"token_checked_at"`
		ExpiresAt   *time.Time `json:"token_expires_at"`
		IsExpiring  bool       `json:"is_expiring"`
	}
	result := []TokenInfo{}
	for rows.Next() {
		var t TokenInfo
		rows.Scan(&t.ID, &t.Name, &t.Status, &t.CheckedAt, &t.ExpiresAt)
		if t.ExpiresAt != nil && time.Until(*t.ExpiresAt) < 24*time.Hour {
			t.IsExpiring = true
		}
		result = append(result, t)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ChannelHandler) StorageStats(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	basePath := filepath.Join(h.Cfg.StorageDir, "assets", strconv.Itoa(id))
	var totalSize int64
	var fileCount int

	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		totalSize += info.Size()
		fileCount++
		return nil
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"channel_id": id,
		"total_size": totalSize,
		"file_count": fileCount,
		"total_size_mb": fmt.Sprintf("%.2f", float64(totalSize)/1024/1024),
	})
}

func (h *ChannelHandler) ConnectURL(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	state := strconv.Itoa(id) // Use channel ID as state
	url := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=https://www.googleapis.com/auth/youtube.force-ssl%%20https://www.googleapis.com/auth/userinfo.profile&state=%s&access_type=offline&prompt=consent",
		h.Cfg.GoogleClientID, h.Cfg.GoogleRedirectURI, state,
	)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *ChannelHandler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "Missing code or state", http.StatusBadRequest)
		return
	}

	// Exchange code for token (stub - would call Google OAuth API)
	// Store tokens on channel
	channelID, _ := strconv.Atoi(state)
	h.DB.Exec(r.Context(),
		`UPDATE channels SET token_status = 'valid', token_checked_at = NOW(),
		 token_expires_at = NOW() + INTERVAL '1 hour' WHERE id = $1`, channelID)

	http.Redirect(w, r, "/channels", http.StatusSeeOther)
}

func (h *ChannelHandler) TokenStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	var status, tokenError string
	var checkedAt *time.Time
	h.DB.QueryRow(r.Context(),
		"SELECT token_status, token_error, token_checked_at FROM channels WHERE id = $1", id,
	).Scan(&status, &tokenError, &checkedAt)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token_status":   status,
		"token_error":    tokenError,
		"token_checked_at": checkedAt,
	})
}

func (h *ChannelHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "channels", map[string]interface{}{
		"Title":   "Channels",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "channels",
	})
}