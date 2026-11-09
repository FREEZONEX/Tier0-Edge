CREATE TABLE IF NOT EXISTS sys_video_camera (
    id BIGSERIAL PRIMARY KEY,
    camera_code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    location VARCHAR(256) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status SMALLINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_video_camera_code_deleted
    ON sys_video_camera(camera_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_video_camera_name ON sys_video_camera(name);

CREATE TABLE IF NOT EXISTS sys_video_channel (
    id BIGSERIAL PRIMARY KEY,
    camera_id BIGINT NOT NULL,
    channel_code VARCHAR(128) NOT NULL,
    transport VARCHAR(32) NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    credential_ciphertext TEXT NOT NULL DEFAULT '',
    rtp_type VARCHAR(16) NOT NULL DEFAULT 'tcp',
    app VARCHAR(64) NOT NULL DEFAULT 'live',
    stream VARCHAR(128) NOT NULL,
    live_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    last_error TEXT NOT NULL DEFAULT '',
    last_tested_at TIMESTAMPTZ,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_time BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sys_video_channel_code_deleted
    ON sys_video_channel(channel_code, deleted_time);
CREATE INDEX IF NOT EXISTS idx_sys_video_channel_camera_deleted
    ON sys_video_channel(camera_id, deleted_time);
