package main

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/jbapul/jb_apulv4/internal/database"
	"github.com/jbapul/jb_apulv4/internal/handlers"
	"github.com/jbapul/jb_apulv4/internal/middleware"
	"github.com/jbapul/jb_apulv4/internal/models"
	"github.com/jbapul/jb_apulv4/internal/worker"
)

func main() {
	cfg := config.Load()
	log.Printf("[Boot] Starting %s (%s)", cfg.AppName, cfg.Env)

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	ctx := context.Background()

	db, err := database.ConnectPostgres(ctx, dsn)
	if err != nil {
		log.Fatalf("[Boot] DB connection failed: %v", err)
	}
	defer database.Close()

	rdb := database.ConnectRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	_ = rdb

	tmpl := loadTemplates("internal/templates")
	_ = tmpl
	handlers.SetBaseDir("internal/templates")
	handlers.SetDBPool(db)

	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)
	r.Use(chimw.Timeout(60 * time.Second))

	// Session middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err == nil && cookie.Value != "" {
				var user models.User
				err := db.QueryRow(r.Context(),
					`SELECT u.id, u.email, u.name, u.avatar_url, u.role, u.created_at, u.updated_at
					 FROM users u JOIN sessions s ON u.id = s.user_id
					 WHERE s.id = $1 AND s.expires_at > NOW()`,
					cookie.Value,
				).Scan(&user.ID, &user.Email, &user.Name, &user.AvatarURL, &user.Role, &user.CreatedAt, &user.UpdatedAt)
				if err == nil {
					ctx := context.WithValue(r.Context(), middleware.UserKey, &user)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	// Static files
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
	r.Handle("/storage/*", http.StripPrefix("/storage/", http.FileServer(http.Dir("storage"))))

	// Init handlers
	authH := handlers.NewAuthHandler(db, cfg)
	dashH := handlers.NewDashboardHandler(db, cfg, tmpl)
	channelH := handlers.NewChannelHandler(db, cfg, tmpl)
	mediaH := handlers.NewMediaHandler(db, cfg, tmpl)
	prodH := handlers.NewProductionHandler(db, cfg, tmpl)
	uploadH := handlers.NewUploadHandler(db, cfg, tmpl)
	liveH := handlers.NewLivestreamHandler(db, cfg, tmpl)
	pipelineH := handlers.NewPipelineHandler(db, cfg, tmpl)
	shortsH := handlers.NewShortsHandler(db, cfg, tmpl)

	// Public routes
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })
	r.Get("/api/system/stats", dashH.SystemStats)

	r.Get("/login", authH.LoginPage)
	r.Post("/login", authH.Login)
	r.Get("/logout", authH.Logout)
	r.Get("/auth/google", authH.GoogleLogin)
	r.Get("/auth/google/callback", authH.GoogleCallback)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		// HTML Pages
		r.Get("/", dashH.Dashboard)
		r.Get("/dashboard", dashH.Dashboard)
		r.Get("/dashboard/stats", dashH.Stats)

		// Channels
		r.Get("/channels", channelH.Page)
		r.Get("/api/channels", channelH.List)
		r.Post("/api/channels", channelH.Create)
		r.Get("/api/channels/token-health", channelH.TokenHealth)
		r.Get("/api/channels/{id}", channelH.Get)
		r.Put("/api/channels/{id}", channelH.Update)
		r.Delete("/api/channels/{id}", channelH.Delete)
		r.Get("/api/channels/{id}/storage", channelH.StorageStats)
		r.Get("/api/channels/{id}/connect", channelH.ConnectURL)
		r.Get("/api/channels/{id}/callback", channelH.Callback)
		r.Get("/api/channels/{id}/token-status", channelH.TokenStatus)

		// Media
		r.Get("/media", mediaH.Index)
		r.Get("/media/upload", mediaH.UploadPage)
		r.Get("/media/{id}", mediaH.Detail)
		r.Get("/api/media/groups", mediaH.ListGroups)
		r.Get("/api/media/stats", mediaH.Stats)
		r.Get("/api/media/disk", mediaH.Disk)
		r.Get("/api/media", mediaH.List)
		r.Post("/api/media/upload", mediaH.Upload)
		r.Get("/api/media/preview/{id}", mediaH.Preview)
		r.Get("/api/media/download/{id}", mediaH.Download)
		r.Get("/api/media/stream/{id}", mediaH.Stream)
		r.Get("/api/media/preview-clip/{id}", mediaH.PreviewClip)
		r.Get("/api/media/preview-page/{id}", mediaH.PreviewPage)
		r.Delete("/api/media/{id}", mediaH.Delete)
		r.Delete("/api/media/group/{group}", mediaH.DeleteGroup)

		// Production
		r.Get("/production", prodH.Page)
		r.Get("/api/production", prodH.List)
		r.Post("/api/production", prodH.Create)
		r.Post("/api/production/batch", prodH.BatchCreate)
		r.Get("/api/production/runtime", prodH.Runtime)
		r.Get("/api/production/{id}", prodH.Get)
		r.Post("/api/production/{id}/send-upload-ready", prodH.SendUploadReady)
		r.Delete("/api/production/{id}", prodH.Delete)
		r.Post("/api/production/{id}/retry", prodH.Retry)
		r.Delete("/api/production/batch/{method}", prodH.DeleteBatch)
		r.Get("/api/production/media/{channel_id}", prodH.AvailableMedia)
		r.Get("/api/production/preview/{id}", prodH.Preview)
		r.Get("/api/production/logs", prodH.Logs)
		r.Get("/api/production/cooldown/{channel_id}", prodH.Cooldown)
		r.Get("/api/production/dynamic-status/{channel_id}", prodH.DynamicStatus)
		r.Get("/api/production/seamless-progress/{channel_id}", prodH.SeamlessProgress)
		r.Get("/api/production/system-logs/{mode}", prodH.SystemLogs)
		r.Post("/api/production/auto-schedule", prodH.AutoSchedule)
		r.Get("/api/production/auto-schedule/{channel_id}", prodH.GetAutoSchedule)

		// Uploads
		r.Get("/uploads", uploadH.Page)
		r.Get("/monitor-upload", uploadH.MonitorPage)
		r.Get("/api/uploads", uploadH.ListBatches)
		r.Get("/api/uploads/items", uploadH.ListItems)
		r.Get("/api/uploads/stats", uploadH.Stats)
		r.Post("/api/uploads", uploadH.CreateBatch)
		r.Get("/api/uploads/bank-title", uploadH.ListTitles)
		r.Post("/api/uploads/bank-title", uploadH.UploadTitle)
		r.Get("/api/uploads/bank-title/random", uploadH.RandomTitle)
		r.Post("/api/uploads/clipboard-description", uploadH.SaveDescription)
		r.Get("/api/uploads/clipboard-description", uploadH.GetDescription)
		r.Post("/api/uploads/clipboard-tags", uploadH.SaveTags)
		r.Get("/api/uploads/clipboard-tags", uploadH.GetTags)
		r.Get("/api/uploads/upload-ready", uploadH.UploadReady)
		r.Post("/api/uploads/preview-schedule", uploadH.PreviewSchedule)
		r.Post("/api/uploads/batch", uploadH.CreateFromSchedule)
		r.Post("/api/uploads/{id}/retry", uploadH.RetryItem)
		r.Delete("/api/uploads/{id}", uploadH.DeleteItem)

		// Livestream
		r.Get("/live", liveH.Page)
		r.Get("/monitor-live", liveH.MonitorPage)
		r.Get("/api/livestream", liveH.List)
		r.Post("/api/livestream", liveH.Create)
		r.Get("/api/livestream/running", liveH.Running)
		r.Get("/api/livestream/checker-global", liveH.CheckerGlobal)
		r.Get("/api/livestream/kill-global", liveH.KillGlobal)
		r.Get("/api/livestream/readiness", liveH.Readiness)
		r.Post("/api/livestream/cleanup-jobs", liveH.CleanupJobs)
		r.Post("/api/livestream/publish-now", liveH.PublishNow)
		r.Post("/api/livestream/schedule", liveH.Schedule)
		r.Post("/api/livestream/check-token", liveH.CheckToken)
		r.Get("/api/livestream/video-sources/{channel_id}", liveH.VideoSources)
		r.Get("/api/livestream/monitor", liveH.Monitor)
		r.Get("/api/livestream/health-dashboard", liveH.HealthDashboard)
		r.Get("/api/livestream/engine-status", liveH.EngineStatus)
		r.Get("/api/livestream/{id}", liveH.Get)
		r.Put("/api/livestream/{id}", liveH.Update)
		r.Post("/api/livestream/{id}/process-check", liveH.ProcessCheck)
		r.Post("/api/livestream/{id}/kill-process", liveH.KillProcess)
		r.Post("/api/livestream/{id}/stop", liveH.Stop)
		r.Delete("/api/livestream/{id}", liveH.Delete)
		r.Get("/api/livestream/{id}/stats", liveH.Stats)

		// Pipeline
		r.Get("/pipeline", pipelineH.Page)
		r.Get("/api/pipeline", pipelineH.List)
		r.Post("/api/pipeline", pipelineH.Create)
		r.Get("/api/pipeline/{id}", pipelineH.Get)
		r.Put("/api/pipeline/{id}", pipelineH.Update)
		r.Post("/api/pipeline/{id}/toggle", pipelineH.Toggle)
		r.Post("/api/pipeline/{id}/toggle-feature", pipelineH.ToggleFeature)
		r.Post("/api/pipeline/{id}/save-upload", pipelineH.SaveUpload)
		r.Post("/api/pipeline/{id}/save-livestream", pipelineH.SaveLivestream)
		r.Post("/api/pipeline/{id}/save-shorts", pipelineH.SaveShorts)
		r.Post("/api/pipeline/{id}/save-config", pipelineH.SaveConfig)
		r.Post("/api/pipeline/{id}/run", pipelineH.Run)
		r.Post("/api/pipeline/{id}/start", pipelineH.Start)
		r.Post("/api/pipeline/{id}/pause", pipelineH.Pause)
		r.Post("/api/pipeline/{id}/resume", pipelineH.Resume)
		r.Get("/api/pipeline/{id}/active-run", pipelineH.ActiveRun)
		r.Get("/api/pipeline/{id}/runs", pipelineH.Runs)
		r.Get("/api/pipeline/run/{run_id}/status", pipelineH.RunStatus)
		r.Post("/api/pipeline/run/{run_id}/cancel", pipelineH.CancelRun)
		r.Delete("/api/pipeline/{id}", pipelineH.Delete)

		// Shorts
		r.Get("/shorts", shortsH.Page)
		r.Get("/monitor-shorts", shortsH.MonitorPage)
		r.Get("/api/shorts", shortsH.List)
		r.Post("/api/shorts", shortsH.Create)
		r.Get("/api/shorts/completed-uploads", shortsH.CompletedUploads)
		r.Get("/api/shorts/{id}", shortsH.Get)
		r.Delete("/api/shorts/{id}", shortsH.Delete)
		r.Post("/api/shorts/{id}/retry", shortsH.Retry)
	})

	// Start workers
	workerMgr := worker.NewManager(db, cfg, rdb)
	workerMgr.Start()
	defer workerMgr.Stop()

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("[Boot] Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		workerMgr.Stop()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("[Boot] Listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[Boot] Server error: %v", err)
	}
}

func loadTemplates(dir string) *template.Template {
	funcMap := template.FuncMap{
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"seq":      func(n int) []int { s := make([]int, n); for i := 0; i < n; i++ { s[i] = i }; return s },
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}

	tmpl := template.New("").Funcs(funcMap)

	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(path)
		if ext == ".html" || ext == ".gohtml" {
			_, err := tmpl.ParseFiles(path)
			if err != nil {
				log.Printf("[Templates] Parse error %s: %v", path, err)
			}
		}
		return nil
	})

	return tmpl
}