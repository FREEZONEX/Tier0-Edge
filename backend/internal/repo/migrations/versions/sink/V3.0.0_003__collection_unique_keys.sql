-- Record business uniqueness is enforced by Collection indexes built by the index worker.
-- Metadata exclusion is shared by all Record/Sample writers independently of indexes.
SELECT pg_advisory_xact_lock(814206108);
CREATE OR REPLACE FUNCTION public.lock_record_constraints() RETURNS trigger
LANGUAGE plpgsql AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock_shared(814206108);
    RETURN NULL;
END
$function$;
DROP TRIGGER IF EXISTS uns_record_constraints_lock ON public.uns_record;
CREATE TRIGGER uns_record_constraints_lock BEFORE INSERT OR UPDATE OR DELETE ON public.uns_record
    FOR EACH STATEMENT EXECUTE FUNCTION public.lock_record_constraints();
