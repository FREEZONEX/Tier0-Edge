-- Namespace recycling retains Collection records/samples. Permanent deletion remains guarded.
SELECT pg_advisory_xact_lock(814206108);
CREATE OR REPLACE FUNCTION public.protect_metadata_data_references() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind NOT IN ('source','collection') THEN
  IF TG_OP='UPDATE' THEN RETURN NEW; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' AND (OLD.deleted_at<>0 OR NEW.deleted_at=0) THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(814206108);
 -- Soft retirement retains Collection data and shared Source lineage.
 -- Hard DELETE retains the existing protection against losing provenance.
 IF TG_OP='UPDATE' AND (OLD.kind='collection' OR (OLD.kind='source' AND OLD.data->>'schemaVersion'='1')) THEN RETURN NEW; END IF;
 IF OLD.kind='collection' AND EXISTS(SELECT 1 FROM public.uns_record WHERE collection_id=OLD.id) THEN
  RAISE EXCEPTION 'collection has records' USING ERRCODE='23503';
 END IF;
 IF OLD.kind='source' AND (EXISTS(SELECT 1 FROM public.uns_record WHERE source_id=OLD.id) OR
   EXISTS(SELECT 1 FROM public.uns_sample WHERE source_id=OLD.id)) THEN
  RAISE EXCEPTION 'source has retained data' USING ERRCODE='23503';
 END IF;
 IF TG_OP='UPDATE' THEN RETURN NEW; END IF;
 RETURN OLD;
END $$;
