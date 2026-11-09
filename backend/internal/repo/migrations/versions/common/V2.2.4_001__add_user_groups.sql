CREATE TABLE IF NOT EXISTS sys_user_group (
    id BIGSERIAL PRIMARY KEY,
    group_name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    deleted_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sys_user_group_name ON sys_user_group(group_name, deleted_time);

CREATE TABLE IF NOT EXISTS sys_user_group_member (
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_sys_user_group_member_user ON sys_user_group_member(user_id, group_id);

CREATE TABLE IF NOT EXISTS project_user_group (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_time BIGINT NOT NULL DEFAULT 0,
    updated_time BIGINT NOT NULL DEFAULT 0,
    UNIQUE(project_id, group_id)
);

CREATE TABLE IF NOT EXISTS project_user_group_role (
    project_id BIGINT NOT NULL,
    project_user_group_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    created_time BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(project_id, project_user_group_id, role_id)
);
CREATE INDEX IF NOT EXISTS idx_project_user_group_group ON project_user_group(group_id, project_id);
CREATE INDEX IF NOT EXISTS idx_project_user_group_role_role ON project_user_group_role(project_id, role_id);
