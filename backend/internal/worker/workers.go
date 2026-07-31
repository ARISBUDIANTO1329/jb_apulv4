package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
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
	DB    *pgxpool.Pool
	Cfg   *config.Config
	queue *JobQueue
	done  chan struct{}
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
	w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'processing' WHERE id = $1", job.JobID)
	w.queue.PublishProgress(job.JobID, job.ChannelID, 0, "processing", "Starting production")

	var method, channelID, videoSource, sfxFile, introFile, mp3File, outputFilename string
	var numSongs int
	var noMP3, noSFX bool
	var durationMode string
	var customDuration int
	var mp3Mode string
	var tailLength int
	var slowmoPercent int
	w.DB.QueryRow(ctx,
		`SELECT production_method, channel_id, video_source, sfx_file, intro_file, mp3_file,
		        num_songs, no_mp3, no_sfx, duration_mode, custom_duration, mp3_mode,
		        tail_length, slowmo_percent, output_filename
		 FROM production_jobs WHERE id = $1`, job.JobID,
	).Scan(&method, &channelID, &videoSource, &sfxFile, &introFile, &mp3File,
		&numSongs, &noMP3, &noSFX, &durationMode, &customDuration, &mp3Mode,
		&tailLength, &slowmoPercent, &outputFilename)

	storageDir := w.Cfg.StorageDir
	tmpDir := filepath.Join(storageDir, "tmp")
	os.MkdirAll(tmpDir, 0755)

	switch method {
	case "raw_video_auto_seamless":
		w.autoSeamless(ctx, job.JobID, channelID, videoSource, tmpDir, tailLength, slowmoPercent, outputFilename)
	case "merge_video":
		w.mergeVideo(ctx, job.JobID, channelID, tmpDir)
	default:
		w.readyVideo(ctx, job.JobID, channelID, videoSource, sfxFile, introFile, mp3File,
			numSongs, noMP3, noSFX, durationMode, customDuration, mp3Mode, tmpDir, outputFilename)
	}
}

// ============================================================
// MODE 1: READY VIDEO (Final Production)
// Pipeline: audio_engine → video_loop_engine → final_renderer
// Ported from v3 audio_engine.py + video_loop_engine.py + final_renderer.py
// ============================================================

