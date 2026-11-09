-- uns_nodered_flow_node 主键从单列 node_id 升级为复合主键 (parent_id, node_id)，
-- 支持同一 UNS 节点同时绑定 source 与 event flow 各一条。
-- 存量数据 node_id 唯一，放宽为复合约束无数据风险；幂等可重复执行。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
          ON tc.constraint_name = kcu.constraint_name
         AND tc.table_schema = kcu.table_schema
         AND tc.table_name = kcu.table_name
        WHERE tc.table_schema = current_schema()
          AND tc.table_name = 'uns_nodered_flow_node'
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'parent_id'
    ) OR NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
          ON tc.constraint_name = kcu.constraint_name
         AND tc.table_schema = kcu.table_schema
         AND tc.table_name = kcu.table_name
        WHERE tc.table_schema = current_schema()
          AND tc.table_name = 'uns_nodered_flow_node'
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'node_id'
    ) THEN
        ALTER TABLE uns_nodered_flow_node DROP CONSTRAINT IF EXISTS uns_nodered_flow_node_pkey;
        ALTER TABLE uns_nodered_flow_node ADD CONSTRAINT uns_nodered_flow_node_pkey PRIMARY KEY(parent_id, node_id);
    END IF;
END $$;
