-- Runtime isolation is table-level, never RLS on the business Sample hypertable.
SELECT pg_advisory_xact_lock(814206108);
SET LOCAL lock_timeout = '10s';

CREATE FUNCTION public.protect_stream_runtime_storage() RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog AS $runtime_acl$
DECLARE object record; grantee record; attribute record;
BEGIN
    FOR object IN
        SELECT c.oid,c.relname,c.relowner,c.relacl
        FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='public' AND c.relkind IN ('r','p')
          AND (left(c.relname,11)='uns_stream_' OR c.relname IN
            ('uns_metadata_event_outbox','stream_receipt','stream_event_outbox','uns_control_invocation'))
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.%I FROM PUBLIC CASCADE',object.relname);
        FOR grantee IN
            SELECT DISTINCT r.rolname FROM aclexplode(object.relacl) a
            JOIN pg_roles r ON r.oid=a.grantee WHERE r.oid<>object.relowner
        LOOP
            EXECUTE format('REVOKE ALL ON TABLE public.%I FROM %I CASCADE',object.relname,grantee.rolname);
        END LOOP;
        FOR attribute IN
            SELECT a.attname,a.attacl FROM pg_attribute a
            WHERE a.attrelid=object.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL
        LOOP
            EXECUTE format('REVOKE ALL (%I) ON TABLE public.%I FROM PUBLIC CASCADE',attribute.attname,object.relname);
            FOR grantee IN
                SELECT DISTINCT r.rolname FROM aclexplode(attribute.attacl) a
                JOIN pg_roles r ON r.oid=a.grantee WHERE r.oid<>object.relowner
            LOOP
                EXECUTE format('REVOKE ALL (%I) ON TABLE public.%I FROM %I CASCADE',attribute.attname,object.relname,grantee.rolname);
            END LOOP;
        END LOOP;
        -- Owner-only execution state, even if a future data-role grant is too broad.
        -- Backend/DBA ownership is the trusted boundary, never a caller-set GUC.
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY',object.relname);
    END LOOP;
END
$runtime_acl$;
REVOKE ALL ON FUNCTION public.protect_stream_runtime_storage() FROM PUBLIC;
SELECT public.protect_stream_runtime_storage();