func (w *ProductionWorker) readyVideo(ctx context.Context, jobID, channelID, videoSource, sfxFile, introFile, mp3File string,
	numSongs int, noMP3, noSFX bool, durationMode string, customDuration int, mp3Mode string,
	tmpDir, outputFilename string) {

	assetsDir := filepath.Join(w.Cfg.StorageDir, "assets")

	// ── Step 1: Audio Engine ──
	w.DB.Exec(ctx, "UPDATE production_jobs SET audio_status = 'processing' WHERE id = $1", jobID)
	w.queue.PublishProgress(jobID, channelID, 10, "processing", "Audio: memproses")

	audioPath := filepath.Join(tmpDir, fmt.Sprintf("audio_%s.wav", jobID))
	useMP3 := !noMP3
	useSFX := !noSFX

	// Pick MP3 files
	var mp3Files []string
	if useMP3 {
		if mp3File != "" {
			// Specific MP3 file
			fullPath := filepath.Join(assetsDir, "mp3", channelID, mp3File)
			if _, err := os.Stat(fullPath); err == nil {
				mp3Files = append(mp3Files, fullPath)
			}
		} else {
			// Random MP3s from channel
			mp3Dir := filepath.Join(assetsDir, "mp3", channelID)
			if entries, err := os.ReadDir(mp3Dir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".mp3") {
						mp3Files = append(mp3Files, filepath.Join(mp3Dir, e.Name()))
					}
				}
			}
			if mp3Mode == "shuffle" && numSongs > 0 && len(mp3Files) > numSongs {
				rand.Shuffle(len(mp3Files), func(i, j int) { mp3Files[i], mp3Files[j] = mp3Files[j], mp3Files[i] })
				mp3Files = mp3Files[:numSongs]
			}
		}
	}

	// Pick SFX file
	var sfxPath string
	if useSFX {
		if sfxFile != "" {
			fullPath := filepath.Join(assetsDir, "sfx", channelID, sfxFile)
			if _, err := os.Stat(fullPath); err == nil {
				sfxPath = fullPath
			}
		} else {
			sfxDir := filepath.Join(assetsDir, "sfx", channelID)
			if entries, err := os.ReadDir(sfxDir); err == nil {
				var sfxFiles []string
				for _, e := range entries {
					if !e.IsDir() {
						name := strings.ToLower(e.Name())
						if strings.HasSuffix(name, ".mp3") || strings.HasSuffix(name, ".wav") {
							sfxFiles = append(sfxFiles, filepath.Join(sfxDir, e.Name()))
						}
					}
				}
				if len(sfxFiles) > 0 {
					sfxPath = sfxFiles[rand.Intn(len(sfxFiles))]
				}
			}
		}
	}

	// Calculate target duration
	var finalDuration int
	if durationMode == "manual" && customDuration > 0 {
		finalDuration = customDuration
	} else if len(mp3Files) > 0 {
		totalDur := 0.0
		for _, f := range mp3Files {
			totalDur += w.getDuration(f)
		}
		finalDuration = int(totalDur)
	} else if sfxPath != "" {
		finalDuration = int(w.getDuration(sfxPath))
	}
	if finalDuration <= 0 {
		finalDuration = 3600 // Default 1 hour
	}

	// Merge MP3s if multiple
	var mergedMP3 string
	if len(mp3Files) > 1 {
		mergedMP3 = filepath.Join(tmpDir, fmt.Sprintf("_mp3_merge_%s.wav", jobID))
		listFile := filepath.Join(tmpDir, fmt.Sprintf("_mp3_list_%s.txt", jobID))
		var listContent strings.Builder
		for _, f := range mp3Files {
			listContent.WriteString(fmt.Sprintf("file '%s'\n", f))
		}
		os.WriteFile(listFile, []byte(listContent.String()), 0644)
		if out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c:a", "pcm_s16le", mergedMP3).CombinedOutput(); err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Audio merge failed: %s", string(out)))
			return
		}
	} else if len(mp3Files) == 1 {
		mergedMP3 = mp3Files[0]
	}

	// Build final audio: MP3 + SFX mix
	if mergedMP3 != "" && sfxPath != "" && finalDuration > 3 {
		// Mix SFX (opening) + MP3
		delayMs := 3000
		out, err := exec.Command("ffmpeg", "-y",
			"-stream_loop", "-1", "-i", sfxPath,
			"-i", mergedMP3,
			"-filter_complex", fmt.Sprintf("[1:a]adelay=%d|%d[mp3d];[0:a][mp3d]amix=inputs=2", delayMs, delayMs),
			"-t", fmt.Sprintf("%d", finalDuration),
			"-c:a", "pcm_s16le", audioPath).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Audio mix failed: %s", string(out)))
			return
		}
	} else if mergedMP3 != "" {
		loopFlag := ""
		if len(mp3Files) <= 1 {
			loopFlag = "-stream_loop"
			loopVal := "-1"
			out, err := exec.Command("ffmpeg", "-y", loopFlag, loopVal, "-i", mergedMP3,
				"-t", fmt.Sprintf("%d", finalDuration), "-c:a", "pcm_s16le", audioPath).CombinedOutput()
			if err != nil {
				w.failJob(ctx, jobID, channelID, fmt.Sprintf("Audio loop failed: %s", string(out)))
				return
			}
			
		} else {
			out, err := exec.Command("ffmpeg", "-y", "-i", mergedMP3,
				"-t", fmt.Sprintf("%d", finalDuration), "-c:a", "pcm_s16le", audioPath).CombinedOutput()
			if err != nil {
				w.failJob(ctx, jobID, channelID, fmt.Sprintf("Audio failed: %s", string(out)))
				return
			}
			
		}
	} else if sfxPath != "" {
		out, err := exec.Command("ffmpeg", "-y", "-stream_loop", "-1", "-i", sfxPath,
			"-t", fmt.Sprintf("%d", finalDuration), "-c:a", "pcm_s16le", audioPath).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("SFX audio failed: %s", string(out)))
			return
		}
		
	} else {
		// Silent audio
		out, err := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo",
			"-t", fmt.Sprintf("%d", finalDuration), "-c:a", "pcm_s16le", audioPath).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Silent audio failed: %s", string(out)))
			return
		}
		
	}

	w.DB.Exec(ctx, "UPDATE production_jobs SET audio_path = $1, audio_status = 'done', progress = 30 WHERE id = $2", audioPath, jobID)
	w.queue.PublishProgress(jobID, channelID, 30, "processing", "Audio done, building video loop")

	// ── Step 2: Video Loop Engine ──
	w.DB.Exec(ctx, "UPDATE production_jobs SET video_status = 'processing' WHERE id = $1", jobID)
	w.queue.PublishProgress(jobID, channelID, 40, "processing", "Video: membuat segmen")

	mainVideo := filepath.Join(assetsDir, "video", channelID, videoSource)
	if _, err := os.Stat(mainVideo); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Video not found: %s", videoSource))
		return
	}

	audioDur := w.getDuration(audioPath)
	mainVideoDur := w.getDuration(mainVideo)

	// Handle intro
	var introDur float64
	var introPath string
	if introFile != "" {
		introPath = filepath.Join(assetsDir, "intro", channelID, introFile)
		if _, err := os.Stat(introPath); err == nil {
			introDur = w.getDuration(introPath)
		} else {
			introPath = ""
		}
	}

	mainTargetDur := audioDur - introDur
	if introPath == "" {
		mainTargetDur = audioDur
	}
	if mainTargetDur <= 0 {
		mainTargetDur = audioDur
		introPath = ""
		introDur = 0
	}

	// Build video segments
	var segments []string

	// Intro segment
	if introPath != "" {
		segIntro := filepath.Join(tmpDir, fmt.Sprintf("seg_intro_%s.mp4", jobID))
		if out, err := exec.Command("ffmpeg", "-y", "-i", introPath, "-t", fmt.Sprintf("%.2f", introDur), "-c", "copy", segIntro).CombinedOutput(); err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Intro segment failed: %s", string(out)))
			return
		}
		
		segments = append(segments, segIntro)
	}

	// Main video segment (skip intro portion)
	segContinue := filepath.Join(tmpDir, fmt.Sprintf("seg_continue_%s.mp4", jobID))
	continueDur := math.Min(math.Max(0, mainVideoDur-introDur), mainTargetDur)
	if out, err := exec.Command("ffmpeg", "-y",
		"-ss", fmt.Sprintf("%.2f", introDur), "-i", mainVideo,
		"-t", fmt.Sprintf("%.2f", continueDur), "-c", "copy", segContinue).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Video segment failed: %s", string(out)))
		return
	}
	
	segments = append(segments, segContinue)

	// Loop remaining
	remaining := mainTargetDur - continueDur
	if remaining > 1 {
		segLoop := filepath.Join(tmpDir, fmt.Sprintf("seg_loop_%s.mp4", jobID))
		if out, err := exec.Command("ffmpeg", "-y",
			"-stream_loop", "-1", "-i", mainVideo,
			"-t", fmt.Sprintf("%.2f", remaining), "-c", "copy", segLoop).CombinedOutput(); err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Video loop failed: %s", string(out)))
			return
		}
		
		segments = append(segments, segLoop)
	}

	// Concat segments
	videoPath := filepath.Join(tmpDir, fmt.Sprintf("video_%s.mp4", jobID))
	if len(segments) == 1 {
		os.Link(segments[0], videoPath)
	} else {
		listFile := filepath.Join(tmpDir, fmt.Sprintf("_video_list_%s.txt", jobID))
		var listContent strings.Builder
		for _, seg := range segments {
			listContent.WriteString(fmt.Sprintf("file '%s'\n", seg))
		}
		os.WriteFile(listFile, []byte(listContent.String()), 0644)
		if out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", videoPath).CombinedOutput(); err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Video concat failed: %s", string(out)))
			return
		}
		
	}

	w.DB.Exec(ctx, "UPDATE production_jobs SET video_path = $1, video_status = 'done', progress = 60 WHERE id = $2", videoPath, jobID)
	w.queue.PublishProgress(jobID, channelID, 60, "processing", "Video done, rendering final")

	// ── Step 3: Final Renderer ──
	w.DB.Exec(ctx, "UPDATE production_jobs SET final_status = 'processing' WHERE id = $1", jobID)
	w.queue.PublishProgress(jobID, channelID, 70, "processing", "Final: menggabung audio+video")

	finalTmp := filepath.Join(tmpDir, fmt.Sprintf("final_%s.mp4", jobID))
	if out, err := exec.Command("ffmpeg", "-y",
		"-i", videoPath, "-i", audioPath,
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
		"-shortest", finalTmp).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Final render failed: %s", string(out)))
		return
	}
	

	// Generate output filename if empty
	if outputFilename == "" {
		outputFilename = fmt.Sprintf("production_%s.mp4", jobID)
	}

	// Move to upload_ready
	uploadDir := filepath.Join(w.Cfg.StorageDir, "assets", "upload_ready", channelID)
	os.MkdirAll(uploadDir, 0755)
	finalDest := filepath.Join(uploadDir, outputFilename)
	os.Rename(finalTmp, finalDest)

	// Get file size
	sizeBytes := int64(0)
	if info, err := os.Stat(finalDest); err == nil {
		sizeBytes = info.Size()
	}

	// Create media item
	var mediaID string
	w.DB.QueryRow(ctx,
		`INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, file_size, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'upload_ready',$5,'active',NOW(),NOW()) RETURNING id`,
		channelID, outputFilename, outputFilename, finalDest, sizeBytes).Scan(&mediaID)

	w.DB.Exec(ctx,
		"UPDATE production_jobs SET final_path = $1, final_status = 'done', status = 'done', progress = 100, output_filename = $2 WHERE id = $3",
		finalDest, outputFilename, jobID)

	w.queue.PublishProgress(jobID, channelID, 100, "completed", "Production complete")
	log.Printf("[Production] Job %s completed: %s (%.1fMB)", jobID, outputFilename, float64(sizeBytes)/1024/1024)

	// Cleanup temp files
	w.cleanupTmp(tmpDir, jobID)
}

