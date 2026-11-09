-- TASK-025 分片上传：asset_file 增加分片上传会话列（op_id 存对象存储 uploadId，
-- expire_at 为上传会话过期时间）。两列均可空，存量行不受影响。
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS op_id VARCHAR(128);
ALTER TABLE asset_file ADD COLUMN IF NOT EXISTS expire_at TIMESTAMPTZ;
