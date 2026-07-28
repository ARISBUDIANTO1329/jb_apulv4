package models

import (
	"time"
)

// ============================================================
// USERS
// ============================================================
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	AvatarURL    string    `json:"avatar_url"`
	GoogleID     string    `json:"google_id"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ============================================================
// SESSIONS
// ============================================================
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ============================================================
// CHANNELS
// ============================================================
type Channel struct {
	ID          int    `json:"id"`
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	YoutubeID   string `json:"youtube_channel_id"`
	YoutubeURL  string `json:"youtube_channel_url"`
	Niche       string `json:"niche"`
	Description string `json:"description"`
	Email       string `json:"email"`
	Status      string `json:"status"` // active/paused/dropped

	AccessToken     string     `json:"-"`
	RefreshToken    string     `json:"-"`
	TokenExpiresAt  *time.Time `json:"token_expires_at,omitempty"`
	TokenStatus     string     `json:"token_status"` // valid/error/expired
	TokenError      string     `json:"token_error,omitempty"`
	TokenCheckedAt  *time.Time `json:"token_checked_at,omitempty"`

	StreamKey  string `json:"stream_key"`
	ProxyHost  string `json:"proxy_host"`
	ProxyPort  int    `json:"proxy_port"`
	ProxyType  string `json:"proxy_type"` // socks5/socks4/http

	SubscriberCount int   `json:"subscriber_count"`
	TotalViews      int64 `json:"total_views"`
	VideoCount      int   `json:"video_count"`

	Notes        string     `json:"notes"`
	LastUpload   *time.Time `json:"last_upload,omitempty"`
	LastLive     *time.Time `json:"last_livestream,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ============================================================
// MEDIA ITEMS
// ============================================================
type MediaItem struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`

	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	FilePath     string `json:"file_path"`
	AssetType    string `json:"asset_type"`
	// video, video-raw, video-live, video-preview, upload_ready,
	// livestream-ready, mp3, sfx, intro, thumbnail, shorts, metadata

	MIME     string  `json:"mime"`
	FileSize int64   `json:"file_size"`
	Duration float64 `json:"duration"`
	Title    string  `json:"title"`
	Note     string  `json:"note"`
	Tags     string  `json:"tags"`
	Status   string  `json:"status"`
	Category string  `json:"category"`
	SHA256   string  `json:"sha256,omitempty"`

	MetadataJSON   string `json:"metadata_json,omitempty"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	YoutubeVideoID string     `json:"youtube_video_id,omitempty"`
	IsUsed         bool       `json:"is_used"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ============================================================
// PRODUCTION JOBS
// ============================================================
type ProductionJob struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`

	VideoSource string `json:"video_source"`
	NumSongs    int    `json:"num_songs"`
	NoMP3       bool   `json:"no_mp3"`
	NoSFX       bool   `json:"no_sfx"`
	SFXFile     string `json:"sfx_file"`
	IntroFile   string `json:"intro_file"`
	MP3File     string `json:"mp3_file"`
	DurationMode  string `json:"duration_mode"`  // mp3/manual
	CustomDuration int   `json:"custom_duration"`

	ProductionMode   string `json:"production_mode"`   // v2/dynamic/static/final
	ProductionMethod string `json:"production_method"` // ready_video/raw_video_auto_seamless/merge_video

	MP3Mode      string  `json:"mp3_mode"` // shuffle/single
	TailLength   float64 `json:"tail_length"`
	SlowmoPercent float64 `json:"slowmo_percent"`

	MergeCount             int     `json:"merge_count"`
	MergeResolution        string  `json:"merge_resolution"`
	MergeTransitionEnabled bool    `json:"merge_transition_enabled"`
	MergeTransitionName    string  `json:"merge_transition_name"`
	MergeTransitionDuration float64 `json:"merge_transition_duration"`
	MergeSpeed             float64 `json:"merge_speed"`
	DynamicOutputCount     int     `json:"dynamic_output_count"`

	Status      string `json:"status"`       // pending/processing/done/failed
	Progress    int    `json:"progress"`
	AudioStatus string `json:"audio_status"`
	VideoStatus string `json:"video_status"`
	FinalStatus string `json:"final_status"`

	AudioPath     string `json:"audio_path"`
	VideoPath     string `json:"video_path"`
	FinalPath     string `json:"final_path"`
	AudioDuration float64 `json:"audio_duration"`
	ErrorMessage  string `json:"error_message,omitempty"`
	ProcessStatus string `json:"process_status"`
	OutputFilename string `json:"output_filename"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ============================================================
// UPLOAD BATCHES
// ============================================================
type UploadBatch struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Status    string `json:"status"` // pending/processing/done/failed
	TotalItems int    `json:"total_items"`
	DoneItems  int    `json:"done_items"`
	CreatedAt time.Time `json:"created_at"`
}

type UploadBatchItem struct {
	ID            string `json:"id"`
	UploadBatchID string `json:"upload_batch_id"`
	ChannelID     string `json:"channel_id"`
	MediaItemID   string `json:"media_item_id,omitempty"`
	UserID        string `json:"user_id"`

	Title       string `json:"title"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	YoutubeID   string `json:"youtube_video_id,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	Visibility  string `json:"visibility"` // private/unlisted/public/scheduled
	Status      string `json:"status"`    // pending/processing/done/failed
	LastError   string `json:"last_error,omitempty"`
	Progress    int    `json:"progress"`
	SourcePath  string `json:"source_path"`
	ThumbnailPath string `json:"thumbnail_path"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ============================================================
