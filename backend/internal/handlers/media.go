package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
)

type MediaHandler struct {
	DB   *pgxpool.Pool
	Cfg  *config.Config
	Tmpl *template.Template
}

func NewMediaHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *MediaHandler {
	return &MediaHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

// 10 asset groups with labels and descriptions
var groupMeta = []struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Exts  string `json:"-"`
}{
	{"video", "Video", "Main video footage", ".mp4,.mkv,.webm,.mov,.avi"},
	{"video-raw", "Video Raw", "Raw footage before processing", ".mp4,.mkv,.webm,.mov,.avi"},
	{"video-live", "Video Live", "Live recording footage", ".mp4,.mkv,.webm,.mov,.avi"},
	{"livestream-ready", "Livestream Ready", "Video ready for livestream", ".mp4,.mkv,.webm,.mov,.avi"},
	{"upload_ready", "Upload Ready", "Final video for YouTube upload", ".mp4,.mkv,.webm,.mov,.avi"},
	{"mp3", "MP3", "Audio / music / voice", ".mp3"},
	{"sfx", "SFX", "Short sound effects", ".mp3,.wav,.ogg,.flac"},
	{"intro", "Intro", "Opening video", ".mp4,.mkv,.webm,.mov,.avi"},
	{"thumbnail", "Thumbnail", "Thumbnail image", ".jpg,.jpeg,.png,.gif,.webp"},
	{"metadata", "Metadata", "Supporting metadata files", ".txt,.json,.csv,.md"},
}

var safeNameRe = regexp.MustCompile(`[^\w.\-]`)

// detectMIME returns a MIME type based on file extension.
// Used as fallback when browser sends generic Content-Type like application/octet-stream.
func detectMIME(ext string) string {
	m := map[string]string{
		".mp4": "video/mp4", ".mkv": "video/x-matroska", ".webm": "video/webm",
		".mov": "video/quicktime", ".avi": "video/x-msvideo",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".flac": "audio/flac",
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
		".gif": "image/gif", ".webp": "image/webp",
		".txt": "text/plain", ".json": "application/json", ".csv": "text/csv", ".md": "text/markdown",
	}
	if v, ok := m[strings.ToLower(ext)]; ok {
		return v
	}
	return "application/octet-stream"
}

func (h *MediaHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	result := []map[string]string{}
	for _, g := range groupMeta {
		result = append(result, map[string]string{
			"type":  g.Type,
			"label": g.Label,
			"desc":  g.Desc,
		})
	}
	json.NewEncoder(w).Encode(result)
}

func (h *MediaHandler) Stats(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	type assetStat struct {
		AssetType string `json:"asset_type"`
		Count     int    `json:"count"`
		TotalSize int64  `json:"total_size"`
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	if channelID != "" {
		where = "WHERE channel_id = $1"
		args = append(args, channelID)
	}

	rows, _ := h.DB.Query(r.Context(),
		`SELECT asset_type, COUNT(*), COALESCE(SUM(file_size),0) FROM media_items `+where+` GROUP BY asset_type`, args...)
	defer rows.Close()

	result := []assetStat{}
	for rows.Next() {
		var s assetStat
		rows.Scan(&s.AssetType, &s.Count, &s.TotalSize)
		result = append(result, s)
	}
	json.NewEncoder(w).Encode(result)
}

func (h *MediaHandler) Disk(w http.ResponseWriter, r *http.Request) {
	var stat os.FileInfo
	stat, _ = os.Stat(h.Cfg.StorageDir)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"storage_path": h.Cfg.StorageDir,
		"exists":       stat != nil,
	})
}

func (h *MediaHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 { page = 1 }
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 { perPage = 12 }
	offset := (page - 1) * perPage

	channelID := r.URL.Query().Get("channel_id")
	assetType := r.URL.Query().Get("asset_type")
	search := r.URL.Query().Get("search")

	where := "WHERE 1=1"
	args := []interface{}{}
	i := 1
	if channelID != "" {
		where += fmt.Sprintf(" AND channel_id = $%d", i)
		args = append(args, channelID); i++
	}
	if assetType != "" {
		where += fmt.Sprintf(" AND asset_type = $%d", i)
		args = append(args, assetType); i++
	}
	if search != "" {
		where += fmt.Sprintf(" AND (filename ILIKE $%d OR original_name ILIKE $%d OR title ILIKE $%d OR tags ILIKE $%d)", i, i, i, i)
		args = append(args, "%"+search+"%"); i++
	}

	args = append(args, perPage, offset)
	rows, err := h.DB.Query(r.Context(),
		`SELECT id, channel_id, user_id, filename, original_name, file_path, asset_type,
		        COALESCE(mime,''), file_size, duration, COALESCE(title,''), COALESCE(note,''),
		        COALESCE(tags,''), status, COALESCE(category,''),
		        scheduled_at, published_at, youtube_video_id, is_used, created_at, updated_at
		 FROM media_items `+where+` ORDER BY created_at DESC LIMIT $`+strconv.Itoa(i)+` OFFSET $`+strconv.Itoa(i+1), args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []models.MediaItem{}
	for rows.Next() {
		var m models.MediaItem
		rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Filename, &m.OriginalName, &m.FilePath, &m.AssetType,
			&m.MIME, &m.FileSize, &m.Duration, &m.Title, &m.Note, &m.Tags, &m.Status, &m.Category,
			&m.ScheduledAt, &m.PublishedAt, &m.YoutubeVideoID, &m.IsUsed, &m.CreatedAt, &m.UpdatedAt)
		items = append(items, m)
	}
	json.NewEncoder(w).Encode(items)
}

