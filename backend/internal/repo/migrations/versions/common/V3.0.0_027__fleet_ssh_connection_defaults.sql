ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS default_username text NOT NULL DEFAULT '';
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS default_port integer NOT NULL DEFAULT 0;
ALTER TABLE fleet_ssh_profile ADD COLUMN IF NOT EXISTS connection_mode text NOT NULL DEFAULT 'automatic';
ALTER TABLE fleet_ssh_terminal ADD COLUMN IF NOT EXISTS username VARCHAR(128) NOT NULL DEFAULT '';
