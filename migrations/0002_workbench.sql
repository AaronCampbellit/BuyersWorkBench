-- Application context is set transaction-locally only after session/job validation.
CREATE FUNCTION bwb_actor_id() RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('bwb.actor_id', true),'')::uuid
$$;
CREATE FUNCTION bwb_can_access(target uuid) RETURNS boolean LANGUAGE sql STABLE
SECURITY DEFINER SET search_path FROM CURRENT AS $$
    SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id
      JOIN workspaces w ON w.id=m.workspace_id
      WHERE m.workspace_id=target AND m.user_id=bwb_actor_id() AND u.active
      AND u.disabled_at IS NULL AND (w.kind<>'private' OR (u.is_owner AND w.owner_user_id=u.id)))
$$;

CREATE TABLE wb_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces(id),
    connector text NOT NULL DEFAULT 'fictional_supplier' CHECK(connector='fictional_supplier'),
    origin text NOT NULL DEFAULT 'fictional' CHECK(origin='fictional'),
    source_revision bigint NOT NULL DEFAULT 1, source_records jsonb NOT NULL,
    source_failure boolean NOT NULL DEFAULT false, status text NOT NULL DEFAULT 'ready',
    fence bigint NOT NULL DEFAULT 0, active_run_id uuid, lease_expires_at timestamptz,
    last_attempt_at timestamptz,last_success_at timestamptz,last_failure_code text,
    schedule_enabled boolean NOT NULL DEFAULT false,cadence_seconds integer NOT NULL DEFAULT 300 CHECK(cadence_seconds BETWEEN 30 AND 86400),
    next_due_at timestamptz,scheduled_by_user_id uuid REFERENCES users(id),initialized_generation bigint,
    UNIQUE(workspace_id),UNIQUE(workspace_id,id),CHECK(jsonb_typeof(source_records)='array')
);
CREATE INDEX wb_connections_due ON wb_connections(next_due_at) WHERE schedule_enabled;
CREATE INDEX wb_connections_scheduler ON wb_connections(scheduled_by_user_id);
CREATE TABLE wb_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL REFERENCES workspaces(id),generation bigint NOT NULL,
    connection_id uuid NOT NULL,actor_user_id uuid NOT NULL REFERENCES users(id),fence bigint NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK(state IN('queued','running','validating','publishing','succeeded','rejected','transient_failed','terminal_failed','cancelled','stale_generation')),
    attempt integer NOT NULL DEFAULT 0,enqueued_at timestamptz NOT NULL DEFAULT now(),started_at timestamptz,finished_at timestamptz,
    record_count integer NOT NULL DEFAULT 0,error_code text,job_id bigint,source_revision bigint,staged_batch jsonb,
    FOREIGN KEY(workspace_id,connection_id) REFERENCES wb_connections(workspace_id,id),UNIQUE(workspace_id,generation,id)
);
CREATE INDEX wb_runs_workspace ON wb_runs(workspace_id,generation,enqueued_at DESC);
CREATE INDEX wb_runs_actor ON wb_runs(actor_user_id);
CREATE TABLE wb_orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL REFERENCES workspaces(id),generation bigint NOT NULL,
    source_key text NOT NULL,record jsonb NOT NULL,first_record jsonb NOT NULL,
    first_observed_at timestamptz NOT NULL,last_observed_at timestamptz NOT NULL,
    UNIQUE(workspace_id,generation,source_key),UNIQUE(workspace_id,generation,id)
);
CREATE TABLE wb_requirements (
    workspace_id uuid NOT NULL REFERENCES workspaces(id),generation bigint NOT NULL,source_key text NOT NULL,data jsonb NOT NULL,
    PRIMARY KEY(workspace_id,generation,source_key)
);
CREATE TABLE wb_observations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL,generation bigint NOT NULL,order_id uuid NOT NULL,run_id uuid NOT NULL,
    record jsonb NOT NULL,observed_at timestamptz NOT NULL,
    FOREIGN KEY(workspace_id,generation,order_id) REFERENCES wb_orders(workspace_id,generation,id),
    FOREIGN KEY(workspace_id,generation,run_id) REFERENCES wb_runs(workspace_id,generation,id),UNIQUE(run_id,order_id)
);
CREATE INDEX wb_observations_order ON wb_observations(workspace_id,generation,order_id,observed_at);
CREATE TABLE wb_changes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL,generation bigint NOT NULL,order_id uuid NOT NULL,run_id uuid NOT NULL,
    field text NOT NULL,data jsonb NOT NULL,detected_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,generation,order_id) REFERENCES wb_orders(workspace_id,generation,id),
    FOREIGN KEY(workspace_id,generation,run_id) REFERENCES wb_runs(workspace_id,generation,id),UNIQUE(run_id,order_id,field)
);
CREATE INDEX wb_changes_order ON wb_changes(workspace_id,generation,order_id,detected_at);
CREATE TABLE wb_conditions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL,generation bigint NOT NULL,order_id uuid NOT NULL,rule text NOT NULL,
    data jsonb NOT NULL,condition_active boolean NOT NULL,severity text NOT NULL,revision bigint NOT NULL DEFAULT 1,material_revision bigint NOT NULL DEFAULT 1,
    acknowledged boolean NOT NULL DEFAULT false,assignee_id uuid REFERENCES users(id),follow_up_date date,status text NOT NULL DEFAULT 'open' CHECK(status IN('open','in_progress','completed')),
    first_seen_at timestamptz NOT NULL DEFAULT now(),last_seen_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,generation,order_id) REFERENCES wb_orders(workspace_id,generation,id),UNIQUE(workspace_id,generation,order_id,rule),UNIQUE(workspace_id,generation,id)
);
CREATE INDEX wb_conditions_assignee ON wb_conditions(assignee_id);
CREATE TABLE wb_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL,generation bigint NOT NULL,condition_id uuid NOT NULL,author_id uuid NOT NULL REFERENCES users(id),
    body text NOT NULL CHECK(length(body) BETWEEN 1 AND 4000),created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,generation,condition_id) REFERENCES wb_conditions(workspace_id,generation,id)
);
CREATE INDEX wb_notes_condition ON wb_notes(workspace_id,generation,condition_id,created_at);
CREATE INDEX wb_notes_author ON wb_notes(author_id);
CREATE TABLE wb_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),workspace_id uuid NOT NULL,generation bigint NOT NULL,condition_id uuid NOT NULL,
    recipient_id uuid NOT NULL REFERENCES users(id),event_key text NOT NULL,message text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),read_at timestamptz,
    FOREIGN KEY(workspace_id,generation,condition_id) REFERENCES wb_conditions(workspace_id,generation,id),UNIQUE(workspace_id,generation,recipient_id,event_key)
);
CREATE INDEX wb_notifications_recipient ON wb_notifications(recipient_id,workspace_id,generation,created_at DESC);
CREATE INDEX wb_notifications_condition ON wb_notifications(workspace_id,generation,condition_id);
CREATE TABLE wb_quote_comparisons (
    workspace_id uuid NOT NULL REFERENCES workspaces(id),generation bigint NOT NULL,data jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,
    PRIMARY KEY(workspace_id,generation)
);
CREATE TABLE wb_operations (
    workspace_id uuid NOT NULL REFERENCES workspaces(id),actor_id uuid NOT NULL REFERENCES users(id),generation bigint NOT NULL,operation text NOT NULL,idempotency_key text NOT NULL,
    payload_hash text NOT NULL,result jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(workspace_id,actor_id,generation,operation,idempotency_key)
);
CREATE INDEX wb_operations_actor ON wb_operations(actor_id);

