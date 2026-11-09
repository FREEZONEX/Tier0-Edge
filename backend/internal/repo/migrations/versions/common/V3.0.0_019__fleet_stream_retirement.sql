ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS policy_only BOOLEAN NOT NULL DEFAULT false;
