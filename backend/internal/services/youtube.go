package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type YouTubeService struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

func NewYouTubeService(clientID, clientSecret string) *YouTubeService {
	return &YouTubeService{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (y *YouTubeService) ExchangeCode(code, redirectURI string) (*TokenResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {y.ClientID},
		"client_secret": {y.ClientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := y.HTTPClient.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("token decode: %w", err)
	}
	return &token, nil
}

func (y *YouTubeService) RefreshToken(refreshToken string) (*TokenResponse, error) {
	data := url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {y.ClientID},
		"client_secret": {y.ClientSecret},
		"grant_type":    {"refresh_token"},
	}

	resp, err := y.HTTPClient.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("refresh decode: %w", err)
	}
	return &token, nil
}

type UserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func (y *YouTubeService) GetUserInfo(accessToken string) (*UserInfo, error) {
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	defer resp.Body.Close()

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("userinfo decode: %w", err)
	}
	return &info, nil
}

// UploadVideo uploads a video to YouTube using the resumable upload API
func (y *YouTubeService) UploadVideo(accessToken, filePath, title, description, tags, privacyStatus string, scheduledAt *time.Time) (string, error) {
	// Step 1: Get file info
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	stat, _ := file.Stat()
	fileSize := stat.Size()

	// Step 2: Create metadata JSON
	metadata := map[string]interface{}{
		"snippet": map[string]interface{}{
			"title":       title,
			"description": description,
			"tags":        strings.Split(tags, ","),
			"categoryId":  "22", // Entertainment
		},
		"status": map[string]interface{}{
			"privacyStatus": privacyStatus,
			"selfDeclaredMadeForKids": false,
		},
	}

	if scheduledAt != nil {
		metadata["status"].(map[string]interface{})["publishAt"] = scheduledAt.UTC().Format(time.RFC3339)
	}

	metaJSON, _ := json.Marshal(metadata)

	// Step 3: Initiate resumable upload
	uploadURL := "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status"

	req, _ := http.NewRequest("POST", uploadURL, bytes.NewReader(metaJSON))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", fileSize))
	req.Header.Set("X-Upload-Content-Type", "video/*")

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("init upload: %w", err)
	}
	defer resp.Body.Close()

	sessionURL := resp.Header.Get("Location")
	if sessionURL == "" {
		return "", fmt.Errorf("no upload URL returned")
	}

	// Step 4: Upload file chunks
	chunkSize := int64(10 * 1024 * 1024) // 10MB
	buf := make([]byte, chunkSize)
	var totalSent int64

	for totalSent < fileSize {
		remaining := fileSize - totalSent
		currentChunk := chunkSize
		if remaining < currentChunk {
			currentChunk = remaining
		}

		_, err := file.Read(buf[:currentChunk])
		if err != nil {
			return "", fmt.Errorf("read chunk: %w", err)
		}

		uploadReq, _ := http.NewRequest("PUT", sessionURL, bytes.NewReader(buf[:currentChunk]))
		uploadReq.Header.Set("Content-Length", fmt.Sprintf("%d", currentChunk))
		uploadReq.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", totalSent, totalSent+currentChunk-1, fileSize))

		uploadResp, err := y.HTTPClient.Do(uploadReq)
		if err != nil {
			return "", fmt.Errorf("upload chunk: %w", err)
		}

		if uploadResp.StatusCode == 200 || uploadResp.StatusCode == 201 {
			// Upload complete
			var result struct {
				ID string `json:"id"`
			}
			json.NewDecoder(uploadResp.Body).Decode(&result)
			uploadResp.Body.Close()
			return result.ID, nil
		}

		uploadResp.Body.Close()
		totalSent += currentChunk
		log.Printf("[YouTube] Upload progress: %d/%d bytes", totalSent, fileSize)
	}

	return "", fmt.Errorf("upload incomplete")
}

// UploadThumbnail uploads a thumbnail for a video
func (y *YouTubeService) UploadThumbnail(accessToken, videoID, thumbnailPath string) error {
	thumbFile, err := os.Open(thumbnailPath)
	if err != nil {
		return fmt.Errorf("open thumbnail: %w", err)
	}
	defer thumbFile.Close()

	url := fmt.Sprintf("https://www.googleapis.com/upload/youtube/v3/thumbnails/set?videoId=%s", videoID)
	req, _ := http.NewRequest("POST", url, thumbFile)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "image/png")

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("thumbnail upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("thumbnail failed: %s", string(body))
	}
	return nil
}

// CreateBroadcast creates a YouTube Live broadcast
func (y *YouTubeService) CreateBroadcast(accessToken, title, description, privacy string, scheduledStart time.Time) (string, string, error) {
	broadcast := map[string]interface{}{
		"snippet": map[string]interface{}{
			"title":       title,
			"description": description,
			"scheduledStartTime": scheduledStart.UTC().Format(time.RFC3339),
		},
		"status": map[string]interface{}{
			"privacyStatus":                privacy,
			"selfDeclaredMadeForKids":      false,
			"lifeCycleStatus":              "ready",
		},
		"contentDetails": map[string]interface{}{
			"enableAutoStart": true,
			"enableAutoStop":  true,
		},
	}

	body, _ := json.Marshal(broadcast)
	req, _ := http.NewRequest("POST", "https://www.googleapis.com/youtube/v3/liveBroadcasts?part=snippet,status,contentDetails", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("create broadcast: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.ID, "", nil
}

// CreateStream creates a YouTube Live stream
func (y *YouTubeService) CreateStream(accessToken, title string) (string, string, error) {
	stream := map[string]interface{}{
		"snippet": map[string]interface{}{
			"title": title,
		},
		"cdn": map[string]interface{}{
			"frameRate":  "30fps",
			"ingestionType": "rtmp",
			"resolution": "1080p",
		},
	}

	body, _ := json.Marshal(stream)
	req, _ := http.NewRequest("POST", "https://www.googleapis.com/youtube/v3/liveStreams?part=snippet,cdn", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("create stream: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ID   string `json:"id"`
		CDN  struct {
			IngestionInfo struct {
				StreamName string `json:"streamName"`
				IngestionAddress string `json:"ingestionAddress"`
			} `json:"ingestionInfo"`
		} `json:"cdn"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.ID, result.CDN.IngestionInfo.StreamName, nil
}

// BindBroadcast binds a broadcast to a stream
func (y *YouTubeService) BindBroadcast(accessToken, broadcastID, streamID string) error {
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/liveBroadcasts/bind?id=%s&part=snippet,status&streamId=%s", broadcastID, streamID)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("bind broadcast: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// EndBroadcast ends a YouTube Live broadcast
func (y *YouTubeService) EndBroadcast(accessToken, broadcastID string) error {
	body, _ := json.Marshal(map[string]string{
		"id":     broadcastID,
		"status": "complete",
	})
	req, _ := http.NewRequest("PUT", fmt.Sprintf("https://www.googleapis.com/youtube/v3/liveBroadcasts?part=status", broadcastID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := y.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("end broadcast: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// DownloadVideo downloads a YouTube video via yt-dlp
func (y *YouTubeService) DownloadVideo(youtubeURL, outputPath string) (string, error) {
	cmd := exec.Command("yt-dlp",
		"-f", "bestvideo[height<=1080]+bestaudio/best[height<=1080]",
		"-o", outputPath,
		"--no-playlist",
		youtubeURL,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("yt-dlp: %w", err)
	}
	return string(out), nil
}