DO $$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['wb_connections','wb_runs','wb_orders','wb_requirements','wb_observations','wb_changes','wb_conditions','wb_notes','wb_quote_comparisons','wb_operations'] LOOP
   EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
   EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
   IF tab IN ('wb_connections','wb_operations') THEN
     EXECUTE format('CREATE POLICY workspace_scope ON %I USING (bwb_can_access(workspace_id)) WITH CHECK (bwb_can_access(workspace_id))',tab);
   ELSE
     EXECUTE format('CREATE POLICY workspace_scope ON %I USING (bwb_can_access(workspace_id) AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id)) WITH CHECK (bwb_can_access(workspace_id) AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id))',tab);
   END IF;
 END LOOP;
 ALTER TABLE wb_notifications ENABLE ROW LEVEL SECURITY;
 ALTER TABLE wb_notifications FORCE ROW LEVEL SECURITY;
 -- Writes can address all authorized recipients; reads remain recipient-only.
 CREATE POLICY notification_read ON wb_notifications FOR SELECT USING(bwb_can_access(workspace_id) AND recipient_id=bwb_actor_id() AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id));
 CREATE POLICY notification_insert ON wb_notifications FOR INSERT WITH CHECK(bwb_can_access(workspace_id) AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id) AND EXISTS(SELECT 1 FROM memberships WHERE workspace_id=wb_notifications.workspace_id AND user_id=recipient_id));
 CREATE POLICY notification_update ON wb_notifications FOR UPDATE USING(bwb_can_access(workspace_id) AND recipient_id=bwb_actor_id() AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id)) WITH CHECK(bwb_can_access(workspace_id) AND recipient_id=bwb_actor_id() AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id));
END $$;

-- The reconciler receives only due fictional workspace/actor identifiers. Normal
-- worker transactions must still reauthorize, lock and recheck all generation.
CREATE FUNCTION bwb_due_schedules() RETURNS TABLE(workspace_id uuid,actor_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path FROM CURRENT AS $$
 SELECT c.workspace_id,c.scheduled_by_user_id FROM wb_connections c
 JOIN users u ON u.id=c.scheduled_by_user_id
 WHERE c.schedule_enabled AND c.next_due_at<=now() AND u.active AND u.disabled_at IS NULL
 ORDER BY c.next_due_at LIMIT 20
$$;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
   GRANT SELECT,INSERT,UPDATE,DELETE ON wb_connections,wb_runs,wb_orders,wb_requirements,wb_observations,wb_changes,wb_conditions,wb_notes,wb_notifications,wb_quote_comparisons,wb_operations TO bwb_app;
   GRANT UPDATE(generation,seed_version,business_date) ON workspaces TO bwb_app;
 END IF;
END $$;
