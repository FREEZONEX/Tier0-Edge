ALTER TABLE fleet_adoption DROP CONSTRAINT IF EXISTS fleet_adoption_phase_check;
ALTER TABLE fleet_adoption ADD CONSTRAINT fleet_adoption_phase_check CHECK(phase IN ('queued','prepared','committed','rejected'));
ALTER TABLE fleet_adoption DROP CONSTRAINT IF EXISTS fleet_adoption_node_id_local_source_id_key;
ALTER TABLE fleet_adoption DROP CONSTRAINT IF EXISTS fleet_adoption_node_id_managed_source_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS fleet_adoption_active_source ON fleet_adoption(node_id,local_source_id) WHERE phase<>'rejected';
CREATE UNIQUE INDEX IF NOT EXISTS fleet_adoption_active_managed ON fleet_adoption(node_id,managed_source_id) WHERE phase<>'rejected';
