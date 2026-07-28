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

type PipelineHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewPipelineHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *PipelineHandler {
	return &PipelineHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *PipelineHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, channel_id, user_id, mode, upload_enabled, upload_count,
		        live_enabled, live_count, live_duration_hours, live_quality, live_use_mp3, live_use_sfx,
		        shorts_enabled, shorts_count, scheduler_time, is_active, created_at, updated_at
		 FROM pipelines WHERE user_id = $1 ORDER BY created_at DESC`, user.ID)
	defer rows.Close()
	result := []models.Pipeline{}
	for rows.Next() {
		var p models.Pipeline
		rows.Scan(&p.ID, &p.ChannelID, &p.UserID, &p.Mode, &p.UploadEnabled, &p.UploadCount,
			&p.LiveEnabled, &p.LiveCount, &p.LiveDurationHours, &p.LiveQuality, &p.LiveUseMP3, &p.LiveUseSFX,
			&p.ShortsEnabled, &p.ShortsCount, &p.SchedulerTime, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
		result = append(result, p)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *PipelineHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var input struct {
		ChannelID string `json:"channel_id"`
		Mode      string `json:"mode"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.Mode == "" {
		input.Mode = "dynamic"
	}
	id := uuid.New().String()
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO pipelines (id, channel_id, user_id, mode, is_active, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,false,NOW(),NOW())`,
		id, input.ChannelID, user.ID, input.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (h *PipelineHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var p models.Pipeline
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, channel_id, user_id, mode, upload_enabled, upload_count,
		        live_enabled, live_count, live_duration_hours, live_quality, live_use_mp3, live_use_sfx,
		        shorts_enabled, shorts_count, scheduler_time, is_active, config_json, created_at, updated_at
		 FROM pipelines WHERE id = $1`, id).Scan(
		&p.ID, &p.ChannelID, &p.UserID, &p.Mode, &p.UploadEnabled, &p.UploadCount,
		&p.LiveEnabled, &p.LiveCount, &p.LiveDurationHours, &p.LiveQuality, &p.LiveUseMP3, &p.LiveUseSFX,
		&p.ShortsEnabled, &p.ShortsCount, &p.SchedulerTime, &p.IsActive, &p.ConfigJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(p)
}

func (h *PipelineHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)
	setClause := ""
	args := []interface{}{}
	i := 1
	for k, v := range input {
		if k == "id" || k == "user_id" || k == "channel_id" || k == "created_at" {
			continue
		}
		if setClause != "" {
			setClause += ", "
		}
		setClause += fmt.Sprintf("%s = $%d", k, i)
		args = append(args, v)
		i++
	}
	args = append(args, id)
	h.DB.Exec(r.Context(), fmt.Sprintf("UPDATE pipelines SET %s, updated_at = NOW() WHERE id = $%d", setClause, i), args...)
	w.WriteHeader(http.StatusOK)
}

func (h *PipelineHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var isActive bool
	h.DB.QueryRow(r.Context(), "SELECT is_active FROM pipelines WHERE id = $1", id).Scan(&isActive)
	h.DB.Exec(r.Context(), "UPDATE pipelines SET is_active = $1, updated_at = NOW() WHERE id = $2", !isActive, id)
	json.NewEncoder(w).Encode(map[string]interface{}{"is_active": !isActive})
}

