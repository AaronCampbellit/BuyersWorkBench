CREATE TABLE wb_import_previews (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 generation bigint NOT NULL CHECK(generation>0),
 actor_user_id uuid NOT NULL REFERENCES users(id),
 requirements jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,
 run_id uuid,
 FOREIGN KEY(workspace_id,generation,run_id) REFERENCES wb_runs(workspace_id,generation,id)
);
CREATE INDEX wb_import_previews_workspace ON wb_import_previews(workspace_id,generation);
ALTER TABLE wb_import_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_import_previews FORCE ROW LEVEL SECURITY;
CREATE POLICY wb_import_scope ON wb_import_previews USING(bwb_can_access(workspace_id) AND actor_user_id=bwb_actor_id()) WITH CHECK(bwb_can_access(workspace_id) AND actor_user_id=bwb_actor_id());

-- Private lineage never enters a public snapshot JSON object. Approval is a
-- separate, audited write from projection preview.
CREATE TABLE wb_sanitized_exports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 generation bigint NOT NULL CHECK(generation>0),
 source_record_id uuid NOT NULL,
 snapshot_id uuid NOT NULL UNIQUE,
 snapshot jsonb NOT NULL,
 actor_user_id uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 approved_at timestamptz,
 FOREIGN KEY(workspace_id,generation,source_record_id) REFERENCES wb_orders(workspace_id,generation,id)
);
ALTER TABLE wb_sanitized_exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_sanitized_exports FORCE ROW LEVEL SECURITY;
CREATE POLICY wb_export_scope ON wb_sanitized_exports USING(bwb_can_access(workspace_id) AND EXISTS(SELECT 1 FROM users WHERE id=bwb_actor_id() AND is_owner)) WITH CHECK(bwb_can_access(workspace_id) AND EXISTS(SELECT 1 FROM users WHERE id=bwb_actor_id() AND is_owner));
CREATE TABLE wb_snapshot_copies (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 generation bigint NOT NULL CHECK(generation>0),
 snapshot_id uuid NOT NULL,
 snapshot jsonb NOT NULL,
 seeded_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,generation,snapshot_id)
);
CREATE INDEX wb_snapshot_copies_workspace ON wb_snapshot_copies(workspace_id,generation);
ALTER TABLE wb_snapshot_copies ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_snapshot_copies FORCE ROW LEVEL SECURITY;
CREATE POLICY wb_snapshot_scope ON wb_snapshot_copies USING(bwb_can_access(workspace_id)) WITH CHECK(bwb_can_access(workspace_id) OR EXISTS(SELECT 1 FROM users WHERE id=bwb_actor_id() AND is_owner AND active AND disabled_at IS NULL));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
 GRANT SELECT,INSERT,UPDATE,DELETE ON wb_import_previews TO bwb_app;
 GRANT SELECT,INSERT,UPDATE ON wb_sanitized_exports TO bwb_app;
 GRANT SELECT,INSERT ON wb_snapshot_copies TO bwb_app;
END IF; END $$;
