package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type ProductionHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewProductionHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *ProductionHandler {
	return &ProductionHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *ProductionHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	channelID := r.URL.Query().Get("channel_id")
	status := r.URL.Query().Get("status")
	method := r.URL.Query().Get("method")

	where := "WHERE p.user_id = $1"
	args := []interface{}{user.ID}
	i := 2
	if channelID != "" {
		where += fmt.Sprintf(" AND p.channel_id = $%d", i)
		args = append(args, channelID); i++
	}
	if status != "" {
		where += fmt.Sprintf(" AND p.status = $%d", i)
		args = append(args, status); i++
	}
	if method != "" {
		where += fmt.Sprintf(" AND p.production_method = $%d", i)
		args = append(args, method); i++
	}

	rows, err := h.DB.Query(r.Context(),
		`SELECT p.id, p.channel_id, p.user_id, p.video_source, p.num_songs, p.no_mp3, p.no_sfx,
		        p.production_mode, p.production_method, p.status, p.progress,
		        p.audio_status, p.video_status, p.final_status, p.error_message,
		        p.output_filename, p.created_at, p.updated_at,
		        COALESCE(c.name,'') as channel_name
		 FROM production_jobs p LEFT JOIN channels c ON p.channel_id = c.id `+where+` ORDER BY p.created_at DESC`, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ProductionWithChannel struct {
		models.ProductionJob
		ChannelName string `json:"channel_name"`
	}
	result := []ProductionWithChannel{}
	for rows.Next() {
		var j ProductionWithChannel
		rows.Scan(&j.ID, &j.ChannelID, &j.UserID, &j.VideoSource, &j.NumSongs, &j.NoMP3, &j.NoSFX,
			&j.ProductionMode, &j.ProductionMethod, &j.Status, &j.Progress,
			&j.AudioStatus, &j.VideoStatus, &j.FinalStatus, &j.ErrorMessage,
			&j.OutputFilename, &j.CreatedAt, &j.UpdatedAt, &j.ChannelName)
		result = append(result, j)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ProductionHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input models.ProductionJob
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO production_jobs (id, channel_id, user_id, video_source, num_songs, no_mp3, no_sfx,
		 sfx_file, intro_file, mp3_file, duration_mode, custom_duration,
		 production_mode, production_method, mp3_mode, tail_length, slowmo_percent,
		 merge_count, merge_resolution, merge_transition_enabled, merge_transition_name,
		 merge_transition_duration, merge_speed, dynamic_output_count,
		 status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,'pending',NOW(),NOW())`,
		id, input.ChannelID, user.ID, input.VideoSource, input.NumSongs, input.NoMP3, input.NoSFX,
		input.SFXFile, input.IntroFile, input.MP3File, input.DurationMode, input.CustomDuration,
		input.ProductionMode, input.ProductionMethod, input.MP3Mode, input.TailLength, input.SlowmoPercent,
		input.MergeCount, input.MergeResolution, input.MergeTransitionEnabled, input.MergeTransitionName,
		input.MergeTransitionDuration, input.MergeSpeed, input.DynamicOutputCount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (h *ProductionHandler) BatchCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		ChannelID string `json:"channel_id"`
		Method    string `json:"method"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, filename FROM media_items WHERE channel_id = $1 AND asset_type = 'video' AND status = 'active'`, input.ChannelID)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var mediaID, filename string
		rows.Scan(&mediaID, &filename)
		jobID := uuid.New().String()
		h.DB.Exec(r.Context(),
			`INSERT INTO production_jobs (id, channel_id, user_id, video_source, production_method, status, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,'pending',NOW(),NOW())`,
			jobID, input.ChannelID, user.ID, mediaID, input.Method)
		count++
	}
	json.NewEncoder(w).Encode(map[string]int{"created": count})
}

func (h *ProductionHandler) Runtime(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "unknown", "message": "Check worker logs"})
}

func (h *ProductionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var j models.ProductionJob
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, channel_id, user_id, video_source, num_songs, no_mp3, no_sfx,
		        sfx_file, intro_file, mp3_file, duration_mode, custom_duration,
		        production_mode, production_method, mp3_mode, tail_length, slowmo_percent,
		        merge_count, merge_resolution, merge_transition_enabled, merge_transition_name,
		        merge_transition_duration, merge_speed, dynamic_output_count,
		        status, progress, audio_status, video_status, final_status,
		        audio_path, video_path, final_path, audio_duration, error_message,
		        output_filename, created_at, updated_at
		 FROM production_jobs WHERE id = $1`, id).Scan(
		&j.ID, &j.ChannelID, &j.UserID, &j.VideoSource, &j.NumSongs, &j.NoMP3, &j.NoSFX,
		&j.SFXFile, &j.IntroFile, &j.MP3File, &j.DurationMode, &j.CustomDuration,
		&j.ProductionMode, &j.ProductionMethod, &j.MP3Mode, &j.TailLength, &j.SlowmoPercent,
		&j.MergeCount, &j.MergeResolution, &j.MergeTransitionEnabled, &j.MergeTransitionName,
		&j.MergeTransitionDuration, &j.MergeSpeed, &j.DynamicOutputCount,
		&j.Status, &j.Progress, &j.AudioStatus, &j.VideoStatus, &j.FinalStatus,
		&j.AudioPath, &j.VideoPath, &j.FinalPath, &j.AudioDuration, &j.ErrorMessage,
		&j.OutputFilename, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(j)
}

func (h *ProductionHandler) SendUploadReady(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var finalPath, channelID, outputFilename string
	h.DB.QueryRow(r.Context(), "SELECT final_path, channel_id, output_filename FROM production_jobs WHERE id = $1", id).Scan(&finalPath, &channelID, &outputFilename)
	if finalPath == "" {
		http.Error(w, "No output file", http.StatusBadRequest)
		return
	}
	uploadDir := filepath.Join(h.Cfg.StorageDir, "assets", "upload_ready", channelID)
	os.MkdirAll(uploadDir, 0755)
	dest := filepath.Join(uploadDir, filepath.Base(finalPath))
	os.Rename(finalPath, dest)
	h.DB.Exec(r.Context(), "UPDATE production_jobs SET status = 'done', final_path = $1 WHERE id = $2", dest, id)
	// Create media item record
	itemID := uuid.New().String()
	h.DB.Exec(r.Context(),
		`INSERT INTO media_items (id, channel_id, user_id, filename, original_name, file_path, asset_type, file_size, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$4,$5,'upload_ready',0,'active',NOW(),NOW())`,
		itemID, channelID, middleware.GetUser(r).ID, filepath.Base(finalPath), dest)
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "sent"})
}

