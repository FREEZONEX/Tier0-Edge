ALTER TABLE sys_ai_provider_config
    ADD COLUMN IF NOT EXISTS embedding_mode VARCHAR(16) NOT NULL DEFAULT 'inherit',
    ADD COLUMN IF NOT EXISTS embedding_base_url VARCHAR(512) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_api_key_ciphertext TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_key_suffix VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(128) NOT NULL DEFAULT '';
