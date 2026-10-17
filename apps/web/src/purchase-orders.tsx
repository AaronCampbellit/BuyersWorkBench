import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { ApiError, errorMessage, isAborted, request } from "./api";
import type {
  Overview,
  PurchaseOrder,
  PurchaseOrderDetail,
  PurchaseOrderInput,
  Supplier,
} from "./workbench-types";
import type { Mutate } from "./workbench";
import { Modal } from "./modal";
import {
  Badge,
  displayText,
  Empty,
  ErrorNotice,
  humanize,
  localTime,
  Panel,
} from "./ui";

const decimalPattern = "[0-9]{1,18}([.][0-9]{1,4})?";
const actionNames: Record<string, string> = {
  approve: "Approve draft",
  revise: "Return to draft",
  place: "Record placement",
  receive: "Record receipt",
  close: "Close received order",
  cancel: "Cancel internal order",
};
function inputFrom(order: PurchaseOrder | null): PurchaseOrderInput {
  return order
    ? {
        supplier_id: order.supplier_id,
        description: displayText(order.description),
        quantity: order.quantity,
        uom: order.uom,
        currency: order.currency,
        unit_price: order.unit_price,
        freight: order.freight,
        other_charges: order.other_charges,
        required_date: order.required_date,
        notes: order.notes,
      }
    : {
        supplier_id: "",
        description: "",
        quantity: "",
        uom: "EA",
        currency: "USD",
        unit_price: "",
        freight: "0.00",
        other_charges: "0.00",
        required_date: "",
        notes: "",
      };
}
function requestKey(
  ref: { current: { signature: string; key: string } | null },
  body: unknown,
) {
  const signature = JSON.stringify(body);
  if (ref.current?.signature !== signature)
    ref.current = { signature, key: crypto.randomUUID() };
  return ref.current!.key;
}

