package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type UploadHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewUploadHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *UploadHandler {
	return &UploadHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *UploadHandler) ListBatches(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	channelID := r.URL.Query().Get("channel_id")
	where := "WHERE user_id = $1"
	args := []interface{}{user.ID}
	if channelID != "" {
		where = "WHERE channel_id = $1"
		args = []interface{}{channelID}
	}
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, name, status, total_items, done_items, created_at
		 FROM upload_batches `+where+` ORDER BY created_at DESC`, args...)
	defer rows.Close()
	result := []models.UploadBatch{}
	for rows.Next() {
		var b models.UploadBatch
		rows.Scan(&b.ID, &b.ChannelID, &b.Name, &b.Status, &b.TotalItems, &b.DoneItems, &b.CreatedAt)
		result = append(result, b)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *UploadHandler) ListItems(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	channelID := r.URL.Query().Get("channel_id")
	status := r.URL.Query().Get("status")

	where := "WHERE 1=1"
	args := []interface{}{}
	i := 1
	if batchID != "" {
		where += fmt.Sprintf(" AND upload_batch_id = $%d", i); args = append(args, batchID); i++
	}
	if channelID != "" {
		where += fmt.Sprintf(" AND channel_id = $%d", i); args = append(args, channelID); i++
	}
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", i); args = append(args, status); i++
	}

	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, upload_batch_id, channel_id, media_item_id, title, description, tags,
		        youtube_video_id, scheduled_at, visibility, status, last_error, progress,
		        source_path, thumbnail_path, created_at, updated_at, finished_at
		 FROM upload_batch_items `+where+` ORDER BY created_at DESC`, args...)
	defer rows.Close()
	result := []models.UploadBatchItem{}
	for rows.Next() {
		var item models.UploadBatchItem
		rows.Scan(&item.ID, &item.UploadBatchID, &item.ChannelID, &item.MediaItemID, &item.Title, &item.Description, &item.Tags,
			&item.YoutubeID, &item.ScheduledAt, &item.Visibility, &item.Status, &item.LastError, &item.Progress,
			&item.SourcePath, &item.ThumbnailPath, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt)
		result = append(result, item)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *UploadHandler) Stats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var pending, processing, done, failed int
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM upload_batch_items WHERE user_id = $1 AND status = 'pending'", user.ID).Scan(&pending)
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM upload_batch_items WHERE user_id = $1 AND status = 'processing'", user.ID).Scan(&processing)
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM upload_batch_items WHERE user_id = $1 AND status = 'done'", user.ID).Scan(&done)
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM upload_batch_items WHERE user_id = $1 AND status = 'failed'", user.ID).Scan(&failed)
	json.NewEncoder(w).Encode(map[string]int{
		"pending": pending, "processing": processing, "done": done, "failed": failed,
	})
}

