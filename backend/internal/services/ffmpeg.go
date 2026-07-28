package services

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

type FFmpegProgress struct {
	Duration float64
	Position float64
	Speed    string
	Percent  float64
	FPS      float64
	Bitrate  float64
	Frame    int
}

type FFmpegService struct {
	Path string
}

func NewFFmpegService() *FFmpegService {
	return &FFmpegService{Path: "ffmpeg"}
}

func (f *FFmpegService) Run(args ...string) (string, error) {
	cmd := exec.Command(f.Path, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *FFmpegService) RunWithProgress(args []string, progressFn func(FFmpegProgress)) (string, error) {
	cmd := exec.Command(f.Path, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	var buf bytes.Buffer
	tee := io.TeeReader(stderr, &buf)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}

	var totalDuration float64
	scanner := bufio.NewScanner(tee)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Duration:") {
			if dur := extractDuration(line); dur > 0 {
				totalDuration = dur
			}
		}
		if strings.Contains(line, "time=") {
			prog := parseProgress(line, totalDuration)
			if progressFn != nil {
				progressFn(prog)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return buf.String(), fmt.Errorf("ffmpeg: %w", err)
	}

	return buf.String(), nil
}

func (f *FFmpegService) AudioEngine(args AudioEngineArgs) (string, float64, error) {
	// Combine MP3s + SFX into audio track
	filterParts := []string{}
	inputs := []string{}
	inputIdx := 0

	for i, mp3 := range args.MP3Files {
		inputs = append(inputs, "-i", mp3)
		filterParts = append(filterParts, fmt.Sprintf("[%d:a]aresample=44100[a%d]", inputIdx, i))
		inputIdx++
	}

	// Concat audio
	ffmpegArgs := []string{"-y"}
	ffmpegArgs = append(ffmpegArgs, inputs...)
	ffmpegArgs = append(ffmpegArgs,
		"-filter_complex", strings.Join(filterParts, ";"),
		"-ac", "2",
		"-ar", "44100",
		args.OutputPath,
	)

	out, err := f.Run(ffmpegArgs...)
	duration := getAudioDuration(args.OutputPath)
	return out, duration, err
}

func (f *FFmpegService) VideoLoop(videoPath, audioPath, outputPath string, introPath string) (string, error) {
	// Loop video to match audio duration, prepend intro if available
	args := []string{"-y", "-stream_loop", "-1", "-i", videoPath, "-i", audioPath}

	if introPath != "" {
		args = append(args, "-i", introPath)
		args = append(args, "-filter_complex", "[0:v]loop=-1:size=1[v];[v][2:v]concat=n=2:v=1:a=0[outv]")
		args = append(args, "-map", "[outv]")
	} else {
		args = append(args, "-filter_complex", "loop=-1:size=1")
	}

	args = append(args,
		"-map", "1:a",
		"-c:v", "libx264",
		"-preset", "fast",
		"-c:a", "aac",
		"-shortest",
		outputPath,
	)
	return f.Run(args...)
}

func (f *FFmpegService) FinalRender(videoPath, audioPath, outputPath string) (string, error) {
	return f.Run("-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "aac",
		"-shortest",
		outputPath,
	)
}

func (f *FFmpegService) MergeVideos(inputs []string, outputPath string, resolution string, transitions bool) (string, error) {
	args := []string{"-y"}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}

	if transitions && len(inputs) > 1 {
		filters := []string{}
		for i := range inputs {
			filters = append(filters, fmt.Sprintf("[%d:v]scale=%s,setsar=1[v%d]", i, resolution, i))
		}
		concat := ""
		for i := range inputs {
			if i > 0 {
				concat += fmt.Sprintf("[v%d]", i)
			} else {
				concat = fmt.Sprintf("[v0]")
			}
		}
		concat += fmt.Sprintf("concat=n=%d:v=1:a=0", len(inputs))
		args = append(args, "-filter_complex", strings.Join(filters, ";")+";"+concat)
		args = append(args, "-map", "[out]")
	} else {
		args = append(args, "-filter_complex", fmt.Sprintf("concat=n=%d:v=1:a=0", len(inputs)))
	}

	args = append(args, "-c:v", "libx264", "-preset", "fast", outputPath)
	return f.Run(args...)
}