func (h *PipelineHandler) ToggleFeature(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input struct {
		Feature string `json:"feature"` // upload/live/shorts
		Enabled bool   `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	var col string
	switch input.Feature {
	case "upload":
		col = "upload_enabled"
	case "live":
		col = "live_enabled"
	case "shorts":
		col = "shorts_enabled"
	default:
		http.Error(w, "Invalid feature", http.StatusBadRequest)
		return
	}
	h.DB.Exec(r.Context(), fmt.Sprintf("UPDATE pipelines SET %s = $1, updated_at = NOW() WHERE id = $2", col), input.Enabled, id)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *PipelineHandler) SaveUpload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input struct {
		UploadCount int    `json:"upload_count"`
		Mode        string `json:"mode"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	h.DB.Exec(r.Context(), "UPDATE pipelines SET upload_count = $1, mode = $2, updated_at = NOW() WHERE id = $3", input.UploadCount, input.Mode, id)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *PipelineHandler) SaveLivestream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input struct {
		LiveCount       int    `json:"live_count"`
		LiveDuration    int    `json:"live_duration_hours"`
		LiveQuality     string `json:"live_quality"`
		LiveUseMP3      bool   `json:"live_use_mp3"`
		LiveUseSFX      bool   `json:"live_use_sfx"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	h.DB.Exec(r.Context(),
		"UPDATE pipelines SET live_count=$1, live_duration_hours=$2, live_quality=$3, live_use_mp3=$4, live_use_sfx=$5, updated_at=NOW() WHERE id=$6",
		input.LiveCount, input.LiveDuration, input.LiveQuality, input.LiveUseMP3, input.LiveUseSFX, id)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *PipelineHandler) SaveShorts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input struct {
		ShortsCount int `json:"shorts_count"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	h.DB.Exec(r.Context(), "UPDATE pipelines SET shorts_count = $1, updated_at = NOW() WHERE id = $2", input.ShortsCount, id)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *PipelineHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)
	configJSON, _ := json.Marshal(input)
	h.DB.Exec(r.Context(), "UPDATE pipelines SET config_json = $1, updated_at = NOW() WHERE id = $2", string(configJSON), id)
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (h *PipelineHandler) Run(w http.ResponseWriter, r *http.Request) {
	_ = middleware.GetUser(r)
	id := chi.URLParam(r, "id")
	var p models.Pipeline
	h.DB.QueryRow(r.Context(), "SELECT id, channel_id, mode FROM pipelines WHERE id = $1", id).Scan(&p.ID, &p.ChannelID, &p.Mode)
	runID := uuid.New().String()
	h.DB.Exec(r.Context(),
		`INSERT INTO pipeline_runs (id, pipeline_id, channel_id, status, current_stage, run_type, created_at, updated_at)
		 VALUES ($1,$2,$3,'pending','', 'manual', NOW(), NOW())`,
		runID, id, p.ChannelID)
	json.NewEncoder(w).Encode(map[string]string{"run_id": runID})
}

func (h *PipelineHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.Run(w, r)
}

func (h *PipelineHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Find active run and pause
	h.DB.Exec(r.Context(),
		"UPDATE pipeline_runs SET status = 'paused', updated_at = NOW() WHERE pipeline_id = $1 AND status IN ('pending','producing','uploading','livestreaming')", id)
	json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
}

func (h *PipelineHandler) Resume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(),
		"UPDATE pipeline_runs SET status = 'pending', updated_at = NOW() WHERE pipeline_id = $1 AND status = 'paused'", id)
	json.NewEncoder(w).Encode(map[string]string{"status": "resumed"})
}

func (h *PipelineHandler) ActiveRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var run models.PipelineRun
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, pipeline_id, channel_id, status, current_stage, progress, run_type, created_at, updated_at, finished_at
		 FROM pipeline_runs WHERE pipeline_id = $1 AND status NOT IN ('completed','failed','cancelled')
		 ORDER BY created_at DESC LIMIT 1`, id).Scan(
		&run.ID, &run.PipelineID, &run.ChannelID, &run.Status, &run.CurrentStage, &run.Progress, &run.RunType, &run.CreatedAt, &run.UpdatedAt, &run.FinishedAt)
	if err != nil {
		json.NewEncoder(w).Encode(nil)
		return
	}
	json.NewEncoder(w).Encode(run)
}

func (h *PipelineHandler) Runs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, pipeline_id, channel_id, status, current_stage, progress, run_type, error_message, created_at, finished_at
		 FROM pipeline_runs WHERE pipeline_id = $1 ORDER BY created_at DESC LIMIT 20`, id)
	defer rows.Close()
	result := []models.PipelineRun{}
	for rows.Next() {
		var run models.PipelineRun
		rows.Scan(&run.ID, &run.PipelineID, &run.ChannelID, &run.Status, &run.CurrentStage, &run.Progress, &run.RunType, &run.ErrorMessage, &run.CreatedAt, &run.FinishedAt)
		result = append(result, run)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *PipelineHandler) RunStatus(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	var run models.PipelineRun
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, pipeline_id, channel_id, status, current_stage, progress, run_type, log, error_message, created_at, finished_at
		 FROM pipeline_runs WHERE id = $1`, runID).Scan(
		&run.ID, &run.PipelineID, &run.ChannelID, &run.Status, &run.CurrentStage, &run.Progress, &run.RunType, &run.Log, &run.ErrorMessage, &run.CreatedAt, &run.FinishedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(run)
}

func (h *PipelineHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	h.DB.Exec(r.Context(), "UPDATE pipeline_runs SET status = 'cancelled', finished_at = NOW(), updated_at = NOW() WHERE id = $1", runID)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

func (h *PipelineHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DB.Exec(r.Context(), "DELETE FROM pipeline_runs WHERE pipeline_id = $1", id)
	h.DB.Exec(r.Context(), "DELETE FROM pipelines WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *PipelineHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "pipeline", map[string]interface{}{
		"Title":   "Pipeline",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "pipeline",
	})
}

func (h *PipelineHandler) Index(w http.ResponseWriter, r *http.Request) {
	h.Page(w, r)
}

func (h *PipelineHandler) Detail(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "pipeline_detail", map[string]interface{}{
		"Title":   "Pipeline Detail",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "pipeline",
	})
}