func (h *UploadHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO upload_batches (id, channel_id, user_id, name, status, created_at)
		 VALUES ($1,$2,$3,$4,'pending',NOW())`,
		id, input.ChannelID, user.ID, input.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (h *UploadHandler) ListTitles(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	rows, _ := h.DB.Query(r.Context(),
		"SELECT id, channel_id, title, used_at, created_at FROM metadata_title_pools WHERE channel_id = $1 ORDER BY created_at DESC", channelID)
	defer rows.Close()
	result := []models.MetadataTitlePool{}
	for rows.Next() {
		var t models.MetadataTitlePool
		rows.Scan(&t.ID, &t.ChannelID, &t.Title, &t.UsedAt, &t.CreatedAt)
		result = append(result, t)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *UploadHandler) UploadTitle(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	r.ParseMultipartForm(10 << 20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var titles []string
	json.NewDecoder(file).Decode(&titles)

	for _, title := range titles {
		if title != "" {
			h.DB.Exec(r.Context(),
				"INSERT INTO metadata_title_pools (id, channel_id, title, created_at) VALUES ($1,$2,$3,NOW()) ON CONFLICT DO NOTHING",
				uuid.New().String(), channelID, title)
		}
	}
	json.NewEncoder(w).Encode(map[string]int{"imported": len(titles)})
}

func (h *UploadHandler) RandomTitle(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var title string
	h.DB.QueryRow(r.Context(),
		"SELECT title FROM metadata_title_pools WHERE channel_id = $1 AND used_at IS NULL ORDER BY RANDOM() LIMIT 1", channelID).Scan(&title)
	if title == "" {
		http.Error(w, "No titles available", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"title": title})
}

func (h *UploadHandler) SaveDescription(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var input struct{ Description string `json:"description"` }
	json.NewDecoder(r.Body).Decode(&input)
	h.DB.Exec(r.Context(),
		"INSERT INTO metadata_description_pools (id, channel_id, description, created_at) VALUES ($1,$2,$3,NOW())",
		uuid.New().String(), channelID, input.Description)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *UploadHandler) GetDescription(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var desc string
	h.DB.QueryRow(r.Context(),
		"SELECT description FROM metadata_description_pools WHERE channel_id = $1 ORDER BY RANDOM() LIMIT 1", channelID).Scan(&desc)
	json.NewEncoder(w).Encode(map[string]string{"description": desc})
}

func (h *UploadHandler) SaveTags(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var input struct{ Tags string `json:"tags"` }
	json.NewDecoder(r.Body).Decode(&input)
	h.DB.Exec(r.Context(),
		"INSERT INTO metadata_tag_pools (id, channel_id, tags, created_at) VALUES ($1,$2,$3,NOW())",
		uuid.New().String(), channelID, input.Tags)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *UploadHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	var tags string
	h.DB.QueryRow(r.Context(),
		"SELECT tags FROM metadata_tag_pools WHERE channel_id = $1 ORDER BY RANDOM() LIMIT 1", channelID).Scan(&tags)
	json.NewEncoder(w).Encode(map[string]string{"tags": tags})
}

func (h *UploadHandler) UploadReady(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, filename, original_name, file_path, file_size, created_at
		 FROM media_items WHERE channel_id = $1 AND asset_type = 'upload_ready' AND status = 'active'
		 ORDER BY created_at DESC`, channelID)
	defer rows.Close()
	result := []models.MediaItem{}
	for rows.Next() {
		var m models.MediaItem
		rows.Scan(&m.ID, &m.Filename, &m.OriginalName, &m.FilePath, &m.FileSize, &m.CreatedAt)
		result = append(result, m)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *UploadHandler) PreviewSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ChannelID  string `json:"channel_id"`
		Count      int    `json:"count"`
		StartTime  string `json:"start_time"`
		Interval   int    `json:"interval_minutes"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.Count == 0 {
		input.Count = 1
	}
	if input.Interval == 0 {
		input.Interval = 60
	}
	now := time.Now()
	if input.StartTime != "" {
		t, _ := time.Parse("15:04", input.StartTime)
		now = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	}

	type ScheduleSlot struct {
		Index     int    `json:"index"`
		Time      string `json:"time"`
		Title     string `json:"title"`
	}
	schedule := []ScheduleSlot{}
	for i := 0; i < input.Count; i++ {
		t := now.Add(time.Duration(i*input.Interval) * time.Minute)
		schedule = append(schedule, ScheduleSlot{
			Index: i + 1,
			Time:  t.Format("2006-01-02 15:04"),
			Title: fmt.Sprintf("Video %d", i+1),
		})
	}
	json.NewEncoder(w).Encode(schedule)
}

func (h *UploadHandler) CreateFromSchedule(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		ChannelID string `json:"channel_id"`
		Items     []struct {
			MediaItemID string `json:"media_item_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
			ScheduledAt string `json:"scheduled_at"`
			Visibility  string `json:"visibility"`
		} `json:"items"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	batchID := uuid.New().String()
	h.DB.Exec(r.Context(),
		"INSERT INTO upload_batches (id, channel_id, user_id, name, status, total_items, created_at) VALUES ($1,$2,$3,'Schedule batch','pending',$4,NOW())",
		batchID, input.ChannelID, user.ID, len(input.Items))

	for _, item := range input.Items {
		itemID := uuid.New().String()
		var scheduledAt *time.Time
		if item.ScheduledAt != "" {
			t, _ := time.Parse("2006-01-02 15:04", item.ScheduledAt)
			scheduledAt = &t
		}
		h.DB.Exec(r.Context(),
			`INSERT INTO upload_batch_items (id, upload_batch_id, channel_id, media_item_id, user_id, title, description, tags, scheduled_at, visibility, status, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',NOW())`,
			itemID, batchID, input.ChannelID, item.MediaItemID, user.ID, item.Title, item.Description, item.Tags, scheduledAt, item.Visibility)
	}
	json.NewEncoder(w).Encode(map[string]string{"batch_id": batchID, "items_count": strconv.Itoa(len(input.Items))})
}

func (h *UploadHandler) RetryItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "UPDATE upload_batch_items SET status = 'pending', last_error = '', progress = 0, updated_at = NOW() WHERE id = $1", id)
	json.NewEncoder(w).Encode(map[string]string{"status": "retrying"})
}

func (h *UploadHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "DELETE FROM upload_batch_items WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *UploadHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "uploads", map[string]interface{}{
		"Title":   "Uploads",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "uploads",
	})
}

func (h *UploadHandler) MonitorPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "monitor-upload", map[string]interface{}{
		"Title":   "Upload Monitor",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "monitor-upload",
	})
}