// LIVE JOBS
// ============================================================
type LiveJob struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`

	Title        string `json:"title"`
	Description  string `json:"description"`
	Tags         string `json:"tags"`
	VideoSource  string `json:"video_source"`
	UseMP3       bool   `json:"use_mp3"`
	UseSFX       bool   `json:"use_sfx"`
	StreamKey    string `json:"stream_key"`
	BroadcastID  string `json:"broadcast_id"`
	Quality      string `json:"quality"`   // high/low
	Visibility   string `json:"visibility"` // public/private/unlisted
	DurationHours int    `json:"duration_hours"`
	MadeForKids  bool   `json:"made_for_kids"`
	ThumbnailPath string `json:"thumbnail_path"`

	StartAtUTC *time.Time `json:"start_at_utc,omitempty"`
	EndAtUTC   *time.Time `json:"end_at_utc,omitempty"`

	Status           string `json:"status"`
	// pending/scheduled/ready/running/finished/failed/stopped
	ProcessID        int    `json:"process_id"`
	ErrorMessage     string `json:"error_message,omitempty"`
	ReconnectCount   int    `json:"reconnect_count"`
	ReconnectAttempts int   `json:"reconnect_attempts"`
	LastHealthCheck  *time.Time `json:"last_health_check,omitempty"`
	StopRequested    bool       `json:"stop_requested"`

	StreamStats   string  `json:"stream_stats,omitempty"`
	CurrentBitrate float64 `json:"current_bitrate"`
	CurrentFPS    float64 `json:"current_fps"`
	ViewerCount   int     `json:"viewer_count"`
	FrameDropCount int    `json:"frame_drop_count"`

	ErrorCategory string     `json:"error_category,omitempty"` // network/auth/quota/ffmpeg/unknown
	RetryCount    int        `json:"retry_count"`
	LastRetryAt   *time.Time `json:"last_retry_at,omitempty"`
	MaxRetries    int        `json:"max_retries"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ============================================================
// SHORTS
// ============================================================
type ShortsJob struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`

	LongUploadID   string `json:"long_upload_id,omitempty"`
	LongYoutubeURL string `json:"long_youtube_url"`
	LongTitle      string `json:"long_title"`
	ShortCount     int    `json:"short_count"`
	ShortDuration  int    `json:"short_duration"`
	SegmentMode    string `json:"segment_mode"`  // auto/manual
	DescTemplate   string `json:"description_template"`

	UploadTime1 string `json:"upload_time_1"`
	UploadTime2 string `json:"upload_time_2"`
	UploadTime3 string `json:"upload_time_3"`

	Status       string `json:"status"` // created/generating/ready/uploading/completed/failed
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type ShortsItem struct {
	ID     string `json:"id"`
	JobID  string `json:"job_id"`

	ShortNumber int     `json:"short_number"`
	VideoPath   string  `json:"video_path"`
	StartSecond float64 `json:"start_second"`
	EndSecond   float64 `json:"end_second"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	YoutubeID   string  `json:"youtube_id,omitempty"`
	UploadTime  string  `json:"upload_time"`

	Status       string `json:"status"` // pending/generated/uploading/uploaded/failed
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
}

