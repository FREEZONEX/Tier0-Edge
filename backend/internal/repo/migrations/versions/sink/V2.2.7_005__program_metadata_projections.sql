-- Writable projections are backed only by uns_meta. They are not configuration tables.
CREATE OR REPLACE VIEW public.source_projection AS
SELECT m.id::bigint AS id, m.data->>'name' AS name, m.data->>'adapter' AS protocol,
 m.data->'config'->'connection' AS document, m.revision,
 COALESCE((m.data->>'configRevision')::bigint,1) AS config_revision,
 COALESCE(m.data->>'desiredState','disabled') AS desired_state,
 COALESCE((SELECT data->>'health' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'unverified') AS health,
 COALESCE((SELECT data->>'healthMessage' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'') AS health_message,
 COALESCE((SELECT data->'resources' FROM public.uns_meta WHERE kind='source_state' AND id=m.id),'[]'::jsonb) AS resources,
 COALESCE((SELECT (data->>'lastDataAt')::bigint FROM public.uns_meta WHERE kind='source_state' AND id=m.id),0) AS last_data_at,
 COALESCE((SELECT (data->>'checkedAt')::bigint FROM public.uns_meta WHERE kind='source_state' AND id=m.id),0) AS checked_at,
 m.created_at,m.updated_at,m.deleted_at
FROM public.uns_meta m WHERE kind='source';

CREATE OR REPLACE FUNCTION public.write_source_projection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'UPDATE' OR NEW.id<>OLD.id THEN RAISE EXCEPTION 'source projection supports updates of existing identity only'; END IF;
 UPDATE public.uns_meta SET data=data || jsonb_build_object('name',NEW.name,'adapter',NEW.protocol,
  'configRevision',NEW.config_revision,'desiredState',NEW.desired_state),
  revision=NEW.revision,updated_at=NEW.updated_at,deleted_at=NEW.deleted_at WHERE kind='source' AND id=OLD.id::text;
 UPDATE public.uns_meta SET data=jsonb_set(data,'{config,connection}',NEW.document) WHERE kind='source' AND id=OLD.id::text;
 UPDATE public.uns_meta SET data=data || jsonb_build_object('health',NEW.health,'healthMessage',NEW.health_message,
  'resources',NEW.resources,'lastDataAt',NEW.last_data_at,'checkedAt',NEW.checked_at),
  revision=revision+1,updated_at=NEW.updated_at WHERE kind='source_state' AND id=OLD.id::text;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS source_projection_write ON public.source_projection;
CREATE TRIGGER source_projection_write INSTEAD OF UPDATE ON public.source_projection
 FOR EACH ROW EXECUTE FUNCTION public.write_source_projection();

-- Encrypted bytes are a private read projection; credential mutations use the
-- repository's locked source JSON update, never ON CONFLICT against this view.
CREATE OR REPLACE VIEW public.source_credential_projection AS
SELECT credential.key AS id,source.id::bigint AS source_id,
 decode(credential.value->>'ciphertext','base64') AS ciphertext,
 (credential.value->>'updatedAt')::bigint AS updated_at
FROM public.uns_meta source CROSS JOIN LATERAL jsonb_each(COALESCE(source.data->'credentials','{}'::jsonb)) credential
WHERE source.kind='source';

CREATE OR REPLACE VIEW public.program_stream_projection AS
SELECT id::bigint AS id, NULL::bigint AS source_id,data->>'name' AS name,data->>'model' AS model,
 data->'document' AS document, COALESCE(data->'draftManifest','{}'::jsonb) AS draft_manifest,
 revision,COALESCE((data->>'activeVersion')::bigint,0) AS active_version,
 COALESCE((data->>'pendingVersion')::bigint,0) AS pending_version,
 data->>'status' AS status,created_at,updated_at,deleted_at
FROM public.uns_meta WHERE kind='stream' AND data->>'model'<>'script';

CREATE OR REPLACE FUNCTION public.write_program_stream_projection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE payload jsonb;
BEGIN
 IF NEW.id<=0 OR NEW.source_id IS NOT NULL OR NEW.model NOT IN ('program','publish','subscribe','request-reply') OR
  NEW.revision<=0 OR NEW.status NOT IN ('draft','enabled','disabled') OR
  jsonb_typeof(NEW.document)<>'object' OR jsonb_typeof(NEW.draft_manifest)<>'object' THEN
  RAISE EXCEPTION 'invalid program metadata';
 END IF;
 payload:=jsonb_build_object('name',NEW.name,'model',NEW.model,'document',NEW.document,
  'draftManifest',NEW.draft_manifest,'revision',NEW.revision,'activeVersion',NEW.active_version,
  'pendingVersion',NEW.pending_version,'status',NEW.status,'createdAt',NEW.created_at,
  'updatedAt',NEW.updated_at,'deletedAt',NEW.deleted_at);
 IF TG_OP='INSERT' THEN
  INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at,deleted_at)
  VALUES ('stream',NEW.id::text,payload,NEW.revision,NEW.created_at,NEW.updated_at,NEW.deleted_at);
 ELSE
  IF NEW.id<>OLD.id THEN RAISE EXCEPTION 'immutable program identity'; END IF;
  UPDATE public.uns_meta SET data=data || payload,revision=NEW.revision,updated_at=NEW.updated_at,
   deleted_at=NEW.deleted_at WHERE kind='stream' AND id=OLD.id::text AND data->>'model'=OLD.model;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS program_stream_projection_write ON public.program_stream_projection;
CREATE TRIGGER program_stream_projection_write INSTEAD OF INSERT OR UPDATE ON public.program_stream_projection
 FOR EACH ROW EXECUTE FUNCTION public.write_program_stream_projection();

CREATE OR REPLACE VIEW public.program_version_projection AS
SELECT id::bigint AS id,(data->>'streamId')::bigint AS stream_id,
 COALESCE((data->>'sourceRevision')::bigint,0) AS source_revision,
 COALESCE(data->'sourceManifest','[]'::jsonb) AS source_manifest,
 COALESCE(data->'programManifest','{}'::jsonb) AS program_manifest,
 COALESCE((data->>'activatedAt')::bigint,0) AS activated_at,data->'document' AS document,created_at
FROM public.uns_meta WHERE kind='source_plan' AND data ? 'programManifest';

CREATE OR REPLACE FUNCTION public.write_program_version_projection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.id<=0 OR NEW.stream_id<=0 OR NOT EXISTS(SELECT 1 FROM public.uns_meta
   WHERE kind='stream' AND id=NEW.stream_id::text AND data->>'model'<>'script') THEN
   RAISE EXCEPTION 'invalid program version parent'; END IF;
  INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at)
   VALUES ('source_plan',NEW.id::text,jsonb_build_object('model',(SELECT data->>'model' FROM public.uns_meta WHERE kind='stream' AND id=NEW.stream_id::text),'streamId',NEW.stream_id,
    'sourceRevision',NEW.source_revision,'sourceManifest',NEW.source_manifest,'programManifest',NEW.program_manifest,
    'activatedAt',NEW.activated_at,'document',NEW.document),1,NEW.created_at,NEW.created_at);
 ELSE
  IF NEW.id<>OLD.id OR NEW.stream_id<>OLD.stream_id OR NEW.document<>OLD.document OR
   NEW.source_manifest<>OLD.source_manifest OR NEW.program_manifest<>OLD.program_manifest OR
   NEW.source_revision<>OLD.source_revision OR OLD.activated_at>0 OR NEW.activated_at<=0 THEN
   RAISE EXCEPTION 'immutable program version'; END IF;
  UPDATE public.uns_meta SET data=jsonb_set(data,'{activatedAt}',to_jsonb(NEW.activated_at)),revision=revision+1
   WHERE kind='source_plan' AND id=OLD.id::text;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS program_version_projection_write ON public.program_version_projection;
