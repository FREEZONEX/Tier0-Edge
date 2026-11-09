-- UNS 3.0.0: split tree roles without copying Collection or business data.
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_kind_check;
UPDATE public.uns_meta SET kind='topic', data=data-'nodeType'
 WHERE kind='directory' AND data->>'nodeType'='topic';
UPDATE public.uns_meta SET data=data-'nodeType' WHERE kind='directory';
ALTER TABLE public.uns_meta ADD CONSTRAINT uns_meta_kind_check CHECK
 (kind IN ('directory','topic','collection','source','stream','source_state','source_plan','source_version','app','subscription','task','index_state'));
DROP INDEX IF EXISTS public.uk_uns_meta_active_name;
CREATE UNIQUE INDEX uk_uns_meta_active_name ON public.uns_meta
 ((CASE WHEN kind IN ('directory','topic') THEN 'namespace' ELSE kind END),
  COALESCE(data->>'parentId',''),lower(data->>'name'))
 WHERE deleted_at=0 AND kind IN ('directory','topic','collection','source');

-- Keep fixed-kind reference checks without storing redundant discriminator columns.
ALTER TABLE public.uns_record DROP CONSTRAINT IF EXISTS uns_record_collection_kind_collection_id_fkey;
ALTER TABLE public.uns_record DROP CONSTRAINT IF EXISTS uns_record_source_kind_source_id_fkey;
ALTER TABLE public.uns_sample DROP CONSTRAINT IF EXISTS uns_sample_source_kind_source_id_fkey;
ALTER TABLE public.uns_meta DROP CONSTRAINT IF EXISTS uns_meta_source_kind_id_fkey;
ALTER TABLE public.uns_record DROP COLUMN IF EXISTS collection_kind;
ALTER TABLE public.uns_record DROP COLUMN IF EXISTS source_kind;
ALTER TABLE public.uns_sample DROP COLUMN IF EXISTS source_kind;
ALTER TABLE public.uns_meta DROP COLUMN IF EXISTS source_kind;

CREATE OR REPLACE FUNCTION public.check_fixed_data_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(814206108);
 IF NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='collection' AND id=NEW.collection_id AND deleted_at=0) THEN
  RAISE EXCEPTION 'missing collection' USING ERRCODE='23503';
 END IF;
 IF NEW.source_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='source' AND id=NEW.source_id AND deleted_at=0) THEN
  RAISE EXCEPTION 'missing source' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER uns_record_fixed_references BEFORE INSERT OR UPDATE OF collection_id,source_id
 ON public.uns_record FOR EACH ROW EXECUTE FUNCTION public.check_fixed_data_references();
CREATE TRIGGER uns_sample_fixed_references BEFORE INSERT OR UPDATE OF collection_id,source_id
 ON public.uns_sample FOR EACH ROW EXECUTE FUNCTION public.check_fixed_data_references();

CREATE OR REPLACE FUNCTION public.protect_metadata_data_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind NOT IN ('source','collection') THEN
  IF TG_OP='UPDATE' THEN RETURN NEW; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' AND (OLD.deleted_at<>0 OR NEW.deleted_at=0) THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(814206108);
 IF OLD.kind='collection' AND EXISTS(SELECT 1 FROM public.uns_record WHERE collection_id=OLD.id AND (TG_OP='DELETE' OR deleted_at IS NULL)) THEN
  RAISE EXCEPTION 'collection has records' USING ERRCODE='23503';
 END IF;
 IF OLD.kind='source' AND (EXISTS(SELECT 1 FROM public.uns_record WHERE source_id=OLD.id) OR
   EXISTS(SELECT 1 FROM public.uns_sample WHERE source_id=OLD.id)) THEN
  RAISE EXCEPTION 'source has retained data' USING ERRCODE='23503';
 END IF;
 IF TG_OP='UPDATE' THEN RETURN NEW; END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER uns_meta_data_delete_guard BEFORE DELETE ON public.uns_meta
 FOR EACH ROW EXECUTE FUNCTION public.protect_metadata_data_references();
CREATE TRIGGER uns_meta_data_archive_guard BEFORE UPDATE OF deleted_at ON public.uns_meta
 FOR EACH ROW EXECUTE FUNCTION public.protect_metadata_data_references();
