-- Local account lifecycle. Provisioning and disable remain privileged CLI tasks.
ALTER TABLE users ADD COLUMN expires_at timestamptz;
CREATE INDEX users_expiry ON users(expires_at) WHERE active;
ALTER TABLE sessions ADD COLUMN reauthenticated_at timestamptz;
UPDATE sessions SET reauthenticated_at=created_at;
ALTER TABLE sessions ALTER COLUMN reauthenticated_at SET NOT NULL;
ALTER TABLE sessions ALTER COLUMN reauthenticated_at SET DEFAULT now();

CREATE OR REPLACE FUNCTION bwb_can_access(target uuid) RETURNS boolean LANGUAGE sql STABLE
SECURITY DEFINER SET search_path FROM CURRENT AS $$
 SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id JOIN workspaces w ON w.id=m.workspace_id
 WHERE m.workspace_id=target AND m.user_id=bwb_actor_id() AND u.active AND u.disabled_at IS NULL
 AND (u.expires_at IS NULL OR u.expires_at>now()) AND (w.kind<>'private' OR (u.is_owner AND w.owner_user_id=u.id)))
$$;
CREATE OR REPLACE FUNCTION bwb_due_schedules() RETURNS TABLE(workspace_id uuid,actor_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path FROM CURRENT AS $$
 SELECT c.workspace_id,c.scheduled_by_user_id FROM wb_connections c JOIN users u ON u.id=c.scheduled_by_user_id
 WHERE c.schedule_enabled AND c.next_due_at<=now() AND u.active AND u.disabled_at IS NULL
 AND (u.expires_at IS NULL OR u.expires_at>now()) ORDER BY c.next_due_at LIMIT 20
$$;

-- The runtime cannot update identity rows directly. A valid active session
-- identifies the only account this narrowly scoped operation can change.
CREATE FUNCTION bwb_change_password(p_session_hash bytea, p_password_hash text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT AS $$
DECLARE actor uuid;
BEGIN
    IF p_password_hash NOT LIKE '$argon2id$v=19$m=65536,t=3,p=1$%' THEN
        RETURN false;
    END IF;
    SELECT s.user_id INTO actor FROM sessions s JOIN users u ON u.id=s.user_id
      WHERE s.token_hash=p_session_hash AND s.revoked_at IS NULL AND s.expires_at>now()
        AND s.last_activity_at>now()-interval '30 minutes' AND s.created_at>now()-interval '12 hours'
        AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now())
      FOR UPDATE OF s,u;
    IF actor IS NULL THEN RETURN false; END IF;
    UPDATE identities SET password_hash=p_password_hash,must_change_password=false
      WHERE user_id=actor AND provider='local' AND issuer='local';
    UPDATE sessions SET revoked_at=now() WHERE user_id=actor AND token_hash<>p_session_hash AND revoked_at IS NULL;
    UPDATE sessions SET reauthenticated_at=now() WHERE token_hash=p_session_hash;
    INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome)
      VALUES(gen_random_uuid(),actor,'password.change','user',actor,'success');
    RETURN true;
END $$;
REVOKE ALL ON FUNCTION bwb_change_password(bytea,text) FROM PUBLIC;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
        GRANT EXECUTE ON FUNCTION bwb_change_password(bytea,text) TO bwb_app;
    END IF;
END $$;
