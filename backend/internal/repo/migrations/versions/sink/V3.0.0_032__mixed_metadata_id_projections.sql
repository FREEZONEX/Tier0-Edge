-- Legacy program execution uses int64 IDs. Modern metadata also contains UUID
-- and arbitrary string IDs; PostgreSQL may evaluate a cast before a WHERE
-- filter. A total conversion function makes every evaluation order safe.
-- This changes only projections, never metadata identities or stored payloads.
CREATE OR REPLACE FUNCTION public.uns_legacy_numeric_id(value text) RETURNS bigint
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT CASE WHEN value ~ '^[1-9][0-9]{0,18}$'
  AND (length(value)<19 OR value COLLATE "C" <= '9223372036854775807')
 THEN value::bigint ELSE NULL END
$$;

CREATE OR REPLACE VIEW public.source_projection AS
SELECT public.uns_legacy_numeric_id(m.id) AS id, m.data->>'name' AS name, m.data->>'adapter' AS protocol,
 m.data->'config'->'connection' AS document, m.revision,
 COALESCE((m.data->>'configRevision')::bigint,1) AS config_revision,
 COALESCE(m.data->>'desiredState','disabled') AS desired_state,
 COALESCE((SELECT data->>'health' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'unverified') AS health,
 COALESCE((SELECT data->>'healthMessage' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'') AS health_message,
 COALESCE((SELECT data->'resources' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'[]'::jsonb) AS resources,
 COALESCE((SELECT (data->>'lastDataAt')::bigint FROM public.uns_meta WHERE kind='source_state' AND id=m.id),0) AS last_data_at,
 COALESCE((SELECT (data->>'checkedAt')::bigint FROM public.uns_meta WHERE kind='source_state' AND id=m.id),0) AS checked_at,
 m.created_at,m.updated_at,m.deleted_at
FROM public.uns_meta m WHERE kind='source' AND public.uns_legacy_numeric_id(m.id) IS NOT NULL;

CREATE OR REPLACE VIEW public.source_credential_projection AS
SELECT credential.key AS id,public.uns_legacy_numeric_id(source.id) AS source_id,
 decode(credential.value->>'ciphertext','base64') AS ciphertext,
 (credential.value->>'updatedAt')::bigint AS updated_at
FROM public.uns_meta source CROSS JOIN LATERAL jsonb_each(COALESCE(source.data->'credentials','{}'::jsonb)) credential
WHERE source.kind='source' AND public.uns_legacy_numeric_id(source.id) IS NOT NULL;

CREATE OR REPLACE VIEW public.program_stream_projection AS
SELECT public.uns_legacy_numeric_id(id) AS id, NULL::bigint AS source_id,data->>'name' AS name,data->>'model' AS model,
 data->'document' AS document, COALESCE(data->'draftManifest','{}'::jsonb) AS draft_manifest,
 revision,COALESCE((data->>'activeVersion')::bigint,0) AS active_version,
 COALESCE((data->>'pendingVersion')::bigint,0) AS pending_version,
 data->>'status' AS status,created_at,updated_at,deleted_at
FROM public.uns_meta WHERE kind='stream' AND data->>'model'<>'script' AND public.uns_legacy_numeric_id(id) IS NOT NULL;

CREATE OR REPLACE VIEW public.program_version_projection AS
SELECT public.uns_legacy_numeric_id(id) AS id,public.uns_legacy_numeric_id(data->>'streamId') AS stream_id,
 COALESCE((data->>'sourceRevision')::bigint,0) AS source_revision,
 COALESCE(data->'sourceManifest','[]'::jsonb) AS source_manifest,
 COALESCE(data->'programManifest','{}'::jsonb) AS program_manifest,
 COALESCE((data->>'activatedAt')::bigint,0) AS activated_at,data->'document' AS document,created_at
FROM public.uns_meta WHERE kind='source_plan' AND data ? 'programManifest' AND public.uns_legacy_numeric_id(id) IS NOT NULL AND public.uns_legacy_numeric_id(data->>'streamId') IS NOT NULL;