export function PurchaseOrders({
  data,
  base,
  selectedId,
  onSelect,
  canManage,
  mutate,
  busy,
  mutationError,
  revision,
  onExpired,
  onViewQuotes,
  onManageSuppliers,
}: {
  data: Overview;
  base: string;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  canManage: boolean;
  mutate: Mutate;
  busy: boolean;
  mutationError: string | null;
  revision: number;
  onExpired: () => void;
  onViewQuotes: () => void;
  onManageSuppliers: () => void;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [supplier, setSupplier] = useState("");
  const orders = data.purchase_orders ?? [];
  const activeSuppliers = data.suppliers.filter(
    (item) => item.status === "active",
  );
  const query = search.trim().toLocaleLowerCase();
  const filtered = orders.filter(
    (order) =>
      (!status || order.status === status) &&
      (!supplier || order.supplier_id === supplier) &&
      (!query ||
        [
          order.number,
          displayText(order.description),
          displayText(order.supplier_name),
          order.placement_reference,
        ].some((value) => value.toLocaleLowerCase().includes(query))),
  );
  const currentEdit =
    editing && editing !== "new"
      ? orders.find((order) => order.id === editing)
      : null;
  return (
    <>
      <Panel
        className="internal-orders-panel"
        title="Internal purchase orders"
        description="One item per internal order. Approvals, external placement references, and receipts are recorded separately from supplier observations."
        actions={
          <div className="form-actions">
            <button
              type="button"
              className="button secondary compact"
              onClick={onViewQuotes}
            >
              View quotes
            </button>
            {canManage ? (
              <button
                type="button"
                className="button primary compact"
                disabled={busy || !activeSuppliers.length}
                onClick={() => setEditing("new")}
              >
                Create draft order
              </button>
            ) : null}
          </div>
        }
      >
        {canManage && !activeSuppliers.length ? (
          <div className="panel-content">
            <p className="muted">
              Add an active supplier before creating a manual draft.
            </p>
            <button
              type="button"
              className="text-button"
              onClick={onManageSuppliers}
            >
              Manage suppliers
            </button>
          </div>
        ) : null}
        {orders.length ? (
          <>
            <div className="filters purchase-order-filters">
              <label htmlFor="po-search">
                Search internal orders
                <input
                  id="po-search"
                  type="search"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="Order number, supplier, item, or reference"
                />
              </label>
              <label htmlFor="po-status">
                Internal order status
                <select
                  id="po-status"
                  value={status}
                  onChange={(event) => setStatus(event.target.value)}
                >
                  <option value="">All statuses</option>
                  {[...new Set(orders.map((order) => order.status))]
                    .sort()
                    .map((value) => (
                      <option key={value} value={value}>
                        {humanize(value)}
                      </option>
                    ))}
                </select>
              </label>
              <label htmlFor="po-supplier">
                Internal order supplier
                <select
                  id="po-supplier"
                  value={supplier}
                  onChange={(event) => setSupplier(event.target.value)}
                >
                  <option value="">All suppliers</option>
                  {[
                    ...new Map(
                      orders.map((order) => [
                        order.supplier_id,
                        order.supplier_name,
                      ]),
                    ).entries(),
                  ].map(([id, name]) => (
                    <option key={id} value={id}>
                      {displayText(name)}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {filtered.length ? (
              <div className="table-scroll">
                <table className="purchase-orders-table">
                  <caption className="sr-only">
                    Internal purchase orders
                  </caption>
                  <thead>
                    <tr>
                      <th>Internal order</th>
                      <th>Supplier / item</th>
                      <th>Ordered quantity</th>
                      <th>Recorded receipts</th>
                      <th>Modeled total</th>
                      <th>Required arrival</th>
                      <th>Status</th>
                      <th>Updated</th>
                      <th>Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filtered.map((order) => (
                      <tr key={order.id}>
                        <th scope="row">
                          <button
                            className="text-button"
                            type="button"
                            onClick={() => onSelect(order.id)}
                          >
                            {order.number}
                          </button>
                          <span className="cell-meta">
                            {order.source_quote_id
                              ? "Copied quote terms"
                              : "Manual internal draft"}
                          </span>
                        </th>
                        <td>
                          {displayText(order.supplier_name)}
                          <span className="cell-meta">
                            {displayText(order.description)}
                          </span>
                        </td>
                        <td>
                          {order.quantity} {order.uom}
                        </td>
                        <td>
                          {order.received_quantity} {order.uom}
                        </td>
                        <td>
                          {order.currency} {order.modeled_total}
                        </td>
                        <td>{order.required_date}</td>
                        <td>
                          <Badge
                            tone={
                              order.status === "cancelled"
                                ? "attention"
                                : order.status === "closed"
                                  ? "fictional"
                                  : "neutral"
                            }
                          >
                            {humanize(order.status)}
                          </Badge>
                        </td>
                        <td>{localTime(order.updated_at)}</td>
                        <td>
                          <button
                            type="button"
                            className="button secondary compact"
                            onClick={() => onSelect(order.id)}
                          >
                            Manage
                            <span className="sr-only"> {order.number}</span>
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty title="No internal orders match these filters" />
            )}
          </>
        ) : (
          <Empty title="No internal purchase orders yet">
            {canManage
              ? "Create a manual draft or copy an eligible saved quote. Draft creation and approval do not place an order with a supplier."
              : "No internal purchase orders are available in this workspace."}
          </Empty>
        )}
        <p className="panel-footnote">
          Internal order status and recorded receipts are buyer-managed records.
          Source fulfillment and buyer exception tasks keep their own states.
        </p>
      </Panel>
      {selectedId && !editing ? (
        <PurchaseOrderInspector
          base={base}
          id={selectedId}
          revision={revision}
          canManage={canManage}
          mutate={mutate}
          busy={busy}
          mutationError={mutationError}
          onClose={() => onSelect(null)}
          onExpired={onExpired}
          onEdit={(order) => {
            onSelect(null);
            setEditing(order.id);
          }}
        />
      ) : null}
      {editing && (editing === "new" || currentEdit) ? (
        <PurchaseOrderEditor
          key={editing}
          order={currentEdit ?? null}
          suppliers={data.suppliers}
          mutate={mutate}
          busy={busy}
          serverError={mutationError}
          onClose={() => setEditing(null)}
          onSaved={() => {
            const id = editing;
            setEditing(null);
            if (id !== "new") onSelect(id);
          }}
        />
      ) : null}
    </>
  );
}

function PurchaseOrderEditor({
  order,
  suppliers,
  mutate,
  busy,
  serverError,
  onClose,
  onSaved,
}: {
  order: PurchaseOrder | null;
  suppliers: Supplier[];
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState(() => inputFrom(order));
  const [baseRevision, setBaseRevision] = useState(order?.revision ?? 0);
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const key = useRef<{ signature: string; key: string } | null>(null);
  const conflicted = !!order && order.revision !== baseRevision;
  const locked = !!order && order.status !== "draft";
  function set<K extends keyof PurchaseOrderInput>(
    field: K,
    value: PurchaseOrderInput[K],
  ) {
    setDraft((current) => ({ ...current, [field]: value }));
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || busy || conflicted || locked) return;
    setSaving(true);
    setAttempted(true);
    const body = {
      ...draft,
      description:
        order && draft.description === displayText(order.description)
          ? order.description
          : draft.description,
      notes: draft.notes.trim(),
      uom: draft.uom.trim(),
    };
    const success = await mutate(
      order
        ? `/purchase-orders/${encodeURIComponent(order.id)}`
        : "/purchase-orders",
      order
        ? { revision: baseRevision, ...body }
        : { ...body, idempotency_key: requestKey(key, body) },
      order ? "PATCH" : "POST",
    );
    setSaving(false);
    if (success) onSaved();
  }
  return (
    <Modal
      title={
        order ? `Edit draft ${order.number}` : "Create internal draft order"
      }
      eyebrow="Internal purchase order · One item"
      onClose={onClose}
      pending={saving}
      className="purchase-order-dialog"
    >
      <form className="dialog-content" onSubmit={submit} aria-busy={saving}>
        <p className="muted po-form-intro">
          Save internal purchasing terms for review and approval. This action
          sends no message and places no order with a supplier.
        </p>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              Newer order terms are available. Reload and review them before
              saving; your unsaved inputs will be replaced.
            </p>
            <button
              type="button"
              className="button secondary compact"
              disabled={busy || saving}
              onClick={() => {
                setDraft(inputFrom(order));
                setBaseRevision(order!.revision);
                setAttempted(false);
              }}
            >
              Reload draft terms
            </button>
          </div>
        ) : null}
        {locked ? (
          <ErrorNotice message="This order is no longer a draft. Close this editor and review its current status." />
        ) : null}
        <fieldset className="po-form-grid" disabled={busy || saving || locked}>
          <div className="po-wide">
            <label htmlFor="po-draft-supplier">Supplier</label>
            <select
              data-initial-focus
              id="po-draft-supplier"
              required
              value={draft.supplier_id}
              onChange={(event) => set("supplier_id", event.target.value)}
            >
              <option value="">Choose an active supplier</option>
              {suppliers.map((item) => (
                <option
                  key={item.id}
                  value={item.id}
                  disabled={item.status !== "active"}
                >
                  {displayText(item.name)}
                  {item.status !== "active" ? " (inactive)" : ""}
                </option>
              ))}
            </select>
          </div>
          <div className="po-wide">
            <label htmlFor="po-description">Item description</label>
            <input
              id="po-description"
              required
              maxLength={500}
              value={draft.description}
              onChange={(event) => set("description", event.target.value)}
            />
          </div>
          <div>
            <label htmlFor="po-quantity">Order quantity</label>
            <input
              id="po-quantity"
              type="text"
              inputMode="decimal"
              pattern={decimalPattern}
              required
              value={draft.quantity}
              onChange={(event) => set("quantity", event.target.value)}
            />
          </div>
          <div>
            <label htmlFor="po-uom">Unit of measure</label>
            <input
              id="po-uom"
              required
              maxLength={24}
              value={draft.uom}
              onChange={(event) => set("uom", event.target.value)}
            />
          </div>
          <div>
            <label htmlFor="po-required-date">Required arrival</label>
            <input
              id="po-required-date"
              type="date"
              required
              value={draft.required_date}
              onChange={(event) => set("required_date", event.target.value)}
            />
          </div>
          {(
            [
              ["unit_price", "Unit price"],
              ["freight", "Included freight"],
              ["other_charges", "Other included charges"],
            ] as const
          ).map(([field, label]) => (
            <div key={field}>
              <label htmlFor={`po-${field}`}>{label} (USD)</label>
              <input
                id={`po-${field}`}
                type="text"
                inputMode="decimal"
                required
                pattern={decimalPattern}
                value={draft[field]}
                onChange={(event) => set(field, event.target.value)}
              />
            </div>
          ))}
          <div className="po-wide">
            <label htmlFor="po-notes">Order notes</label>
            <textarea
              id="po-notes"
              rows={3}
              maxLength={4000}
              value={draft.notes}
              onChange={(event) => set("notes", event.target.value)}
            />
          </div>
          {order?.quote_source ? (
            <p className="po-wide cell-meta">
              Original quote snapshot remains fixed at comparison revision{" "}
              {order.quote_source.revision}. Editing draft terms does not change
              that snapshot or the saved comparison.
            </p>
          ) : null}
          <div className="po-wide form-actions">
            <button
              type="submit"
              className="button primary"
              disabled={
                conflicted || !draft.description.trim() || !draft.supplier_id
              }
            >
              {saving
                ? "Saving draft…"
                : order
                  ? "Save draft terms"
                  : "Create internal draft"}
            </button>
            <button
              type="button"
              className="button secondary"
              disabled={saving}
              onClick={onClose}
            >
              Cancel
            </button>
            <span className="cell-meta">
              USD only. The modeled total is calculated when saved.
            </span>
          </div>
        </fieldset>
      </form>
    </Modal>
  );
}

function PurchaseOrderInspector({
  base,
  id,
  revision,
  canManage,
  mutate,
  busy,
  mutationError,
  onClose,
  onExpired,
  onEdit,
}: {
  base: string;
  id: string;
  revision: number;
  canManage: boolean;
  mutate: Mutate;
  busy: boolean;
  mutationError: string | null;
  onClose: () => void;
  onExpired: () => void;
  onEdit: (order: PurchaseOrder) => void;
}) {
  const [data, setData] = useState<PurchaseOrderDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const [action, setAction] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState(false);
  const expired = useRef(onExpired);
  expired.current = onExpired;
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    request<PurchaseOrderDetail>(
      `${base}/purchase-orders/${encodeURIComponent(id)}`,
      { signal: controller.signal },
    )
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
  const order = data?.order;
  return (
    <Modal
      title={order ? order.number : "Internal order details"}
      eyebrow="Internal purchase order"
      onClose={onClose}
      pending={actionPending}
      className="purchase-order-dialog"
    >
      <div className="dialog-content">
        {loading && !data ? <p role="status">Loading internal order…</p> : null}
        {error ? (
          <ErrorNotice
            message={error}
            retry={() => setRetry((value) => value + 1)}
          />
        ) : null}
        {order && data ? (
          <>
            <div className="detail-intro">
              <div>
                <h3>{displayText(order.description)}</h3>
                <p className="muted">
                  {displayText(order.supplier_name)} · Supplier name saved with
                  these order terms
                </p>
              </div>
              <Badge
                tone={order.status === "cancelled" ? "attention" : "neutral"}
              >
                {humanize(order.status)}
              </Badge>
            </div>
            <dl className="workspace-facts detail-facts">
              <div>
                <dt>Ordered quantity</dt>
                <dd>
                  {order.quantity} {order.uom}
                </dd>
              </div>
              <div>
                <dt>Recorded receipts</dt>
                <dd>
                  {order.received_quantity} {order.uom}
                </dd>
              </div>
              <div>
                <dt>Required arrival</dt>
                <dd>{order.required_date}</dd>
              </div>
              <div>
                <dt>Unit price</dt>
                <dd>
                  {order.currency} {order.unit_price}
                </dd>
              </div>
              <div>
                <dt>Included freight / other charges</dt>
                <dd>
                  {order.currency} {order.freight} / {order.other_charges}
                </dd>
              </div>
              <div>
                <dt>Modeled total</dt>
                <dd>
                  {order.currency} {order.modeled_total}
                </dd>
              </div>
              <div>
                <dt>External placement reference</dt>
                <dd>{order.placement_reference || "Not recorded"}</dd>
              </div>
              <div>
                <dt>Created / updated</dt>
                <dd>
                  {localTime(order.created_at)}
                  <span className="cell-meta">
                    Updated {localTime(order.updated_at)}
                  </span>
                </dd>
              </div>
              <div>
                <dt>Current saved revision</dt>
                <dd>{order.revision}</dd>
              </div>
            </dl>
            {order.notes ? (
              <div className="supplier-profile-notes">
                <h3>Order notes</h3>
                <p>{order.notes}</p>
              </div>
            ) : null}
            {order.quote_source ? (
              <div className="coverage-note">
                <strong>
                  Copied quote terms · Saved revision{" "}
                  {order.quote_source.revision}
                </strong>
                <p>
                  {displayText(order.quote_source.quote.supplier)}:{" "}
                  {order.quote_source.quantity} {order.quote_source.quote.uom}{" "}
                  at {order.quote_source.quote.currency}{" "}
                  {order.quote_source.quote.unit_price}; freight{" "}
                  {order.quote_source.quote.freight}; other charges{" "}
                  {order.quote_source.quote.other_charges}; arrival{" "}
                  {order.quote_source.quote.arrival_date}. Excluded costs:{" "}
                  {order.quote_source.excluded_costs
                    .map(displayText)
                    .join(", ") || "None declared"}
                  . This original snapshot stays fixed when quotes or draft
                  terms change.
                </p>
              </div>
            ) : (
              <p className="cell-meta">
                Manual internal order. No quote comparison is linked.
              </p>
            )}
            <div className="coverage-note">
              <strong>Internal records only</strong>
              <p>
                Approval does not place this order. Placement is recorded only
                after it happens outside this app. Receipts below are explicit
                buyer entries and do not change supplier observations.
              </p>
            </div>
            {canManage ? (
              <div className="po-actions">
                {order.status === "draft" ? (
                  <button
                    className="button secondary compact"
                    disabled={busy || loading || !!error || actionPending}
                    type="button"
                    onClick={() => onEdit(order)}
                  >
                    Edit draft terms
                  </button>
                ) : null}
                {order.allowed_actions.map((value) => (
                  <button
                    key={value}
                    className={`button ${value === "cancel" ? "danger" : "secondary"} compact`}
                    disabled={busy || loading || !!error || actionPending}
                    type="button"
                    onClick={() => setAction(value)}
                  >
                    {actionNames[value] ?? humanize(value)}
                  </button>
                ))}
              </div>
            ) : null}
            {action && canManage ? (
              <PurchaseOrderAction
                key={action}
                action={action}
                order={order}
                mutate={mutate}
                busy={busy || loading || !!error}
                serverError={mutationError}
                onCancel={() => setAction(null)}
                onDone={() => setAction(null)}
                onPending={setActionPending}
              />
            ) : null}
            <h3 className="section-title">Internal order history</h3>
            {data.events.length ? (
              <ol className="po-history">
                {data.events.map((event) => (
                  <li key={event.id}>
                    <div>
                      <strong>
                        {actionNames[event.action] ?? humanize(event.action)}
                      </strong>
                      <Badge>
                        {humanize(event.from_status || "created")} →{" "}
                        {humanize(event.to_status)}
                      </Badge>
                    </div>
                    <p>
                      {event.quantity
                        ? `${event.quantity} ${order.uom} · `
                        : ""}
                      {event.reference ? `Reference: ${event.reference}` : ""}
                    </p>
                    {event.note ? (
                      <p className="plain-text">{event.note}</p>
                    ) : null}
                    <span className="cell-meta">
                      {event.actor_name} · {localTime(event.created_at)}
                    </span>
                  </li>
                ))}
              </ol>
            ) : (
              <p className="muted">No internal order events recorded.</p>
            )}
          </>
        ) : null}
      </div>
    </Modal>
  );
}

function PurchaseOrderAction({
  action,
  order,
  mutate,
  busy,
  serverError,
  onCancel,
  onDone,
  onPending,
}: {
  action: string;
  order: PurchaseOrder;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onCancel: () => void;
  onDone: () => void;
  onPending: (pending: boolean) => void;
}) {
  const [quantity, setQuantity] = useState("");
  const [reference, setReference] = useState("");
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [baseRevision, setBaseRevision] = useState(order.revision);
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const key = useRef<{ signature: string; key: string } | null>(null);
  const conflicted = order.revision !== baseRevision;
  const allowed = order.allowed_actions.includes(action);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || busy || conflicted || !allowed) return;
    setSaving(true);
    onPending(true);
    setAttempted(true);
    const body = {
      revision: baseRevision,
      action,
      quantity: quantity.trim(),
      reference: reference.trim(),
      note: note.trim(),
      confirm,
    };
    const success = await mutate(
      `/purchase-orders/${encodeURIComponent(order.id)}/actions`,
      { ...body, idempotency_key: requestKey(key, body) },
    );
    setSaving(false);
    onPending(false);
    if (success) onDone();
  }
  return (
    <form className="po-action-form" onSubmit={submit} aria-busy={saving}>
      <h3>{actionNames[action] ?? humanize(action)}</h3>
      {attempted && !saving && serverError ? (
        <ErrorNotice message={serverError} />
      ) : null}
      {conflicted ? (
        <div className="notice" role="alert">
          <p>
            This order changed. Review its current terms and status before
            retrying.
          </p>
          {allowed ? (
            <button
              className="button secondary compact"
              type="button"
              disabled={busy || saving}
              onClick={() => {
                setBaseRevision(order.revision);
                setQuantity("");
                setReference("");
                setNote("");
                setConfirm(false);
                setAttempted(false);
              }}
            >
              Reload action details
            </button>
          ) : null}
        </div>
      ) : null}
      {!allowed ? (
        <p className="muted">
          This action is unavailable in the current order state. Cancel this
          action to review the available choices.
        </p>
      ) : null}
      <fieldset disabled={busy || saving || !allowed}>
        {action === "place" ? (
          <p className="muted">
            Record placement after you have completed it outside
            BuyersWorkBench. This app sends no supplier order or message.
          </p>
        ) : action === "receive" ? (
          <p className="muted">
            Enter this receipt's positive quantity and reference. Previously
            recorded receipts remain in the total.
          </p>
        ) : action === "cancel" ? (
          <p className="muted">
            Record a reason for cancelling this internal order. Previously
            recorded receipts remain in history.
          </p>
        ) : action === "close" ? (
          <p className="muted">
            Close this order after its full quantity has been recorded as
            received.
          </p>
        ) : null}
        {action === "receive" ? (
          <label htmlFor="po-action-quantity">
            This receipt quantity
            <input
              id="po-action-quantity"
              type="text"
              inputMode="decimal"
              required
              pattern={decimalPattern}
              value={quantity}
              onChange={(event) => setQuantity(event.target.value)}
            />
          </label>
        ) : null}
        {action === "place" || action === "receive" ? (
          <label htmlFor="po-action-reference">
            {action === "place"
              ? "External placement reference"
              : "Receipt reference"}
            <input
              id="po-action-reference"
              required
              maxLength={100}
              value={reference}
              onChange={(event) => setReference(event.target.value)}
            />
          </label>
        ) : null}
        <label htmlFor="po-action-note">
          {action === "cancel"
            ? "Cancellation reason (required)"
            : "Action note (optional)"}
          <textarea
            id="po-action-note"
            rows={3}
            required={action === "cancel"}
            maxLength={4000}
            value={note}
            onChange={(event) => setNote(event.target.value)}
          />
        </label>
        {action === "place" ? (
          <label className="check-label">
            <input
              type="checkbox"
              required
              checked={confirm}
              onChange={(event) => setConfirm(event.target.checked)}
            />{" "}
            I confirm this order was placed with the supplier outside this app.
          </label>
        ) : null}
        <div className="form-actions">
          <button
            className={`button ${action === "cancel" ? "danger" : "primary"}`}
            type="submit"
            disabled={conflicted || (action === "place" && !confirm)}
          >
            {saving
              ? "Saving action…"
              : `Confirm: ${actionNames[action] ?? humanize(action)}`}
          </button>
          <button
            className="button secondary"
            type="button"
            disabled={saving}
            onClick={onCancel}
          >
            Cancel action
          </button>
        </div>
      </fieldset>
    </form>
  );
}
