package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
)

// ProductionWorker handles video assembly with FFmpeg
type ProductionWorker struct {
	DB      *pgxpool.Pool
	Cfg     *config.Config
	queue   *JobQueue
	done    chan struct{}
}

func NewProductionWorker(db *pgxpool.Pool, cfg *config.Config, queue *JobQueue) *ProductionWorker {
	return &ProductionWorker{
		DB:    db,
		Cfg:   cfg,
		queue: queue,
		done:  make(chan struct{}),
	}
}

func (w *ProductionWorker) Run(ctx context.Context) {
	log.Println("[Worker:production] Started (Redis queue mode)")
	for {
		select {
		case <-ctx.Done():
			close(w.done)
			return
		default:
			job, err := w.queue.Pop(5 * time.Second)
			if err != nil || job == nil || job.Type != "production" {
				continue
			}
			w.processJob(ctx, job)
		}
	}
}

func (w *ProductionWorker) processJob(ctx context.Context, job *JobPayload) {
	log.Printf("[Production] Processing job %s", job.JobID)

	w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'processing', started_at = NOW() WHERE id = $1", job.JobID)
	w.queue.PublishProgress(job.JobID, job.ChannelID, 0, "processing", "Starting production")

	// Get job details
	var method, channelID, videoSource, sfxFile, introFile, mp3File, outputFilename string
	var numSongs int
	w.DB.QueryRow(ctx,
		`SELECT production_method, channel_id, video_source, sfx_file, intro_file, mp3_file, num_songs, output_filename
		 FROM production_jobs WHERE id = $1`, job.JobID,
	).Scan(&method, &channelID, &videoSource, &sfxFile, &introFile, &mp3File, &numSongs, &outputFilename)

	storageDir := w.Cfg.StorageDir
	tmpDir := filepath.Join(storageDir, "tmp")
	os.MkdirAll(tmpDir, 0755)

	switch method {
	case "ready_video":
		w.readyVideo(ctx, job.JobID, channelID, videoSource, sfxFile, introFile, mp3File, numSongs, tmpDir)
	case "raw_video_auto_seamless":
		w.autoSeamless(ctx, job.JobID, channelID, videoSource, tmpDir)
	case "merge_video":
		w.mergeVideo(ctx, job.JobID, channelID, tmpDir)
	default:
		w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'failed', error_message = 'Unknown method' WHERE id = $1", job.JobID)
	}
}

