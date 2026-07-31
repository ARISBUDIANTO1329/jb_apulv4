package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type LivestreamHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewLivestreamHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *LivestreamHandler {
	return &LivestreamHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *LivestreamHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	channelID := r.URL.Query().Get("channel_id")
	where := "WHERE user_id = $1"
	args := []interface{}{user.ID}
	if channelID != "" {
		where = "WHERE channel_id = $1"
		args = []interface{}{channelID}
	}
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, title, status, quality, visibility, duration_hours,
		        broadcast_id, stream_key, start_at_utc, end_at_utc, reconnect_count, viewer_count,
		        error_message, error_category, created_at, updated_at, started_at, finished_at
		 FROM live_jobs `+where+` ORDER BY created_at DESC`, args...)
	defer rows.Close()
	result := []models.LiveJob{}
	for rows.Next() {
		var j models.LiveJob
		rows.Scan(&j.ID, &j.ChannelID, &j.Title, &j.Status, &j.Quality, &j.Visibility, &j.DurationHours,
			&j.BroadcastID, &j.StreamKey, &j.StartAtUTC, &j.EndAtUTC, &j.ReconnectCount, &j.ViewerCount,
			&j.ErrorMessage, &j.ErrorCategory, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.FinishedAt)
		result = append(result, j)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *LivestreamHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input models.LiveJob
	json.NewDecoder(r.Body).Decode(&input)
	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO live_jobs (id, channel_id, user_id, title, description, tags, video_source,
		 use_mp3, use_sfx, stream_key, quality, visibility, duration_hours, made_for_kids,
		 status, max_retries, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'pending',3,NOW(),NOW())`,
		id, input.ChannelID, user.ID, input.Title, input.Description, input.Tags, input.VideoSource,
		input.UseMP3, input.UseSFX, input.StreamKey, input.Quality, input.Visibility, input.DurationHours, input.MadeForKids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (h *LivestreamHandler) Running(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, title, status, broadcast_id, stream_key, started_at, viewer_count,
		        current_bitrate, current_fps, reconnect_count, duration_hours
		 FROM live_jobs WHERE user_id = $1 AND status = 'running' ORDER BY started_at DESC`, user.ID)
	defer rows.Close()
	result := []models.LiveJob{}
	for rows.Next() {
		var j models.LiveJob
		rows.Scan(&j.ID, &j.ChannelID, &j.Title, &j.Status, &j.BroadcastID, &j.StreamKey, &j.StartedAt,
			&j.ViewerCount, &j.CurrentBitrate, &j.CurrentFPS, &j.ReconnectCount, &j.DurationHours)
		result = append(result, j)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *LivestreamHandler) CheckerGlobal(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Checker running"})
}

func (h *LivestreamHandler) KillGlobal(w http.ResponseWriter, r *http.Request) {
	h.DB.Exec(r.Context(),
		"UPDATE live_jobs SET stop_requested = true, status = 'stopped', finished_at = NOW() WHERE status IN ('running','scheduled')")
	json.NewEncoder(w).Encode(map[string]string{"status": "killed"})
}

func (h *LivestreamHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var tokenStatus, streamKey string
	h.DB.QueryRow(r.Context(), "SELECT token_status, COALESCE(stream_key,'') FROM channels WHERE id = $1", channelID).Scan(&tokenStatus, &streamKey)
	ready := tokenStatus == "valid" && streamKey != ""
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":       ready,
		"token_status": tokenStatus,
		"has_stream_key": streamKey != "",
	})
}

func (h *LivestreamHandler) CleanupJobs(w http.ResponseWriter, r *http.Request) {
	h.DB.Exec(r.Context(),
		`UPDATE live_jobs SET status = 'failed', error_message = 'Cleaned up' WHERE status = 'running' AND started_at < NOW() - INTERVAL '24 hours'`)
	json.NewEncoder(w).Encode(map[string]string{"status": "cleaned"})
}

func (h *LivestreamHandler) PublishNow(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input models.LiveJob
	json.NewDecoder(r.Body).Decode(&input)

	id := uuid.New().String()
	now := time.Now()
	end := now.Add(time.Duration(input.DurationHours) * time.Hour)
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO live_jobs (id, channel_id, user_id, title, description, tags, video_source,
		 use_mp3, use_sfx, stream_key, quality, visibility, duration_hours, made_for_kids,
		 status, start_at_utc, end_at_utc, max_retries, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'scheduled',$15,$16,3,NOW(),NOW())`,
		id, input.ChannelID, user.ID, input.Title, input.Description, input.Tags, input.VideoSource,
		input.UseMP3, input.UseSFX, input.StreamKey, input.Quality, input.Visibility, input.DurationHours, input.MadeForKids,
		now, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "scheduled"})
}

func (h *LivestreamHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	h.PublishNow(w, r)
}

func (h *LivestreamHandler) CheckToken(w http.ResponseWriter, r *http.Request) {
	var input struct{ ChannelID string `json:"channel_id"` }
	json.NewDecoder(r.Body).Decode(&input)
	var tokenStatus string
	h.DB.QueryRow(r.Context(), "SELECT token_status FROM channels WHERE id = $1", input.ChannelID).Scan(&tokenStatus)
	json.NewEncoder(w).Encode(map[string]string{"token_status": tokenStatus})
}

