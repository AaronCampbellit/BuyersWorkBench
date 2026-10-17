-- Accepted observations are append-only for runtime roles. Privileged local
-- maintenance may compact only redundant snapshots; no automatic cleanup runs.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
  REVOKE UPDATE,DELETE ON wb_observations FROM bwb_app;
 END IF;
END $$;

CREATE INDEX wb_runs_retention ON wb_runs(workspace_id,finished_at,id)
 WHERE staged_batch IS NOT NULL
 AND state IN ('succeeded','rejected','transient_failed','terminal_failed','cancelled','stale_generation');
CREATE INDEX wb_changes_observation_evidence ON wb_changes(workspace_id,generation,order_id,run_id);
CREATE INDEX wb_connections_active_run ON wb_connections(active_run_id) WHERE active_run_id IS NOT NULL;
