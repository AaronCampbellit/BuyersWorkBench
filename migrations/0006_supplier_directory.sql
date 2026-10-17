-- Supplier profiles are editable workspace data; immutable aliases continue to
-- bind observations and requirements independently from display-name edits.
CREATE TABLE wb_supplier_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    generation bigint NOT NULL CHECK(generation>0),
    alias text NOT NULL CHECK(length(alias) BETWEEN 1 AND 240),
    name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
    contact_name text NOT NULL DEFAULT '' CHECK(length(contact_name)<=100),
    email text NOT NULL DEFAULT '' CHECK(octet_length(email)<=254),
    phone text NOT NULL DEFAULT '' CHECK(length(phone)<=50),
    website text NOT NULL DEFAULT '' CHECK(octet_length(website)<=2048),
    notes text NOT NULL DEFAULT '' CHECK(length(notes)<=4000),
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','inactive')),
    revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
    origin text NOT NULL CHECK(origin IN ('fictional','manual_private')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id,generation,alias),
    UNIQUE(workspace_id,generation,id)
);
CREATE INDEX wb_supplier_profiles_names ON wb_supplier_profiles(workspace_id,generation,lower(name),id);
CREATE INDEX wb_orders_supplier_alias ON wb_orders(workspace_id,generation,(record->>'supplier_alias'));

-- Backfill current, accepted source aliases without inventing contact details.
INSERT INTO wb_supplier_profiles(workspace_id,generation,alias,name,origin)
 SELECT DISTINCT o.workspace_id,o.generation,o.record->>'supplier_alias',
        left(o.record->>'supplier_alias',100),
        CASE WHEN w.kind='private' THEN 'manual_private' ELSE 'fictional' END
   FROM wb_orders o JOIN workspaces w ON w.id=o.workspace_id AND w.generation=o.generation
  WHERE length(o.record->>'supplier_alias') BETWEEN 1 AND 240
 ON CONFLICT DO NOTHING;

ALTER TABLE wb_supplier_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_supplier_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY supplier_read ON wb_supplier_profiles FOR SELECT
 USING(bwb_can_access(workspace_id) AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=workspace_id));
CREATE POLICY supplier_create ON wb_supplier_profiles FOR INSERT WITH CHECK(
 bwb_can_access(workspace_id)
 AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
    WHERE w.id=wb_supplier_profiles.workspace_id AND w.generation=wb_supplier_profiles.generation AND w.owner_user_id=u.id
      AND u.is_owner AND w.kind IN ('private','presentation')
      AND origin=CASE WHEN w.kind='private' THEN 'manual_private' ELSE 'fictional' END));
CREATE POLICY supplier_update ON wb_supplier_profiles FOR UPDATE USING(
 bwb_can_access(workspace_id)
 AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
    WHERE w.id=wb_supplier_profiles.workspace_id AND w.generation=wb_supplier_profiles.generation AND w.owner_user_id=u.id
      AND u.is_owner AND w.kind IN ('private','presentation')))
 WITH CHECK(bwb_can_access(workspace_id)
 AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
    WHERE w.id=wb_supplier_profiles.workspace_id AND w.generation=wb_supplier_profiles.generation AND w.owner_user_id=u.id
      AND u.is_owner AND w.kind IN ('private','presentation')
      AND origin=CASE WHEN w.kind='private' THEN 'manual_private' ELSE 'fictional' END));

-- Seed/collection may create a missing alias for an authorized reviewer, while
-- manual profile writes remain owner-only. This operation cannot edit contacts,
-- existing names or matching aliases, or copy profiles between workspaces.
CREATE FUNCTION bwb_ensure_supplier_profile(target_workspace uuid,target_generation bigint,target_alias text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT AS $$
DECLARE workspace_kind text;
BEGIN
    IF NOT bwb_can_access(target_workspace) THEN
        RAISE EXCEPTION 'supplier workspace is unavailable' USING ERRCODE='42501';
    END IF;
    SELECT kind INTO workspace_kind FROM workspaces
      WHERE id=target_workspace AND generation=target_generation FOR UPDATE;
    IF workspace_kind IS NULL THEN
        RAISE EXCEPTION 'supplier generation is stale' USING ERRCODE='23514';
    END IF;
    IF target_alias IS NULL OR length(target_alias) NOT BETWEEN 1 AND 240
       OR btrim(target_alias)='' OR target_alias ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'supplier alias is invalid' USING ERRCODE='23514';
    END IF;
    INSERT INTO wb_supplier_profiles(workspace_id,generation,alias,name,origin)
      VALUES(target_workspace,target_generation,target_alias,left(target_alias,100),
             CASE WHEN workspace_kind='private' THEN 'manual_private' ELSE 'fictional' END)
      ON CONFLICT(workspace_id,generation,alias) DO NOTHING;
END $$;
REVOKE ALL ON FUNCTION bwb_ensure_supplier_profile(uuid,bigint,text) FROM PUBLIC;
DO $$ DECLARE migration_schema text := current_schema(); BEGIN
    EXECUTE format('ALTER FUNCTION %I.bwb_ensure_supplier_profile(uuid,bigint,text) SET search_path TO %I,pg_temp',migration_schema,migration_schema);
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
        GRANT SELECT,INSERT ON wb_supplier_profiles TO bwb_app;
        GRANT UPDATE(name,contact_name,email,phone,website,notes,status,revision,updated_at) ON wb_supplier_profiles TO bwb_app;
        GRANT EXECUTE ON FUNCTION bwb_ensure_supplier_profile(uuid,bigint,text) TO bwb_app;
    END IF;
END $$;
