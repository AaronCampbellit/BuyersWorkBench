-- Accepted changes are the canonical internal source-event log. Existing
-- history becomes typed without copying it or generating historical notices.
ALTER TABLE wb_changes
 ADD COLUMN event_type text GENERATED ALWAYS AS ('order.' || field || '.changed') STORED,
 ADD COLUMN schema_version integer NOT NULL DEFAULT 1 CHECK(schema_version=1),
 ADD CONSTRAINT wb_changes_event_identity UNIQUE(workspace_id,generation,id);

-- A notice addresses either a purchasing condition or a source event. Both
-- subjects remain workspace/generation bound and recipient-scoped by RLS.
ALTER TABLE wb_notifications ALTER COLUMN condition_id DROP NOT NULL;
ALTER TABLE wb_notifications ADD COLUMN source_event_id uuid;
ALTER TABLE wb_notifications ADD CONSTRAINT wb_notifications_source_event
 FOREIGN KEY(workspace_id,generation,source_event_id)
 REFERENCES wb_changes(workspace_id,generation,id);
ALTER TABLE wb_notifications ADD CONSTRAINT wb_notifications_one_subject
 CHECK(num_nonnulls(condition_id,source_event_id)=1);
CREATE INDEX wb_notifications_source_event ON wb_notifications(workspace_id,generation,source_event_id)
 WHERE source_event_id IS NOT NULL;

-- Runtime collection may append history, but may never rewrite accepted events.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
   REVOKE UPDATE,DELETE ON wb_changes FROM bwb_app;
 END IF;
END $$;
