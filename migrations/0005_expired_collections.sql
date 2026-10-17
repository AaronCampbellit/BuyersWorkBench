-- A fixed maintenance operation repairs abandoned manual and scheduled runs
-- after worker/database outages. It cannot read source records or publish data.
CREATE FUNCTION bwb_reconcile_expired_collections() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT AS $$
DECLARE expired record; repaired integer := 0;
BEGIN
    FOR expired IN
        SELECT c.id AS connection_id,c.workspace_id,c.fence,
               r.id AS run_id,r.actor_user_id
          FROM wb_connections c
          JOIN workspaces w ON w.id=c.workspace_id
          JOIN wb_runs r ON r.id=c.active_run_id AND r.workspace_id=c.workspace_id
                       AND r.generation=w.generation AND r.fence=c.fence
         WHERE c.lease_expires_at<=now()
           AND r.state IN ('queued','running','validating','publishing')
         ORDER BY c.lease_expires_at
         LIMIT 20 FOR UPDATE OF w,c SKIP LOCKED
    LOOP
        UPDATE wb_runs SET state='terminal_failed',finished_at=now(),
               error_code='collection_lease_expired'
         WHERE id=expired.run_id AND workspace_id=expired.workspace_id
           AND fence=expired.fence
           AND state IN ('queued','running','validating','publishing');
        IF FOUND THEN
            UPDATE wb_connections
               SET status='attention',last_failure_code='collection_lease_expired',
                   active_run_id=NULL,lease_expires_at=NULL,fence=fence+1
             WHERE id=expired.connection_id AND active_run_id=expired.run_id
               AND fence=expired.fence;
            INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome)
              VALUES(gen_random_uuid(),expired.actor_user_id,'collection.lease_expired',
                     'collection_run',expired.run_id,'failure');
            repaired := repaired+1;
        END IF;
    END LOOP;
    RETURN repaired;
END $$;

-- Bind privileged relation lookup to the actual migration schema (including
-- isolated test schemas), and put pg_temp explicitly last. Without that explicit
-- entry PostgreSQL searches temporary relations before the configured path.
DO $$ DECLARE migration_schema text := current_schema(); BEGIN
    EXECUTE format('ALTER FUNCTION %I.bwb_can_access(uuid) SET search_path TO %I, pg_temp', migration_schema,migration_schema);
    EXECUTE format('ALTER FUNCTION %I.bwb_due_schedules() SET search_path TO %I, pg_temp', migration_schema,migration_schema);
    EXECUTE format('ALTER FUNCTION %I.bwb_change_password(bytea,text) SET search_path TO %I, pg_temp', migration_schema,migration_schema);
    EXECUTE format('ALTER FUNCTION %I.bwb_reconcile_expired_collections() SET search_path TO %I, pg_temp', migration_schema,migration_schema);
END $$;
REVOKE ALL ON FUNCTION bwb_reconcile_expired_collections() FROM PUBLIC;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
        GRANT EXECUTE ON FUNCTION bwb_reconcile_expired_collections() TO bwb_app;
    END IF;
END $$;
