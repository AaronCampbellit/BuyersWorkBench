-- Locally authored purchasing workflow, independent from supplier observations.
-- Quote provenance is immutable; supplier and commercial fields may change only
-- through the application's draft workflow. No supplier operation is performed.
CREATE TABLE wb_purchase_orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    generation bigint NOT NULL CHECK(generation>0),
    number text NOT NULL DEFAULT ('BWB-'||upper(substr(replace(gen_random_uuid()::text,'-',''),1,12))) CHECK(length(number) BETWEEN 1 AND 100),
    supplier_id uuid NOT NULL,
    data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
    supplier_alias text NOT NULL CHECK(length(supplier_alias) BETWEEN 1 AND 240),
    supplier_name text NOT NULL CHECK(length(supplier_name) BETWEEN 1 AND 100),
    origin text NOT NULL CHECK(origin IN ('fictional','manual_private')),
    status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','approved','placed','partially_received','received','closed','cancelled')),
    received_quantity numeric(22,4) NOT NULL DEFAULT 0 CHECK(received_quantity>=0),
    placement_reference text NOT NULL DEFAULT '' CHECK(length(placement_reference)<=100),
    source_quote_id text,
    quote_source jsonb,
    revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id,generation,id),
    UNIQUE(workspace_id,generation,number),
    UNIQUE(workspace_id,generation,source_quote_id),
    FOREIGN KEY(workspace_id,generation,supplier_id) REFERENCES wb_supplier_profiles(workspace_id,generation,id),
    CHECK(COALESCE((data->>'quantity') ~ '^(0|[1-9][0-9]{0,17})(\.[0-9]{1,4})?$',false)),
    CHECK((data->>'quantity')::numeric>0 AND received_quantity<=(data->>'quantity')::numeric),
    CHECK((source_quote_id IS NULL AND quote_source IS NULL)
       OR (source_quote_id IS NOT NULL AND length(source_quote_id) BETWEEN 1 AND 240
           AND COALESCE(jsonb_typeof(quote_source)='object',false)))
);
CREATE INDEX wb_purchase_orders_current ON wb_purchase_orders(workspace_id,generation,created_at DESC,id);
CREATE INDEX wb_purchase_orders_status ON wb_purchase_orders(workspace_id,generation,status,updated_at DESC);
CREATE INDEX wb_purchase_orders_supplier ON wb_purchase_orders(workspace_id,generation,supplier_id);
CREATE INDEX wb_purchase_orders_creator ON wb_purchase_orders(created_by);

CREATE TABLE wb_purchase_order_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    generation bigint NOT NULL CHECK(generation>0),
    order_id uuid NOT NULL,
    actor_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL CHECK(length(action) BETWEEN 1 AND 100),
    from_status text NOT NULL DEFAULT '',
    to_status text NOT NULL CHECK(to_status IN ('draft','approved','placed','partially_received','received','closed','cancelled')),
    quantity text NOT NULL DEFAULT '',
    reference text NOT NULL DEFAULT '' CHECK(length(reference)<=100),
    note text NOT NULL DEFAULT '' CHECK(length(note)<=4000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,generation,order_id) REFERENCES wb_purchase_orders(workspace_id,generation,id)
);
CREATE INDEX wb_purchase_order_events_order ON wb_purchase_order_events(workspace_id,generation,order_id,created_at,id);
CREATE INDEX wb_purchase_order_events_actor ON wb_purchase_order_events(actor_id);

ALTER TABLE wb_purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_purchase_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_order_read ON wb_purchase_orders FOR SELECT USING(
    bwb_can_access(workspace_id)
    AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=wb_purchase_orders.workspace_id));
CREATE POLICY purchase_order_insert ON wb_purchase_orders FOR INSERT WITH CHECK(
    bwb_can_access(workspace_id) AND created_by=bwb_actor_id()
    AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
      WHERE w.id=wb_purchase_orders.workspace_id AND w.generation=wb_purchase_orders.generation
        AND w.owner_user_id=u.id AND u.is_owner AND w.kind IN ('private','presentation')
        AND origin=CASE WHEN w.kind='private' THEN 'manual_private' ELSE 'fictional' END));
CREATE POLICY purchase_order_update ON wb_purchase_orders FOR UPDATE USING(
    bwb_can_access(workspace_id)
    AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
      WHERE w.id=wb_purchase_orders.workspace_id AND w.generation=wb_purchase_orders.generation
        AND w.owner_user_id=u.id AND u.is_owner AND w.kind IN ('private','presentation')))
 WITH CHECK(bwb_can_access(workspace_id)
    AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
      WHERE w.id=wb_purchase_orders.workspace_id AND w.generation=wb_purchase_orders.generation
        AND w.owner_user_id=u.id AND u.is_owner AND w.kind IN ('private','presentation')
        AND origin=CASE WHEN w.kind='private' THEN 'manual_private' ELSE 'fictional' END));

ALTER TABLE wb_purchase_order_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wb_purchase_order_events FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_order_event_read ON wb_purchase_order_events FOR SELECT USING(
    bwb_can_access(workspace_id)
    AND generation=(SELECT w.generation FROM workspaces w WHERE w.id=wb_purchase_order_events.workspace_id));
CREATE POLICY purchase_order_event_insert ON wb_purchase_order_events FOR INSERT WITH CHECK(
    bwb_can_access(workspace_id) AND actor_id=bwb_actor_id()
    AND EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=bwb_actor_id()
      WHERE w.id=wb_purchase_order_events.workspace_id AND w.generation=wb_purchase_order_events.generation
        AND w.owner_user_id=u.id AND u.is_owner AND w.kind IN ('private','presentation')));

DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
        GRANT SELECT,INSERT ON wb_purchase_orders TO bwb_app;
        GRANT UPDATE(data,supplier_id,supplier_alias,supplier_name,status,received_quantity,placement_reference,revision,updated_at) ON wb_purchase_orders TO bwb_app;
        GRANT SELECT,INSERT ON wb_purchase_order_events TO bwb_app;
    END IF;
END $$;
