ALTER TABLE fleet_publication_operation ADD COLUMN IF NOT EXISTS cleanup_only BOOLEAN NOT NULL DEFAULT false;