func (w *ProductionWorker) readyVideo(ctx context.Context, jobID, channelID, videoSource, sfxFile, introFile, mp3File string, numSongs int, tmpDir string) {
	w.DB.Exec(ctx, "UPDATE production_jobs SET audio_status = 'processing' WHERE id = $1", jobID)
	w.queue.PublishProgress(jobID, channelID, 10, "processing", "Building audio track")

	// Step 1: Audio engine - pick MP3s
	mp3s := []string{}
	if numSongs > 0 {
		rows, _ := w.DB.Query(ctx,
			"SELECT file_path FROM media_items WHERE channel_id = $1 AND asset_type = 'mp3' AND status = 'active' ORDER BY RANDOM() LIMIT $2",
			channelID, numSongs)
		defer rows.Close()
		for rows.Next() {
			var fp string
			rows.Scan(&fp)
			mp3s = append(mp3s, fp)
		}
	}
	if mp3File != "" {
		mp3s = append(mp3s, mp3File)
	}

	sfxs := []string{}
	if sfxFile != "" {
		sfxs = append(sfxs, sfxFile)
	} else {
		rows, _ := w.DB.Query(ctx,
			"SELECT file_path FROM media_items WHERE channel_id = $1 AND asset_type = 'sfx' AND status = 'active' ORDER BY RANDOM() LIMIT 1", channelID)
		if rows.Next() {
			var fp string
			rows.Scan(&fp)
			sfxs = append(sfxs, fp)
		}
		rows.Close()
	}

	audioPath := filepath.Join(tmpDir, fmt.Sprintf("%s_audio.wav", jobID))
	audioArgs := []string{"-y"}

	for _, mp3 := range mp3s {
		audioArgs = append(audioArgs, "-i", mp3)
	}
	if len(mp3s) > 0 {
		// Concat all audio files
		filterParts := []string{}
		for i := range mp3s {
			filterParts = append(filterParts, fmt.Sprintf("[%d:a]aresample=44100[a%d]", i, i))
		}
		concatInputs := ""
		for i := range mp3s {
			if i > 0 {
				concatInputs += fmt.Sprintf("[a%d]", i)
			} else {
				concatInputs = "[a0]"
			}
		}
		filterParts = append(filterParts, fmt.Sprintf("%sconcat=n=%d:v=0:a=1[out]", concatInputs, len(mp3s)))
		audioArgs = append(audioArgs, "-filter_complex", strings.Join(filterParts, ";"), "-map", "[out]")
	} else {
		// No MP3 - generate silent audio
		audioArgs = append(audioArgs, "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono")
	}
	audioArgs = append(audioArgs, "-acodec", "pcm_s16le", "-ar", "44100", audioPath)

	if out, err := exec.Command("ffmpeg", audioArgs...).CombinedOutput(); err != nil {
		w.DB.Exec(ctx, "UPDATE production_jobs SET audio_status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
		w.queue.PublishProgress(jobID, channelID, 0, "failed", fmt.Sprintf("Audio failed: %s", err))
		return
	}
	w.DB.Exec(ctx, "UPDATE production_jobs SET audio_path = $1, audio_status = 'done', progress = 30 WHERE id = $2", audioPath, jobID)
	w.queue.PublishProgress(jobID, channelID, 30, "processing", "Audio done, building video loop")

	// Step 2: Video loop
	w.DB.Exec(ctx, "UPDATE production_jobs SET video_status = 'processing' WHERE id = $1", jobID)
	videoPath := filepath.Join(tmpDir, fmt.Sprintf("%s_video.mp4", jobID))
	videoArgs := []string{"-y", "-stream_loop", "-1", "-i", videoSource, "-i", audioPath}

	if introFile != "" {
		videoArgs = append(videoArgs, "-i", introFile)
		videoArgs = append(videoArgs, "-filter_complex", "[0:v]loop=-1:size=1[v];[v][2:v]concat=n=2:v=1:a=0[outv]", "-map", "[outv]")
	} else {
		videoArgs = append(videoArgs, "-filter_complex", "loop=-1:size=1")
	}

	videoArgs = append(videoArgs, "-map", "1:a", "-c:v", "libx264", "-preset", "fast", "-c:a", "aac", "-shortest", videoPath)

	if out, err := exec.Command("ffmpeg", videoArgs...).CombinedOutput(); err != nil {
		w.DB.Exec(ctx, "UPDATE production_jobs SET video_status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
		return
	}
	w.DB.Exec(ctx, "UPDATE production_jobs SET video_path = $1, video_status = 'done', progress = 60 WHERE id = $2", videoPath, jobID)
	w.queue.PublishProgress(jobID, channelID, 60, "processing", "Video done, rendering final")

	// Step 3: Final render
	finalPath := filepath.Join(tmpDir, fmt.Sprintf("%s_final.mp4", jobID))

	if out, err := exec.Command("ffmpeg", "-y", "-i", videoPath, "-i", audioPath, "-c:v", "copy", "-c:a", "aac", "-shortest", finalPath).CombinedOutput(); err != nil {
		w.DB.Exec(ctx, "UPDATE production_jobs SET final_status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
		return
	}

	// Move to upload_ready
	uploadDir := filepath.Join(w.Cfg.StorageDir, "assets", "upload_ready", channelID)
	os.MkdirAll(uploadDir, 0755)
	outputFilename := fmt.Sprintf("production_%s.mp4", jobID)
	finalDest := filepath.Join(uploadDir, outputFilename)
	os.Rename(finalPath, finalDest)

	w.DB.Exec(ctx,
		"UPDATE production_jobs SET final_path = $1, final_status = 'done', status = 'done', progress = 100, output_filename = $2 WHERE id = $3",
		finalDest, outputFilename, jobID)

	// Create media item for upload_ready
	var mediaID string
	w.DB.QueryRow(ctx, "INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, file_size, status, created_at, updated_at) VALUES ($1,$2,$3,$4,'upload_ready',0,'active',NOW(),NOW()) RETURNING id",
		channelID, outputFilename, outputFilename, finalDest).Scan(&mediaID)

	w.queue.PublishProgress(jobID, channelID, 100, "completed", "Production complete, ready for upload")
	log.Printf("[Production] Job %s completed", jobID)
}

func (w *ProductionWorker) autoSeamless(ctx context.Context, jobID, channelID, videoSource, tmpDir string) {
	// Seamless loop preprocessing
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("%s_seamless.mp4", jobID))

	// Create entry in auto_seamless_progresses
	w.DB.Exec(ctx,
		"INSERT INTO auto_seamless_progresses (id, channel_id, raw_filename, input_path, output_path, progress, status, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,0,'processing',NOW(),NOW())",
		jobID, channelID, filepath.Base(videoSource), videoSource, outputPath)

	out, err := exec.Command("ffmpeg", "-y", "-i", videoSource,
		"-vf", "fade=t=out:st=5:d=1:alpha=1,format=rgba",
		"-c:v", "libx264", "-preset", "fast", "-pix_fmt", "yuv420p",
		outputPath).CombinedOutput()
	if err != nil {
		w.DB.Exec(ctx, "UPDATE auto_seamless_progresses SET status = 'failed', message = $1 WHERE id = $2", string(out), jobID)
		w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
		return
	}

	w.DB.Exec(ctx, "UPDATE auto_seamless_progresses SET progress = 100, status = 'done' WHERE id = $1", jobID)

	// Save as media item
	w.DB.Exec(ctx,
		"INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, status, created_at, updated_at) VALUES ($1,$2,$3,$4,'video','active',NOW(),NOW())",
		channelID, filepath.Base(outputPath), "seamless_"+filepath.Base(videoSource), outputPath)
	w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'done', progress = 100 WHERE id = $1", jobID)
}

func (w *ProductionWorker) mergeVideo(ctx context.Context, jobID, channelID, tmpDir string) {
	// Get up to 5 raw videos
	rows, _ := w.DB.Query(ctx,
		"SELECT file_path FROM media_items WHERE channel_id = $1 AND asset_type = 'video-raw' AND status = 'active' ORDER BY RANDOM() LIMIT 5", channelID)
	defer rows.Close()

	inputs := []string{}
	for rows.Next() {
		var fp string
		rows.Scan(&fp)
		inputs = append(inputs, fp)
	}
	if len(inputs) < 1 {
		w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'failed', error_message = 'No video-raw assets' WHERE id = $1", jobID)
		return
	}

	outputPath := filepath.Join(tmpDir, fmt.Sprintf("%s_merged.mp4", jobID))
	args := []string{"-y"}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}
	filters := []string{}
	for i := range inputs {
		filters = append(filters, fmt.Sprintf("[%d:v]scale=1920:1080,setsar=1[v%d]", i, i))
	}
	concatParts := []string{}
	for i := range inputs {
		concatParts = append(concatParts, fmt.Sprintf("[v%d]", i))
	}
	filters = append(filters, fmt.Sprintf("%sconcat=n=%d:v=1:a=0", strings.Join(concatParts, ""), len(inputs)))
	args = append(args, "-filter_complex", strings.Join(filters, ";"))
	args = append(args, "-c:v", "libx264", "-preset", "fast", outputPath)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
		return
	}

	w.DB.Exec(ctx,
		"INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, status, created_at, updated_at) VALUES ($1,$2,$3,$4,'video','active',NOW(),NOW())",
		channelID, filepath.Base(outputPath), "merged_"+filepath.Base(outputPath), outputPath)
	w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'done', progress = 100, final_path = $1 WHERE id = $2", outputPath, jobID)
}

