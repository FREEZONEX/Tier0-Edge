-- A native Edge installation is one workspace. Business roles grant App access only.
CREATE TABLE workspace_business_role (
    role_key TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    user_ids JSONB NOT NULL DEFAULT '[]',
    group_ids JSONB NOT NULL DEFAULT '[]',
    revision BIGINT NOT NULL DEFAULT 1
);

-- Preserve declared roles from earlier native definitions without turning an
-- old per-App member assignment into a workspace-wide grant.
INSERT INTO workspace_business_role(role_key,name)
SELECT DISTINCT ON (r->>'id') r->>'id',r->>'name'
FROM uns_meta m, LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(m.data->'roles')='array' THEN m.data->'roles' ELSE '[]' END) r
WHERE m.kind='app' AND m.mount_id IS NULL AND m.data->'entry'->>'basePath'='/apps/'||m.id AND r->>'id'<>'' AND r->>'name'<>''
ORDER BY r->>'id',m.created_at,m.id;