// ============================================================
// MODE 2: AUTO SEAMLESS
// Ported from v3 master_preprocess.py
// Creates seamless loop with fade-to-transparent tail overlay
// ============================================================

func (w *ProductionWorker) autoSeamless(ctx context.Context, jobID, channelID, videoSource, tmpDir string, tailLength, slowmoPercent int, outputFilename string) {
	assetsDir := filepath.Join(w.Cfg.StorageDir, "assets")
	rawPath := filepath.Join(assetsDir, "video-raw", channelID, videoSource)

	if _, err := os.Stat(rawPath); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Raw video not found: %s", videoSource))
		return
	}

	// Create progress record
	w.DB.Exec(ctx,
		`INSERT INTO auto_seamless_progresses (id, channel_id, raw_filename, input_path, output_path, progress, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,0,'processing',NOW(),NOW())
		 ON CONFLICT (id) DO UPDATE SET status = 'processing', progress = 0`,
		jobID, channelID, videoSource, rawPath, "")

	w.queue.PublishProgress(jobID, channelID, 5, "processing", "Seamless: membaca durasi")

	duration := w.getDuration(rawPath)
	if duration <= float64(tailLength) {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Video too short for tail_length=%d", tailLength))
		return
	}

	// Calculate fade parameters
	tailLen := float64(tailLength)
	fadeStart := tailLen * 0.10
	fadeEnd := tailLen * 0.90
	fadeDuration := fadeEnd - fadeStart
	splitPoint := duration - tailLen

	seamlessTmp := filepath.Join(tmpDir, fmt.Sprintf("%s_seamless.mp4", jobID))
	bodyPath := filepath.Join(tmpDir, fmt.Sprintf("body_%s.mp4", jobID))
	tailPath := filepath.Join(tmpDir, fmt.Sprintf("tail_%s.mp4", jobID))
	tailAlphaPath := filepath.Join(tmpDir, fmt.Sprintf("tail_alpha_%s.mov", jobID))

	// Step 1: Body (start to split_point)
	w.queue.PublishProgress(jobID, channelID, 15, "processing", "Seamless: membuat body")
	if out, err := exec.Command("ffmpeg", "-y",
		"-i", rawPath,
		"-t", fmt.Sprintf("%.3f", splitPoint),
		"-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-an", bodyPath).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Body creation failed: %s", string(out)))
		return
	}
	

	// Step 2: Tail (last N seconds)
	w.queue.PublishProgress(jobID, channelID, 35, "processing", "Seamless: membuat tail")
	if out, err := exec.Command("ffmpeg", "-y",
		"-ss", fmt.Sprintf("%.3f", splitPoint), "-i", rawPath,
		"-t", fmt.Sprintf("%.3f", tailLen),
		"-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-an", tailPath).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Tail creation failed: %s", string(out)))
		return
	}
	

	// Step 3: Tail with alpha fade-out
	w.queue.PublishProgress(jobID, channelID, 55, "processing", "Seamless: alpha fade")
	if out, err := exec.Command("ffmpeg", "-y",
		"-i", tailPath,
		"-vf", fmt.Sprintf("format=rgba,fade=t=out:st=%.3f:d=%.3f:alpha=1", fadeStart, fadeDuration),
		"-c:v", "qtrle", tailAlphaPath).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Tail alpha failed: %s", string(out)))
		return
	}
	

	// Step 4: Final overlay
	w.queue.PublishProgress(jobID, channelID, 75, "processing", "Seamless: overlay final")
	if out, err := exec.Command("ffmpeg", "-y",
		"-i", bodyPath, "-i", tailAlphaPath,
		"-filter_complex",
		"[0:v][1:v]overlay=0:0:eof_action=pass,scale=1920:1080:force_original_aspect_ratio=increase,crop=1920:1080,setsar=1[v]",
		"-map", "[v]",
		"-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-movflags", "+faststart",
		seamlessTmp).CombinedOutput(); err != nil {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Overlay failed: %s", string(out)))
		return
	}
	

	// Step 5: Slowmo post-processing (if enabled)
	finalTmp := seamlessTmp
	if slowmoPercent > 0 {
		w.queue.PublishProgress(jobID, channelID, 85, "processing", "Seamless: slowmo")
		speedFactor := 1.0 - float64(slowmoPercent)/100.0
		if speedFactor <= 0 {
			speedFactor = 1.0
		}
		ptsMultiplier := 1.0 / speedFactor
		slowmoTmp := filepath.Join(tmpDir, fmt.Sprintf("%s_slowmo.mp4", jobID))

		if out, err := exec.Command("ffmpeg", "-y",
			"-i", seamlessTmp,
			"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
			"-filter:v", fmt.Sprintf("setpts=%.3f*PTS", ptsMultiplier),
			"-map", "0:v:0", "-map", "1:a:0", "-shortest",
			"-c:v", "libx264", "-preset", "medium", "-crf", "18",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart", slowmoTmp).CombinedOutput(); err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Slowmo failed: %s", string(out)))
			return
		}
		
		os.Remove(seamlessTmp)
		finalTmp = slowmoTmp
	}

	// Generate output filename
	if outputFilename == "" {
		outputFilename = fmt.Sprintf("seamless_%s_%s.mp4", strings.TrimSuffix(videoSource, filepath.Ext(videoSource)), jobID[:8])
	}

	// Move to video folder
	videoDir := filepath.Join(assetsDir, "video", channelID)
	os.MkdirAll(videoDir, 0755)
	outputPath := filepath.Join(videoDir, outputFilename)
	os.Rename(finalTmp, outputPath)

	// Create media item
	sizeBytes := int64(0)
	if info, err := os.Stat(outputPath); err == nil {
		sizeBytes = info.Size()
	}
	relativePath := fmt.Sprintf("assets/video/%s/%s", channelID, outputFilename)
	w.DB.Exec(ctx,
		`INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, file_size, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'video',$5,'active',NOW(),NOW())`,
		channelID, outputFilename, "seamless_"+videoSource, relativePath, sizeBytes)

	// Update progress and job
	w.DB.Exec(ctx, "UPDATE auto_seamless_progresses SET progress = 100, status = 'done' WHERE id = $1", jobID)
	w.DB.Exec(ctx,
		"UPDATE production_jobs SET status = 'done', progress = 100, final_path = $1, output_filename = $2 WHERE id = $3",
		outputPath, outputFilename, jobID)

	w.queue.PublishProgress(jobID, channelID, 100, "completed", fmt.Sprintf("Seamless done: %s", outputFilename))
	log.Printf("[Production] Seamless job %s completed: %s", jobID, outputFilename)

	// Cleanup
	os.Remove(bodyPath)
	os.Remove(tailPath)
	os.Remove(tailAlphaPath)
}