func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	r.ParseMultipartForm(h.Cfg.MaxFileSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	channelID := r.FormValue("channel_id")
	assetType := r.FormValue("asset_type")
	metadataCategory := r.FormValue("metadata_category")

	if assetType == "" { assetType = "video" }
	if channelID == "" {
		http.Error(w, "channel_id required", http.StatusBadRequest)
		return
	}

	// Validate extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	valid := false
	for _, g := range groupMeta {
		if g.Type == assetType {
			for _, e := range strings.Split(g.Exts, ",") {
				if ext == e { valid = true; break }
			}
			break
		}
	}
	if !valid {
		http.Error(w, fmt.Sprintf("Invalid file extension %s for %s", ext, assetType), http.StatusBadRequest)
		return
	}

	// Safe filename
	orig := header.Filename
	if orig == "" { orig = "upload" }
	base := safeNameRe.ReplaceAllString(strings.TrimSuffix(orig, ext), "_")
	if base == "" { base = "file" }
	safeName := fmt.Sprintf("%s-%s%s", base, uuid.New().String()[:8], ext)

	// Save file
	uploadDir := filepath.Join(h.Cfg.StorageDir, "assets", assetType, channelID)
	os.MkdirAll(uploadDir, 0755)
	destPath := filepath.Join(uploadDir, safeName)

	dest, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	written, err := io.Copy(dest, file)
	if err != nil {
		os.Remove(destPath)
		http.Error(w, "Upload failed", http.StatusInternalServerError)
		return
	}

	mime := header.Header.Get("Content-Type")
	// Use server-side detection if browser sent generic type
	if mime == "" || mime == "application/octet-stream" {
		mime = detectMIME(ext)
	}
	itemID := uuid.New().String()

	_, err = h.DB.Exec(r.Context(),
		`INSERT INTO media_items (id, channel_id, user_id, filename, original_name, file_path, asset_type, mime, file_size, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'active',NOW(),NOW())`,
		itemID, channelID, user.ID, safeName, header.Filename, destPath, assetType, mime, written)
	if err != nil {
		os.Remove(destPath)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Handle metadata upload (title bank, description, tags)
	if assetType == "metadata" && metadataCategory != "" {
		content, _ := os.ReadFile(destPath)
		lines := strings.Split(string(content), "\n")
		var validLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				validLines = append(validLines, trimmed)
			}
		}

		switch metadataCategory {
		case "title_bank":
			h.DB.Exec(r.Context(), "DELETE FROM metadata_title_pools WHERE channel_id = $1", channelID)
			for _, title := range validLines {
				h.DB.Exec(r.Context(), "INSERT INTO metadata_title_pools (id, channel_id, title, created_at) VALUES ($1,$2,$3,NOW())",
					uuid.New().String(), channelID, title)
			}
		case "description_bank":
			h.DB.Exec(r.Context(), "DELETE FROM metadata_description_pools WHERE channel_id = $1", channelID)
			for _, desc := range validLines {
				h.DB.Exec(r.Context(), "INSERT INTO metadata_description_pools (id, channel_id, description, created_at) VALUES ($1,$2,$3,NOW())",
					uuid.New().String(), channelID, desc)
			}
		case "tag_bank":
			h.DB.Exec(r.Context(), "DELETE FROM metadata_tag_pools WHERE channel_id = $1", channelID)
			for _, tag := range validLines {
				h.DB.Exec(r.Context(), "INSERT INTO metadata_tag_pools (id, channel_id, tags, created_at) VALUES ($1,$2,$3,NOW())",
					uuid.New().String(), channelID, tag)
			}
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": itemID, "filename": safeName})
}

func (h *MediaHandler) Preview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var filePath, mime string
	err := h.DB.QueryRow(r.Context(), "SELECT file_path, COALESCE(mime,'') FROM media_items WHERE id = $1", id).Scan(&filePath, &mime)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if mime != "" { w.Header().Set("Content-Type", mime) }
	http.ServeFile(w, r, filePath)
}

