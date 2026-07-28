-- JB Apul v4 - Full Schema (v3 compatible)

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
-- USERS
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(100) UNIQUE DEFAULT '',
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    avatar_url TEXT DEFAULT '',
    google_id VARCHAR(255) UNIQUE DEFAULT '',
    password_hash VARCHAR(255) DEFAULT '',
    role VARCHAR(20) DEFAULT 'user',
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- SESSIONS
-- ============================================================
CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- ============================================================
-- CHANNELS (INTEGER ID - #1, #2, #3...)
-- ============================================================
CREATE TABLE IF NOT EXISTS channels (
    id SERIAL PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL DEFAULT '',
    youtube_channel_id VARCHAR(255) DEFAULT '',
    youtube_channel_url TEXT DEFAULT '',
    niche VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    email VARCHAR(255) DEFAULT '',
    status VARCHAR(20) DEFAULT 'active', -- active/paused/dropped

    -- OAuth tokens
    access_token TEXT DEFAULT '',
    refresh_token TEXT DEFAULT '',
    token_expires_at TIMESTAMPTZ,
    token_status VARCHAR(20) DEFAULT 'not_connected', -- not_connected/valid/error/expired
    token_error TEXT DEFAULT '',
    token_checked_at TIMESTAMPTZ,

    -- Stream settings
    stream_key VARCHAR(255) DEFAULT '',
    proxy_host VARCHAR(255) DEFAULT '',
    proxy_port INT DEFAULT 0,
    proxy_type VARCHAR(20) DEFAULT '', -- socks5/socks4/http

    -- Stats
    subscriber_count INT DEFAULT 0,
    total_views BIGINT DEFAULT 0,
    video_count INT DEFAULT 0,

    notes TEXT DEFAULT '',
    last_upload TIMESTAMPTZ,
    last_livestream TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_channels_user_id ON channels(user_id);

-- ============================================================
-- MEDIA ITEMS (12 asset types)
-- ============================================================
CREATE TABLE IF NOT EXISTS media_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    filename VARCHAR(255) NOT NULL,
    original_name VARCHAR(255) NOT NULL DEFAULT '',
    file_path TEXT NOT NULL DEFAULT '',
    asset_type VARCHAR(30) NOT NULL DEFAULT 'video',
    -- video, video-raw, video-live, video-preview, upload_ready,
    -- livestream-ready, mp3, sfx, intro, thumbnail, shorts, metadata

    mime VARCHAR(100) DEFAULT '',
    file_size BIGINT DEFAULT 0,
    duration FLOAT DEFAULT 0,
    title VARCHAR(255) DEFAULT '',
    note TEXT DEFAULT '',
    tags TEXT DEFAULT '',
    status VARCHAR(20) DEFAULT 'active',
    category VARCHAR(100) DEFAULT '',
    sha256 VARCHAR(64) DEFAULT '',
    metadata_json JSONB DEFAULT '{}',

    scheduled_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    youtube_video_id VARCHAR(255) DEFAULT '',
    is_used BOOLEAN DEFAULT false,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_media_channel_id ON media_items(channel_id);
CREATE INDEX IF NOT EXISTS idx_media_asset_type ON media_items(asset_type);
CREATE INDEX IF NOT EXISTS idx_media_status ON media_items(status);

-- ============================================================
-- PRODUCTION JOBS
-- ============================================================
CREATE TABLE IF NOT EXISTS production_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    video_source VARCHAR(255) DEFAULT '',
    num_songs INT DEFAULT 0,
    no_mp3 BOOLEAN DEFAULT false,
    no_sfx BOOLEAN DEFAULT false,
    sfx_file VARCHAR(255) DEFAULT '',
    intro_file VARCHAR(255) DEFAULT '',
    mp3_file VARCHAR(255) DEFAULT '',
    duration_mode VARCHAR(20) DEFAULT 'mp3',
    custom_duration INT DEFAULT 0,

    production_mode VARCHAR(20) DEFAULT 'v2',
    production_method VARCHAR(40) DEFAULT 'ready_video',

    mp3_mode VARCHAR(20) DEFAULT 'shuffle',
    tail_length FLOAT DEFAULT 2.0,
    slowmo_percent FLOAT DEFAULT 0,

    merge_count INT DEFAULT 3,
    merge_resolution VARCHAR(20) DEFAULT '1920x1080',
    merge_transition_enabled BOOLEAN DEFAULT true,
    merge_transition_name VARCHAR(50) DEFAULT 'fade',
    merge_transition_duration FLOAT DEFAULT 0.5,
    merge_speed FLOAT DEFAULT 1.0,
    dynamic_output_count INT DEFAULT 1,

    status VARCHAR(20) DEFAULT 'pending',
    progress INT DEFAULT 0,
    audio_status VARCHAR(20) DEFAULT 'pending',
    video_status VARCHAR(20) DEFAULT 'pending',
    final_status VARCHAR(20) DEFAULT 'pending',

    audio_path TEXT DEFAULT '',
    video_path TEXT DEFAULT '',
    final_path TEXT DEFAULT '',
    audio_duration FLOAT DEFAULT 0,
    error_message TEXT DEFAULT '',
    process_status VARCHAR(20) DEFAULT '',
    output_filename VARCHAR(255) DEFAULT '',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_production_channel_id ON production_jobs(channel_id);
CREATE INDEX IF NOT EXISTS idx_production_status ON production_jobs(status);

-- ============================================================
-- UPLOAD BATCHES
-- ============================================================
CREATE TABLE IF NOT EXISTS upload_batches (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) DEFAULT '',
    status VARCHAR(20) DEFAULT 'pending',
    total_items INT DEFAULT 0,
    done_items INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_upload_batches_channel_id ON upload_batches(channel_id);

-- ============================================================
-- UPLOAD BATCH ITEMS
-- ============================================================
CREATE TABLE IF NOT EXISTS upload_batch_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    upload_batch_id UUID REFERENCES upload_batches(id) ON DELETE CASCADE,
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    media_item_id UUID REFERENCES media_items(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    title VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    tags TEXT DEFAULT '',
    youtube_video_id VARCHAR(255) DEFAULT '',
    scheduled_at TIMESTAMPTZ,
    visibility VARCHAR(20) DEFAULT 'private',
    status VARCHAR(20) DEFAULT 'pending',
    last_error TEXT DEFAULT '',
    progress INT DEFAULT 0,
    source_path TEXT DEFAULT '',
    thumbnail_path TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_upload_items_batch_id ON upload_batch_items(upload_batch_id);
CREATE INDEX IF NOT EXISTS idx_upload_items_status ON upload_batch_items(status);

-- ============================================================
-- LIVE JOBS
-- ============================================================
CREATE TABLE IF NOT EXISTS live_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    title VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    tags TEXT DEFAULT '',
    video_source VARCHAR(255) DEFAULT '',
    use_mp3 BOOLEAN DEFAULT false,
    use_sfx BOOLEAN DEFAULT false,
    stream_key VARCHAR(255) DEFAULT '',
    broadcast_id VARCHAR(255) DEFAULT '',
    quality VARCHAR(10) DEFAULT 'high',
    visibility VARCHAR(20) DEFAULT 'public',
    duration_hours INT DEFAULT 2,
    start_at_utc TIMESTAMPTZ,
    end_at_utc TIMESTAMPTZ,
    made_for_kids BOOLEAN DEFAULT false,
    thumbnail_path TEXT DEFAULT '',

    status VARCHAR(20) DEFAULT 'pending',
    process_id INT DEFAULT 0,
    error_message TEXT DEFAULT '',
    reconnect_count INT DEFAULT 0,
    reconnect_attempts INT DEFAULT 0,
    last_health_check TIMESTAMPTZ,
    stop_requested BOOLEAN DEFAULT false,

    stream_stats JSONB DEFAULT '{}',
    current_bitrate FLOAT DEFAULT 0,
    current_fps FLOAT DEFAULT 0,
    viewer_count INT DEFAULT 0,
    frame_drop_count INT DEFAULT 0,

    error_category VARCHAR(20) DEFAULT '',
    retry_count INT DEFAULT 0,
    last_retry_at TIMESTAMPTZ,
    max_retries INT DEFAULT 3,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_live_jobs_channel_id ON live_jobs(channel_id);
CREATE INDEX IF NOT EXISTS idx_live_jobs_status ON live_jobs(status);

-- ============================================================
-- SHORTS JOBS
-- ============================================================
CREATE TABLE IF NOT EXISTS shorts_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    long_upload_id UUID REFERENCES upload_batch_items(id) ON DELETE SET NULL,
    long_youtube_url TEXT DEFAULT '',
    long_title VARCHAR(255) DEFAULT '',
    short_count INT DEFAULT 3,
    short_duration INT DEFAULT 60,
    segment_mode VARCHAR(10) DEFAULT 'auto',
    description_template TEXT DEFAULT '',

    upload_time_1 VARCHAR(10) DEFAULT '09:00',
    upload_time_2 VARCHAR(10) DEFAULT '10:00',
    upload_time_3 VARCHAR(10) DEFAULT '11:00',

    status VARCHAR(20) DEFAULT 'created',
    error_message TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_shorts_jobs_channel_id ON shorts_jobs(channel_id);

-- ============================================================
-- SHORTS ITEMS
-- ============================================================
CREATE TABLE IF NOT EXISTS shorts_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id UUID REFERENCES shorts_jobs(id) ON DELETE CASCADE,

    short_number INT DEFAULT 0,
    video_path TEXT DEFAULT '',
    start_second FLOAT DEFAULT 0,
    end_second FLOAT DEFAULT 0,
    title VARCHAR(255) DEFAULT '',
    description TEXT DEFAULT '',
    youtube_id VARCHAR(255) DEFAULT '',
    upload_time VARCHAR(10) DEFAULT '09:00',

    status VARCHAR(20) DEFAULT 'pending',
    error_message TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    uploaded_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_shorts_items_job_id ON shorts_items(job_id);

-- ============================================================
-- PIPELINES
-- ============================================================
CREATE TABLE IF NOT EXISTS pipelines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,

    mode VARCHAR(20) DEFAULT 'dynamic',
    upload_enabled BOOLEAN DEFAULT false,
    upload_count INT DEFAULT 1,
    live_enabled BOOLEAN DEFAULT false,
    live_count INT DEFAULT 1,
    live_duration_hours INT DEFAULT 2,
    live_quality VARCHAR(10) DEFAULT 'high',
    live_use_mp3 BOOLEAN DEFAULT false,
    live_use_sfx BOOLEAN DEFAULT false,
    shorts_enabled BOOLEAN DEFAULT false,
    shorts_count INT DEFAULT 3,

    scheduler_time VARCHAR(10) DEFAULT '08:00',
    is_active BOOLEAN DEFAULT false,
    config_json JSONB DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pipelines_channel_id ON pipelines(channel_id);

-- ============================================================
-- PIPELINE RUNS
-- ============================================================
CREATE TABLE IF NOT EXISTS pipeline_runs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    pipeline_id UUID REFERENCES pipelines(id) ON DELETE CASCADE,
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,

    status VARCHAR(20) DEFAULT 'pending',
    current_stage VARCHAR(20) DEFAULT '',
    progress INT DEFAULT 0,
    run_type VARCHAR(10) DEFAULT 'manual',
    scheduled_at TIMESTAMPTZ,
    log TEXT DEFAULT '',
    error_message TEXT DEFAULT '',
    result_json JSONB DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_id ON pipeline_runs(pipeline_id);

-- ============================================================
-- METADATA POOLS
-- ============================================================
CREATE TABLE IF NOT EXISTS metadata_title_pools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_title_pools_channel_id ON metadata_title_pools(channel_id);

CREATE TABLE IF NOT EXISTS metadata_description_pools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    description TEXT NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_desc_pools_channel_id ON metadata_description_pools(channel_id);

CREATE TABLE IF NOT EXISTS metadata_tag_pools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
    tags TEXT NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tag_pools_channel_id ON metadata_tag_pools(channel_id);

-- ============================================================
-- ASSET USAGE LOGS (cooldown tracking)
-- ============================================================
CREATE TABLE IF NOT EXISTS asset_usage_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,

    asset_key VARCHAR(255) DEFAULT '',
    asset_source VARCHAR(255) DEFAULT '',
    asset_filename VARCHAR(255) DEFAULT '',
    file_path TEXT DEFAULT '',
    asset_type VARCHAR(30) DEFAULT '',

    usage_type VARCHAR(30) DEFAULT '',
    used_for VARCHAR(255) DEFAULT '',
    usage_date DATE DEFAULT CURRENT_DATE,
    cooldown_until DATE DEFAULT (CURRENT_DATE + INTERVAL '30 days'),

    related_type VARCHAR(50) DEFAULT '',
    related_id VARCHAR(50) DEFAULT '',
    meta_json JSONB DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_asset_logs_channel_id ON asset_usage_logs(channel_id);
CREATE INDEX IF NOT EXISTS idx_asset_logs_cooldown ON asset_usage_logs(cooldown_until);

-- ============================================================
-- AUTO PRODUCTION SCHEDULES
-- ============================================================
CREATE TABLE IF NOT EXISTS auto_production_schedules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,

    target VARCHAR(30) DEFAULT 'upload_regular',
    workflow VARCHAR(30) DEFAULT 'static',
    schedule_time TIME DEFAULT '08:00:00',
    start_mode VARCHAR(10) DEFAULT 'today',
    is_active BOOLEAN DEFAULT false,
    config_json JSONB DEFAULT '{}',
    next_run_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_auto_schedule_channel_id ON auto_production_schedules(channel_id);

-- ============================================================
-- AUTO CONTROL ROOM JOBS
-- ============================================================
CREATE TABLE IF NOT EXISTS auto_control_room_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    auto_production_schedule_id UUID REFERENCES auto_production_schedules(id) ON DELETE CASCADE,
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,

    target VARCHAR(30) DEFAULT '',
    workflow VARCHAR(30) DEFAULT '',
    run_date DATE DEFAULT CURRENT_DATE,
    status VARCHAR(20) DEFAULT 'waiting',
    current_stage VARCHAR(30) DEFAULT '',
    progress INT DEFAULT 0,
    total_items INT DEFAULT 0,
    done_items INT DEFAULT 0,
    current_item_order INT DEFAULT 0,
    config_json JSONB DEFAULT '{}',
    error_message TEXT DEFAULT '',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cr_jobs_schedule_id ON auto_control_room_jobs(auto_production_schedule_id);

-- ============================================================
-- AUTO CONTROL ROOM JOB ITEMS
-- ============================================================
CREATE TABLE IF NOT EXISTS auto_control_room_job_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    auto_control_room_job_id UUID REFERENCES auto_control_room_jobs(id) ON DELETE CASCADE,

    queue_order INT DEFAULT 0,
    target VARCHAR(30) DEFAULT '',
    workflow VARCHAR(30) DEFAULT '',
    source_type VARCHAR(30) DEFAULT '',
    status VARCHAR(20) DEFAULT 'pending',
    current_stage VARCHAR(30) DEFAULT '',
    progress INT DEFAULT 0,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cr_job_items_job_id ON auto_control_room_job_items(auto_control_room_job_id);

-- ============================================================
-- AUTO SEAMLESS PROGRESS
-- ============================================================
CREATE TABLE IF NOT EXISTS auto_seamless_progresses (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,

    raw_filename VARCHAR(255) DEFAULT '',
    input_path TEXT DEFAULT '',
    output_path TEXT DEFAULT '',
    progress INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'pending',
    message TEXT DEFAULT '',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_seamless_progress_channel_id ON auto_seamless_progresses(channel_id);

-- ============================================================
-- PARTIAL UNIQUE INDEX (only on non-empty youtube_channel_id)
-- ============================================================
CREATE UNIQUE INDEX IF NOT EXISTS idx_ch_yt_unique ON channels(youtube_channel_id) WHERE youtube_channel_id IS NOT NULL AND length(youtube_channel_id) > 0;