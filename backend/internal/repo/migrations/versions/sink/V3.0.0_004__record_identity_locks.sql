-- Keep schema/reference changes exclusive while unrelated data transactions run
-- concurrently. Existing 003 record statement trigger already takes this gate.
SELECT pg_advisory_xact_lock(814206108);

CREATE OR REPLACE FUNCTION public.lock_data_record_identity() RETURNS trigger
LANGUAGE plpgsql AS $function$
DECLARE identities text[] := ARRAY[]::text[]; lock_key bigint;
BEGIN
    IF TG_TABLE_NAME = 'uns_record' THEN
        IF TG_OP <> 'INSERT' THEN identities := array_append(identities, OLD.collection_id || '/' || OLD.id); END IF;
        IF TG_OP <> 'DELETE' THEN identities := array_append(identities, NEW.collection_id || '/' || NEW.id); END IF;
    ELSE
        IF TG_OP <> 'INSERT' THEN identities := array_append(identities, OLD.collection_id || '/' || OLD.record_id); END IF;
        IF TG_OP <> 'DELETE' THEN identities := array_append(identities, NEW.collection_id || '/' || NEW.record_id); END IF;
    END IF;
    FOR lock_key IN SELECT DISTINCT hashtextextended(value, 814206109) FROM unnest(identities) AS value ORDER BY 1 LOOP
        -- SQL UPDATE may already hold a tuple lock before a row trigger runs.
        -- Never wait for the application advisory lock in that inverted order:
        -- reject the entire transaction and let the caller retry its intent.
        -- Application writers prelock their full set; these calls are reentrant.
        IF NOT pg_try_advisory_xact_lock(lock_key) THEN
            RAISE EXCEPTION 'UNS record identity is busy; retry the complete transaction'
                USING ERRCODE = '55P03';
        END IF;
    END LOOP;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$function$;

DROP TRIGGER IF EXISTS uns_record_identity_lock ON public.uns_record;
CREATE TRIGGER uns_record_identity_lock BEFORE INSERT OR UPDATE OR DELETE ON public.uns_record
FOR EACH ROW EXECUTE FUNCTION public.lock_data_record_identity();

DROP TRIGGER IF EXISTS uns_sample_metadata_lock ON public.uns_sample;
CREATE TRIGGER uns_sample_metadata_lock BEFORE INSERT OR UPDATE OR DELETE ON public.uns_sample
FOR EACH STATEMENT EXECUTE FUNCTION public.lock_record_constraints();

DROP TRIGGER IF EXISTS uns_sample_identity_lock ON public.uns_sample;
CREATE TRIGGER uns_sample_identity_lock BEFORE INSERT OR UPDATE OR DELETE ON public.uns_sample
FOR EACH ROW EXECUTE FUNCTION public.lock_data_record_identity();
