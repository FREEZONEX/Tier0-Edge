-- Edge actor provenance is not a local user identity or role.
ALTER TABLE fleet_managed_draft ADD COLUMN IF NOT EXISTS updated_actor JSONB NOT NULL DEFAULT '{}';
