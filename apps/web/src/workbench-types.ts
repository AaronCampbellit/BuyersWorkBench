export type RecordFacts = {
  source_key: string;
  source_order_id: string;
  source_line_id: string;
  supplier_alias: string;
  supplier_part: string;
  description: string;
  origin: string;
  ordered: string;
  cancelled: string;
  shipped: string;
  delivered: string;
  uom: string;
  status: string;
  confirmation: string;
  placed_at: string | null;
  confirmed_at: string | null;
  arrival_date: string;
  arrival_kind: string;
  arrival_label: string;
  original_date: string;
  original_kind: string;
  original_label: string;
  remaining_schedule: string;
};
export type Requirement = {
  source_key: string;
  internal_po: string;
  line: string;
  supplier_alias: string;
  internal_part: string;
  quantity: string;
  uom: string;
  required_date: string;
  buyer_alias: string;
  origin: string;
};
export type OrderLifecycle = {
  fulfillment_state:
    | "open"
    | "partially_shipped"
    | "shipped"
    | "partially_delivered"
    | "delivered"
    | "cancelled"
    | "unknown";
  confirmation_state: "confirmed" | "unconfirmed" | "unavailable";
  cancellation_state: "none" | "partial" | "full" | "unknown";
  receipt_state: "unavailable";
  net_ordered: string;
  unshipped: string;
  unreceived: string;
  evidence: "quantities" | "source_status" | "insufficient";
  issues: string[];
};
export type Order = RecordFacts & {
  id: string;
  connection_id: string;
  requirement: Requirement | null;
  first_observed_at: string;
  last_observed_at: string;
  lifecycle: OrderLifecycle;
};
export type Exception = {
  id: string;
  order_id: string;
  source_key: string;
  supplier_alias: string;
  description: string;
  internal_po: string;
  required_date: string;
  rule: string;
  severity: string;
  explanation: string;
  delay_days: number;
  active: boolean;
  evidence: Record<string, string>;
  revision: number;
  material_revision: number;
  acknowledged: boolean;
  assignee_id: string | null;
  follow_up_date: string | null;
  status: string;
  first_seen_at: string;
  last_seen_at: string;
  resolution: IssueResolution | null;
  resolved_at: string | null;
};
export type Note = {
  id: string;
  exception_id: string;
  author_name: string;
  body: string;
  created_at: string;
};
export type History = {
  id: string;
  event_type: string;
  schema_version: number;
  run_id: string;
  connection_id: string;
  observation_id: string;
  field: string;
  before: string;
  after: string;
  semantics: string;
  delta_days: number;
  worsened: boolean;
  detected_at: string;
};
export type OrderDetail = {
  order: Order;
  history: History[];
  conditions: Exception[];
  notes: Note[];
  issue_events: IssueEvent[];
};
export type Run = {
  id: string;
  connection_id: string;
  generation: number;
  state: string;
  attempt: number;
  enqueued_at: string;
  started_at: string | null;
  finished_at: string | null;
  record_count: number;
  error_code: string | null;
};
export type Connection = {
  id: string;
  name: string;
  supplier_id: string | null;
  buyer_user_id: string | null;
  method: string;
  revision: number;
  paused: boolean;
  minimum_cadence_seconds: number;
  is_demo_default: boolean;
  runnable: boolean;
  connector: string;
  origin: string;
  status: string;
  source_revision: number;
  last_attempt_at: string | null;
  last_success_at: string | null;
  last_failure_code: string | null;
  schedule_enabled: boolean;
  cadence_seconds: number;
  next_due_at: string | null;
};
export type Notification = {
  id: string;
  exception_id: string;
  source_event_id: string;
  order_id: string;
  message: string;
  created_at: string;
  read_at: string | null;
};
export type Quote = {
  id: string;
  supplier: string;
  unit_price: string;
  freight: string;
  other_charges: string;
  approval: string;
  arrival_date: string;
  uom: string;
  currency: string;
};
export type QuoteResult = Quote & {
  modeled_total: string;
  eligible: boolean;
  reasons: string[];
  rank: number;
};
export type Quotes = {
  comparison: {
    id: string;
    description: string;
    origin: string;
    quantity: string;
    uom: string;
    currency: string;
    required_date: string;
    excluded_costs: string[];
    quotes: Quote[];
  };
  calculation: {
    results: QuoteResult[];
    winner_id: string;
    winner_supplier: string;
    savings_versus_atlas: string;
  };
  revision: number;
};
export type Supplier = {
  id: string;
  alias: string;
  name: string;
  contact_name: string;
  email: string;
  phone: string;
  website: string;
  notes: string;
  status: "active" | "inactive";
  revision: number;
  origin: "fictional" | "manual_private";
  order_count: number;
  active_conditions: number;
  coverage_label: string;
};
export type Overview = {
  workspace_id: string;
  generation: number;
  origin: string;
  business_date: string;
  seeded: boolean;
  seed_version: string;
  ready: boolean;
  state: string;
  orders: Order[];
  priorities: Exception[];
  connections: Connection[];
  runs: Run[];
  notifications: Notification[];
  quotes: Quotes | null;
  suppliers: Supplier[];
  purchase_orders: PurchaseOrder[];
};

export type PurchaseOrderInput = {
  supplier_id: string;
  description: string;
  quantity: string;
  uom: string;
  currency: string;
  unit_price: string;
  freight: string;
  other_charges: string;
  required_date: string;
  notes: string;
};
export type PurchaseOrder = PurchaseOrderInput & {
  id: string;
  number: string;
  generation: number;
  supplier_alias: string;
  supplier_name: string;
  origin: string;
  status: string;
  revision: number;
  received_quantity: string;
  placement_reference: string;
  modeled_total: string;
  source_quote_id: string;
  quote_source: {
    comparison_id: string;
    revision: number;
    quote: Quote;
    quantity: string;
    description: string;
    required_date: string;
    excluded_costs: string[];
  } | null;
  allowed_actions: string[];
  created_at: string;
  updated_at: string;
};
export type PurchaseOrderEvent = {
  id: string;
  action: string;
  from_status: string;
  to_status: string;
  quantity: string;
  reference: string;
  note: string;
  actor_name: string;
  created_at: string;
};
export type PurchaseOrderDetail = {
  order: PurchaseOrder;
  events: PurchaseOrderEvent[];
};

export type IssueResolutionInput = {
  resolution_type: string;
  action_taken: string;
  result: string;
  reference: string;
  supplier_contact: string;
  revised_date: string;
  confirmed_quantity: string;
  follow_up_date: string;
};
export type IssueResolution = IssueResolutionInput & {
  id: string;
  submitted_by: string;
  submitted_at: string;
  material_revision: number;
  condition_active: boolean;
  rule: string;
  evidence: Record<string, string>;
  quantity_uom: string;
};
export type WorkflowInput = {
  acknowledged: boolean;
  assignee_id: string | null;
  follow_up_date: string | null;
  status: string;
};
export type IssueEvent = {
  id: string;
  exception_id: string;
  action: string;
  from_status: string;
  to_status: string;
  actor_name: string;
  created_at: string;
  reason: string;
  resolution: IssueResolution | null;
  material_revision: number;
  condition_active: boolean;
  rule: string;
  evidence: Record<string, string>;
  quantity_uom: string;
  before_workflow: WorkflowInput | null;
  after_workflow: WorkflowInput | null;
};