CREATE TRIGGER program_version_projection_write INSTEAD OF INSERT OR UPDATE ON public.program_version_projection
 FOR EACH ROW EXECUTE FUNCTION public.write_program_version_projection();

CREATE UNIQUE INDEX IF NOT EXISTS uk_independent_stream_name ON public.uns_meta(lower(data->>'name'))
 WHERE kind='stream' AND deleted_at=0;

-- These are execution/relation indexes, in the same sink database as metadata.
-- Configuration parents are guarded under metadata locks by repository methods.
CREATE TABLE IF NOT EXISTS public.uns_stream_source (
 stream_id bigint NOT NULL,source_id bigint NOT NULL,PRIMARY KEY(stream_id,source_id));
CREATE INDEX IF NOT EXISTS idx_program_source_dependency ON public.uns_stream_source(source_id);
CREATE TABLE IF NOT EXISTS public.uns_stream_topic (
 stream_id bigint NOT NULL,topic_id bigint NOT NULL,PRIMARY KEY(stream_id,topic_id));
CREATE INDEX IF NOT EXISTS idx_program_topic_dependency ON public.uns_stream_topic(topic_id);
CREATE TABLE IF NOT EXISTS public.uns_stream_run (
 id varchar(64) PRIMARY KEY,source_id bigint,stream_id bigint NOT NULL,version_id bigint NOT NULL DEFAULT 0,
 entry_id varchar(128) NOT NULL DEFAULT '',model varchar(24) NOT NULL,status varchar(32) NOT NULL,
 message_id varchar(128) NOT NULL DEFAULT '',summary jsonb NOT NULL DEFAULT '{}',duration_ms bigint NOT NULL DEFAULT 0,created_at bigint NOT NULL);
