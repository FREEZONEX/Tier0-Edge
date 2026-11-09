-- Upgrade only this in-development metadata schema, without importing legacy
-- uns_source/uns_stream data or deleting their stored configuration.
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_kind_check;
ALTER TABLE public.uns_meta ALTER COLUMN id TYPE VARCHAR(256);
ALTER TABLE public.uns_record DROP CONSTRAINT IF EXISTS uns_record_collection_id_fkey;
ALTER TABLE public.uns_record DROP CONSTRAINT IF EXISTS uns_record_collection_kind_collection_id_fkey;
ALTER TABLE public.uns_record DROP CONSTRAINT IF EXISTS uns_record_source_kind_source_id_fkey;
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_source_kind_id_fkey;
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_pkey;
ALTER TABLE public.uns_meta ADD PRIMARY KEY (kind,id);
ALTER TABLE public.uns_meta ADD COLUMN IF NOT EXISTS source_kind VARCHAR(16)
    GENERATED ALWAYS AS (CASE WHEN kind='source_state' THEN 'source' END) STORED;
ALTER TABLE public.uns_meta ADD FOREIGN KEY (source_kind,id) REFERENCES public.uns_meta(kind,id);
DROP INDEX IF EXISTS public.uk_uns_meta_active_name;
UPDATE public.uns_meta SET data=data || jsonb_build_object('nodeType',kind),kind='directory'
    WHERE kind IN ('folder','topic');
ALTER TABLE public.uns_meta ADD CONSTRAINT uns_meta_kind_check CHECK
    (kind IN ('directory','collection','source','source_state','source_plan','source_version','app','subscription','task'));
CREATE UNIQUE INDEX uk_uns_meta_active_name ON public.uns_meta
    (kind,(COALESCE(data->>'parentId','')),(lower(data->>'name')))
    WHERE deleted_at=0 AND kind IN ('directory','collection','source');
CREATE INDEX IF NOT EXISTS idx_uns_meta_streams ON public.uns_meta USING GIN ((data->'config'->'streams'))
    WHERE kind='source' AND deleted_at=0;
CREATE INDEX IF NOT EXISTS idx_uns_meta_task_type_time ON public.uns_meta
    ((data->>'type'),created_at DESC) WHERE kind='task';
ALTER TABLE public.uns_record ADD COLUMN IF NOT EXISTS collection_kind VARCHAR(16) NOT NULL DEFAULT 'collection' CHECK (collection_kind='collection');
ALTER TABLE public.uns_record ADD COLUMN IF NOT EXISTS source_kind VARCHAR(16) NOT NULL DEFAULT 'source' CHECK (source_kind='source');
ALTER TABLE public.uns_record ADD COLUMN IF NOT EXISTS source_id VARCHAR(128);
ALTER TABLE public.uns_record ADD FOREIGN KEY (collection_kind,collection_id) REFERENCES public.uns_meta(kind,id);
ALTER TABLE public.uns_record ADD FOREIGN KEY (source_kind,source_id) REFERENCES public.uns_meta(kind,id);
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS source_kind VARCHAR(16) NOT NULL DEFAULT 'source' CHECK (source_kind='source');
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS source_id VARCHAR(128);
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS stream_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS epoch VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.uns_sample ADD COLUMN IF NOT EXISTS producer_id VARCHAR(256) NOT NULL DEFAULT '';
ALTER TABLE public.uns_sample ADD FOREIGN KEY (source_kind,source_id) REFERENCES public.uns_meta(kind,id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_uns_sample_source_sequence ON public.uns_sample (time,source_id,epoch,seq)
    WHERE source_id IS NOT NULL;
CREATE OR REPLACE VIEW public.uns_collection AS
    SELECT DISTINCT id,data->>'name' AS name,data->'recordSchema' AS schema,
           data->'sampleSchema' AS sample_schema,data->>'ownerApp' AS owner_app_id,
           data->>'managedSource' AS managed_source,data->>'managedStream' AS managed_stream,
           data->>'kind' AS kind,data->'references' AS "references",revision,updated_at
    FROM public.uns_meta WHERE kind='collection' AND deleted_at=0;
