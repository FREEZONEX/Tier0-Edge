-- Pin only the dedicated callback origin authenticated by node enrollment.
ALTER TABLE fleet_collaboration_node ADD COLUMN IF NOT EXISTS tls_fingerprint text NOT NULL DEFAULT '';
