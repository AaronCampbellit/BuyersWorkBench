import { Connections } from "./connections";
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { ApiError, errorMessage, isAborted, request } from "./api";
import type { Session, Workspace } from "./api";
import type {
  Exception,
  Notification,
  PurchaseOrder,
  QuoteResult,
  Overview,
  Quote,
  Quotes,
  Requirement,
} from "./workbench-types";
import type { Mutate } from "./workbench";
import { Modal } from "./modal";
import {
  Badge,
  dateOnly,
  displayText,
  displayWorkspaceName,
  Empty,
  ErrorNotice,
  humanize,
  localTime,
  Panel,
} from "./ui";

export function QuoteComparison({
  quotes,
  mutate,
  busy,
  actionError,
  purchaseOrders,
  canCreateOrder,
  onViewOrders,
  onOpenPurchaseOrder,
}: {
  purchaseOrders: PurchaseOrder[];
  canCreateOrder: boolean;
  onViewOrders: () => void;
  onOpenPurchaseOrder: (id: string) => void;
  quotes: Quotes | null;
  mutate: Mutate;
  busy: boolean;
  actionError: string | null;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const [converting, setConverting] = useState<string | null>(null);
  const [calculating, setCalculating] = useState(false);
  const [failed, setFailed] = useState(false);
  const [savedRevision, setSavedRevision] = useState<number | null>(null);
  if (!quotes)
    return (
      <Panel title="Quote comparison">
        <Empty title="No comparison is loaded">
          The comparison becomes available after the workspace is initialized.
          Manual internal drafts are available from Orders.
        </Empty>
      </Panel>
    );
  const comparison = quotes.comparison;
  const calculation = quotes.calculation;
  const awaitingCalculation =
    calculating || (savedRevision !== null && quotes.revision <= savedRevision);
  const selected = comparison.quotes.find((item) => item.id === editing);
  async function save(quote: Quote) {
    setCalculating(true);
    setFailed(false);
    setSavedRevision(quotes!.revision);
    const success = await mutate(
      `/quotes/${encodeURIComponent(quote.id)}`,
      { revision: quotes!.revision, quote },
      "PATCH",
    );
    setCalculating(false);
    if (success) setEditing(null);
    else {
      setFailed(true);
      setSavedRevision(null);
    }
    return success;
  }
  return (
    <>
      <div className="coverage-note">
        <strong>
          Comparison · {comparison.quantity} {comparison.uom} · Required{" "}
          {comparison.required_date}
        </strong>
        <p>
          {displayText(comparison.description)} Excluded costs:{" "}
          {comparison.excluded_costs?.map(displayText).join(", ") ||
            "None declared"}
          . Currency conversion is outside this model. This comparison is a
          pre-order evaluation; editing an offer does not create or fulfill an
          order.
        </p>
      </div>
      <Panel
        title="Server-calculated offer comparison"
        description={`Saved revision ${quotes.revision}. Modeled total = quantity × unit price + freight + included charges.`}
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
        {awaitingCalculation ? (
          <p className="winner-strip" role="status">
            Saving and recalculating. The previous winner is withheld.
          </p>
        ) : failed ? (
          <div className="notice">
            <p>
              Recalculation did not complete. No current winner is shown.{" "}
              {displayText(actionError) ||
                "Review your inputs and retry the save."}
            </p>
          </div>
        ) : (
          <div className="winner-strip">
            <div>
              <span className="eyebrow">Lowest eligible offer</span>
              <strong>
                {displayText(calculation.winner_supplier) ||
                  "No eligible offer"}
              </strong>
            </div>
            {calculation.savings_versus_atlas ? (
              <span>
                Modeled savings versus Atlas: {comparison.currency}{" "}
                {calculation.savings_versus_atlas}
              </span>
            ) : null}
          </div>
        )}
        <div className="table-scroll">
          <table>
            <caption className="sr-only">
              Offers and calculated eligibility
            </caption>
            <thead>
              <tr>
                <th>Supplier</th>
                <th>Unit price</th>
                <th>Freight</th>
                <th>Other charges</th>
                <th>Modeled total</th>
                <th>Arrival</th>
                <th>Approval / eligibility</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {(calculation.results ?? []).map((item) => (
                <tr
                  key={item.id}
                  className={
                    !failed &&
                    !awaitingCalculation &&
                    item.id === calculation.winner_id
                      ? "selected-row"
                      : undefined
                  }
                >
                  <th scope="row">
                    {displayText(item.supplier)}
                    {item.rank > 0 ? (
                      <span className="cell-meta">
                        Eligible rank {item.rank}
                      </span>
                    ) : null}
                  </th>
                  <td>
                    {item.currency} {item.unit_price}
                  </td>
                  <td>
                    {item.currency} {item.freight}
                  </td>
                  <td>
                    {item.currency} {item.other_charges}
                  </td>
                  <td>
                    <strong>
                      {item.currency} {item.modeled_total || "Not calculable"}
                    </strong>
                    <span className="cell-meta">
                      {comparison.quantity} {item.uom}
                    </span>
                  </td>
                  <td>{dateOnly(item.arrival_date)}</td>
                  <td>
                    <Badge tone={item.eligible ? "fictional" : "attention"}>
                      {item.eligible ? "Eligible" : "Ineligible"}
                    </Badge>
                    <span className="cell-meta">{humanize(item.approval)}</span>
                    {item.reasons?.map((reason) => (
                      <span className="cell-meta" key={reason}>
                        {displayText(reason)}
                      </span>
                    ))}
                  </td>
                  <td>
                    <div className="quote-row-actions">
                      <button
                        type="button"
                        className="button secondary compact"
                        disabled={busy || calculating}
                        onClick={() => setEditing(item.id)}
                      >
                        Edit
                        <span className="sr-only">
                          {" "}
                          {displayText(item.supplier)}
                        </span>
                      </button>
                      {purchaseOrders.find(
                        (order) => order.source_quote_id === item.id,
                      ) ? (
                        <button
                          type="button"
                          className="text-button"
                          onClick={() =>
                            onOpenPurchaseOrder(
                              purchaseOrders.find(
                                (order) => order.source_quote_id === item.id,
                              )!.id,
                            )
                          }
                        >
                          View{" "}
                          {
                            purchaseOrders.find(
                              (order) => order.source_quote_id === item.id,
                            )!.number
                          }
                        </button>
                      ) : canCreateOrder ? (
                        <button
                          type="button"
                          className="button secondary compact"
                          disabled={
                            busy ||
                            awaitingCalculation ||
                            failed ||
                            !item.eligible ||
                            editing !== null
                          }
                          onClick={() => setConverting(item.id)}
                        >
                          Create draft order
                          <span className="sr-only">
                            {" "}
                            from {displayText(item.supplier)}
                          </span>
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="panel-footnote">
          All offers stay visible, including cheaper ineligible offers. Inputs
          are persisted and totals are calculated by the server using exact
          decimal arithmetic.
        </p>
      </Panel>
      {converting &&
      calculation.results.find((item) => item.id === converting) ? (
        <QuoteDraftConfirmation
          key={converting}
          quotes={quotes}
          quote={calculation.results.find((item) => item.id === converting)!}
          mutate={mutate}
          busy={busy}
          serverError={actionError}
          onCancel={() => setConverting(null)}
          onSaved={() => {
            setConverting(null);
            onViewOrders();
          }}
        />
      ) : null}
      {selected ? (
        <QuoteEditor
          key={`${selected.id}-${quotes.revision}`}
          quote={selected}
          busy={busy || calculating}
          onSave={save}
          onCancel={() => setEditing(null)}
        />
      ) : null}
    </>
  );
}

function QuoteDraftConfirmation({
  quotes,
  quote,
  mutate,
  busy,
  serverError,
  onCancel,
  onSaved,
}: {
  quotes: Quotes;
  quote: QuoteResult;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onCancel: () => void;
  onSaved: () => void;
}) {
  const [snapshot, setSnapshot] = useState(() => ({ quotes, quote }));
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const key = useRef<{ revision: number; key: string } | null>(null);
  const conflicted = snapshot.quotes.revision !== quotes.revision;
  const offer = snapshot.quote;
  const comparison = snapshot.quotes.comparison;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || busy || conflicted || !offer.eligible) return;
    setSaving(true);
    setAttempted(true);
    if (key.current?.revision !== snapshot.quotes.revision)
      key.current = {
        revision: snapshot.quotes.revision,
        key: crypto.randomUUID(),
      };
    const success = await mutate(
      `/quotes/${encodeURIComponent(offer.id)}/purchase-order`,
      { revision: snapshot.quotes.revision, idempotency_key: key.current!.key },
    );
    setSaving(false);
    if (success) onSaved();
  }
  return (
    <Modal
      title={`Create draft from ${displayText(offer.supplier)}`}
      eyebrow="Saved eligible quote → Internal draft"
      onClose={onCancel}
      pending={saving}
      className="purchase-order-dialog"
    >
      <form className="dialog-content" onSubmit={submit}>
        <p className="muted">
          Copy the saved terms below into an internal draft order for review and
          approval. This sends no supplier message and places no order. Later
          quote edits do not change the copied snapshot.
        </p>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              The saved comparison changed. Reload and review its current terms
              before creating a draft.
            </p>
            <button
              type="button"
              className="button secondary compact"
              disabled={busy || saving}
              onClick={() => {
                setSnapshot({ quotes, quote });
                setAttempted(false);
              }}
            >
              Reload saved quote terms
            </button>
          </div>
        ) : null}
        <dl className="workspace-facts detail-facts">
          <div>
            <dt>Item</dt>
            <dd>{displayText(comparison.description)}</dd>
          </div>
          <div>
            <dt>Quantity</dt>
            <dd>
              {comparison.quantity} {comparison.uom}
            </dd>
          </div>
          <div>
            <dt>Unit price</dt>
            <dd>
              {offer.currency} {offer.unit_price}
            </dd>
          </div>
          <div>
            <dt>Freight / other charges</dt>
            <dd>
              {offer.currency} {offer.freight} / {offer.other_charges}
            </dd>
          </div>
          <div>
            <dt>Modeled total</dt>
            <dd>
              {offer.currency} {offer.modeled_total}
            </dd>
          </div>
          <div>
            <dt>Supplier-offered arrival</dt>
            <dd>{offer.arrival_date}</dd>
          </div>
          <div>
            <dt>Required arrival</dt>
            <dd>{comparison.required_date}</dd>
          </div>
          <div>
            <dt>Approval / saved revision</dt>
            <dd>
              {humanize(offer.approval)} · {snapshot.quotes.revision}
            </dd>
          </div>
          <div>
            <dt>Excluded costs</dt>
            <dd>
              {comparison.excluded_costs.map(displayText).join(", ") ||
                "None declared"}
            </dd>
          </div>
        </dl>
        {!offer.eligible ? (
          <ErrorNotice message="This offer is no longer eligible for draft conversion." />
        ) : null}
        <div className="form-actions">
          <button
            data-initial-focus
            type="submit"
            className="button primary"
            disabled={busy || saving || conflicted || !offer.eligible}
          >
            {saving
              ? "Creating internal draft…"
              : "Confirm: Create internal draft"}
          </button>
          <button
            type="button"
            className="button secondary"
            disabled={saving}
            onClick={onCancel}
          >
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  );
}

function QuoteEditor({
  quote,
  busy,
  onSave,
  onCancel,
}: {
  quote: Quote;
  busy: boolean;
  onSave: (quote: Quote) => Promise<boolean>;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(quote);
  const set = (key: keyof Quote, value: string) =>
    setDraft((current) => ({ ...current, [key]: value }));
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void onSave(draft);
  }
  return (
    <Panel
      title={`Edit offer · ${displayText(quote.supplier)}`}
      description="The saved comparison remains unchanged until you save these inputs."
    >
      <form className="panel-content" onSubmit={submit}>
        <fieldset className="quote-form" disabled={busy}>
          {(
            [
              ["unit_price", "Unit price"],
              ["freight", "Freight"],
              ["other_charges", "Other included charges"],
            ] as const
          ).map(([key, label]) => (
            <label key={key}>
              {label}
              <input
                type="text"
                inputMode="decimal"
                pattern="[0-9]+([.][0-9]{1,4})?"
                required
                value={draft[key]}
                onChange={(e) => set(key, e.target.value)}
              />
            </label>
          ))}
          <label>
            Arrival date
            <input
              type="date"
              required
              value={draft.arrival_date}
              onChange={(e) => set("arrival_date", e.target.value)}
            />
          </label>
          <label>
            Approval
            <select
              value={draft.approval}
              onChange={(e) => set("approval", e.target.value)}
            >
              <option value="approved">Approved</option>
              <option value="pending">Pending</option>
              <option value="rejected">Rejected</option>
            </select>
          </label>
          <label>
            Unit of measure
            <input
              value={draft.uom}
              onChange={(e) => set("uom", e.target.value)}
              required
              maxLength={12}
            />
          </label>
          <label>
            Currency
            <select
              value={draft.currency}
              onChange={(e) => set("currency", e.target.value)}
            >
              <option value="USD">USD</option>
              {draft.currency !== "USD" ? (
                <option>{draft.currency}</option>
              ) : null}
            </select>
          </label>
          <div className="form-actions">
            <button className="button primary" type="submit">
              {busy ? "Saving…" : "Save and recalculate"}
            </button>
            <button
              className="button secondary"
              type="button"
              onClick={onCancel}
            >
              Cancel edit
            </button>
          </div>
        </fieldset>
      </form>
    </Panel>
  );
}

const scenarios = [
  {
    id: "SC01",
    name: "Baseline",
    description:
      "Restore the four source lines and collect their baseline facts.",
  },
  {
    id: "SC02",
    name: "Delivery delay",
    description:
      "Move 10482 arrival from October 21 to October 28. Rules evaluate the change.",
  },
  {
    id: "SC03",
    name: "Partial shipment",
    description:
      "Report 40 shipped, zero delivered, and an unknown schedule for the remaining 60.",
  },
  {
    id: "SC04",
    name: "Confirmation restored",
    description: "Explicitly confirm 10520 and collect. Existing notes remain.",
  },
  {
    id: "SC05",
    name: "Source failure",
    description:
      "Simulate a collection failure. Prior accepted observations remain intact.",
  },
  {
    id: "SC06",
    name: "Quote trade-off",
    description:
      "Load the versioned comparison for cost and eligibility exploration.",
  },
  {
    id: "SC07",
    name: "Repeat unchanged",
    description:
      "Collect the same source revision. Verify freshness without duplicate material changes.",
  },
  {
    id: "SC08",
    name: "Recovery",
    description:
      "Restore 10482 arrival to October 21 and collect the improvement.",
  },
];

export function DemoControls({
  workspace,
  data,
  mutate,
  busy,
  resetting,
}: {
  workspace: Workspace;
  data: Overview;
  mutate: Mutate;
  busy: boolean;
  resetting: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const [name, setName] = useState("");
  const workspaceLabel = displayWorkspaceName(workspace.name);
  const keys = useRef(new Map<string, string>());
  useEffect(() => {
    keys.current.clear();
    setConfirming(false);
    setName("");
  }, [data.generation]);
  async function scenario(id: string) {
    const key = keys.current.get(id) ?? crypto.randomUUID();
    keys.current.set(id, key);
    if (await mutate(`/demo/scenarios/${id}`, { idempotency_key: key }))
      keys.current.delete(id);
  }
  async function reset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (name !== workspaceLabel) return;
    const key = keys.current.get("reset") ?? crypto.randomUUID();
    keys.current.set("reset", key);
    if (await mutate("/demo/reset", { idempotency_key: key, confirm: true })) {
      keys.current.delete("reset");
      setConfirming(false);
      setName("");
    }
  }
  return (
    <>
      <div className="coverage-note">
        <strong>These controls affect only {workspaceLabel}.</strong>
        <p>
          Changes enqueue collection through the common pipeline. Business date:{" "}
          {data.business_date}. Generation: {data.generation}.
        </p>
      </div>
      <Panel
        title="Source scenarios"
        description="Wait for the collection run to finish, then inspect calculated conditions and source health."
      >
        <div className="scenario-list">
          {scenarios.map((item) => (
            <div className="scenario-row" key={item.id}>
              <span className="scenario-id">{item.id}</span>
              <div>
                <h3>{item.name}</h3>
                <p>{displayText(item.description)}</p>
              </div>
              <button
                type="button"
                className="button secondary compact"
                disabled={busy || !data.seeded}
                onClick={() => scenario(item.id)}
              >
                Apply {item.id}
              </button>
            </div>
          ))}
        </div>
      </Panel>
      <Panel
        title="Reset this workspace"
        description="Reset is scoped to this workspace. Identities, memberships, and security audit records are preserved."
      >
        <div className="panel-content">
          <p className="muted">
            Reset restores seeded supplier profiles and removes added suppliers
            and profile edits from this demo workspace. It also clears its
            scenario edits, notes, buyer workflow, notifications, quote edits,
            and internal purchase orders with their recorded action history.
            Reset advances the generation, and queues the pinned baseline.
            Private data and other workspaces are unaffected. Old jobs cannot
            publish into the new generation.
          </p>
          {confirming ? (
            <form onSubmit={reset} className="reset-form">
              <label htmlFor="reset-name">
                Type “{workspaceLabel}” to confirm
              </label>
              <input
                id="reset-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                autoComplete="off"
              />
              <div className="form-actions">
                <button
                  className="button danger"
                  type="submit"
                  disabled={resetting || name !== workspaceLabel}
                >
                  {resetting ? "Resetting…" : "Confirm workspace reset"}
                </button>
                <button
                  className="button secondary"
                  type="button"
                  disabled={resetting}
                  onClick={() => setConfirming(false)}
                >
                  Cancel
                </button>
              </div>
            </form>
          ) : (
            <button
              className="button danger"
              type="button"
              disabled={resetting || !data.seeded}
              onClick={() => setConfirming(true)}
            >
              Reset {workspaceLabel}
            </button>
          )}
        </div>
      </Panel>
    </>
  );
}

export function DataHealth({
  data,
  fictional,
  mutate,
  busy,
  base,
  session,
  onExpired,
  serverError,
}: {
  data: Overview;
  fictional: boolean;
  mutate: Mutate;
  busy: boolean;
  base: string;
  session: Session;
  onExpired: () => void;
  serverError: string | null;
}) {
  return (
    <>
      <Connections
        data={data}
        fictional={fictional}
        session={session}
        base={base}
        mutate={mutate}
        busy={busy}
        serverError={serverError}
      />
      <Panel
        title="Collection runs"
        description="Run times use America/Chicago. Record counts describe accepted collection scope, not supplier performance."
      >
        {data.runs?.length ? (
          <div className="table-scroll">
            <table>
              <caption className="sr-only">Collection run history</caption>
              <thead>
                <tr>
                  <th>Connection</th>
                  <th>State</th>
                  <th>Queued</th>
                  <th>Finished</th>
                  <th>Records</th>
                  <th>Generation</th>
                  <th>Safe diagnostic</th>
                </tr>
              </thead>
              <tbody>
                {data.runs.map((run) => (
                  <tr key={run.id}>
                    <td>
                      {displayText(
                        data.connections.find(
                          (connection) => connection.id === run.connection_id,
                        )?.name || "Source connection",
                      )}
                    </td>
                    <th scope="row">
                      <Badge
                        tone={run.state === "failed" ? "attention" : "neutral"}
                      >
                        {humanize(run.state)}
                      </Badge>
                    </th>
                    <td>{localTime(run.enqueued_at)}</td>
                    <td>
                      {run.finished_at
                        ? localTime(run.finished_at)
                        : "In progress"}
                    </td>
                    <td>{run.record_count}</td>
                    <td>{run.generation}</td>
                    <td>{run.error_code ? humanize(run.error_code) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty title="No collection runs yet" />
        )}
      </Panel>
      {fictional && session.user.role === "owner" ? (
        <CsvImport
          key={data.generation}
          generation={data.generation}
          base={base}
          session={session}
          mutate={mutate}
          busy={
            busy ||
            !data.seeded ||
            data.runs.some((run) =>
              ["queued", "running", "validating", "publishing"].includes(
                run.state,
              ),
            )
          }
          onExpired={onExpired}
        />
      ) : null}
    </>
  );
}

type ImportPreview = {
  id: string;
  generation: number;
  requirements: Requirement[];
  row_count: number;
};
function CsvImport({
  generation,
  base,
  session,
  mutate,
  busy,
  onExpired,
}: {
  generation: number;
  base: string;
  session: Session;
  mutate: Mutate;
  busy: boolean;
  onExpired: () => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [pending, setPending] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | null>(null);
  const downloadController = useRef<AbortController | null>(null);
  const downloadUrl = useRef<string | null>(null);
  useEffect(
    () => () => {
      controller.current?.abort();
      downloadController.current?.abort();
      if (downloadUrl.current) URL.revokeObjectURL(downloadUrl.current);
    },
    [],
  );
  async function downloadTemplate() {
    if (busy || pending || downloading) return;
    setDownloading(true);
    setError(null);
    const abort = new AbortController();
    downloadController.current = abort;
    try {
      const response = await fetch(
        `${base}/imports/internal-orders/template.csv`,
        {
          credentials: "same-origin",
          headers: { Accept: "text/csv" },
          signal: abort.signal,
        },
      );
      if (!response.ok) {
        let message = "Could not download the template. Please retry.";
        let code: string | undefined;
        try {
          const body = (await response.json()) as {
            error?: string;
            code?: string;
          };
          if (typeof body.error === "string") message = body.error;
          if (typeof body.code === "string") code = body.code;
        } catch {
          /* An unavailable proxy may return a non-JSON response. */
        }
        throw new ApiError(response.status, message, code);
      }
      const contents = await response.blob();
      if (abort.signal.aborted) return;
      if (downloadUrl.current) URL.revokeObjectURL(downloadUrl.current);
      const url = URL.createObjectURL(contents);
      downloadUrl.current = url;
      const link = document.createElement("a");
      link.href = url;
      link.download = "internal-orders-template.csv";
      document.body.append(link);
      link.click();
      link.remove();
    } catch (cause) {
      if (isAborted(cause)) return;
      if (
        cause instanceof ApiError &&
        (cause.status === 401 || cause.code === "password_change_required")
      )
        onExpired();
      else setError(errorMessage(cause));
    } finally {
      if (!abort.signal.aborted) setDownloading(false);
    }
  }
  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file || pending || downloading) return;
    if (file.size > 1024 * 1024) {
      setError("Choose a CSV smaller than 1 MiB.");
      return;
    }
    setPending(true);
    setError(null);
    setPreview(null);
    const abort = new AbortController();
    controller.current = abort;
    try {
      const result = await request<ImportPreview>(
        `${base}/imports/internal-orders/preview`,
        {
          method: "POST",
          headers: {
            "Content-Type": "text/csv",
            "X-CSRF-Token": session.csrf_token,
          },
          body: file,
          signal: abort.signal,
        },
      );
      setPreview(result);
    } catch (cause) {
      if (isAborted(cause)) return;
      if (cause instanceof ApiError && cause.status === 401) onExpired();
      else setError(errorMessage(cause));
    } finally {
      if (!abort.signal.aborted) setPending(false);
    }
  }
  async function publish() {
    if (!preview || preview.generation !== generation) return;
    if (
      await mutate(
        `/imports/internal-orders/${encodeURIComponent(preview.id)}/publish`,
        { confirm: true },
      )
    )
      setPreview(null);
  }
  return (
    <Panel
      title="Internal requirement CSV import"
      description="Owner review is required before mappings are published and recalculated."
    >
      <div className="panel-content">
        <p className="muted">
          Use stable source keys and exact date/decimal values. Every mapping
          must match a current order, supplier, and unit.
        </p>
        <button
          className="text-button template-link"
          type="button"
          disabled={busy || pending || downloading}
          onClick={downloadTemplate}
        >
          {downloading ? "Downloading template…" : "Download CSV template"}
        </button>
        <form onSubmit={upload} className="import-form">
          <fieldset disabled={busy || pending || downloading}>
            <label htmlFor="requirements-file">CSV file</label>
            <input
              id="requirements-file"
              type="file"
              accept=".csv,text/csv"
              required
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setPreview(null);
              }}
            />
            <button className="button secondary" type="submit" disabled={!file}>
              {pending ? "Validating…" : "Validate and preview"}
            </button>
          </fieldset>
        </form>
        {error ? <ErrorNotice message={error} /> : null}
        {preview ? (
          <>
            <p className="import-preview-heading">
              Review {preview.row_count} mappings · Generation{" "}
              {preview.generation}
            </p>
            <div className="table-scroll">
              <table>
                <caption className="sr-only">
                  Validated CSV mapping preview
                </caption>
                <thead>
                  <tr>
                    <th>Internal PO / line</th>
                    <th>Supplier</th>
                    <th>Quantity</th>
                    <th>Required arrival</th>
                    <th>Source key</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.requirements.map((req) => (
                    <tr key={req.source_key}>
                      <th scope="row">
                        {req.internal_po} / {req.line}
                      </th>
                      <td>{humanize(req.supplier_alias)}</td>
                      <td>
                        {req.quantity} {req.uom}
                      </td>
                      <td>{req.required_date}</td>
                      <td>{req.source_key}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <button
              className="button primary import-publish"
              type="button"
              disabled={busy || pending || preview.generation !== generation}
              onClick={publish}
            >
              Approve mappings and collect
            </button>
          </>
        ) : null}
      </div>
    </Panel>
  );
}

export function Notifications({
  items,
  priorities,
  mutate,
  busy,
  onOpenOrder,
}: {
  items: Notification[];
  priorities: Exception[];
  mutate: Mutate;
  busy: boolean;
  onOpenOrder: (id: string) => void;
}) {
  return (
    <Panel
      title="Workspace notifications"
      description="Order status changes and material supplier conditions appear here. Reading a notification does not resolve an issue."
    >
      {items.length ? (
        <ol className="notification-list">
          {items.map((item) => {
            const condition = priorities.find(
              (row) => row.id === item.exception_id,
            );
            const orderId = item.order_id || condition?.order_id;
            return (
              <li key={item.id} className={item.read_at ? "read" : "unread"}>
                <div>
                  <Badge tone={item.read_at ? "neutral" : "attention"}>
                    {item.read_at ? "Read" : "Unread"}
                  </Badge>
                  <p>{displayText(item.message)}</p>
                  <span className="cell-meta">
                    {localTime(item.created_at)}
                  </span>
                </div>
                <div className="form-actions">
                  {orderId ? (
                    <button
                      className="button secondary compact"
                      type="button"
                      onClick={() => onOpenOrder(orderId)}
                    >
                      View evidence
                    </button>
                  ) : null}
                  {!item.read_at ? (
                    <button
                      type="button"
                      className="button secondary compact"
                      disabled={busy}
                      onClick={() =>
                        mutate(
                          `/notifications/${encodeURIComponent(item.id)}/read`,
                          {},
                        )
                      }
                    >
                      Mark read
                    </button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      ) : (
        <Empty title="No notifications">
          No order status or supplier condition notifications are available for
          this workspace and recipient.
        </Empty>
      )}
    </Panel>
  );
}
