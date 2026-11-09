DO $$
DECLARE
    qualified_table regclass;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        RETURN;
    END IF;

    qualified_table := to_regclass('public.uns_timeserial');
    IF qualified_table IS NULL OR NOT EXISTS (
        SELECT 1
        FROM timescaledb_information.hypertables
        WHERE hypertable_schema = 'public'
          AND hypertable_name = 'uns_timeserial'
    ) THEN
        RETURN;
    END IF;

    -- These settings apply to newly created chunks. Existing chunks remain
    -- queryable in place and are not rewritten by this online migration.
    PERFORM set_chunk_time_interval(
        qualified_table,
        INTERVAL '7 days',
        dimension_name => '_timestamp'
    );
    PERFORM set_number_partitions(
        qualified_table,
        4,
        dimension_name => '_id'
    );

    PERFORM remove_retention_policy(qualified_table, if_exists => TRUE);
    PERFORM add_retention_policy(
        qualified_table,
        INTERVAL '10 years'
    );

    PERFORM remove_compression_policy(qualified_table, if_exists => TRUE);
    PERFORM add_compression_policy(
        qualified_table,
        compress_after => INTERVAL '7 days',
        schedule_interval => INTERVAL '1 day'
    );
END
$$;
