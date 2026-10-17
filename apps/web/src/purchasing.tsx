import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { ApiError, errorMessage, isAborted, request } from "./api";
import type { Session } from "./api";
import type {
  Exception,
  Order,
  OrderDetail,
  Overview,
  Supplier,
  Connection,
} from "./workbench-types";
import type { Mutate } from "./workbench";
import { SupplierEditor } from "./supplier-editor";
import {
  IssueCard,
  IssueActivity,
  issueStatusLabel,
  needsResolution,
} from "./issue-resolution";
import {
  Badge,
  dateOnly,
  displayText,
  originLabel,
  Empty,
  ErrorNotice,
  humanize,
  localTime,
  Panel,
} from "./ui";

const ruleNames: Record<string, string> = {
  R01: "Required arrival risk",
  R02: "Observed date change",
  R03: "Missing confirmation",
  R04: "Partial shipment",
  R05: "Source data attention",
};
const issueName = (condition: Exception) =>
  ruleNames[condition.rule] ?? condition.rule;
const tone = (severity: string) =>
  ["critical", "high"].includes(severity)
    ? "risk"
    : severity === "medium"
      ? "attention"
      : "neutral";
const facts = (value: string | null | undefined) => value || "Unknown";

export function Priorities({
  data,
  userId,
  onOpenOrder,
  onSupplier,
  onViewOrders,
}: {
  data: Overview;
  userId: string;
  onOpenOrder: (id: string, conditionId?: string) => void;
  onSupplier: (alias: string) => void;
  onViewOrders: () => void;
}) {
  const [supplier, setSupplier] = useState("");
  const [rule, setRule] = useState("");
  const [severity, setSeverity] = useState("");
  const [ack, setAck] = useState("");
  const [assignment, setAssignment] = useState("");
  const [active, setActive] = useState("all");
  const [workQueue, setWorkQueue] = useState("needs_action");
  const all = data.priorities ?? [];
  const queueViews = [
    {
      value: "needs_action",
      label: "Needs action",
      count: all.filter((item) => needsResolution(item.status)).length,
    },
    {
      value: "awaiting_verification",
      label: "Awaiting verification",
      count: all.filter((item) => item.status === "awaiting_verification")
        .length,
    },
    {
      value: "resolved",
      label: "Resolved",
      count: all.filter((item) => item.status === "resolved").length,
    },
    { value: "all", label: "All issues", count: all.length },
  ];
  const supplierNames = new Map(
    data.suppliers.map((item) => [
      item.alias,
      displayText(item.name) || humanize(item.alias),
    ]),
  );
  if (data.seeded && !data.ready)
    return (
      <Panel
        title="Initializing workspace"
        actions={
          <button
            className="button secondary compact"
            type="button"
            onClick={onViewOrders}
          >
            View orders
          </button>
        }
      >
        <Empty title="Baseline collection is not ready yet">
          The worker is validating and publishing the pinned source. An empty
          initialization state does not establish that suppliers are healthy.
        </Empty>
      </Panel>
    );
  const orders = new Map((data.orders ?? []).map((order) => [order.id, order]));
  const filtered = all.filter(
    (item) =>
      (workQueue === "all" ||
        (workQueue === "needs_action"
          ? needsResolution(item.status)
          : item.status === workQueue)) &&
      (!supplier || item.supplier_alias === supplier) &&
      (!rule || item.rule === rule) &&
      (!severity || item.severity === severity) &&
      (!ack || String(item.acknowledged) === ack) &&
      (!assignment || item.assignee_id === userId) &&
      (active === "all" || (active === "active" ? item.active : !item.active)),
  );
  return (
    <>
      <Panel
        title="Buyer work queue"
        description="Conditions are calculated from accepted observations and internal requirements."
        actions={
          <button
            className="button secondary compact"
            type="button"
            onClick={onViewOrders}
          >
            View orders
          </button>
        }
      >
        <div
          className="issue-queue-views"
          role="group"
          aria-label="Work queue views"
        >
          {queueViews.map((item) => (
            <button
              type="button"
              key={item.value}
              className={`issue-queue-view${workQueue === item.value ? " selected" : ""}`}
              aria-pressed={workQueue === item.value}
              onClick={() => setWorkQueue(item.value)}
            >
              <span>{item.label}</span>
              <strong>{item.count}</strong>
            </button>
          ))}
        </div>
        <div className="filters">
          <label>
            Supplier
            <select
              value={supplier}
              onChange={(e) => setSupplier(e.target.value)}
            >
              <option value="">All suppliers</option>
              {[...new Set(all.map((item) => item.supplier_alias))].map(
                (alias) => (
                  <option key={alias} value={alias}>
                    {supplierNames.get(alias) || humanize(alias)}
                  </option>
                ),
              )}
            </select>
          </label>
          <label>
            Condition
            <select value={rule} onChange={(e) => setRule(e.target.value)}>
              <option value="">All conditions</option>
              {[...new Set(all.map((item) => item.rule))].sort().map((key) => (
                <option key={key} value={key}>
                  {ruleNames[key] ?? key}
                </option>
              ))}
            </select>
          </label>
          <label>
            Severity
            <select
              value={severity}
              onChange={(e) => setSeverity(e.target.value)}
            >
              <option value="">All severities</option>
              {[...new Set(all.map((item) => item.severity))].map((value) => (
                <option key={value} value={value}>
                  {humanize(value)}
                </option>
              ))}
            </select>
          </label>
          <label>
            Acknowledgment
            <select value={ack} onChange={(e) => setAck(e.target.value)}>
              <option value="">All</option>
              <option value="false">Unacknowledged</option>
              <option value="true">Acknowledged</option>
            </select>
          </label>
          <label>
            Assignment
            <select
              value={assignment}
              onChange={(e) => setAssignment(e.target.value)}
            >
              <option value="">Everyone</option>
              <option value="me">Assigned to me</option>
            </select>
          </label>
          <label>
            Risk state
            <select value={active} onChange={(e) => setActive(e.target.value)}>
              <option value="active">Active conditions</option>
              <option value="all">All history</option>
              <option value="cleared">Cleared conditions</option>
            </select>
          </label>
        </div>
        {filtered.length ? (
          <div className="table-scroll">
            <table className="priorities-table">
              <caption className="sr-only">Calculated buyer priorities</caption>
              <thead>
                <tr>
                  <th>Priority / issue</th>
                  <th>Order / component</th>
                  <th>Supplier</th>
                  <th>Required arrival</th>
                  <th>Latest reported date</th>
                  <th>Workflow</th>
                  <th>Last verified</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((item) => {
                  const order = orders.get(item.order_id);
                  return (
                    <tr key={item.id}>
                      <td>
                        <Badge tone={tone(item.severity)}>
                          {humanize(item.severity)}
                        </Badge>
                        <strong className="cell-title">
                          {issueName(item)}
                        </strong>
                        <span className="cell-meta">
                          {item.rule} ·{" "}
                          {item.active ? "Active risk" : "Cleared risk"}
                        </span>
                      </td>
                      <th scope="row">
                        <button
                          className="text-button"
                          onClick={() => onOpenOrder(item.order_id, item.id)}
                        >
                          {item.internal_po ||
                            order?.source_order_id ||
                            item.source_key}
                          {order ? ` / ${order.source_line_id}` : ""}
                        </button>
                        <span className="cell-meta">
                          {displayText(item.description)}
                        </span>
                      </th>
                      <td>
                        <button
                          className="text-button"
                          onClick={() => onSupplier(item.supplier_alias)}
                        >
                          {supplierNames.get(item.supplier_alias) ||
                            humanize(item.supplier_alias)}
                        </button>
                        <span className="cell-meta">
                          {originLabel(order?.origin ?? data.origin)}
                        </span>
                      </td>
                      <td>
                        {dateOnly(item.required_date)}
                        <span className="cell-meta">Internal requirement</span>
                      </td>
                      <td>
                        {dateOnly(order?.arrival_date)}
                        <span className="cell-meta">
                          {displayText(order?.arrival_label) ||
                            humanize(order?.arrival_kind)}
                        </span>
                      </td>
                      <td>
                        {issueStatusLabel(item.status)}
                        <span className="cell-meta">
                          {item.acknowledged
                            ? "Acknowledged"
                            : "Unacknowledged"}
                        </span>
                        <span className="cell-meta">
                          {item.assignee_id === userId
                            ? "Assigned to you"
                            : item.assignee_id
                              ? "Assigned"
                              : "Unassigned"}
                        </span>
                        {item.follow_up_date ? (
                          <span className="cell-meta">
                            Follow-up {item.follow_up_date}
                          </span>
                        ) : null}
                      </td>
                      <td>{localTime(order?.last_observed_at)}</td>
                      <td>
                        <button
                          className="button secondary compact"
                          type="button"
                          onClick={() => onOpenOrder(item.order_id, item.id)}
                        >
                          {needsResolution(item.status)
                            ? "Review / resolve"
                            : "Review resolution"}
                          <span className="sr-only">
                            {" "}
                            {item.internal_po || item.source_key} {item.rule}
                          </span>
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty
            title={
              all.length
                ? "No conditions match these filters"
                : "No current known exceptions"
            }
          >
            {data.seeded
              ? "Source freshness and collection health remain separate from the exception queue."
              : "No observations have been collected in this workspace."}
          </Empty>
        )}
      </Panel>
    </>
  );
}

export function ObservedOrders({
  data,
  onOpenOrder,
}: {
  data: Overview;
  onOpenOrder: (id: string) => void;
}) {
  const [supplier, setSupplier] = useState("");
  const [status, setStatus] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [search, setSearch] = useState("");
  const orders = data.orders ?? [];
  const supplierNames = new Map(
    data.suppliers.map((item) => [
      item.alias,
      displayText(item.name) || humanize(item.alias),
    ]),
  );
  const searchValue = search.trim().toLocaleLowerCase();
  const filtered = orders.filter(
    (order) =>
      (!supplier || order.supplier_alias === supplier) &&
      (!status ||
        (order.lifecycle?.fulfillment_state ?? "unknown") === status) &&
      (!confirmation ||
        (order.lifecycle?.confirmation_state ?? order.confirmation) ===
          confirmation) &&
      (!searchValue ||
        [
          order.source_order_id,
          order.source_line_id,
          displayText(order.description),
          order.supplier_part,
          order.requirement?.internal_po,
          order.requirement?.internal_part,
          supplierNames.get(order.supplier_alias),
          data.connections.find(
            (connection) => connection.id === order.connection_id,
          )?.name,
        ].some((value) => value?.toLocaleLowerCase().includes(searchValue))),
  );
  return (
    <Panel
      title="Supplier-observed orders"
      description="Accepted source records with independent fulfillment, confirmation, and delivery evidence."
    >
      <div className="coverage-note observed-orders-note">
        <strong>
          Source observations and buyer tasks have separate lifecycles.
        </strong>
        <p>
          Shipment and delivery quantities are reported separately. Internal
          receipt verification is unavailable from this supplier source;
          completing a buyer follow-up does not change these facts.
        </p>
      </div>
      <div className="filters order-filters">
        <label htmlFor="order-search">
          Search orders
          <input
            id="order-search"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Order, part, description, or supplier"
          />
        </label>
        <label htmlFor="order-supplier">
          Supplier
          <select
            id="order-supplier"
            value={supplier}
            onChange={(event) => setSupplier(event.target.value)}
          >
            <option value="">All suppliers</option>
            {[...new Set(orders.map((order) => order.supplier_alias))].map(
              (alias) => (
                <option key={alias} value={alias}>
                  {supplierNames.get(alias) || humanize(alias)}
                </option>
              ),
            )}
          </select>
        </label>
        <label htmlFor="order-status">
          Fulfillment state
          <select
            id="order-status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All states</option>
            {[
              ...new Set(
                orders.map(
                  (order) => order.lifecycle?.fulfillment_state ?? "unknown",
                ),
              ),
            ]
              .sort()
              .map((value) => (
                <option key={value} value={value}>
                  {humanize(value)}
                </option>
              ))}
          </select>
        </label>
        <label htmlFor="order-confirmation">
          Confirmation
          <select
            id="order-confirmation"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
          >
            <option value="">All confirmations</option>
            {[
              ...new Set(
                orders.map(
                  (order) =>
                    order.lifecycle?.confirmation_state ?? order.confirmation,
                ),
              ),
            ]
              .sort()
              .map((value) => (
                <option key={value} value={value}>
                  {humanize(value)}
                </option>
              ))}
          </select>
        </label>
      </div>
      {filtered.length ? (
        <OrderTable
          orders={filtered}
          suppliers={data.suppliers}
          connections={data.connections}
          onOpenOrder={onOpenOrder}
        />
      ) : (
        <Empty
          title={
            orders.length
              ? "No orders match these filters"
              : data.seeded && !data.ready
                ? "Supplier baseline is not ready yet"
                : "No supplier orders collected"
          }
        >
          {orders.length
            ? "Change the supplier, fulfillment, confirmation, or search filters to see other records."
            : data.origin === "fictional"
              ? "Initialize the workspace and collect its source observations to see order lines."
              : "No supplier observations have been collected in this private workspace."}
        </Empty>
      )}
      <p className="panel-footnote">
        Showing {filtered.length} of {orders.length} accepted source order
        lines. The latest reported date retains its source label; original
        commitments remain unknown when the source does not provide them.
      </p>
    </Panel>
  );
}

function OrderTable({
  orders,
  suppliers,
  connections,
  onOpenOrder,
}: {
  orders: Order[];
  suppliers: Supplier[];
  connections: Connection[];
  onOpenOrder: (id: string) => void;
}) {
  const supplierNames = new Map(
    suppliers.map((item) => [
      item.alias,
      displayText(item.name) || humanize(item.alias),
    ]),
  );
  return (
    <div className="table-scroll">
      <table className="observed-orders-table">
        <caption className="sr-only">
          Accepted supplier-observed order lines
        </caption>
        <thead>
          <tr>
            <th>Source order / line</th>
            <th>Component</th>
            <th>Supplier</th>
            <th>Ordered</th>
            <th>Shipped</th>
            <th>Delivered</th>
            <th>Source status</th>
            <th>Confirmation</th>
            <th>Fulfillment lifecycle</th>
            <th>Reported date</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {orders.map((order) => (
            <tr key={order.id}>
              <th scope="row">
                {order.source_order_id} / {order.source_line_id}
                <span className="cell-meta">
                  {displayText(
                    connections.find(
                      (connection) => connection.id === order.connection_id,
                    )?.name,
                  ) || "Source connection"}
                </span>
                {order.requirement ? (
                  <span className="cell-meta">
                    Requirement {order.requirement.internal_po} /{" "}
                    {order.requirement.line}
                  </span>
                ) : null}
              </th>
              <td>
                {displayText(order.description)}
                <span className="cell-meta">{order.supplier_part}</span>
              </td>
              <td>
                {supplierNames.get(order.supplier_alias) ||
                  humanize(order.supplier_alias)}
              </td>
              <td>
                {facts(order.ordered)} {order.uom}
              </td>
              <td>
                {facts(order.shipped)} {order.uom}
              </td>
              <td>
                {facts(order.delivered)} {order.uom}
              </td>
              <td>{humanize(order.status)}</td>
              <td>
                {humanize(
                  order.lifecycle?.confirmation_state ?? order.confirmation,
                )}
              </td>
              <td>
                <Badge>
                  {humanize(order.lifecycle?.fulfillment_state ?? "unknown")}
                </Badge>
                <span className="cell-meta">
                  {order.lifecycle?.evidence === "quantities"
                    ? "Source quantities"
                    : order.lifecycle?.evidence === "source_status"
                      ? "Source status fallback"
                      : "Insufficient source evidence"}
                </span>
              </td>
              <td>
                {dateOnly(order.arrival_date)}
                <span className="cell-meta">
                  {displayText(order.arrival_label) ||
                    humanize(order.arrival_kind)}
                </span>
              </td>
              <td>
                <button
                  type="button"
                  className="button secondary compact"
                  onClick={() => onOpenOrder(order.id)}
                >
                  Details
                  <span className="sr-only"> {order.source_order_id}</span>
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function supplierWebsite(value: string): string | null {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : null;
  } catch {
    return null;
  }
}

export function SupplierPerformance({
  data,
  selected,
  onSelect,
  onOpenOrder,
  canManage,
  mutate,
  busy,
  mutationError,
}: {
  data: Overview;
  selected: string | null;
  onSelect: (alias: string | null) => void;
  onOpenOrder: (id: string) => void;
  canManage: boolean;
  mutate: Mutate;
  busy: boolean;
  mutationError: string | null;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const suppliers = data.suppliers ?? [];
  const visible = selected
    ? suppliers.filter((item) => item.alias === selected)
    : suppliers;
  const editingSupplier =
    editing && editing !== "new"
      ? suppliers.find((item) => item.id === editing)
      : null;
  useEffect(() => setEditing(null), [data.generation]);
  return (
    <>
      <div className="supplier-directory-toolbar">
        <div>
          <h2>Supplier directory</h2>
          <p>
            {canManage
              ? "Add supplier profiles and maintain contact details in this workspace."
              : "Supplier profiles and coverage in your workspace. Directory editing is available to its owner."}
          </p>
        </div>
        {canManage ? (
          <button
            className="button primary"
            type="button"
            disabled={busy}
            onClick={() => setEditing("new")}
          >
            Add supplier
          </button>
        ) : (
          <Badge>Read-only directory</Badge>
        )}
      </div>
      <div className="coverage-note">
        <strong>
          Actual arrival performance: Not available from this source.
        </strong>
        <p>
          No on-time percentage is calculated without actual arrivals, an
          explicit comparison baseline, and a defensible denominator. Supplier
          profiles can be saved before any orders are collected.
        </p>
      </div>
      <div className="inline-filter">
        <label htmlFor="supplier-filter">Supplier</label>
        <select
          id="supplier-filter"
          value={selected ?? ""}
          onChange={(event) => onSelect(event.target.value || null)}
        >
          <option value="">All suppliers</option>
          {suppliers.map((item) => (
            <option key={item.alias} value={item.alias}>
              {displayText(item.name) || humanize(item.alias)}
            </option>
          ))}
        </select>
      </div>
      {visible.length ? (
        visible.map((item) => {
          const orders = data.orders.filter(
            (order) => order.supplier_alias === item.alias,
          );
          const website = supplierWebsite(item.website);
          return (
            <Panel
              key={item.id || item.alias}
              className="supplier-profile-card"
              title={displayText(item.name) || humanize(item.alias)}
              description={displayText(item.coverage_label)}
              actions={
                <div className="form-actions">
                  <Badge
                    tone={item.status === "inactive" ? "neutral" : "fictional"}
                  >
                    {humanize(item.status || "active")}
                  </Badge>
                  {canManage ? (
                    <button
                      type="button"
                      className="button secondary compact"
                      disabled={busy}
                      onClick={() => setEditing(item.id)}
                    >
                      Edit supplier
                      <span className="sr-only">
                        {" "}
                        {displayText(item.name) || humanize(item.alias)}
                      </span>
                    </button>
                  ) : null}
                </div>
              }
            >
              <div className="supplier-summary">
                <span>
                  <strong>{item.order_count}</strong> observed supplier order
                  lines
                </span>
                <span>
                  <strong>{item.active_conditions}</strong> active conditions
                </span>
                <span>
                  <strong>
                    {
                      (data.purchase_orders ?? []).filter(
                        (order) => order.supplier_id === item.id,
                      ).length
                    }
                  </strong>{" "}
                  internal purchase orders
                </span>
              </div>
              <dl className="supplier-contact-facts">
                <div>
                  <dt>Contact</dt>
                  <dd>{item.contact_name || "Not provided"}</dd>
                </div>
                <div>
                  <dt>Email</dt>
                  <dd>{item.email || "Not provided"}</dd>
                </div>
                <div>
                  <dt>Phone</dt>
                  <dd>{item.phone || "Not provided"}</dd>
                </div>
                <div>
                  <dt>Website</dt>
                  <dd>
                    {website ? (
                      <a
                        href={website}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {item.website}
                      </a>
                    ) : (
                      item.website || "Not provided"
                    )}
                  </dd>
                </div>
              </dl>
              {item.notes ? (
                <div className="supplier-profile-notes">
                  <h3>Profile notes</h3>
                  <p>{item.notes}</p>
                </div>
              ) : null}
              {orders.length ? (
                <>
                  <OrderTable
                    orders={orders}
                    suppliers={suppliers}
                    connections={data.connections}
                    onOpenOrder={onOpenOrder}
                  />
                  <p className="panel-footnote">
                    Open an order for first-observed and latest verification
                    times, comparable change history, and original commitment
                    availability.
                  </p>
                </>
              ) : (
                <Empty title="No linked order lines">
                  This supplier profile is saved. No observed order lines are
                  linked to it.
                </Empty>
              )}
            </Panel>
          );
        })
      ) : (
        <Panel title="Supplier profiles">
          <Empty
            title={
              selected ? "No supplier matches this filter" : "No suppliers yet"
            }
          >
            {canManage
              ? "Select Add supplier to save your first supplier profile. No supplier connection is required."
              : "No supplier profiles are available in this workspace."}
          </Empty>
        </Panel>
      )}
      {editing !== null && (editing === "new" || editingSupplier) ? (
        <SupplierEditor
          key={editing}
          supplier={editingSupplier ?? null}
          mutate={mutate}
          busy={busy}
          serverError={mutationError}
          onClose={() => setEditing(null)}
          onSaved={() => {
            if (editing === "new") onSelect(null);
            setEditing(null);
          }}
        />
      ) : null}
    </>
  );
}

export function OrderInspector({
  base,
  id,
  generation,
  revision,
  session,
  busy,
  mutationError,
  mutate,
  onClose,
  onExpired,
  onSupplier,
  suppliers,
  connections,
  selectedConditionId,
}: {
  selectedConditionId: string | null;
  base: string;
  id: string;
  generation: number;
  revision: number;
  session: Session;
  busy: boolean;
  mutationError: string | null;
  mutate: Mutate;
  onClose: () => void;
  onExpired: () => void;
  onSupplier: (alias: string) => void;
  suppliers: Supplier[];
  connections: Connection[];
}) {
  const [data, setData] = useState<OrderDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  const [issuePending, setIssuePending] = useState(false);
  const focusedIssue = useRef<string | null>(null);
  const dialog = useRef<HTMLDialogElement | null>(null);
  const expired = useRef(onExpired);
  expired.current = onExpired;
  useEffect(() => {
    const current = dialog.current;
    const opener =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    current?.showModal();
    return () => {
      current?.close();
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  useEffect(() => {
    const current = dialog.current;
    if (current?.open && !current.contains(document.activeElement))
      current.querySelector<HTMLButtonElement>("button")?.focus();
  }, [data, loading]);
  function trapFocus(event: KeyboardEvent<HTMLDialogElement>) {
    if (
      event.target instanceof Element &&
      event.target.closest("dialog") !== event.currentTarget
    )
      return;
    if (event.key !== "Tab") return;
    const current = event.currentTarget;
    const controls = Array.from(
      current.querySelectorAll<HTMLElement>(
        "button, a[href], input, select, textarea, [tabindex]",
      ),
    ).filter(
      (element) =>
        !element.matches(":disabled") &&
        element.tabIndex >= 0 &&
        element.getClientRects().length > 0,
    );
    const first = controls[0],
      last = controls.at(-1);
    if (!first || !last) {
      event.preventDefault();
      current.focus();
      return;
    }
    if (
      event.shiftKey &&
      (document.activeElement === first ||
        !current.contains(document.activeElement))
    ) {
      event.preventDefault();
      last.focus();
    } else if (
      !event.shiftKey &&
      (document.activeElement === last ||
        !current.contains(document.activeElement))
    ) {
      event.preventDefault();
      first.focus();
    }
  }
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    request<OrderDetail>(`${base}/orders/${encodeURIComponent(id)}`, {
      signal: controller.signal,
    })
      .then((result) => {
        setData(result);
        setError(null);
      })
      .catch((cause) => {
        if (isAborted(cause)) return;
        if (
          cause instanceof ApiError &&
          (cause.status === 401 || cause.code === "password_change_required")
        )
          expired.current();
        else setError(errorMessage(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [base, id, revision, retry]);
  useEffect(() => {
    if (
      !selectedConditionId ||
      !data ||
      focusedIssue.current === selectedConditionId
    )
      return;
    const card = dialog.current?.querySelector<HTMLElement>(
      `[data-condition-id="${CSS.escape(selectedConditionId)}"]`,
    );
    if (!card) return;
    focusedIssue.current = selectedConditionId;
    card.focus({ preventScroll: true });
    card.scrollIntoView({ block: "start" });
  }, [data, selectedConditionId]);
  const order = data?.order;
  return (
    <dialog
      ref={dialog}
      className="detail-dialog"
      aria-labelledby="order-title"
      onCancel={(event) => {
        if (event.target !== event.currentTarget) return;
        if (issuePending) event.preventDefault();
        else onClose();
      }}
      onKeyDown={trapFocus}
    >
      <div className="dialog-header">
        <div>
          <p className="eyebrow">
            Supplier order evidence · Generation {generation}
          </p>
          <h2 id="order-title">
            {order
              ? `${order.source_order_id} / line ${order.source_line_id}`
              : "Order details"}
          </h2>
        </div>
        <button
          className="button secondary compact"
          type="button"
          disabled={issuePending}
          onClick={onClose}
        >
          Close
        </button>
      </div>
      <div className="dialog-content">
        {loading && !data ? <p role="status">Loading order evidence…</p> : null}
        {error ? (
          <ErrorNotice message={error} retry={() => setRetry((n) => n + 1)} />
        ) : null}
        {mutationError ? <ErrorNotice message={mutationError} /> : null}
        {data && order ? (
          <>
            <div className="detail-intro">
              <div>
                <h3>{displayText(order.description)}</h3>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => onSupplier(order.supplier_alias)}
                >
                  {displayText(
                    suppliers.find(
                      (item) => item.alias === order.supplier_alias,
                    )?.name,
                  ) || humanize(order.supplier_alias)}{" "}
                  · View supplier coverage
                </button>
              </div>
              <Badge tone="fictional">{originLabel(order.origin)}</Badge>
            </div>
            <div className="coverage-note">
              <strong>Supplier-reported facts and internal requirement</strong>
              <p>
                The requirement is an explicit internal mapping. Original
                contractual dates remain unknown when not provided by the
                source.
              </p>
            </div>
            <dl className="workspace-facts detail-facts">
              <div>
                <dt>Source connection</dt>
                <dd>
                  {displayText(
                    connections.find(
                      (connection) => connection.id === order.connection_id,
                    )?.name,
                  ) || "Source connection"}
                </dd>
              </div>
              <div>
                <dt>Source fulfillment status</dt>
                <dd>{humanize(order.status)}</dd>
              </div>
              <div>
                <dt>Fulfillment lifecycle</dt>
                <dd>
                  {humanize(order.lifecycle?.fulfillment_state ?? "unknown")}
                  <span className="cell-meta">
                    {order.lifecycle?.evidence === "quantities"
                      ? "Derived from source quantities"
                      : order.lifecycle?.evidence === "source_status"
                        ? "Source status fallback"
                        : "Insufficient source evidence"}
                  </span>
                </dd>
              </div>
              <div>
                <dt>Internal receipt verification</dt>
                <dd>Unavailable from this source</dd>
              </div>
              <div>
                <dt>Required arrival</dt>
                <dd>{dateOnly(order.requirement?.required_date)}</dd>
              </div>
              <div>
                <dt>Latest reported date</dt>
                <dd>
                  {dateOnly(order.arrival_date)}
                  <span className="cell-meta">
                    {displayText(order.arrival_label) ||
                      humanize(order.arrival_kind)}
                  </span>
                </dd>
              </div>
              <div>
                <dt>Original contractual date</dt>
                <dd>
                  {order.original_date || "Not provided by this source"}
                  <span className="cell-meta">
                    {displayText(order.original_label)}
                  </span>
                </dd>
              </div>
              <div>
                <dt>Confirmation</dt>
                <dd>
                  {humanize(
                    order.lifecycle?.confirmation_state ?? order.confirmation,
                  )}
                </dd>
              </div>
              <div>
                <dt>Cancellation state</dt>
                <dd>
                  {humanize(order.lifecycle?.cancellation_state ?? "unknown")}
                </dd>
              </div>
              <div>
                <dt>Ordered / cancelled</dt>
                <dd>
                  {facts(order.ordered)} / {facts(order.cancelled)} {order.uom}
                </dd>
              </div>
              <div>
                <dt>Shipped / delivered</dt>
                <dd>
                  {facts(order.shipped)} / {facts(order.delivered)} {order.uom}
                </dd>
              </div>
              <div>
                <dt>Remaining schedule</dt>
                <dd>{humanize(order.remaining_schedule)}</dd>
              </div>
              <div>
                <dt>Net ordered − source delivered</dt>
                <dd>
                  {facts(order.lifecycle?.unreceived)} {order.uom}
                  <span className="cell-meta">
                    Source-reported delivery balance
                  </span>
                </dd>
              </div>
              <div>
                <dt>Net ordered − source shipped</dt>
                <dd>
                  {facts(order.lifecycle?.unshipped)} {order.uom}
                </dd>
              </div>
              <div>
                <dt>First observed</dt>
                <dd>{localTime(order.first_observed_at)}</dd>
              </div>
              <div>
                <dt>Latest verification</dt>
                <dd>{localTime(order.last_observed_at)}</dd>
              </div>
              <div>
                <dt>Internal part / buyer</dt>
                <dd>
                  {order.requirement?.internal_part || "Unmapped"} /{" "}
                  {order.requirement?.buyer_alias || "Unassigned"}
                </dd>
              </div>
              {order.requirement ? (
                <div>
                  <dt>Mapped internal requirement</dt>
                  <dd>
                    {order.requirement.internal_po} / {order.requirement.line}
                  </dd>
                </div>
              ) : null}
            </dl>
            {order.lifecycle?.issues?.length ? (
              <div className="notice">
                <p>
                  Lifecycle evidence needs attention:{" "}
                  {order.lifecycle.issues.map(displayText).join("; ")}
                </p>
              </div>
            ) : null}
            <h3 className="section-title">Conditions and buyer workflow</h3>
            {data.conditions?.length ? (
              [...data.conditions]
                .sort(
                  (a, b) =>
                    Number(b.id === selectedConditionId) -
                    Number(a.id === selectedConditionId),
                )
                .map((condition) => (
                  <IssueCard
                    key={condition.id}
                    condition={condition}
                    title={issueName(condition)}
                    severityTone={tone(condition.severity)}
                    mutationError={mutationError}
                    onPending={setIssuePending}
                    notes={(data.notes ?? []).filter(
                      (note) => note.exception_id === condition.id,
                    )}
                    session={session}
                    mutate={mutate}
                    busy={busy || loading || !!error}
                  />
                ))
            ) : (
              <Empty title="No calculated conditions for this order" />
            )}
            <IssueActivity events={data.issue_events ?? []} />
            <h3 className="section-title">Observed change history</h3>
            {data.history?.length ? (
              <div className="table-scroll history-table">
                <table>
                  <caption className="sr-only">Stored order changes</caption>
                  <thead>
                    <tr>
                      <th>Detected</th>
                      <th>Field / semantics</th>
                      <th>Previous</th>
                      <th>Current</th>
                      <th>Change</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.history.map((event) => (
                      <tr key={event.id}>
                        <td>{localTime(event.detected_at)}</td>
                        <th scope="row">
                          {humanize(event.field)}
                          <span className="cell-meta">
                            {displayText(event.semantics)}
                          </span>
                        </th>
                        <td>{displayText(event.before) || "Unknown"}</td>
                        <td>{displayText(event.after) || "Unknown"}</td>
                        <td>
                          {event.delta_days
                            ? `${event.delta_days > 0 ? "+" : ""}${event.delta_days} calendar days`
                            : humanize(
                                event.worsened ? "worsened" : "observed_change",
                              )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="muted">
                No comparable source changes have been stored. The current
                observation is not an original contractual promise.
              </p>
            )}
          </>
        ) : null}
      </div>
    </dialog>
  );
}
