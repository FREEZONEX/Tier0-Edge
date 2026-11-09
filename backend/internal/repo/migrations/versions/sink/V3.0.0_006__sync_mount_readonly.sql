-- A mount belongs to the receiving connection, not the local mountpoint folder.
ALTER TABLE public.uns_meta ADD COLUMN IF NOT EXISTS mount_id text;
ALTER TABLE public.uns_meta ADD CONSTRAINT sync_mount_id_valid
    CHECK (mount_id IS NULL OR (mount_id <> '' AND kind IN ('directory','topic','collection')));
CREATE INDEX IF NOT EXISTS idx_uns_meta_mount ON public.uns_meta(mount_id,kind,id) WHERE mount_id IS NOT NULL;
COMMENT ON COLUMN public.uns_meta.mount_id IS
    'Receiving sync connection ID. NULL is local; non-NULL is an immutable mirror, including its Collection data.';

-- The source owns mirror naming. Bounded snapshots can briefly retain an old
-- name while a replacement arrives; retained Collections can keep old names
-- indefinitely. Include mirror identity to avoid blocking convergence, while
-- preserving the original naming constraints for every local resource.
DROP INDEX public.uk_uns_meta_active_name;
CREATE UNIQUE INDEX uk_uns_meta_active_name ON public.uns_meta
    ((CASE WHEN kind IN ('directory','topic') THEN 'namespace' ELSE kind END),
     (CASE WHEN kind='collection' THEN COALESCE(mount_id,'') ELSE '' END),
     (CASE WHEN mount_id IS NOT NULL THEN id ELSE '' END),
     COALESCE(data->>'parentId',''),lower(data->>'name'))
    WHERE deleted_at=0 AND kind IN ('directory','topic','collection','source');

-- A user-settable GUC alone is not an authorization boundary. Only the UNS
-- table owner (the backend service login) or a database administrator can use
-- this transaction-local escape. App/data SQL roles must remain non-owners.
CREATE FUNCTION public.sync_write_authorized() RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog AS $$
    SELECT current_setting('public.sync_write', true) = 'on' AND EXISTS (
        SELECT 1 FROM pg_roles r, pg_class c
        WHERE r.rolname = session_user AND c.oid = 'public.uns_meta'::regclass
          AND (r.rolsuper OR r.oid = c.relowner)
    )
$$;

CREATE FUNCTION public.sync_mount_write_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
    old_collection text;
    new_collection text;
BEGIN
    IF public.sync_write_authorized() THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'uns_meta' THEN
        IF TG_OP <> 'INSERT' AND OLD.mount_id IS NOT NULL THEN
            RAISE EXCEPTION 'mounted UNS resources are read-only'
                USING ERRCODE='42501', CONSTRAINT='sync_mount_readonly';
        END IF;
        IF TG_OP <> 'DELETE' THEN
            IF NEW.mount_id IS NOT NULL THEN
                RAISE EXCEPTION 'mount ownership is reserved for the sync receiver'
                    USING ERRCODE='42501', CONSTRAINT='sync_mount_readonly';
            END IF;
            IF NEW.kind IN ('directory','topic') AND EXISTS (
                SELECT 1 FROM public.uns_meta p
                WHERE p.kind='directory' AND p.id=NEW.data->>'parentId' AND p.mount_id IS NOT NULL
            ) THEN
                RAISE EXCEPTION 'cannot add or move local resources inside a mounted directory'
                    USING ERRCODE='42501', CONSTRAINT='sync_mount_readonly';
            END IF;
        END IF;
    ELSE
        IF TG_OP <> 'INSERT' THEN old_collection := OLD.collection_id; END IF;
        IF TG_OP <> 'DELETE' THEN new_collection := NEW.collection_id; END IF;
        IF EXISTS (SELECT 1 FROM public.uns_meta c WHERE c.kind='collection'
                   AND c.id IN (old_collection,new_collection) AND c.mount_id IS NOT NULL) THEN
            RAISE EXCEPTION 'mounted Collection records and samples are read-only'
                USING ERRCODE='42501', CONSTRAINT='sync_mount_readonly';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;
REVOKE ALL ON FUNCTION public.sync_mount_write_guard() FROM PUBLIC;
CREATE TRIGGER aa_sync_mount_readonly BEFORE INSERT OR UPDATE OR DELETE ON public.uns_meta
    FOR EACH ROW EXECUTE FUNCTION public.sync_mount_write_guard();
CREATE TRIGGER aa_sync_mount_readonly BEFORE INSERT OR UPDATE OR DELETE ON public.uns_record
    FOR EACH ROW EXECUTE FUNCTION public.sync_mount_write_guard();
CREATE TRIGGER aa_sync_mount_readonly BEFORE INSERT OR UPDATE OR DELETE ON public.uns_sample
    FOR EACH ROW EXECUTE FUNCTION public.sync_mount_write_guard();

-- Append a column without dropping the read-only projection or its ACLs.
CREATE OR REPLACE VIEW public.uns_collection AS
    SELECT DISTINCT id, data->>'name' AS name, data->>'description' AS description,
           data->'recordSchema' AS record_schema, data->'sampleSchema' AS sample_schema,
           revision, updated_at, mount_id
    FROM public.uns_meta WHERE kind='collection' AND deleted_at=0;
