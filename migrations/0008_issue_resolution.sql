-- Buyer resolution is an immutable account of work performed. Source condition
-- verification remains independent: active observations await verification.
ALTER TABLE wb_conditions DROP CONSTRAINT wb_conditions_status_check;
ALTER TABLE wb_conditions ADD COLUMN resolved_at timestamptz;
ALTER TABLE wb_conditions ADD CONSTRAINT wb_conditions_status_check
 CHECK(status IN('open','in_progress','completed','awaiting_verification','resolved'));
ALTER TABLE wb_conditions ADD CONSTRAINT wb_conditions_resolved_at_check
 CHECK((status='resolved')=(resolved_at IS NOT NULL));

CREATE TABLE wb_issue_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    generation bigint NOT NULL CHECK(generation>0),
    condition_id uuid NOT NULL,
    actor_id uuid REFERENCES users(id),
    action text NOT NULL CHECK(action IN('resolution_submitted','source_verified','reopened','workflow_updated')),
    from_status text NOT NULL CHECK(from_status IN('open','in_progress','completed','awaiting_verification','resolved')),
    to_status text NOT NULL CHECK(to_status IN('open','in_progress','completed','awaiting_verification','resolved')),
    details jsonb NOT NULL CHECK(jsonb_typeof(details)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,generation,condition_id) REFERENCES wb_conditions(workspace_id,generation,id),
    CHECK((action='source_verified' AND actor_id IS NULL)
       OR (action IN('resolution_submitted','workflow_updated') AND actor_id IS NOT NULL)
       OR action='reopened')
);
CREATE INDEX wb_issue_events_condition ON wb_issue_events(workspace_id,generation,condition_id,created_at DESC,id DESC);
CREATE INDEX wb_issue_events_latest_resolution ON wb_issue_events(workspace_id,generation,condition_id,created_at DESC,id DESC)
 WHERE action='resolution_submitted';
CREATE INDEX wb_issue_events_actor ON wb_issue_events(actor_id) WHERE actor_id IS NOT NULL;

ALTER TABLE wb_issue_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_issue_events FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_event_read ON wb_issue_events FOR SELECT USING(
 bwb_can_access(workspace_id)
 AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=wb_issue_events.workspace_id));
CREATE POLICY issue_event_insert ON wb_issue_events FOR INSERT WITH CHECK(
 bwb_can_access(workspace_id)
 AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=wb_issue_events.workspace_id)
 AND ((actor_id=bwb_actor_id() AND action IN('resolution_submitted','workflow_updated','reopened'))
      OR (actor_id IS NULL AND action IN('source_verified','reopened'))));

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
   GRANT SELECT,INSERT ON wb_issue_events TO bwb_app;
 END IF;
END $$;
