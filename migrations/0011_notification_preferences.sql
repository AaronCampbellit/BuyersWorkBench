-- Preferences belong to the member, not a demo generation. Reset preserves
-- them; opting in affects future notices and never replays historical events.
CREATE TABLE wb_notification_preferences (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status_changes boolean NOT NULL DEFAULT true,
    supplier_conditions boolean NOT NULL DEFAULT true,
    revision bigint NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(workspace_id,user_id),
    FOREIGN KEY(workspace_id,user_id) REFERENCES memberships(workspace_id,user_id)
);
ALTER TABLE wb_notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_notification_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY preference_self ON wb_notification_preferences
 USING(bwb_can_access(workspace_id) AND user_id=bwb_actor_id())
 WITH CHECK(bwb_can_access(workspace_id) AND user_id=bwb_actor_id());

ALTER TABLE wb_connections ADD COLUMN buyer_user_id uuid;
ALTER TABLE wb_connections ADD CONSTRAINT wb_connections_buyer_membership
 FOREIGN KEY(workspace_id,buyer_user_id) REFERENCES memberships(workspace_id,user_id);
CREATE INDEX wb_connections_buyer ON wb_connections(workspace_id,buyer_user_id)
 WHERE buyer_user_id IS NOT NULL;

-- A narrow privileged lookup permits publication to honor recipient preferences
-- while ordinary runtime reads and writes remain self-only. Account eligibility
-- is checked before fallback; an opted-out assigned recipient does not cause
-- notices to be broadcast to unrelated members.
CREATE FUNCTION bwb_notification_recipients(target_workspace uuid, assigned uuid, buyer uuid, notice_kind text)
RETURNS TABLE(user_id uuid) LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT AS $$
 WITH eligible AS (
   SELECT m.user_id FROM memberships m JOIN users u ON u.id=m.user_id
   WHERE m.workspace_id=target_workspace AND bwb_can_access(target_workspace)
     AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now())
 ), selected AS (
   SELECT COALESCE((SELECT e.user_id FROM eligible e WHERE e.user_id=assigned),
                   (SELECT e.user_id FROM eligible e WHERE e.user_id=buyer)) AS recipient
 )
 SELECT e.user_id FROM eligible e CROSS JOIN selected s
 LEFT JOIN wb_notification_preferences p ON p.workspace_id=target_workspace AND p.user_id=e.user_id
 WHERE (s.recipient IS NULL OR e.user_id=s.recipient)
   AND CASE notice_kind WHEN 'status_changes' THEN COALESCE(p.status_changes,true)
        WHEN 'supplier_conditions' THEN COALESCE(p.supplier_conditions,true) ELSE false END
$$;
REVOKE ALL ON FUNCTION bwb_notification_recipients(uuid,uuid,uuid,text) FROM PUBLIC;
DO $$ DECLARE migration_schema text := current_schema(); BEGIN
 EXECUTE format('ALTER FUNCTION %I.bwb_notification_recipients(uuid,uuid,uuid,text) SET search_path TO %I,pg_temp',migration_schema,migration_schema);
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
   GRANT SELECT,INSERT,UPDATE ON wb_notification_preferences TO bwb_app;
   GRANT EXECUTE ON FUNCTION bwb_notification_recipients(uuid,uuid,uuid,text) TO bwb_app;
 END IF;
END $$;
