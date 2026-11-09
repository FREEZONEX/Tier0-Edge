ALTER TABLE fleet_adoption ADD COLUMN IF NOT EXISTS binding_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
