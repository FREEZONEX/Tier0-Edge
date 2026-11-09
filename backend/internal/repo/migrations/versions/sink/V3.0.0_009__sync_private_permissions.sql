-- App/data roles must not inspect queued payloads or forge receiver mappings.
-- Remove existing explicit grants (including group roles and PUBLIC) while
-- preserving ownership. Backend synchronization runs as the UNS table owner.
DO $sync_acl$
DECLARE object record; grantee record; object_type text;
BEGIN
    FOR object IN
        SELECT c.oid,c.relname,c.relkind,c.relowner,c.relacl
        FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='public' AND left(c.relname,5)='sync_'
          AND c.relkind IN ('r','p','v','m','f','S')
    LOOP
        object_type := CASE WHEN object.relkind='S' THEN 'SEQUENCE' ELSE 'TABLE' END;
        EXECUTE format('REVOKE ALL ON %s public.%I FROM PUBLIC',object_type,object.relname);
        FOR grantee IN
            SELECT DISTINCT r.rolname FROM aclexplode(object.relacl) a
            JOIN pg_roles r ON r.oid=a.grantee
            WHERE r.oid<>object.relowner AND NOT r.rolsuper
        LOOP
            EXECUTE format('REVOKE ALL ON %s public.%I FROM %I',object_type,object.relname,grantee.rolname);
        END LOOP;
    END LOOP;
END
$sync_acl$;

-- Capture is invoked by the existing Sample trigger, including writes from
-- restricted App roles. Its owner may insert into the private queue; callers
-- cannot invoke it directly or attach it to their own tables.
ALTER FUNCTION public.sync_capture_sample() SECURITY DEFINER;
ALTER FUNCTION public.sync_capture_sample() SET search_path = pg_catalog;
REVOKE ALL ON FUNCTION public.sync_capture_sample() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.sync_write_authorized() FROM PUBLIC;

DO $sync_function_acl$
DECLARE fn record; grantee record;
BEGIN
    FOR fn IN
        SELECT p.oid,p.proowner,p.proacl FROM pg_proc p
        JOIN pg_namespace n ON n.oid=p.pronamespace
        WHERE n.nspname='public' AND p.proname IN
            ('sync_capture_sample','sync_write_authorized','sync_mount_write_guard')
    LOOP
        FOR grantee IN
            SELECT DISTINCT r.rolname FROM aclexplode(fn.proacl) a
            JOIN pg_roles r ON r.oid=a.grantee
            WHERE r.oid<>fn.proowner AND NOT r.rolsuper
        LOOP
            EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',fn.oid::regprocedure,grantee.rolname);
        END LOOP;
    END LOOP;
END
$sync_function_acl$;