// UploadWorker handles YouTube uploads
type UploadWorker struct {
	DB    *pgxpool.Pool
	Cfg   *config.Config
	queue *JobQueue
}

func NewUploadWorker(db *pgxpool.Pool, cfg *config.Config, queue *JobQueue) *UploadWorker {
	return &UploadWorker{DB: db, Cfg: cfg, queue: queue}
}

func (w *UploadWorker) Run(ctx context.Context) {
	log.Println("[Worker:upload] Started (Redis queue mode)")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			job, err := w.queue.Pop(5 * time.Second)
			if err != nil || job == nil || job.Type != "upload" {
				continue
			}
			w.processUpload(ctx, job)
		}
	}
}

func (w *UploadWorker) processUpload(ctx context.Context, job *JobPayload) {
	log.Printf("[Upload] Processing item %s", job.JobID)

	w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'processing' WHERE id = $1", job.JobID)
	w.queue.PublishProgress(job.JobID, job.ChannelID, 0, "processing", "Starting upload")

	var sourcePath, title, description, tags, visibility, channelID, accessToken, refreshToken string
	var scheduledAt *time.Time

	w.DB.QueryRow(ctx,
		`SELECT ubi.source_path, ubi.title, ubi.description, ubi.tags, ubi.visibility, ubi.scheduled_at,
		        ubi.channel_id, ch.access_token, ch.refresh_token
		 FROM upload_batch_items ubi
		 JOIN channels ch ON ubi.channel_id = ch.id
		 WHERE ubi.id = $1`, job.JobID,
	).Scan(&sourcePath, &title, &description, &tags, &visibility, &scheduledAt, &channelID, &accessToken, &refreshToken)

	if sourcePath == "" || accessToken == "" {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'Missing file or token' WHERE id = $1", job.JobID)
		w.queue.PublishProgress(job.JobID, channelID, 0, "failed", "Missing file or token")
		return
	}

	// Upload via YouTube API
	youtubeURL := "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status"

	metadata := map[string]interface{}{
		"snippet": map[string]interface{}{
			"title":       title,
			"description": description,
			"tags":        strings.Split(tags, ","),
			"categoryId":  "22",
		},
		"status": map[string]interface{}{
			"privacyStatus": visibility,
			"selfDeclaredMadeForKids": false,
		},
	}
	if scheduledAt != nil {
		metadata["status"].(map[string]interface{})["publishAt"] = scheduledAt.UTC().Format(time.RFC3339)
	}

	metaJSON, _ := json.Marshal(metadata)

	// Get file size
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'File not found' WHERE id = $1", job.JobID)
		return
	}
	fileSize := fileInfo.Size()

	// Init upload
	req, _ := http.NewRequest("POST", youtubeURL, bytes.NewReader(metaJSON))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", fileSize))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = $1 WHERE id = $2", err.Error(), job.JobID)
		return
	}
	defer resp.Body.Close()

	sessionURL := resp.Header.Get("Location")
	if sessionURL == "" {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'No upload URL' WHERE id = $1", job.JobID)
		return
	}

	// Upload file
	file, _ := os.Open(sourcePath)
	defer file.Close()

	putReq, _ := http.NewRequest("PUT", sessionURL, file)
	putReq.Header.Set("Content-Length", fmt.Sprintf("%d", fileSize))

	uploadResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = $1 WHERE id = $2", err.Error(), job.JobID)
		return
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode == 200 || uploadResp.StatusCode == 201 {
		var result struct {
			ID string `json:"id"`
		}
		json.NewDecoder(uploadResp.Body).Decode(&result)

		w.DB.Exec(ctx,
			"UPDATE upload_batch_items SET status = 'done', youtube_video_id = $1, progress = 100, finished_at = NOW() WHERE id = $2",
			result.ID, job.JobID)
		w.DB.Exec(ctx, "UPDATE upload_batches SET done_items = done_items + 1 WHERE id = (SELECT upload_batch_id FROM upload_batch_items WHERE id = $1)", job.JobID)

		w.queue.PublishProgress(job.JobID, channelID, 100, "completed", fmt.Sprintf("Uploaded: %s", result.ID))
		log.Printf("[Upload] Item %s -> video %s", job.JobID, result.ID)
	} else {
		body, _ := json.Marshal(uploadResp.Body)
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = $1 WHERE id = $2", string(body), job.JobID)
		w.queue.PublishProgress(job.JobID, channelID, 0, "failed", "Upload failed")
	}
}

