-- Freeze the public contract alongside the already frozen execution binding.
-- Old receipts retain the legacy identity; query resolves their immutable version.
ALTER TABLE public.uns_control_invocation
 ADD COLUMN capability_version VARCHAR(64) NOT NULL DEFAULT '',
 ADD COLUMN contract JSONB NOT NULL DEFAULT '{}',
 ADD COLUMN expires_at BIGINT NOT NULL DEFAULT 0 CHECK (expires_at>=0),
 ADD COLUMN principal VARCHAR(256) NOT NULL DEFAULT '';

CREATE TABLE public.uns_control_grant (
 principal VARCHAR(256) NOT NULL,
 control_code VARCHAR(128) NOT NULL,
 grant_id VARCHAR(64) NOT NULL UNIQUE,
 revision BIGINT NOT NULL CHECK(revision>0),
 capability_version VARCHAR(64) NOT NULL,
 input_schema JSONB NOT NULL,
 max_ttl_millis BIGINT NOT NULL CHECK(max_ttl_millis BETWEEN 1000 AND 300000),
 min_interval_millis BIGINT NOT NULL CHECK(min_interval_millis BETWEEN 1000 AND 86400000),
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 last_invoked_at BIGINT NOT NULL DEFAULT 0,
 updated_by BIGINT NOT NULL,
 updated_at BIGINT NOT NULL,
 PRIMARY KEY(principal,control_code)
);