func (h *MediaHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var filePath, originalName string
	h.DB.QueryRow(r.Context(), "SELECT file_path, original_name FROM media_items WHERE id = $1", id).Scan(&filePath, &originalName)
	if filePath == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", originalName))
	http.ServeFile(w, r, filePath)
}

func (h *MediaHandler) Stream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var filePath, mime string
	h.DB.QueryRow(r.Context(), "SELECT file_path, COALESCE(mime,'video/mp4') FROM media_items WHERE id = $1", id).Scan(&filePath, &mime)
	if filePath == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "File error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var start, end int64
		fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		if end == 0 { end = stat.Size() - 1 }
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		f.Seek(start, 0)
		io.CopyN(w, f, end-start+1)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	io.Copy(w, f)
}

func (h *MediaHandler) PreviewClip(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (h *MediaHandler) PreviewPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var m models.MediaItem
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, filename, file_path, asset_type, COALESCE(mime,''), file_size, duration, COALESCE(title,''), created_at
		 FROM media_items WHERE id = $1`, id).Scan(
		&m.ID, &m.Filename, &m.FilePath, &m.AssetType, &m.MIME, &m.FileSize, &m.Duration, &m.Title, &m.CreatedAt)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	renderTemplate(w, r, "preview", map[string]interface{}{
		"Title":   "Preview: " + m.Filename,
		"AppName": h.Cfg.AppName,
		"Media":   m,
	})
}

func (h *MediaHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var filePath string
	h.DB.QueryRow(r.Context(), "SELECT file_path FROM media_items WHERE id = $1", id).Scan(&filePath)
	if filePath != "" { os.Remove(filePath) }
	h.DB.Exec(r.Context(), "DELETE FROM media_items WHERE id = $1", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *MediaHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	channelID := r.URL.Query().Get("channel_id")
	if channelID == "" {
		http.Error(w, "channel_id required", http.StatusBadRequest)
		return
	}
	rows, _ := h.DB.Query(r.Context(), "SELECT file_path FROM media_items WHERE asset_type = $1 AND channel_id = $2", group, channelID)
	defer rows.Close()
	for rows.Next() {
		var fp string
		rows.Scan(&fp)
		if fp != "" { os.Remove(fp) }
	}
	h.DB.Exec(r.Context(), "DELETE FROM media_items WHERE asset_type = $1 AND channel_id = $2", group, channelID)

	// Also clear metadata pools if deleting metadata group
	if group == "metadata" {
		h.DB.Exec(r.Context(), "DELETE FROM metadata_title_pools WHERE channel_id = $1", channelID)
		h.DB.Exec(r.Context(), "DELETE FROM metadata_description_pools WHERE channel_id = $1", channelID)
		h.DB.Exec(r.Context(), "DELETE FROM metadata_tag_pools WHERE channel_id = $1", channelID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *MediaHandler) Index(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	// SSR: load channels and groups data directly
	channelRows, _ := h.DB.Query(r.Context(), "SELECT id, name, niche FROM channels WHERE user_id = $1 ORDER BY id ASC", user.ID)
	defer channelRows.Close()
	type chData struct{ ID int; Name, Niche string }
	channels := []chData{}
	for channelRows.Next() {
		var c chData
		channelRows.Scan(&c.ID, &c.Name, &c.Niche)
		channels = append(channels, c)
	}

	groups := []map[string]string{}
	for _, g := range groupMeta {
		groups = append(groups, map[string]string{"type": g.Type, "label": g.Label, "desc": g.Desc})
	}

	channelsJSON, _ := json.Marshal(channels)
	groupsJSON, _ := json.Marshal(groups)

	renderTemplate(w, r, "media", map[string]interface{}{
		"Title":        "Media Library",
		"AppName":      h.Cfg.AppName,
		"User":         user,
		"Active":       "media",
		"ChannelsJSON": string(channelsJSON),
		"GroupsJSON":   string(groupsJSON),
	})
}

func (h *MediaHandler) UploadPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "upload", map[string]interface{}{
		"Title":   "Upload Media",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "media",
	})
}

func (h *MediaHandler) Detail(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	renderTemplate(w, r, "media_detail", map[string]interface{}{
		"Title":   "Media Detail",
		"AppName": h.Cfg.AppName,
		"User":    user,
		"Active":  "media",
	})
}