// LivestreamWorker handles 24/7 livestream
type LivestreamWorker struct {
	DB      *pgxpool.Pool
	Cfg     *config.Config
	queue   *JobQueue
	processes map[string]*exec.Cmd
}

func NewLivestreamWorker(db *pgxpool.Pool, cfg *config.Config, queue *JobQueue) *LivestreamWorker {
	return &LivestreamWorker{
		DB:        db,
		Cfg:       cfg,
		queue:     queue,
		processes: make(map[string]*exec.Cmd),
	}
}

func (w *LivestreamWorker) Run(ctx context.Context) {
	log.Println("[Worker:livestream] Started")
	// Check for scheduled/running jobs every 10s
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.killAll()
			return
		case <-ticker.C:
			w.checkJobs(ctx)
		}
	}
}

func (w *LivestreamWorker) checkJobs(ctx context.Context) {
	// Check for scheduled jobs that need to start
	rows, _ := w.DB.Query(ctx,
		`SELECT id, channel_id, title, description, tags, video_source, use_mp3, use_sfx,
		        stream_key, quality, duration_hours, stop_requested
		 FROM live_jobs WHERE status IN ('scheduled','running') AND start_at_utc <= NOW()`)
	defer rows.Close()

	for rows.Next() {
		var id, channelID, title, desc, tags, videoSource, streamKey, quality string
		var useMP3, useSFX, stopRequested bool
		var durationHours int
		rows.Scan(&id, &channelID, &title, &desc, &tags, &videoSource, &useMP3, &useSFX,
			&streamKey, &quality, &durationHours, &stopRequested)

		// Check if process is still tracked
		cmd, exists := w.processes[id]
		if stopRequested {
			if exists && cmd != nil && cmd.Process != nil {
				cmd.Process.Kill()
			}
			w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'stopped', finished_at = NOW() WHERE id = $1", id)
			w.queue.PublishProgress(id, channelID, 0, "stopped", "Livestream stopped")
			delete(w.processes, id)
			continue
		}

		if !exists || cmd == nil {
			// Start new livestream
			if videoSource == "" {
				continue
			}

			rtmpURL := "rtmp://a.rtmp.youtube.com/live2"
			ffmpegArgs := []string{
				"-y", "-stream_loop", "-1", "-re", "-i", videoSource,
				"-c:v", "libx264", "-preset", "veryfast",
				"-b:v", "4500k", "-maxrate", "4500k", "-bufsize", "9000k",
				"-pix_fmt", "yuv420p", "-g", "60",
				"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
				"-f", "flv",
				fmt.Sprintf("%s/%s", rtmpURL, streamKey),
			}

			cmd := exec.Command("ffmpeg", ffmpegArgs...)
			if err := cmd.Start(); err != nil {
				log.Printf("[Livestream] Start error %s: %v", id, err)
				continue
			}

			w.processes[id] = cmd
			w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'running', started_at = NOW() WHERE id = $1", id)
			log.Printf("[Livestream] Started %s", id)
			w.queue.PublishProgress(id, channelID, 50, "running", "Livestream running")
		}
	}

	// Cleanup finished processes
	for id, cmd := range w.processes {
		if cmd != nil && cmd.Process != nil {
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'finished', finished_at = NOW() WHERE id = $1", id)
				w.queue.PublishProgress(id, "", 100, "finished", "Livestream ended")
				delete(w.processes, id)
			}
		}
	}
}

