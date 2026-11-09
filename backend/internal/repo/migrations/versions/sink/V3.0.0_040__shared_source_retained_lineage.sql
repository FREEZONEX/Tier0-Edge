-- Shared Source retirement retains lineage without deleting business data.
CREATE OR REPLACE FUNCTION public.check_fixed_data_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(814206108);
 IF NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='collection' AND id=NEW.collection_id AND deleted_at=0) THEN
  RAISE EXCEPTION 'missing collection' USING ERRCODE='23503';
 END IF;
 IF NEW.source_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.uns_meta WHERE kind='source' AND id=NEW.source_id AND (deleted_at=0 OR data->>'schemaVersion'='1')) THEN
  RAISE EXCEPTION 'missing source' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION public.protect_metadata_data_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind NOT IN ('source','collection') THEN
  IF TG_OP='UPDATE' THEN RETURN NEW; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' AND (OLD.deleted_at<>0 OR NEW.deleted_at=0) THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(814206108);
 -- Soft retirement preserves a shared Source identity for frozen deliveries and history.
 -- Hard DELETE retains the existing protection against losing provenance.
 IF TG_OP='UPDATE' AND OLD.kind='source' AND OLD.data->>'schemaVersion'='1' THEN RETURN NEW; END IF;
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