func (h *ProductionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ap, vp, fp string
	h.DB.QueryRow(r.Context(), "SELECT audio_path, video_path, final_path FROM production_jobs WHERE id = $1", id).Scan(&ap, &vp, &fp)
	for _, p := range []string{ap, vp, fp} {
		if p != "" {
			os.Remove(p)
		}
	}
	h.DB.Exec(r.Context(), "DELETE FROM production_jobs WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProductionHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(),
		`UPDATE production_jobs SET status = 'pending', progress = 0, error_message = '',
		 audio_status = 'pending', video_status = 'pending', final_status = 'pending',
		 updated_at = NOW() WHERE id = $1`, id)
	json.NewEncoder(w).Encode(map[string]string{"status": "retrying"})
}

func (h *ProductionHandler) DeleteBatch(w http.ResponseWriter, r *http.Request) {
	method := chi.URLParam(r, "method")
	h.DB.Exec(r.Context(), "DELETE FROM production_jobs WHERE production_method = $1", method)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProductionHandler) AvailableMedia(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, filename, asset_type, file_size, duration FROM media_items
		 WHERE channel_id = $1 AND asset_type IN ('video','video-raw','video-live','mp3','sfx','intro')
		 AND status = 'active' ORDER BY asset_type, filename`, channelID)
	defer rows.Close()

	type MediaOption struct {
		ID        string  `json:"id"`
		Filename  string  `json:"filename"`
		AssetType string  `json:"asset_type"`
		FileSize  int64   `json:"file_size"`
		Duration  float64 `json:"duration"`
	}
	result := []MediaOption{}
	for rows.Next() {
		var m MediaOption
		rows.Scan(&m.ID, &m.Filename, &m.AssetType, &m.FileSize, &m.Duration)
		result = append(result, m)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ProductionHandler) Preview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var fp string
	h.DB.QueryRow(r.Context(), "SELECT final_path FROM production_jobs WHERE id = $1", id).Scan(&fp)
	if fp == "" {
		http.Error(w, "No preview", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, fp)
}

func (h *ProductionHandler) Logs(w http.ResponseWriter, r *http.Request) {
	// Stub: would tail production worker log
	json.NewEncoder(w).Encode(map[string]string{"logs": "Worker logs not available via API"})
}

func (h *ProductionHandler) Cooldown(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, asset_key, asset_filename, asset_type, usage_type, usage_date, cooldown_until, created_at
		 FROM asset_usage_logs WHERE channel_id = $1 AND cooldown_until > CURRENT_DATE
		 ORDER BY cooldown_until ASC`, channelID)
	defer rows.Close()
	result := []models.AssetUsageLog{}
	for rows.Next() {
		var l models.AssetUsageLog
		rows.Scan(&l.ID, &l.AssetKey, &l.AssetFilename, &l.AssetType, &l.UsageType, &l.UsageDate, &l.CooldownUntil, &l.CreatedAt)
		result = append(result, l)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ProductionHandler) DynamicStatus(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	json.NewEncoder(w).Encode(map[string]string{"channel_id": channelID, "status": "idle"})
}

