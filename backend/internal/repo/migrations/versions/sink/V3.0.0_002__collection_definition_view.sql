-- Collection definitions only. Relations belong to Topic, Source bindings and
-- App grants; old single-value projections on Collection were not maintained.
-- The migrator executes this whole file and its ledger update in one transaction.
DO $migration$
DECLARE
    previous_owner text;
    previous_acl aclitem[];
    permission_row record;
    grantee_name text;
BEGIN
    SELECT pg_get_userbyid(relowner), relacl
      INTO previous_owner, previous_acl
      FROM pg_class WHERE oid = to_regclass('public.uns_collection');

    -- Do not silently discard custom column grants when changing the contract.
    IF EXISTS (SELECT 1 FROM pg_attribute
               WHERE attrelid = to_regclass('public.uns_collection') AND attacl IS NOT NULL) THEN
        RAISE EXCEPTION 'public.uns_collection has custom column grants; review them before upgrading the view';
    END IF;

    -- No CASCADE: dependent SQL objects must be explicitly adapted by their owner.
    DROP VIEW IF EXISTS public.uns_collection;
    -- DISTINCT intentionally makes this projection non-updatable even for its
    -- owner. (kind,id) already makes the selected Collection rows unique.
    CREATE VIEW public.uns_collection AS
        SELECT DISTINCT id,
               data->>'name' AS name,
               data->>'description' AS description,
               data->'recordSchema' AS record_schema,
               data->'sampleSchema' AS sample_schema,
               revision, updated_at
          FROM public.uns_meta
         WHERE kind='collection' AND deleted_at=0;

    -- Remove inherited default ACLs, then preserve existing SELECT grants only.
    -- Recreating the view must not expose definitions to additional DB roles.
    FOR permission_row IN
        SELECT DISTINCT a.grantee FROM pg_class c,
             LATERAL aclexplode(c.relacl) a
         WHERE c.oid='public.uns_collection'::regclass AND a.grantee<>c.relowner
    LOOP
        grantee_name := CASE WHEN permission_row.grantee=0 THEN 'PUBLIC'
                            ELSE quote_ident(pg_get_userbyid(permission_row.grantee)) END;
        EXECUTE 'REVOKE ALL PRIVILEGES ON public.uns_collection FROM ' || grantee_name;
    END LOOP;
    FOR permission_row IN
        SELECT grantee, bool_or(is_grantable) AS grantable
          FROM aclexplode(previous_acl)
         WHERE privilege_type='SELECT'
           AND pg_get_userbyid(grantee) IS DISTINCT FROM previous_owner
         GROUP BY grantee
    LOOP
        grantee_name := CASE WHEN permission_row.grantee=0 THEN 'PUBLIC'
                            ELSE quote_ident(pg_get_userbyid(permission_row.grantee)) END;
        EXECUTE 'GRANT SELECT ON public.uns_collection TO ' || grantee_name ||
                CASE WHEN permission_row.grantable THEN ' WITH GRANT OPTION' ELSE '' END;
    END LOOP;
    IF previous_owner IS NOT NULL THEN
        EXECUTE format('ALTER VIEW public.uns_collection OWNER TO %I', previous_owner);
    END IF;
END
$migration$;

COMMENT ON VIEW public.uns_collection IS
    'Read-only Collection definitions from active uns_meta rows; no separate data, business records, relationship expansion or application authorization.';
COMMENT ON COLUMN public.uns_collection.id IS 'ID of uns_meta(kind=collection); Record.collection_id references this identity.';
COMMENT ON COLUMN public.uns_collection.name IS 'Collection display name from data.name.';
COMMENT ON COLUMN public.uns_collection.description IS 'Optional description from data.description; NULL when absent.';
COMMENT ON COLUMN public.uns_collection.record_schema IS 'JSON Schema for uns_record.data from data.recordSchema.';
COMMENT ON COLUMN public.uns_collection.sample_schema IS 'Optional JSON Schema for uns_sample.data; NULL does not imply an empty schema.';
COMMENT ON COLUMN public.uns_collection.revision IS 'Revision of the Collection metadata definition.';
COMMENT ON COLUMN public.uns_collection.updated_at IS 'Collection definition update time, Unix milliseconds.';
