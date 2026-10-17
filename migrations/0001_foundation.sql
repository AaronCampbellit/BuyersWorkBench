-- M0 identity and workspace foundation. Workspace RLS is a later M2 gate;
-- these roles and tables must not be used for external reviewer access yet.
CREATE TABLE users (
    id uuid PRIMARY KEY,
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 100),
    active boolean NOT NULL DEFAULT true,
    is_owner boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,
    CHECK (active OR disabled_at IS NOT NULL)
);
CREATE UNIQUE INDEX users_single_owner ON users (is_owner) WHERE is_owner;

CREATE TABLE identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    provider text NOT NULL,
    issuer text NOT NULL,
    subject text NOT NULL,
    password_hash text,
    must_change_password boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, issuer, subject),
    CHECK (provider <> 'local' OR (issuer = 'local' AND password_hash IS NOT NULL)),
    CHECK (provider = 'local' OR password_hash IS NULL)
);
CREATE INDEX identities_user_id ON identities(user_id);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES users(id),
    csrf_token_hash bytea NOT NULL CHECK (octet_length(csrf_token_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id ON sessions(user_id);
CREATE INDEX sessions_active_expiry ON sessions(expires_at) WHERE revoked_at IS NULL;

CREATE TABLE workspaces (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('private', 'presentation', 'reviewer')),
    owner_user_id uuid NOT NULL REFERENCES users(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    seed_version text,
    business_date date,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (kind <> 'private' OR (seed_version IS NULL AND business_date IS NULL)),
    CHECK (kind = 'private' OR (seed_version IS NOT NULL AND business_date IS NOT NULL))
);
CREATE INDEX workspaces_owner_user_id ON workspaces(owner_user_id);
CREATE UNIQUE INDEX workspaces_owner_primary_kind ON workspaces(owner_user_id, kind)
    WHERE kind IN ('private', 'presentation');

CREATE TABLE memberships (
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    user_id uuid NOT NULL REFERENCES users(id),
    role text NOT NULL CHECK (role IN ('owner', 'reviewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX memberships_user_id ON memberships(user_id);

CREATE FUNCTION enforce_membership_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.role = 'reviewer' AND NOT EXISTS (
        SELECT 1 FROM workspaces WHERE id = NEW.workspace_id AND kind = 'reviewer'
    ) THEN
        RAISE EXCEPTION 'reviewer membership requires a reviewer workspace'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.role = 'owner' AND NOT EXISTS (
        SELECT 1 FROM users WHERE id = NEW.user_id AND is_owner = true
    ) THEN
        RAISE EXCEPTION 'owner membership requires the owner account'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER memberships_scope BEFORE INSERT OR UPDATE ON memberships
    FOR EACH ROW EXECUTE FUNCTION enforce_membership_scope();

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    actor_user_id uuid REFERENCES users(id),
    action text NOT NULL,
    target_type text NOT NULL,
    target_id uuid,
    outcome text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_actor_user_id ON audit_events(actor_user_id);
CREATE INDEX audit_events_occurred_at ON audit_events(occurred_at);

-- A real persisted worker probe, rather than invented order or supplier data.
CREATE TABLE foundation_checks (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    job_id bigint UNIQUE,
    checked_at timestamptz NOT NULL DEFAULT now(),
    result text NOT NULL
);
CREATE INDEX foundation_checks_workspace_id ON foundation_checks(workspace_id);

-- Docker initializes this constrained runtime role separately. Integration-test
-- databases need not create a cluster-global application role.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bwb_app') THEN
        GRANT USAGE ON SCHEMA public TO bwb_app;
        GRANT SELECT ON users, identities, workspaces, memberships,
            bwb_schema_migrations TO bwb_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON sessions, foundation_checks TO bwb_app;
        GRANT INSERT ON audit_events TO bwb_app;
    END IF;
END
$$;
