package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type ShortsHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewShortsHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *ShortsHandler {
	return &ShortsHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *ShortsHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	channelID := r.URL.Query().Get("channel_id")
	where := "WHERE user_id = $1"
	args := []interface{}{user.ID}
	if channelID != "" {
		where = "WHERE channel_id = $1"
		args = []interface{}{channelID}
	}
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, user_id, long_upload_id, long_youtube_url, long_title,
		        short_count, short_duration, segment_mode, status, error_message, created_at, completed_at
		 FROM shorts_jobs `+where+` ORDER BY created_at DESC`, args...)
	defer rows.Close()
	result := []models.ShortsJob{}
	for rows.Next() {
		var j models.ShortsJob
		rows.Scan(&j.ID, &j.ChannelID, &j.UserID, &j.LongUploadID, &j.LongYoutubeURL, &j.LongTitle,
			&j.ShortCount, &j.ShortDuration, &j.SegmentMode, &j.Status, &j.ErrorMessage, &j.CreatedAt, &j.CompletedAt)
		result = append(result, j)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ShortsHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		ChannelID      string `json:"channel_id"`
		LongUploadID   string `json:"long_upload_id"`
		LongYoutubeURL string `json:"long_youtube_url"`
		LongTitle      string `json:"long_title"`
		ShortCount     int    `json:"short_count"`
		ShortDuration  int    `json:"short_duration"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.ShortCount == 0 {
		input.ShortCount = 3
	}
	if input.ShortDuration == 0 {
		input.ShortDuration = 60
	}

	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO shorts_jobs (id, channel_id, user_id, long_upload_id, long_youtube_url, long_title,
		 short_count, short_duration, segment_mode, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'auto','created',NOW())`,
		id, input.ChannelID, user.ID, input.LongUploadID, input.LongYoutubeURL, input.LongTitle,
		input.ShortCount, input.ShortDuration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create shorts items
	for i := 0; i < input.ShortCount; i++ {
		itemID := uuid.New().String()
		startSecond := float64(i * input.ShortDuration)
		endSecond := startSecond + float64(input.ShortDuration)
		h.DB.Exec(r.Context(),
			`INSERT INTO shorts_items (id, job_id, short_number, start_second, end_second, title, status, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,'pending',NOW())`,
			itemID, id, i+1, startSecond, endSecond, fmt.Sprintf("%s #%d", input.LongTitle, i+1))
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id, "items_count": fmt.Sprintf("%d", input.ShortCount)})
}

func (h *ShortsHandler) CompletedUploads(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT ubi.id, ubi.title, ubi.youtube_video_id, ubi.finished_at, ubi.channel_id
		 FROM upload_batch_items ubi
		 WHERE ubi.channel_id = $1 AND ubi.status = 'done' AND ubi.youtube_video_id IS NOT NULL
		 AND NOT EXISTS (SELECT 1 FROM shorts_jobs sj WHERE sj.long_upload_id = ubi.id)
		 ORDER BY ubi.finished_at DESC LIMIT 20`, channelID)
	defer rows.Close()
	result := []models.UploadBatchItem{}
	for rows.Next() {
		var item models.UploadBatchItem
		rows.Scan(&item.ID, &item.Title, &item.YoutubeID, &item.FinishedAt, &item.ChannelID)
		result = append(result, item)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *ShortsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var j models.ShortsJob
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, channel_id, user_id, long_upload_id, long_youtube_url, long_title,
		        short_count, short_duration, segment_mode, description_template,
		        upload_time_1, upload_time_2, upload_time_3,
		        status, error_message, created_at, completed_at
		 FROM shorts_jobs WHERE id = $1`, id).Scan(
		&j.ID, &j.ChannelID, &j.UserID, &j.LongUploadID, &j.LongYoutubeURL, &j.LongTitle,
		&j.ShortCount, &j.ShortDuration, &j.SegmentMode, &j.DescTemplate,
		&j.UploadTime1, &j.UploadTime2, &j.UploadTime3,
		&j.Status, &j.ErrorMessage, &j.CreatedAt, &j.CompletedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Get items
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, job_id, short_number, video_path, start_second, end_second, title, description,
		        youtube_id, upload_time, status, error_message, created_at, uploaded_at
		 FROM shorts_items WHERE job_id = $1 ORDER BY short_number`, id)
	defer rows.Close()
	items := []models.ShortsItem{}
	for rows.Next() {
		var item models.ShortsItem
		rows.Scan(&item.ID, &item.JobID, &item.ShortNumber, &item.VideoPath, &item.StartSecond, &item.EndSecond,
			&item.Title, &item.Description, &item.YoutubeID, &item.UploadTime, &item.Status, &item.ErrorMessage, &item.CreatedAt, &item.UploadedAt)
		items = append(items, item)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job":   j,
		"items": items,
	})
}

func (h *ShortsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "DELETE FROM shorts_items WHERE job_id = $1", id)
	h.DB.Exec(r.Context(), "DELETE FROM shorts_jobs WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ShortsHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "UPDATE shorts_jobs SET status = 'created', error_message = '', completed_at = NULL WHERE id = $1", id)
	h.DB.Exec(r.Context(), "UPDATE shorts_items SET status = 'pending', error_message = '' WHERE job_id = $1", id)
	json.NewEncoder(w).Encode(map[string]string{"status": "retrying"})
}

func (h *ShortsHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "shorts", map[string]interface{}{
		"Title":   "Shorts",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "shorts",
	})
}

func (h *ShortsHandler) MonitorPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "monitor-shorts", map[string]interface{}{
		"Title":   "Shorts Monitor",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "monitor-shorts",
	})
}