func (h *LivestreamHandler) VideoSources(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, filename, file_path, asset_type, duration FROM media_items
		 WHERE channel_id = $1 AND asset_type IN ('video','video-raw','video-live','livestream-ready')
		 AND status = 'active' ORDER BY asset_type, filename`, channelID)
	defer rows.Close()
	type Source struct {
		ID        string  `json:"id"`
		Filename  string  `json:"filename"`
		FilePath  string  `json:"file_path"`
		AssetType string  `json:"asset_type"`
		Duration  float64 `json:"duration"`
	}
	result := []Source{}
	for rows.Next() {
		var s Source
		rows.Scan(&s.ID, &s.Filename, &s.FilePath, &s.AssetType, &s.Duration)
		result = append(result, s)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *LivestreamHandler) Monitor(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, title, status, viewer_count, current_bitrate, current_fps,
		        frame_drop_count, started_at, duration_hours, reconnect_count, stream_key
		 FROM live_jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`, user.ID)
	defer rows.Close()
	result := []models.LiveJob{}
	for rows.Next() {
		var j models.LiveJob
		rows.Scan(&j.ID, &j.ChannelID, &j.Title, &j.Status, &j.ViewerCount, &j.CurrentBitrate, &j.CurrentFPS,
			&j.FrameDropCount, &j.StartedAt, &j.DurationHours, &j.ReconnectCount, &j.StreamKey)
		result = append(result, j)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *LivestreamHandler) HealthDashboard(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (h *LivestreamHandler) EngineStatus(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"engine": "idle"})
}

func (h *LivestreamHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var j models.LiveJob
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, channel_id, user_id, title, description, tags, video_source,
		 use_mp3, use_sfx, stream_key, broadcast_id, quality, visibility, duration_hours,
		 made_for_kids, thumbnail_path, status, start_at_utc, end_at_utc,
		 error_message, reconnect_count, viewer_count, current_bitrate, current_fps,
		 frame_drop_count, error_category, retry_count, max_retries,
		 created_at, updated_at, started_at, finished_at
		 FROM live_jobs WHERE id = $1`, id).Scan(
		&j.ID, &j.ChannelID, &j.UserID, &j.Title, &j.Description, &j.Tags, &j.VideoSource,
		&j.UseMP3, &j.UseSFX, &j.StreamKey, &j.BroadcastID, &j.Quality, &j.Visibility, &j.DurationHours,
		&j.MadeForKids, &j.ThumbnailPath, &j.Status, &j.StartAtUTC, &j.EndAtUTC,
		&j.ErrorMessage, &j.ReconnectCount, &j.ViewerCount, &j.CurrentBitrate, &j.CurrentFPS,
		&j.FrameDropCount, &j.ErrorCategory, &j.RetryCount, &j.MaxRetries,
		&j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(j)
}

// allowedLiveJobColumns is the allowlist of columns that can be updated via the API.
var allowedLiveJobColumns = map[string]bool{
	"channel_id":          true,
	"title":               true,
	"description":         true,
	"tags":                true,
	"video_source":        true,
	"use_mp3":             true,
	"use_sfx":             true,
	"stream_key":          true,
	"broadcast_id":        true,
	"quality":             true,
	"visibility":          true,
	"duration_hours":      true,
	"made_for_kids":       true,
	"thumbnail_path":      true,
	"status":              true,
	"start_at_utc":        true,
	"end_at_utc":          true,
	"stop_requested":      true,
	"error_message":       true,
	"error_category":      true,
	"reconnect_count":     true,
	"viewer_count":        true,
	"current_bitrate":     true,
	"current_fps":         true,
	"frame_drop_count":    true,
	"stream_stats":        true,
	"process_id":          true,
	"retry_count":         true,
	"max_retries":         true,
}

func (h *LivestreamHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	setClause := ""
	args := []interface{}{}
	i := 1
	for k, v := range input {
		if !allowedLiveJobColumns[k] {
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
	_, err := h.DB.Exec(r.Context(), fmt.Sprintf("UPDATE live_jobs SET %s, updated_at = NOW() WHERE id = $%d", setClause, i), args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *LivestreamHandler) ProcessCheck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var processID int
	h.DB.QueryRow(r.Context(), "SELECT process_id FROM live_jobs WHERE id = $1", id).Scan(&processID)
	alive := processID > 0
	json.NewEncoder(w).Encode(map[string]interface{}{"alive": alive, "process_id": processID})
}

func (h *LivestreamHandler) KillProcess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "UPDATE live_jobs SET stop_requested = true WHERE id = $1", id)
	json.NewEncoder(w).Encode(map[string]string{"status": "stopping"})
}

func (h *LivestreamHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.KillProcess(w, r)
}

func (h *LivestreamHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "DELETE FROM live_jobs WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *LivestreamHandler) Stats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var j models.LiveJob
	h.DB.QueryRow(r.Context(),
		"SELECT viewer_count, current_bitrate, current_fps, frame_drop_count, started_at, stream_stats FROM live_jobs WHERE id = $1", id).Scan(
		&j.ViewerCount, &j.CurrentBitrate, &j.CurrentFPS, &j.FrameDropCount, &j.StartedAt, &j.StreamStats)
	json.NewEncoder(w).Encode(j)
}

func (h *LivestreamHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "live", map[string]interface{}{
		"Title":   "Livestream",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "live",
	})
}

func (h *LivestreamHandler) MonitorPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "monitor-live", map[string]interface{}{
		"Title":   "Livestream Monitor",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "monitor-live",
	})
}