func (h *ProductionHandler) SeamlessProgress(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, raw_filename, progress, status, message, created_at, updated_at
		 FROM auto_seamless_progresses WHERE channel_id = $1 ORDER BY created_at DESC LIMIT 20`, channelID)
	defer rows.Close()
	result := []models.AutoSeamlessProgress{}
	for rows.Next() {
		var p models.AutoSeamlessProgress
		rows.Scan(&p.ID, &p.RawFilename, &p.Progress, &p.Status, &p.Message, &p.CreatedAt, &p.UpdatedAt)
		result = append(result, p)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ProductionHandler) SystemLogs(w http.ResponseWriter, r *http.Request) {
	mode := chi.URLParam(r, "mode")
	json.NewEncoder(w).Encode(map[string]string{"mode": mode, "logs": "System logs not available"})
}

func (h *ProductionHandler) AutoSchedule(w http.ResponseWriter, r *http.Request) {
	_ = middleware.GetUser(r)
	var input struct {
		ChannelID  string `json:"channel_id"`
		Target     string `json:"target"`
		Workflow   string `json:"workflow"`
		ScheduleTime string `json:"schedule_time"`
		IsActive   bool   `json:"is_active"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.ScheduleTime == "" {
		input.ScheduleTime = "08:00:00"
	}

	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO auto_production_schedules (id, channel_id, target, workflow, schedule_time, is_active, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5::time,$6,NOW(),NOW())
		 ON CONFLICT DO NOTHING`,
		id, input.ChannelID, input.Target, input.Workflow, input.ScheduleTime, input.IsActive)
	if err != nil {
		// Update existing
		h.DB.Exec(r.Context(),
			`UPDATE auto_production_schedules SET target=$1, workflow=$2, schedule_time=$3::time, is_active=$4, updated_at=NOW()
			 WHERE channel_id=$5`, input.Target, input.Workflow, input.ScheduleTime, input.IsActive, input.ChannelID)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *ProductionHandler) GetAutoSchedule(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, target, workflow, schedule_time, is_active, next_run_at, created_at, updated_at
		 FROM auto_production_schedules WHERE channel_id = $1 ORDER BY created_at DESC`, channelID)
	defer rows.Close()
	result := []models.AutoProductionSchedule{}
	for rows.Next() {
		var s models.AutoProductionSchedule
		rows.Scan(&s.ID, &s.ChannelID, &s.Target, &s.Workflow, &s.ScheduleTime, &s.IsActive, &s.NextRunAt, &s.CreatedAt, &s.UpdatedAt)
		result = append(result, s)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ProductionHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "production", map[string]interface{}{
		"Title":   "Production",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "production",
	})
}