func (f *FFmpegService) CropToShorts(inputPath, outputPath string, start, duration float64) (string, error) {
	return f.Run("-y",
		"-ss", fmt.Sprintf("%.2f", start),
		"-t", fmt.Sprintf("%.2f", duration),
		"-i", inputPath,
		"-vf", "crop=ih*(9/16):ih,scale=1080:1920",
		"-c:v", "libx264",
		"-preset", "fast",
		"-c:a", "aac",
		outputPath,
	)
}

func (f *FFmpegService) Livestream(inputPath, streamKey, rtmpURL string, durationHours int, useMP3, useSFX bool) (*exec.Cmd, error) {
	// Build livestream FFmpeg command
	args := []string{
		"-y",
		"-stream_loop", "-1",
		"-re",
		"-i", inputPath,
	}

	if useMP3 {
		args = append(args, "-i", inputPath) // Would be MP3 file
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-b:v", "4500k",
		"-maxrate", "4500k",
		"-bufsize", "9000k",
		"-pix_fmt", "yuv420p",
		"-g", "60",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "44100",
		"-f", "flv",
		fmt.Sprintf("%s/%s", rtmpURL, streamKey),
	)

	cmd := exec.Command(f.Path, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("livestream start: %w", err)
	}
	return cmd, nil
}

type AudioEngineArgs struct {
	MP3Files   []string
	SFXFiles   []string
	OutputPath string
	Duration   float64 // 0 = auto from MP3s
}

func extractDuration(line string) float64 {
	parts := strings.Split(line, "Duration:")
	if len(parts) < 2 {
		return 0
	}
	durStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
	// Format: HH:MM:SS.mm
	durParts := strings.Split(durStr, ":")
	if len(durParts) < 3 {
		return 0
	}
	h, _ := strconv.ParseFloat(durParts[0], 64)
	m, _ := strconv.ParseFloat(durParts[1], 64)
	s, _ := strconv.ParseFloat(durParts[2], 64)
	return h*3600 + m*60 + s
}

func parseProgress(line string, totalDuration float64) FFmpegProgress {
	var p FFmpegProgress
	p.Duration = totalDuration

	if idx := strings.Index(line, "time="); idx >= 0 {
		timeStr := strings.Fields(line[idx:])[0][5:]
		parts := strings.Split(timeStr, ":")
		if len(parts) >= 3 {
			h, _ := strconv.ParseFloat(parts[0], 64)
			m, _ := strconv.ParseFloat(parts[1], 64)
			s, _ := strconv.ParseFloat(strings.Split(parts[2], ".")[0], 64)
			p.Position = h*3600 + m*60 + s
			if totalDuration > 0 {
				p.Percent = (p.Position / totalDuration) * 100
			}
		}
	}

	if idx := strings.Index(line, "speed="); idx >= 0 {
		p.Speed = strings.Fields(line[idx:])[0][6:]
	}
	if idx := strings.Index(line, "fps="); idx >= 0 {
		fpsStr := strings.Fields(line[idx:])[0][4:]
		p.FPS, _ = strconv.ParseFloat(fpsStr, 64)
	}
	if idx := strings.Index(line, "bitrate="); idx >= 0 {
		br := strings.Fields(line[idx:])[0][8:]
		p.Bitrate, _ = strconv.ParseFloat(br, 64)
	}
	return p
}

func getAudioDuration(path string) float64 {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Printf("[FFmpeg] ffprobe error: %v", err)
		return 0
	}
	dur, _ := strconv.ParseFloat(strings.TrimSpace(out.String()), 64)
	return dur
}