// ============================================================
// MODE 3: MERGE VIDEO (Dynamic)
// Ported from v3 merge_video_worker.py
// Random merge with normalization and xfade transitions
// ============================================================

func (w *ProductionWorker) mergeVideo(ctx context.Context, jobID, channelID, tmpDir string) {
	assetsDir := filepath.Join(w.Cfg.StorageDir, "assets")
	rawFolder := filepath.Join(assetsDir, "video-raw", channelID)

	// Get merge config from job
	var mergeCount int
	var mergeResolution, mergeTransitionName string
	var mergeTransitionEnabled bool
	var mergeTransitionDuration, mergeSpeed float64
	w.DB.QueryRow(ctx,
		`SELECT COALESCE(merge_count,10), COALESCE(merge_resolution,'1920x1080'),
		        COALESCE(merge_transition_enabled,true), COALESCE(merge_transition_name,'fade'),
		        COALESCE(merge_transition_duration,1.0), COALESCE(merge_speed,1.0)
		 FROM production_jobs WHERE id = $1`, jobID,
	).Scan(&mergeCount, &mergeResolution, &mergeTransitionEnabled, &mergeTransitionName, &mergeTransitionDuration, &mergeSpeed)

	// Parse resolution
	width, height := 1920, 1080
	if parts := strings.SplitN(mergeResolution, "x", 2); len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &width)
		fmt.Sscanf(parts[1], "%d", &height)
	}

	// List raw videos
	entries, err := os.ReadDir(rawFolder)
	if err != nil || len(entries) == 0 {
		w.failJob(ctx, jobID, channelID, "No video-raw assets found")
		return
	}
	var videos []string
	for _, e := range entries {
		if !e.IsDir() {
			name := strings.ToLower(e.Name())
			if strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".mkv") || strings.HasSuffix(name, ".webm") || strings.HasSuffix(name, ".mov") {
				videos = append(videos, filepath.Join(rawFolder, e.Name()))
			}
		}
	}
	if len(videos) < 2 {
		w.failJob(ctx, jobID, channelID, fmt.Sprintf("Need at least 2 videos, found %d", len(videos)))
		return
	}

	// Random select
	if mergeCount > len(videos) {
		mergeCount = len(videos)
	}
	rand.Shuffle(len(videos), func(i, j int) { videos[i], videos[j] = videos[j], videos[i] })
	selected := videos[:mergeCount]

	// Temp dir for this job
	jobTmpDir := filepath.Join(tmpDir, fmt.Sprintf("merge_%s", jobID))
	os.MkdirAll(jobTmpDir, 0755)

	// Step 1: Normalize each video
	w.queue.PublishProgress(jobID, channelID, 10, "processing", fmt.Sprintf("Normalizing %d videos", len(selected)))
	normalized := []string{}
	slowEnabled := mergeSpeed != 1.0

	for i, vid := range selected {
		normPath := filepath.Join(jobTmpDir, fmt.Sprintf("norm_%03d.mp4", i))
		videoFilter := fmt.Sprintf("fps=30,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1,format=yuv420p", width, height, width, height)
		if slowEnabled {
			videoFilter += fmt.Sprintf(",setpts=PTS/%.3f", mergeSpeed)
		}

		outDur := w.getDuration(vid)
		if slowEnabled && mergeSpeed > 0 {
			outDur = outDur / mergeSpeed
		}

		out, err := exec.Command("ffmpeg", "-y",
			"-i", vid,
			"-f", "lavfi", "-t", fmt.Sprintf("%.2f", outDur), "-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
			"-filter_complex", fmt.Sprintf("[0:v]%s[v];[1:a]aformat=sample_fmts=fltp:sample_rates=44100:channel_layouts=stereo[a]", videoFilter),
			"-map", "[v]", "-map", "[a]",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
			"-pix_fmt", "yuv420p", "-profile:v", "main", "-level", "4.1",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart", normPath).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Normalize failed for video %d: %s", i, string(out)))
			return
		}
		
		normalized = append(normalized, normPath)
		w.queue.PublishProgress(jobID, channelID, 10+int(40*float64(i+1)/float64(len(selected))), "processing", fmt.Sprintf("Normalized %d/%d", i+1, len(selected)))
	}

	// Step 2: Concat with or without transitions
	outputFilename := fmt.Sprintf("dynamic_%s.mp4", jobID[:8])
	outputPath := filepath.Join(assetsDir, "video", channelID, outputFilename)
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	if !mergeTransitionEnabled || mergeTransitionDuration <= 0 || len(normalized) < 2 {
		// Simple concat
		w.queue.PublishProgress(jobID, channelID, 60, "processing", "Concatenating videos")
		listFile := filepath.Join(jobTmpDir, "_concat.txt")
		var listContent strings.Builder
		for _, p := range normalized {
			listContent.WriteString(fmt.Sprintf("file '%s'\n", p))
		}
		os.WriteFile(listFile, []byte(listContent.String()), 0644)
		out, err := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", outputPath).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Concat failed: %s", string(out)))
			return
		}
		
	} else {
		// Xfade transitions
		w.queue.PublishProgress(jobID, channelID, 60, "processing", "Applying transitions")

		args := []string{"-y"}
		for _, p := range normalized {
			args = append(args, "-i", p)
		}

		// Build xfade filter chain
		var filterParts []string
		current := "[0:v]"
		for i := 1; i < len(normalized); i++ {
			segDur := w.getDuration(normalized[i-1])
			offset := math.Max(0, segDur-mergeTransitionDuration)
			nextIn := fmt.Sprintf("[%d:v]", i)
			out := "[vout]"
			if i < len(normalized)-1 {
				out = fmt.Sprintf("[v%d]", i)
			}
			filterParts = append(filterParts, fmt.Sprintf("%s%sxfade=transition=%s:duration=%.3f:offset=%.3f%s", current, nextIn, mergeTransitionName, mergeTransitionDuration, offset, out))
			current = out
		}

		// Audio crossfade
		currentA := "[0:a]"
		for i := 1; i < len(normalized); i++ {
			nextA := fmt.Sprintf("[%d:a]", i)
			outA := "[aout]"
			if i < len(normalized)-1 {
				outA = fmt.Sprintf("[a%d]", i)
			}
			filterParts = append(filterParts, fmt.Sprintf("%s%sacrossfade=d=%.3f%s", currentA, nextA, mergeTransitionDuration, outA))
			currentA = outA
		}

		args = append(args, "-filter_complex", strings.Join(filterParts, ";"))
		args = append(args, "-map", "[vout]", "-map", "[aout]")
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20")
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart")
		args = append(args, outputPath)

		out, err := exec.Command("ffmpeg", args...).CombinedOutput()
		if err != nil {
			w.failJob(ctx, jobID, channelID, fmt.Sprintf("Xfade concat failed: %s", string(out)))
			return
		}
		
	}

	// Create media item
	sizeBytes := int64(0)
	if info, err := os.Stat(outputPath); err == nil {
		sizeBytes = info.Size()
	}
	relativePath := fmt.Sprintf("assets/video/%s/%s", channelID, outputFilename)
	w.DB.Exec(ctx,
		`INSERT INTO media_items (channel_id, filename, original_name, file_path, asset_type, file_size, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'video',$5,'active',NOW(),NOW())`,
		channelID, outputFilename, "merged_"+outputFilename, relativePath, sizeBytes)

	w.DB.Exec(ctx,
		"UPDATE production_jobs SET status = 'done', progress = 100, final_path = $1, output_filename = $2 WHERE id = $3",
		outputPath, outputFilename, jobID)

	w.queue.PublishProgress(jobID, channelID, 100, "completed", fmt.Sprintf("Dynamic merge done: %s", outputFilename))
	log.Printf("[Production] Merge job %s completed: %s", jobID, outputFilename)

	// Cleanup
	os.RemoveAll(jobTmpDir)
}

