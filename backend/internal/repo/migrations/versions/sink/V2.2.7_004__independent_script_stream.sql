-- 脚本 Stream 独立保存，允许不绑定 Source 或引用多个 Source。
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_kind_check;
ALTER TABLE public.uns_meta ADD CONSTRAINT uns_meta_kind_check CHECK
 (kind IN ('directory','collection','source','stream','source_state','source_plan','source_version','app','subscription','task'));
CREATE INDEX IF NOT EXISTS idx_uns_meta_script_sources ON public.uns_meta USING GIN ((data->'sources'))
 WHERE kind='stream' AND deleted_at=0;
