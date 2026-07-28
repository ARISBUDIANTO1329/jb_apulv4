package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/middleware"
)

type DashboardHandler struct {
	DB     *pgxpool.Pool
	Cfg    *config.Config
	Tmpl   *template.Template
}

func NewDashboardHandler(db *pgxpool.Pool, cfg *config.Config, tmpl *template.Template) *DashboardHandler {
	return &DashboardHandler{DB: db, Cfg: cfg, Tmpl: tmpl}
}

func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// SSR: load channels data directly
	rows, _ := h.DB.Query(r.Context(),
		`SELECT id, name, niche, token_status, stream_key, subscriber_count, total_views, video_count, last_upload, last_livestream
		 FROM channels WHERE user_id = $1 ORDER BY id ASC`, user.ID)
	defer rows.Close()

	type chData struct {
		ID int; Name, Niche, TokenStatus, StreamKey string
		SubscriberCount, VideoCount int; TotalViews int64
		LastUpload, LastLivestream *time.Time
	}
	channels := []chData{}
	for rows.Next() {
		var c chData
		rows.Scan(&c.ID, &c.Name, &c.Niche, &c.TokenStatus, &c.StreamKey,
			&c.SubscriberCount, &c.TotalViews, &c.VideoCount, &c.LastUpload, &c.LastLivestream)
		channels = append(channels, c)
	}

	channelsJSON, _ := json.Marshal(channels)

	renderTemplate(w, r, "dashboard", map[string]interface{}{
		"Title":        "Dashboard",
		"User":         user,
		"AppName":      h.Cfg.AppName,
		"Active":       "dashboard",
		"ChannelsJSON": string(channelsJSON),
	})
}

func (h *DashboardHandler) Stats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var videoCount, channelCount int
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM media_items WHERE user_id = $1", user.ID).Scan(&videoCount)
	h.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM channels WHERE user_id = $1", user.ID).Scan(&channelCount)

	data := map[string]interface{}{
		"VideoCount":   videoCount,
		"ChannelCount": channelCount,
		"ActiveJobs":   0,
	}

	h.Tmpl.ExecuteTemplate(w, "stats", data)
}

func (h *DashboardHandler) SystemStats(w http.ResponseWriter, r *http.Request) {
	var memTotal, memAvail float64
	var diskTotal, diskUsed, diskPercent float64

	// Memory from /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					memTotal, _ = strconv.ParseFloat(fields[1], 64)
				}
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					memAvail, _ = strconv.ParseFloat(fields[1], 64)
				}
			}
		}
	}

	// Disk via statfs
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		diskTotal = float64(stat.Blocks * uint64(stat.Bsize)) / 1024 / 1024
		diskUsed = float64((stat.Blocks - stat.Bfree) * uint64(stat.Bsize)) / 1024 / 1024
		diskPercent = diskUsed / diskTotal * 100
	}

	memUsed := (memTotal - memAvail) / 1024
	memTotalMB := memTotal / 1024
	memPercent := float64(0)
	if memTotal > 0 {
		memPercent = (memTotal - memAvail) / memTotal * 100
	}

	output := map[string]interface{}{
		"cpu_percent": 0,
		"memory": map[string]interface{}{
			"total_mb": int(memTotalMB),
			"used_mb":  int(memUsed),
			"percent":  int(memPercent),
		},
		"disk": map[string]interface{}{
			"total":   formatSizeMB(diskTotal),
			"used":    formatSizeMB(diskUsed),
			"percent": int(diskPercent),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(output)
}

func formatSizeMB(mb float64) string {
	if mb > 1024*1024 {
		return fmt.Sprintf("%.0fTB", mb/1024/1024)
	}
	if mb > 1024 {
		return fmt.Sprintf("%.0fGB", mb/1024)
	}
	return fmt.Sprintf("%.0fMB", mb)
}