// ============================================================
// PIPELINES
// ============================================================
type Pipeline struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`

	Mode            string `json:"mode"` // dynamic/static/final/direct
	UploadEnabled   bool   `json:"upload_enabled"`
	UploadCount     int    `json:"upload_count"`
	LiveEnabled     bool   `json:"live_enabled"`
	LiveCount       int    `json:"live_count"`
	LiveDurationHours int  `json:"live_duration_hours"`
	LiveQuality     string `json:"live_quality"`
	LiveUseMP3      bool   `json:"live_use_mp3"`
	LiveUseSFX      bool   `json:"live_use_sfx"`
	ShortsEnabled   bool   `json:"shorts_enabled"`
	ShortsCount     int    `json:"shorts_count"`

	SchedulerTime string `json:"scheduler_time"` // HH:MM WIB
	IsActive      bool   `json:"is_active"`
	ConfigJSON    string `json:"config_json,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PipelineRun struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipeline_id"`
	ChannelID  string `json:"channel_id"`

	Status       string `json:"status"` // pending/producing/uploading/livestreaming/completed/failed/partial
	CurrentStage string `json:"current_stage"`
	Progress     int    `json:"progress"`
	RunType      string `json:"run_type"` // manual/scheduled
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	Log          string `json:"log,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ResultJSON   string `json:"result_json,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ============================================================
// METADATA POOLS
// ============================================================
type MetadataTitlePool struct {
	ID        string     `json:"id"`
	ChannelID string     `json:"channel_id"`
	Title     string     `json:"title"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type MetadataDescriptionPool struct {
	ID          string     `json:"id"`
	ChannelID   string     `json:"channel_id"`
	Description string     `json:"description"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type MetadataTagPool struct {
	ID        string     `json:"id"`
	ChannelID string     `json:"channel_id"`
	Tags      string     `json:"tags"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ============================================================
// ASSET USAGE LOGS
// ============================================================
type AssetUsageLog struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`

	AssetKey      string `json:"asset_key"`
	AssetSource   string `json:"asset_source"`
	AssetFilename string `json:"asset_filename"`
	FilePath      string `json:"file_path"`
	AssetType     string `json:"asset_type"` // video/mp3/sfx

	UsageType    string `json:"usage_type"` // upload_regular/livestream/production
	UsedFor      string `json:"used_for"`
	UsageDate    string `json:"usage_date"`
	CooldownUntil string `json:"cooldown_until"`

	RelatedType string `json:"related_type"`
	RelatedID   string `json:"related_id"`
	MetaJSON    string `json:"meta_json,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// ============================================================
// AUTO PRODUCTION SCHEDULES
// ============================================================
type AutoProductionSchedule struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`

	Target     string `json:"target"`   // upload_regular/livestream/production_daily
	Workflow   string `json:"workflow"` // static/dynamic/final_production
	ScheduleTime string `json:"schedule_time"`
	StartMode  string `json:"start_mode"` // today/tomorrow
	IsActive   bool   `json:"is_active"`
	ConfigJSON string `json:"config_json,omitempty"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AutoControlRoomJob struct {
	ID        string `json:"id"`
	ScheduleID string `json:"auto_production_schedule_id"`
	ChannelID string `json:"channel_id"`

	Target         string `json:"target"`
	Workflow       string `json:"workflow"`
	RunDate        string `json:"run_date"`
	Status         string `json:"status"` // waiting/blocked/running/done/failed
	CurrentStage   string `json:"current_stage"`
	Progress       int    `json:"progress"`
	TotalItems     int    `json:"total_items"`
	DoneItems      int    `json:"done_items"`
	CurrentItemOrder int  `json:"current_item_order"`
	ConfigJSON     string `json:"config_json,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AutoControlRoomJobItem struct {
	ID        string `json:"id"`
	ControlRoomJobID string `json:"auto_control_room_job_id"`

	QueueOrder   int    `json:"queue_order"`
	Target       string `json:"target"`
	Workflow     string `json:"workflow"`
	SourceType   string `json:"source_type"`
	Status       string `json:"status"`
	CurrentStage string `json:"current_stage"`
	Progress     int    `json:"progress"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AutoSeamlessProgress struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`

	RawFilename string `json:"raw_filename"`
	InputPath   string `json:"input_path"`
	OutputPath  string `json:"output_path"`
	Progress    int    `json:"progress"`
	Status      string `json:"status"` // pending/processing/done/failed
	Message     string `json:"message,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}