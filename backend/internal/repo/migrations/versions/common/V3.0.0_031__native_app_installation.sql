CREATE TABLE IF NOT EXISTS native_app_installation (
 app_id TEXT PRIMARY KEY,
 release_id TEXT NOT NULL DEFAULT '',
 attempt_release_id TEXT NOT NULL DEFAULT '',
 manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
 package_sha256 TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL DEFAULT '',
 version TEXT NOT NULL DEFAULT '',
 desired_state TEXT NOT NULL DEFAULT 'stopped' CHECK (desired_state IN ('running','stopped')),
 status TEXT NOT NULL DEFAULT 'stopped' CHECK (status IN ('running','stopped','failed','starting')),
 operation_id TEXT NOT NULL DEFAULT '',
 stage TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 revision BIGINT NOT NULL DEFAULT 0,
 created_at BIGINT NOT NULL,
 updated_at BIGINT NOT NULL
);
REVOKE ALL ON native_app_installation FROM PUBLIC;
