-- Keep Sample identity stable even though a hypertable's unique indexes must
-- include its time dimension. The existing record-identity trigger serializes
-- writers before this check, including direct SQL and sync receivers.
CREATE OR REPLACE FUNCTION public.check_sample_logical_identity() RETURNS trigger
LANGUAGE plpgsql AS $function$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.collection_id,NEW.record_id,NEW.id,NEW.time)
        IS NOT DISTINCT FROM (OLD.collection_id,OLD.record_id,OLD.id,OLD.time) THEN
        RETURN NEW;
    END IF;
    -- A repeatable-read snapshot can predate the identity lock and miss a
    -- committed row in another chunk. Serializable writers have SSI protection;
    -- normal ingestion uses read committed. Reject unsafe snapshot-only writes.
    IF current_setting('transaction_isolation')='repeatable read' THEN
        RAISE EXCEPTION 'Sample identity writes require READ COMMITTED or SERIALIZABLE isolation'
            USING ERRCODE='0A000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM public.uns_sample s
        WHERE s.collection_id=NEW.collection_id AND s.record_id=NEW.record_id
          AND s.id=NEW.id AND s.time<>NEW.time
          AND (TG_OP='INSERT' OR (s.collection_id,s.record_id,s.id,s.time)
               IS DISTINCT FROM (OLD.collection_id,OLD.record_id,OLD.id,OLD.time))
    ) THEN
        RAISE EXCEPTION 'Sample identity already exists at a different time'
            USING ERRCODE='23505', CONSTRAINT='uns_sample_logical_identity';
    END IF;
    RETURN NEW;
END
$function$;

-- Called after all sink migrations on every Backend startup. A database that
-- initially lacks TimescaleDB can therefore be promoted on a later restart.
-- Conversion is atomic: any unsupported feature rolls back the PK/trigger
-- changes together with the conversion, leaving the original ordinary table.
CREATE OR REPLACE FUNCTION public.configure_sample_timeseries(
    retention_years integer DEFAULT 10, compression_after_days integer DEFAULT 90
) RETURNS text LANGUAGE plpgsql AS $function$
DECLARE
    hypertable_exists boolean;
    previous_lock_timeout text;
BEGIN
    IF retention_years<=0 OR compression_after_days<0 THEN
        RAISE EXCEPTION 'invalid Sample storage policy' USING ERRCODE='22023';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname='timescaledb') THEN
        RETURN 'plain';
    END IF;
    BEGIN
        SELECT EXISTS (
            SELECT 1 FROM timescaledb_information.hypertables
            WHERE hypertable_schema='public' AND hypertable_name='uns_sample'
        ) INTO hypertable_exists;
    EXCEPTION WHEN OTHERS THEN
        RAISE WARNING 'Sample TimescaleDB catalog unavailable; keep existing storage: %', SQLERRM;
        RETURN 'unavailable';
    END;
    IF NOT hypertable_exists THEN
        previous_lock_timeout := current_setting('lock_timeout');
        BEGIN
            PERFORM set_config('lock_timeout','5s',true);
            IF NOT pg_try_advisory_xact_lock(814206108) THEN
                RAISE EXCEPTION 'UNS metadata is busy' USING ERRCODE='55P03';
            END IF;
            -- Lock referenced ordinary tables first, as required for conversion
            -- with retained data and foreign keys. Never drop those references.
            LOCK TABLE public.uns_meta,public.uns_record IN SHARE ROW EXCLUSIVE MODE;
            ALTER TABLE public.uns_sample DROP CONSTRAINT uns_sample_pkey;
            ALTER TABLE public.uns_sample ADD CONSTRAINT uns_sample_pkey
                PRIMARY KEY(collection_id,record_id,id,time);
            CREATE TRIGGER uns_sample_logical_identity BEFORE INSERT OR UPDATE ON public.uns_sample
                FOR EACH ROW EXECUTE FUNCTION public.check_sample_logical_identity();
            PERFORM create_hypertable('public.uns_sample'::regclass,'time',
                chunk_time_interval=>INTERVAL '7 days',migrate_data=>true,if_not_exists=>true);
        EXCEPTION WHEN OTHERS THEN
            RAISE WARNING 'Sample hypertable conversion unavailable; use plain PostgreSQL table: %', SQLERRM;
            PERFORM set_config('lock_timeout',previous_lock_timeout,true);
            RETURN 'plain';
        END;
        PERFORM set_config('lock_timeout',previous_lock_timeout,true);
    END IF;
    -- Optional optimizations must not prevent Sample reads/writes or startup.
    BEGIN
        IF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.jobs
            WHERE hypertable_schema='public' AND hypertable_name='uns_sample'
              AND proc_name='policy_retention'
              AND (config->>'drop_after')::interval=make_interval(years=>retention_years)
        ) THEN
            PERFORM remove_retention_policy('public.uns_sample'::regclass,if_exists=>true);
            PERFORM add_retention_policy('public.uns_sample'::regclass,make_interval(years=>retention_years));
        END IF;
    EXCEPTION WHEN OTHERS THEN
        RAISE WARNING 'Sample retention policy unavailable: %', SQLERRM;
    END;
    BEGIN
        IF compression_after_days=0 THEN
            PERFORM remove_compression_policy('public.uns_sample'::regclass,if_exists=>true);
        ELSIF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.jobs
            WHERE hypertable_schema='public' AND hypertable_name='uns_sample'
              AND proc_name='policy_compression'
              AND (config->>'compress_after')::interval=make_interval(days=>compression_after_days)
              AND schedule_interval=INTERVAL '7 days'
        ) THEN
            ALTER TABLE public.uns_sample SET (
                timescaledb.compress,
                timescaledb.compress_segmentby='collection_id,record_id',
                timescaledb.compress_orderby='time DESC,id DESC'
            );
            PERFORM remove_compression_policy('public.uns_sample'::regclass,if_exists=>true);
            PERFORM add_compression_policy('public.uns_sample'::regclass,
                compress_after=>make_interval(days=>compression_after_days),
                schedule_interval=>INTERVAL '7 days',initial_start=>now()+INTERVAL '7 days');
        END IF;
    EXCEPTION WHEN OTHERS THEN
        RAISE WARNING 'Sample compression policy unavailable: %', SQLERRM;
    END;
    RETURN 'hypertable';
END
$function$;
REVOKE ALL ON FUNCTION public.configure_sample_timeseries(integer,integer) FROM PUBLIC;