func (w *LivestreamWorker) killAll() {
	for id, cmd := range w.processes {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
		delete(w.processes, id)
	}
}

// ShortsWorker handles short video generation
type ShortsWorker struct {
	DB    *pgxpool.Pool
	Cfg   *config.Config
	queue *JobQueue
}

func NewShortsWorker(db *pgxpool.Pool, cfg *config.Config, queue *JobQueue) *ShortsWorker {
	return &ShortsWorker{DB: db, Cfg: cfg, queue: queue}
}

func (w *ShortsWorker) Run(ctx context.Context) {
	log.Println("[Worker:shorts] Started")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.autoCreate(ctx)
			w.processJobs(ctx)
		}
	}
}

func (w *ShortsWorker) autoCreate(ctx context.Context) {
	// Auto-create shorts jobs from completed uploads
	rows, _ := w.DB.Query(ctx,
		`SELECT ubi.id, ubi.youtube_video_id, ubi.title, ubi.channel_id
		 FROM upload_batch_items ubi
		 WHERE ubi.status = 'done' AND ubi.youtube_video_id IS NOT NULL
		 AND NOT EXISTS (SELECT 1 FROM shorts_jobs sj WHERE sj.long_upload_id = ubi.id)
		 ORDER BY ubi.finished_at DESC LIMIT 5`)
	defer rows.Close()

	for rows.Next() {
		var uploadID, youtubeID, title, channelID string
		rows.Scan(&uploadID, &youtubeID, &title, &channelID)

		// Create shorts job with 3 items
		jobID := fmt.Sprintf("%s_shorts", uploadID)
		w.DB.Exec(ctx,
			`INSERT INTO shorts_jobs (id, channel_id, long_upload_id, long_youtube_url, long_title, short_count, short_duration, segment_mode, status, created_at)
			 VALUES ($1,$2,$3,$4,$5,3,60,'auto','created',NOW())
			 ON CONFLICT DO NOTHING`,
			jobID, channelID, uploadID, fmt.Sprintf("https://youtube.com/watch?v=%s", youtubeID), title)

		// Create 3 items with different durations
		durations := []int{60, 45, 30}
		for i, dur := range durations {
			itemID := fmt.Sprintf("%s_item_%d", jobID, i+1)
			w.DB.Exec(ctx,
				`INSERT INTO shorts_items (id, job_id, short_number, start_second, end_second, title, upload_time, status, created_at)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',NOW())
				 ON CONFLICT DO NOTHING`,
				itemID, jobID, i+1, float64(i*60), float64(i*60+dur),
				fmt.Sprintf("%s #%d", title, i+1),
				fmt.Sprintf("%02d:00", 9+i+1))
		}
	}
}

