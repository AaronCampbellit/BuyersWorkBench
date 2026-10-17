import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { errorMessage, isAborted, request } from "./api";
import type { Session } from "./api";
import type { Connection, Overview } from "./workbench-types";
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

type Member = { id: string; display_name: string };
const methodLabels: Record<string, string> = {
  fictional: "Demo source",
  api_polling: "API polling",
  webhook: "Webhook",
  browser: "Browser automation",
  public_scrape: "Public-page extraction",
  email: "Email ingestion",
};

export function Connections({
  data,
  fictional,
  session,
  base,
  mutate,
  busy,
  serverError,
}: {
  data: Overview;
  fictional: boolean;
  session: Session;
  base: string;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [memberError, setMemberError] = useState<string | null>(null);
  const [memberAttempt, setMemberAttempt] = useState(0);
  const owner = session.user.role === "owner";
  useEffect(() => {
    if (!owner) return;
    const controller = new AbortController();
    request<{ members: Member[] }>(`${base}/members`, {
      signal: controller.signal,
    })
      .then((result) => {
        setMembers(result.members);
        setMemberError(null);
      })
      .catch((error) => {
        if (!isAborted(error)) setMemberError(errorMessage(error));
      });
    return () => controller.abort();
  }, [base, owner, memberAttempt]);
  const connections = data.connections ?? [];
  const selected =
    editing && editing !== "new"
      ? connections.find((item) => item.id === editing)
      : null;
  return (
    <>
      <Panel
        title="Supplier connections"
        description={
          fictional
            ? "Each source keeps its own schedule, accepted observations and run history."
            : "Prepare a supplier connection here. Live collection becomes available after its account and connector are verified."
        }
        actions={
          owner ? (
            <button
              type="button"
              className="button primary compact"
              disabled={busy || connections.length >= 50}
              onClick={() => setEditing("new")}
            >
              Add connection
            </button>
          ) : undefined
        }
      >
        <div className="panel-content">
          {memberError ? (
            <ErrorNotice
              message={`Buyer choices could not be loaded. ${memberError}`}
              retry={() => setMemberAttempt((n) => n + 1)}
            />
          ) : null}
          {!connections.length ? (
            <Empty title="No supplier connections">
              {fictional
                ? "Initialize the workspace to create its demonstration source, or add an empty demo source."
                : "Add a connection to save its supplier, collection method and intended schedule."}
            </Empty>
          ) : (
            <div className="connection-list">
              {connections.map((connection) => (
                <ConnectionCard
                  key={connection.id}
                  connection={connection}
                  data={data}
                  owner={owner}
                  members={members}
                  mutate={mutate}
                  busy={busy}
                  onEdit={() => setEditing(connection.id)}
                />
              ))}
            </div>
          )}
        </div>
      </Panel>
      {editing === "new" || selected ? (
        <ConnectionEditor
          key={editing}
          connection={selected || null}
          fictional={fictional}
          suppliers={data.suppliers ?? []}
          members={members}
          memberError={memberError}
          mutate={mutate}
          busy={busy}
          serverError={serverError}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </>
  );
}

function ConnectionCard({
  connection,
  data,
  owner,
  members,
  mutate,
  busy,
  onEdit,
}: {
  connection: Connection;
  data: Overview;
  owner: boolean;
  members: Member[];
  mutate: Mutate;
  busy: boolean;
  onEdit: () => void;
}) {
  const supplier = data.suppliers?.find(
    (item) => item.id === connection.supplier_id,
  );
  const buyer = members.find((item) => item.id === connection.buyer_user_id);
  const running = data.runs?.some(
    (run) =>
      run.connection_id === connection.id &&
      ["queued", "running", "validating", "publishing"].includes(run.state),
  );
  const runnable = connection.runnable && !connection.paused;
  return (
    <article
      className="connection-card"
      aria-label={displayText(connection.name)}
    >
      <div className="connection-heading">
        <div>
          <h3>
            {displayText(connection.name) || humanize(connection.connector)}
          </h3>
          <p className="muted">
            {methodLabels[connection.method] || humanize(connection.method)}
            {supplier ? ` · ${displayText(supplier.name)}` : ""}
            {connection.is_demo_default ? " · Default demo source" : ""}
          </p>
        </div>
        <Badge tone={connection.last_failure_code ? "attention" : "neutral"}>
          {connection.paused
            ? "Paused"
            : connection.runnable
              ? humanize(connection.status)
              : "Setup required"}
        </Badge>
      </div>
      {!connection.runnable ? (
        <p className="coverage-note">
          Connection settings are saved. Collection is unavailable until a
          supported connector and account access have been verified.
        </p>
      ) : null}
      <dl className="workspace-facts health-facts">
        <div>
          <dt>Latest attempt</dt>
          <dd>{localTime(connection.last_attempt_at)}</dd>
        </div>
        <div>
          <dt>Latest accepted collection</dt>
          <dd>{localTime(connection.last_success_at)}</dd>
        </div>
        <div>
          <dt>Next scheduled attempt</dt>
          <dd>
            {connection.schedule_enabled && !connection.paused
              ? localTime(connection.next_due_at)
              : "Not scheduled"}
          </dd>
        </div>
        <div>
          <dt>Buyer routing</dt>
          <dd>
            {buyer?.display_name ||
              (connection.buyer_user_id
                ? "Assigned workspace buyer"
                : "Active workspace members")}
          </dd>
        </div>
        {connection.last_failure_code ? (
          <div>
            <dt>Source issue</dt>
            <dd>{humanize(connection.last_failure_code)}</dd>
          </div>
        ) : null}
      </dl>
      <div className="source-actions">
        {(owner || connection.is_demo_default) && connection.runnable ? (
          <button
            type="button"
            className="button primary compact"
            disabled={busy || !runnable || running}
            onClick={() =>
              mutate(
                owner
                  ? `/connections/${encodeURIComponent(connection.id)}/refresh`
                  : "/refresh",
                {},
              )
            }
          >
            {running ? "Collection in progress" : "Refresh source"}
          </button>
        ) : null}
        {owner ? (
          <>
            <button
              type="button"
              className="button secondary compact"
              disabled={busy}
              onClick={onEdit}
            >
              Edit connection<span className="sr-only"> {connection.name}</span>
            </button>
            <button
              type="button"
              className="button secondary compact"
              disabled={busy}
              onClick={() =>
                mutate(
                  `/connections/${encodeURIComponent(connection.id)}/pause`,
                  { revision: connection.revision, paused: !connection.paused },
                  "PATCH",
                )
              }
            >
              {connection.paused ? "Resume connection" : "Pause connection"}
              <span className="sr-only"> {connection.name}</span>
            </button>
          </>
        ) : null}
      </div>
      {owner && connection.runnable ? (
        <ConnectionSchedule
          connection={connection}
          mutate={mutate}
          busy={busy || connection.paused}
        />
      ) : null}
      {owner &&
      connection.method === "fictional" &&
      !connection.is_demo_default ? (
        <DemoSourceInput
          connection={connection}
          supplierAlias={supplier?.alias || "demo_supplier"}
          mutate={mutate}
          busy={busy || connection.paused}
        />
      ) : null}
    </article>
  );
}

function DemoSourceInput({
  connection,
  supplierAlias,
  mutate,
  busy,
}: {
  connection: Connection;
  supplierAlias: string;
  mutate: Mutate;
  busy: boolean;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [expectedRevision, setExpectedRevision] = useState(connection.revision);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState("");
  const key = useRef(crypto.randomUUID());
  const input = useRef<HTMLInputElement | null>(null);
  const conflicted = expectedRevision !== connection.revision;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file || busy || saving || conflicted) return;
    setSaving(true);
    setError(null);
    setStatus("");
    try {
      if (file.size > 4 * 1024 * 1024)
        throw new Error("Use a source file smaller than 4 MiB.");
      let value: unknown;
      try {
        value = JSON.parse(await file.text());
      } catch {
        throw new Error("Choose a valid JSON source file.");
      }
      if (
        !value ||
        typeof value !== "object" ||
        !("records" in value) ||
        !Array.isArray(value.records)
      )
        throw new Error("The source file must contain a records array.");
      const requirements = "requirements" in value ? value.requirements : [];
      if (!Array.isArray(requirements))
        throw new Error("Requirements must be an array.");
      const success = await mutate(
        `/connections/${encodeURIComponent(connection.id)}/source`,
        {
          revision: expectedRevision,
          idempotency_key: key.current,
          records: value.records,
          requirements,
        },
      );
      if (success) {
        setFile(null);
        if (input.current) input.current.value = "";
        key.current = crypto.randomUUID();
        setStatus(
          "Demo source accepted for collection. Review the collection run for its publication result.",
        );
      } else
        setError(
          "Source collection was not accepted. Review the error above; your selected file is retained for retry.",
        );
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not read the source file.",
      );
    } finally {
      setSaving(false);
    }
  }
  function template() {
    const record = {
      source_key: "SAMPLE-ORDER:1",
      source_order_id: "SAMPLE-ORDER",
      source_line_id: "1",
      supplier_alias: supplierAlias,
      supplier_part: "SAMPLE-PART",
      description: "Sample computer component",
      origin: "fictional",
      ordered: "10",
      cancelled: "0",
      shipped: "0",
      delivered: "0",
      uom: "EA",
      status: "open",
      confirmation: "confirmed",
      placed_at: "2026-10-17T12:00:00Z",
      confirmed_at: "2026-10-17T12:00:00Z",
      source_event_at: null,
      arrival_date: "2026-10-21",
      arrival_kind: "committed_arrival",
      arrival_label: "Demo committed delivery",
      original_date: "",
      original_kind: "",
      original_label: "",
      remaining_schedule: "known",
    };
    const requirements = [
      {
        source_key: record.source_key,
        internal_po: "SAMPLE-PO",
        line: "1",
        supplier_alias: supplierAlias,
        internal_part: "SAMPLE-COMPONENT",
        quantity: "10",
        uom: "EA",
        required_date: "2026-10-25",
        buyer_alias: "demo_buyer",
        origin: "fictional",
      },
    ];
    const url = URL.createObjectURL(
      new Blob([JSON.stringify({ records: [record], requirements }, null, 2)], {
        type: "application/json",
      }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = "demo-source-template.json";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return (
    <form className="demo-source-form" onSubmit={submit}>
      <h4>Demo source data</h4>
      <p className="muted">
        Upload normalized fictional records to exercise collection and change
        detection for this connection. Demo reset clears this additional source.
      </p>
      {error ? <ErrorNotice message={error} /> : null}
      {file && conflicted ? (
        <div className="notice" role="alert">
          <p>
            Connection settings changed. Review them before submitting your
            retained file.
          </p>
          <button
            className="button secondary compact"
            type="button"
            disabled={busy || saving}
            onClick={() => {
              key.current = crypto.randomUUID();
              setExpectedRevision(connection.revision);
            }}
          >
            Use current connection revision
          </button>
        </div>
      ) : null}
      <fieldset disabled={busy || saving}>
        <label>
          Source JSON file
          <input
            ref={input}
            type="file"
            accept="application/json,.json"
            aria-label={`Demo source file for ${connection.name}`}
            onChange={(event) => {
              setFile(event.target.files?.[0] || null);
              setError(null);
              setStatus("");
              key.current = crypto.randomUUID();
              setExpectedRevision(connection.revision);
            }}
          />
        </label>
        <div className="form-actions">
          <button
            type="submit"
            className="button secondary compact"
            disabled={!file || conflicted}
          >
            {saving ? "Submitting source…" : "Collect demo file"}
          </button>
          <button
            type="button"
            className="button secondary compact"
            onClick={template}
          >
            Download source template
          </button>
        </div>
      </fieldset>
      {status ? <p role="status">{status}</p> : null}
    </form>
  );
}

function ConnectionSchedule({
  connection,
  mutate,
  busy,
}: {
  connection: Connection;
  mutate: Mutate;
  busy: boolean;
}) {
  const [draft, setDraft] = useState({
    value: String(connection.cadence_seconds),
    saved: connection.cadence_seconds,
    revision: connection.revision,
  });
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    setDraft((current) =>
      current.value === String(current.saved)
        ? {
            value: String(connection.cadence_seconds),
            saved: connection.cadence_seconds,
            revision: connection.revision,
          }
        : current,
    );
  }, [connection.revision, connection.cadence_seconds]);
  const min = connection.minimum_cadence_seconds || 30;
  const valid =
    Number.isInteger(Number(draft.value)) &&
    Number(draft.value) >= min &&
    Number(draft.value) <= 86400;
  const conflicted = draft.revision !== connection.revision;
  async function save(enabled: boolean) {
    if (saving || busy || !valid || conflicted) return;
    setSaving(true);
    try {
      const success = await mutate(
        `/connections/${encodeURIComponent(connection.id)}/schedule`,
        {
          revision: draft.revision,
          enabled,
          cadence_seconds: Number(draft.value),
        },
        "PATCH",
      );
      if (success)
        setDraft((current) => ({
          ...current,
          saved: Number(current.value),
          revision: current.revision + 1,
        }));
    } finally {
      setSaving(false);
    }
  }
  return (
    <form
      className="schedule-form"
      onSubmit={(event) => {
        event.preventDefault();
        void save(true);
      }}
    >
      {conflicted ? (
        <div className="notice" role="alert">
          <p>
            Saved connection settings changed. The current interval is{" "}
            {connection.cadence_seconds} seconds; your interval is retained.
          </p>
          <button
            className="button secondary compact"
            type="button"
            disabled={busy || saving}
            onClick={() =>
              setDraft((current) => ({
                ...current,
                revision: connection.revision,
              }))
            }
          >
            Use current revision with my interval
          </button>
        </div>
      ) : null}
      <fieldset disabled={busy || saving}>
        <label>
          Check every (seconds)
          <input
            aria-label={`Check interval for ${connection.name}`}
            type="number"
            min={min}
            max={86400}
            step={1}
            required
            value={draft.value}
            onChange={(event) =>
              setDraft({ ...draft, value: event.target.value })
            }
          />
        </label>
        <button
          type="submit"
          className="button secondary compact"
          disabled={!valid || conflicted}
        >
          {connection.schedule_enabled ? "Update schedule" : "Enable schedule"}
          <span className="sr-only"> {connection.name}</span>
        </button>
        {connection.schedule_enabled ? (
          <button
            type="button"
            className="button secondary compact"
            disabled={!valid || conflicted}
            onClick={() => void save(false)}
          >
            Disable schedule<span className="sr-only"> {connection.name}</span>
          </button>
        ) : null}
      </fieldset>
    </form>
  );
}

function ConnectionEditor({
  connection,
  fictional,
  suppliers,
  members,
  memberError,
  mutate,
  busy,
  serverError,
  onClose,
}: {
  connection: Connection | null;
  fictional: boolean;
  suppliers: Overview["suppliers"];
  members: Member[];
  memberError: string | null;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onClose: () => void;
}) {
  const [name, setName] = useState(connection?.name || "");
  const [supplier, setSupplier] = useState(connection?.supplier_id || "");
  const [buyer, setBuyer] = useState(connection?.buyer_user_id || "");
  const [method, setMethod] = useState(
    connection?.method || (fictional ? "fictional" : "api_polling"),
  );
  const [minimum, setMinimum] = useState(
    String(connection?.minimum_cadence_seconds || (fictional ? 30 : 300)),
  );
  const [cadence, setCadence] = useState(
    String(connection?.cadence_seconds || 300),
  );
  const [baseRevision, setBaseRevision] = useState(connection?.revision || 0);
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const creationKey = useRef(crypto.randomUUID());
  const submission = useRef<string | null>(null);
  const conflicted = !!connection && baseRevision !== connection.revision;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || busy || conflicted) return;
    const payload = {
      name: name.trim(),
      supplier_id: supplier || null,
      buyer_user_id: buyer || null,
      method,
      minimum_cadence_seconds: Number(minimum),
      cadence_seconds: Number(cadence),
    };
    const encoded = JSON.stringify(payload);
    if (submission.current !== null && submission.current !== encoded)
      creationKey.current = crypto.randomUUID();
    submission.current = encoded;
    setAttempted(true);
    setSaving(true);
    try {
      const success = await mutate(
        connection
          ? `/connections/${encodeURIComponent(connection.id)}`
          : "/connections",
        connection
          ? { ...payload, revision: baseRevision }
          : { ...payload, idempotency_key: creationKey.current },
        connection ? "PATCH" : "POST",
      );
      if (success) onClose();
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal
      title={connection ? "Edit connection" : "Add supplier connection"}
      eyebrow="Source settings"
      onClose={onClose}
      pending={saving}
      className="supplier-dialog"
    >
      <form className="dialog-content" onSubmit={submit}>
        <p className="muted">
          {fictional
            ? "A new demo connection starts with no source records. Its observations remain separate from the default demonstration."
            : "Save the intended method and schedule now. Account authentication and live collection will be configured after supplier discovery."}
        </p>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              Saved settings changed. Current name: {connection?.name}. Your
              draft is retained. Review the latest settings before applying it.
            </p>
            <button
              className="button secondary compact"
              type="button"
              disabled={saving || busy}
              onClick={() => setBaseRevision(connection!.revision)}
            >
              Use current revision with my draft
            </button>
          </div>
        ) : null}
        <fieldset className="supplier-form-grid" disabled={busy || saving}>
          <div className="supplier-wide-field">
            <label htmlFor="connection-name">Connection name (required)</label>
            <input
              id="connection-name"
              required
              maxLength={100}
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
            />
          </div>
          <div>
            <label htmlFor="connection-method">Collection method</label>
            <select
              id="connection-method"
              value={method}
              disabled={!!connection}
              onChange={(event) => setMethod(event.target.value)}
            >
              {Object.entries(methodLabels)
                .filter(([value]) =>
                  fictional ? value === "fictional" : value !== "fictional",
                )
                .map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
            </select>
          </div>
          <div>
            <label htmlFor="connection-supplier">Supplier</label>
            <select
              id="connection-supplier"
              value={supplier}
              disabled={connection?.is_demo_default}
              onChange={(event) => setSupplier(event.target.value)}
            >
              <option value="">
                {connection?.is_demo_default
                  ? "Multiple demo suppliers"
                  : "No supplier selected"}
              </option>
              {suppliers.map((item) => (
                <option key={item.id} value={item.id}>
                  {displayText(item.name)}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="connection-buyer">Default buyer</label>
            <select
              id="connection-buyer"
              value={buyer}
              disabled={!!memberError}
              onChange={(event) => setBuyer(event.target.value)}
            >
              <option value="">Active workspace members</option>
              {buyer && !members.some((item) => item.id === buyer) ? (
                <option value={buyer}>Previously assigned buyer</option>
              ) : null}
              {members.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.display_name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="connection-minimum">
              Minimum interval (seconds)
            </label>
            <input
              id="connection-minimum"
              type="number"
              min={30}
              max={86400}
              step={1}
              required
              value={minimum}
              onChange={(event) => setMinimum(event.target.value)}
            />
          </div>
          <div>
            <label htmlFor="connection-cadence">Check interval (seconds)</label>
            <input
              id="connection-cadence"
              type="number"
              min={Number(minimum) || 30}
              max={86400}
              step={1}
              required
              value={cadence}
              onChange={(event) => setCadence(event.target.value)}
            />
          </div>
          <div className="form-actions supplier-wide-field">
            <button
              className="button primary"
              type="submit"
              disabled={conflicted || !name.trim()}
            >
              {saving
                ? "Saving…"
                : connection
                  ? "Save connection"
                  : "Create connection"}
            </button>
            <button
              className="button secondary"
              type="button"
              disabled={saving}
              onClick={onClose}
            >
              Cancel
            </button>
          </div>
        </fieldset>
      </form>
    </Modal>
  );
}
