-- Connection identity belongs to the account/feed, while source keys remain
-- unchanged within that feed. Existing IDs and their accepted evidence survive.
ALTER TABLE wb_connections
 DROP CONSTRAINT wb_connections_workspace_id_key,
 DROP CONSTRAINT wb_connections_connector_check,
 DROP CONSTRAINT wb_connections_origin_check,
 ADD COLUMN name text NOT NULL DEFAULT 'Demo supplier feed' CHECK(length(name) BETWEEN 1 AND 100 AND name !~ '[[:cntrl:]]'),
 ADD COLUMN supplier_id uuid,
 ADD COLUMN method text NOT NULL DEFAULT 'fictional' CHECK(method IN ('fictional','api_polling','webhook','browser','public_scrape','email')),
 ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 ADD COLUMN paused boolean NOT NULL DEFAULT false,
 ADD COLUMN minimum_cadence_seconds integer NOT NULL DEFAULT 30 CHECK(minimum_cadence_seconds BETWEEN 30 AND 86400),
 ADD COLUMN is_demo_default boolean NOT NULL DEFAULT true,
 ADD CONSTRAINT wb_connections_cadence_approved CHECK(cadence_seconds>=minimum_cadence_seconds),
 ADD CONSTRAINT wb_connections_transport CHECK(
   (method='fictional' AND connector='fictional_supplier' AND origin='fictional') OR
   (method<>'fictional' AND connector='unconfigured' AND origin='live_private'
    AND NOT is_demo_default AND NOT schedule_enabled AND active_run_id IS NULL AND last_success_at IS NULL
    AND initialized_generation IS NULL AND status='not_configured')),
 ADD CONSTRAINT wb_connections_default_method CHECK(NOT is_demo_default OR (method='fictional' AND supplier_id IS NULL));
CREATE UNIQUE INDEX wb_connections_one_demo_default ON wb_connections(workspace_id) WHERE is_demo_default;
ALTER TABLE wb_supplier_profiles ADD CONSTRAINT wb_supplier_profiles_workspace_identity UNIQUE(workspace_id,id);
ALTER TABLE wb_connections ADD CONSTRAINT wb_connections_supplier FOREIGN KEY(workspace_id,supplier_id) REFERENCES wb_supplier_profiles(workspace_id,id);

ALTER TABLE wb_orders ADD COLUMN connection_id uuid;
UPDATE wb_orders o SET connection_id=c.id FROM wb_connections c WHERE c.workspace_id=o.workspace_id AND c.is_demo_default;
ALTER TABLE wb_orders ALTER COLUMN connection_id SET NOT NULL;
ALTER TABLE wb_orders DROP CONSTRAINT wb_orders_workspace_id_generation_source_key_key;
ALTER TABLE wb_orders ADD CONSTRAINT wb_orders_source_identity UNIQUE(workspace_id,generation,connection_id,source_key),
 ADD CONSTRAINT wb_orders_connection FOREIGN KEY(workspace_id,connection_id) REFERENCES wb_connections(workspace_id,id);
ALTER TABLE wb_requirements ADD COLUMN connection_id uuid;
UPDATE wb_requirements r SET connection_id=c.id FROM wb_connections c WHERE c.workspace_id=r.workspace_id AND c.is_demo_default;
ALTER TABLE wb_requirements ALTER COLUMN connection_id SET NOT NULL;
ALTER TABLE wb_requirements DROP CONSTRAINT wb_requirements_pkey;
ALTER TABLE wb_requirements ADD PRIMARY KEY(workspace_id,generation,connection_id,source_key),
 ADD CONSTRAINT wb_requirements_connection FOREIGN KEY(workspace_id,connection_id) REFERENCES wb_connections(workspace_id,id);
ALTER TABLE wb_import_previews ADD COLUMN connection_id uuid;
UPDATE wb_import_previews p SET connection_id=c.id FROM wb_connections c WHERE c.workspace_id=p.workspace_id AND c.is_demo_default;
ALTER TABLE wb_import_previews ADD CONSTRAINT wb_import_previews_connection FOREIGN KEY(workspace_id,connection_id) REFERENCES wb_connections(workspace_id,id);
CREATE INDEX wb_connections_supplier ON wb_connections(workspace_id,supplier_id) WHERE supplier_id IS NOT NULL;

DROP FUNCTION bwb_due_schedules();
CREATE FUNCTION bwb_due_schedules() RETURNS TABLE(workspace_id uuid,actor_id uuid,connection_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path FROM CURRENT AS $$
 SELECT c.workspace_id,c.scheduled_by_user_id,c.id FROM wb_connections c
 JOIN users u ON u.id=c.scheduled_by_user_id
 JOIN memberships m ON m.workspace_id=c.workspace_id AND m.user_id=u.id
 JOIN workspaces w ON w.id=c.workspace_id
 WHERE c.schedule_enabled AND NOT c.paused AND c.method='fictional' AND c.origin='fictional'
 AND c.connector='fictional_supplier' AND c.cadence_seconds>=c.minimum_cadence_seconds
 AND w.kind<>'private' AND c.next_due_at<=now() AND u.is_owner AND u.active AND u.disabled_at IS NULL
 AND (u.expires_at IS NULL OR u.expires_at>now()) ORDER BY c.next_due_at,c.id LIMIT 20
$$;
-- Repair only a run belonging to the exact connection and fence.
CREATE OR REPLACE FUNCTION bwb_reconcile_expired_collections() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT AS $$
DECLARE expired record; repaired integer := 0;
BEGIN
 FOR expired IN
  SELECT c.id AS connection_id,c.workspace_id,c.fence,r.id AS run_id,r.actor_user_id
  FROM wb_connections c JOIN workspaces w ON w.id=c.workspace_id
  JOIN wb_runs r ON r.id=c.active_run_id AND r.workspace_id=c.workspace_id
   AND r.connection_id=c.id AND r.generation=w.generation AND r.fence=c.fence
  WHERE c.lease_expires_at<=now() AND r.state IN ('queued','running','validating','publishing')
  ORDER BY c.lease_expires_at LIMIT 20 FOR UPDATE OF w,c SKIP LOCKED
 LOOP
  UPDATE wb_runs SET state='terminal_failed',finished_at=now(),error_code='collection_lease_expired'
   WHERE id=expired.run_id AND workspace_id=expired.workspace_id AND connection_id=expired.connection_id
   AND fence=expired.fence AND state IN ('queued','running','validating','publishing');
  IF FOUND THEN
   UPDATE wb_connections SET status='attention',last_failure_code='collection_lease_expired',active_run_id=NULL,lease_expires_at=NULL,fence=fence+1
    WHERE id=expired.connection_id AND active_run_id=expired.run_id AND fence=expired.fence;
   INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome)
    VALUES(gen_random_uuid(),expired.actor_user_id,'collection.lease_expired','collection_run',expired.run_id,'failure');
   repaired := repaired+1;
  END IF;
 END LOOP;
 RETURN repaired;
END $$;
DO $$ DECLARE migration_schema text := current_schema(); BEGIN
 EXECUTE format('ALTER FUNCTION %I.bwb_due_schedules() SET search_path TO %I,pg_temp',migration_schema,migration_schema);
 EXECUTE format('ALTER FUNCTION %I.bwb_reconcile_expired_collections() SET search_path TO %I,pg_temp',migration_schema,migration_schema);
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
  GRANT EXECUTE ON FUNCTION bwb_due_schedules() TO bwb_app;
 END IF;
END $$;
REVOKE ALL ON FUNCTION bwb_due_schedules() FROM PUBLIC;