func (w *ShortsWorker) processJobs(ctx context.Context) {
	// Process pending shorts jobs
	rows, _ := w.DB.Query(ctx,
		`SELECT id, channel_id, long_youtube_url, long_title, short_count, short_duration
		 FROM shorts_jobs WHERE status = 'created' ORDER BY created_at ASC LIMIT 1`)
	defer rows.Close()

	for rows.Next() {
		var jobID, channelID, youtubeURL, title string
		var shortCount, shortDuration int
		rows.Scan(&jobID, &channelID, &youtubeURL, &title, &shortCount, &shortDuration)

		w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'generating' WHERE id = $1", jobID)

		// Download video via yt-dlp
		tmpDir := filepath.Join(w.Cfg.StorageDir, "tmp", jobID)
		os.MkdirAll(tmpDir, 0755)
		videoPath := filepath.Join(tmpDir, "source.mp4")

		log.Printf("[Shorts] Downloading %s", youtubeURL)
		if out, err := exec.Command("yt-dlp", "-f", "best[height<=1080]", "-o", videoPath, "--no-playlist", youtubeURL).CombinedOutput(); err != nil {
			w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
			return
		}

		// Generate each short
		items, _ := w.DB.Query(ctx,
			"SELECT id, short_number, start_second, end_second, title FROM shorts_items WHERE job_id = $1 ORDER BY short_number", jobID)
		defer items.Close()

		for items.Next() {
			var itemID, title string
			var shortNumber int
			var startSecond, endSecond float64
			items.Scan(&itemID, &shortNumber, &startSecond, &endSecond, &title)

			duration := endSecond - startSecond
			if duration <= 0 {
				duration = float64(shortDuration)
			}

			outputPath := filepath.Join(tmpDir, fmt.Sprintf("short_%d.mp4", shortNumber))
			out, err := exec.Command("ffmpeg", "-y",
				"-ss", fmt.Sprintf("%.2f", startSecond),
				"-t", fmt.Sprintf("%.2f", duration),
				"-i", videoPath,
				"-vf", "crop=ih*(9/16):ih,scale=1080:1920",
				"-c:v", "libx264", "-preset", "fast",
				"-c:a", "aac",
				outputPath).CombinedOutput()
			if err != nil {
				w.DB.Exec(ctx, "UPDATE shorts_items SET status = 'failed', error_message = $1 WHERE id = $2", string(out), itemID)
				continue
			}

			w.DB.Exec(ctx, "UPDATE shorts_items SET status = 'generated', video_path = $1 WHERE id = $2", outputPath, itemID)
		}

		w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'ready' WHERE id = $1", jobID)
		log.Printf("[Shorts] Job %s ready", jobID)
	}
}