-- A local installation owns its authorization identity. A source package may
-- be installed in several Projects, and deployment UUIDs can change on update.
ALTER TABLE project_app ADD COLUMN IF NOT EXISTS app_principal_id UUID;
UPDATE project_app SET app_principal_id = gen_random_uuid() WHERE app_principal_id IS NULL;
ALTER TABLE project_app ALTER COLUMN app_principal_id SET DEFAULT gen_random_uuid();
ALTER TABLE project_app ALTER COLUMN app_principal_id SET NOT NULL;
ALTER TABLE project_app ADD CONSTRAINT ck_project_app_principal_nonzero
    CHECK (app_principal_id <> '00000000-0000-0000-0000-000000000000'::uuid);

-- Include deleted rows: retiring an App never releases its authorization identity.
CREATE UNIQUE INDEX idx_project_app_principal ON project_app(app_principal_id);

CREATE FUNCTION tier0_project_app_principal_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.app_principal_id IS DISTINCT FROM OLD.app_principal_id THEN
        RAISE EXCEPTION 'project App authorization identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER project_app_principal_immutable
    BEFORE UPDATE OF app_principal_id ON project_app
    FOR EACH ROW EXECUTE FUNCTION tier0_project_app_principal_immutable();