CREATE INDEX IF NOT EXISTS idx_program_run_history ON public.uns_stream_run(stream_id,created_at DESC);
CREATE TABLE IF NOT EXISTS public.uns_stream_task (
 id varchar(64) PRIMARY KEY,source_id bigint,stream_id bigint NOT NULL,version_id bigint NOT NULL,
 source_revision bigint NOT NULL,entry_id varchar(128) NOT NULL DEFAULT '',
 model varchar(24) NOT NULL CHECK(model IN ('publish','subscribe','program')),
 output jsonb NOT NULL,received_at bigint NOT NULL,status varchar(32) NOT NULL DEFAULT 'pending',
 sent_at bigint NOT NULL DEFAULT 0,created_at bigint NOT NULL);
CREATE INDEX IF NOT EXISTS idx_program_delivery_pending ON public.uns_stream_task(created_at)
 WHERE model='subscribe' AND status='pending' AND sent_at=0;
CREATE TABLE IF NOT EXISTS public.uns_stream_event_match (
 id varchar(200) PRIMARY KEY,"references" jsonb NOT NULL,created_at bigint NOT NULL);
CREATE TABLE IF NOT EXISTS public.uns_stream_effect (
 run_id varchar(64) NOT NULL REFERENCES public.uns_stream_run(id),entry_id varchar(128) NOT NULL,
 call_id varchar(128) NOT NULL,op_id varchar(256) NOT NULL,
 request_hash varchar(64) NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 status varchar(24) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','dispatching','completed','unknown','rejected')),
 result jsonb NOT NULL DEFAULT 'null',created_at bigint NOT NULL,updated_at bigint NOT NULL,PRIMARY KEY(run_id,entry_id,call_id));
CREATE TABLE IF NOT EXISTS public.uns_stream_interval_cursor (
 stream_id bigint NOT NULL,version_id bigint NOT NULL,entry_id varchar(128) NOT NULL,
 interval_ms bigint NOT NULL CHECK(interval_ms>0),next_due_at bigint NOT NULL CHECK(next_due_at>0),
 updated_at bigint NOT NULL,PRIMARY KEY(stream_id,version_id,entry_id));
CREATE TABLE IF NOT EXISTS public.uns_stream_interval_outbox (
 id varchar(64) PRIMARY KEY,stream_id bigint NOT NULL,version_id bigint NOT NULL,entry_id varchar(128) NOT NULL,
 due_at bigint NOT NULL,payload jsonb NOT NULL,created_at bigint NOT NULL,sent_at bigint NOT NULL DEFAULT 0,
 UNIQUE(stream_id,version_id,entry_id,due_at));
CREATE INDEX IF NOT EXISTS idx_program_interval_pending ON public.uns_stream_interval_outbox(created_at,id) WHERE sent_at=0;