// ============================================================
// HELPERS
// ============================================================

func (w *ProductionWorker) getDuration(path string) float64 {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	var dur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur
}

func (w *ProductionWorker) failJob(ctx context.Context, jobID, channelID, message string) {
	w.DB.Exec(ctx, "UPDATE production_jobs SET status = 'failed', error_message = $1 WHERE id = $2", message, jobID)
	w.queue.PublishProgress(jobID, channelID, 0, "failed", message)
	log.Printf("[Production] Job %s FAILED: %s", jobID, message)
}

func (w *ProductionWorker) cleanupTmp(tmpDir, jobID string) {
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), jobID) {
			os.Remove(filepath.Join(tmpDir, e.Name()))
		}
	}
}

// ============================================================
// UploadWorker (unchanged)
// ============================================================

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
		 FROM upload_batch_items ubi JOIN channels ch ON ubi.channel_id = ch.id WHERE ubi.id = $1`, job.JobID,
	).Scan(&sourcePath, &title, &description, &tags, &visibility, &scheduledAt, &channelID, &accessToken, &refreshToken)

	if sourcePath == "" || accessToken == "" {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'Missing file or token' WHERE id = $1", job.JobID)
		return
	}

	youtubeURL := "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status"
	metadata := map[string]interface{}{
		"snippet": map[string]interface{}{"title": title, "description": description, "tags": strings.Split(tags, ","), "categoryId": "22"},
		"status":  map[string]interface{}{"privacyStatus": visibility, "selfDeclaredMadeForKids": false},
	}
	if scheduledAt != nil {
		metadata["status"].(map[string]interface{})["publishAt"] = scheduledAt.UTC().Format(time.RFC3339)
	}
	metaJSON, _ := json.Marshal(metadata)

	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'File not found' WHERE id = $1", job.JobID)
		return
	}

	req, _ := http.NewRequest("POST", youtubeURL, bytes.NewReader(metaJSON))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
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

	file, _ := os.Open(sourcePath)
	defer file.Close()
	putReq, _ := http.NewRequest("PUT", sessionURL, file)
	putReq.Header.Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	uploadResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = $1 WHERE id = $2", err.Error(), job.JobID)
		return
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode == 200 || uploadResp.StatusCode == 201 {
		var result struct{ ID string `json:"id"` }
		json.NewDecoder(uploadResp.Body).Decode(&result)
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'done', youtube_video_id = $1, progress = 100, finished_at = NOW() WHERE id = $2", result.ID, job.JobID)
		w.DB.Exec(ctx, "UPDATE upload_batches SET done_items = done_items + 1 WHERE id = (SELECT upload_batch_id FROM upload_batch_items WHERE id = $1)", job.JobID)
		w.queue.PublishProgress(job.JobID, channelID, 100, "completed", fmt.Sprintf("Uploaded: %s", result.ID))
	} else {
		w.DB.Exec(ctx, "UPDATE upload_batch_items SET status = 'failed', last_error = 'Upload failed' WHERE id = $1", job.JobID)
	}
}

// ============================================================
// LivestreamWorker (unchanged)
// ============================================================

type LivestreamWorker struct {
	DB        *pgxpool.Pool
	Cfg       *config.Config
	queue     *JobQueue
	processes map[string]*exec.Cmd
}

func NewLivestreamWorker(db *pgxpool.Pool, cfg *config.Config, queue *JobQueue) *LivestreamWorker {
	return &LivestreamWorker{DB: db, Cfg: cfg, queue: queue, processes: make(map[string]*exec.Cmd)}
}

func (w *LivestreamWorker) Run(ctx context.Context) {
	log.Println("[Worker:livestream] Started")
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
	rows, _ := w.DB.Query(ctx,
		`SELECT id, channel_id, title, video_source, stream_key, quality, duration_hours, stop_requested
		 FROM live_jobs WHERE status IN ('scheduled','running') AND start_at_utc <= NOW()`)
	defer rows.Close()
	for rows.Next() {
		var id, channelID, title, videoSource, streamKey, quality string
		var durationHours int
		var stopRequested bool
		rows.Scan(&id, &channelID, &title, &videoSource, &streamKey, &quality, &durationHours, &stopRequested)

		cmd, exists := w.processes[id]
		if stopRequested {
			if exists && cmd != nil && cmd.Process != nil {
				cmd.Process.Kill()
			}
			w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'stopped', finished_at = NOW() WHERE id = $1", id)
			delete(w.processes, id)
			continue
		}
		if !exists && videoSource != "" {
			bitrate := "4500k"
			if quality == "low" {
				bitrate = "2500k"
			}
			cmd := exec.Command("ffmpeg", "-y", "-stream_loop", "-1", "-re", "-i", videoSource,
				"-c:v", "libx264", "-preset", "veryfast", "-b:v", bitrate, "-maxrate", bitrate, "-bufsize", "9000k",
				"-pix_fmt", "yuv420p", "-g", "60", "-c:a", "aac", "-b:a", "128k", "-ar", "44100",
				"-f", "flv", fmt.Sprintf("rtmp://a.rtmp.youtube.com/live2/%s", streamKey))
			if err := cmd.Start(); err == nil {
				w.processes[id] = cmd
				w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'running', started_at = NOW() WHERE id = $1", id)
			}
		}
	}
	for id, cmd := range w.processes {
		if cmd != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			w.DB.Exec(ctx, "UPDATE live_jobs SET status = 'finished', finished_at = NOW() WHERE id = $1", id)
			delete(w.processes, id)
		}
	}
}

func (w *LivestreamWorker) killAll() {
	for _, cmd := range w.processes {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
}

// ============================================================
// ShortsWorker (unchanged)
// ============================================================

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
			w.processJobs(ctx)
		}
	}
}

func (w *ShortsWorker) processJobs(ctx context.Context) {
	rows, _ := w.DB.Query(ctx,
		`SELECT id, channel_id, long_youtube_url, short_count, short_duration
		 FROM shorts_jobs WHERE status = 'created' ORDER BY created_at ASC LIMIT 1`)
	defer rows.Close()
	for rows.Next() {
		var jobID, channelID, youtubeURL string
		var shortCount, shortDuration int
		rows.Scan(&jobID, &channelID, &youtubeURL, &shortCount, &shortDuration)

		w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'generating' WHERE id = $1", jobID)
		tmpDir := filepath.Join(w.Cfg.StorageDir, "tmp", jobID)
		os.MkdirAll(tmpDir, 0755)
		videoPath := filepath.Join(tmpDir, "source.mp4")

		if out, err := exec.Command("yt-dlp", "-f", "best[height<=1080]", "-o", videoPath, "--no-playlist", youtubeURL).CombinedOutput(); err != nil {
			w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'failed', error_message = $1 WHERE id = $2", string(out), jobID)
			continue
		}

		items, _ := w.DB.Query(ctx, "SELECT id, short_number, start_second, end_second FROM shorts_items WHERE job_id = $1 ORDER BY short_number", jobID)
		for items.Next() {
			var itemID string
			var shortNumber int
			var startSecond, endSecond float64
			items.Scan(&itemID, &shortNumber, &startSecond, &endSecond)
			dur := endSecond - startSecond
			if dur <= 0 {
				dur = float64(shortDuration)
			}
			outputPath := filepath.Join(tmpDir, fmt.Sprintf("short_%d.mp4", shortNumber))
			out, err := exec.Command("ffmpeg", "-y",
				"-ss", fmt.Sprintf("%.2f", startSecond), "-t", fmt.Sprintf("%.2f", dur), "-i", videoPath,
				"-vf", "crop=ih*(9/16):ih,scale=1080:1920",
				"-c:v", "libx264", "-preset", "fast", "-c:a", "aac", outputPath).CombinedOutput()
			if err != nil {
				w.DB.Exec(ctx, "UPDATE shorts_items SET status = 'failed', error_message = $1 WHERE id = $2", string(out), itemID)
				continue
			}
			w.DB.Exec(ctx, "UPDATE shorts_items SET status = 'generated', video_path = $1 WHERE id = $2", outputPath, itemID)
		}
		items.Close()
		w.DB.Exec(ctx, "UPDATE shorts_jobs SET status = 'ready' WHERE id = $1", jobID)
	}
}
