CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;
-- 
CREATE TABLE IF NOT EXISTS public."supos_timeserial_string" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" text NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_string', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_string SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近30天的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_string', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_string', INTERVAL '12 hours');


CREATE TABLE IF NOT EXISTS public."supos_timeserial_integer" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" int4 NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_integer', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_integer SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_integer', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_integer', INTERVAL '12 hours');

CREATE TABLE IF NOT EXISTS public."supos_timeserial_long" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" int8 NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_long', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_long SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_long', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_long', INTERVAL '12 hours');


CREATE TABLE IF NOT EXISTS public."supos_timeserial_double" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" float8 NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_double', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_double SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_double', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_double', INTERVAL '12 hours');


CREATE TABLE IF NOT EXISTS public."supos_timeserial_float" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" float4 NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_float', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_float SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_float', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_float', INTERVAL '12 hours');

CREATE TABLE IF NOT EXISTS public."supos_timeserial_boolean" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" BOOLEAN  NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_boolean', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_boolean SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_boolean', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_boolean', INTERVAL '12 hours');

CREATE TABLE IF NOT EXISTS public."supos_timeserial_datetime" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" timestamptz(3) NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_datetime', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 50,        -- 按位号分50区
    chunk_time_interval => INTERVAL '6 hours'
);

ALTER TABLE supos_timeserial_datetime SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_datetime', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_datetime', INTERVAL '24 hours');

CREATE TABLE IF NOT EXISTS public."supos_timeserial_blob" (
    "tag_name" varchar(200) NOT NULL,
    "timeStamp" timestamptz(3) NOT NULL DEFAULT now(),
    "status" int8 default 0,
    "value" varchar(512) NULL,
    "_st" timestamptz(3) DEFAULT now() NULL,
    PRIMARY KEY ("tag_name", "timeStamp") 
);
SELECT create_hypertable (
    'supos_timeserial_blob', 
    'timeStamp',
    partitioning_column => 'tag_name',
    number_partitions => 10,        -- 按位号分50区
    chunk_time_interval => INTERVAL '1 day'
);

ALTER TABLE supos_timeserial_blob SET (
    timescaledb.compress,                          -- 启用压缩
    timescaledb.compress_segmentby = 'tag_name',   -- 按设备分组压缩
    timescaledb.compress_orderby = '"timeStamp" DESC'      -- 按时间降序排序
);

-- 保留最近2年的数据，自动删除旧分区
SELECT add_retention_policy('supos_timeserial_blob', INTERVAL '2 years');
-- 压缩每天的数据
SELECT add_compression_policy('supos_timeserial_blob', INTERVAL '24 hours');