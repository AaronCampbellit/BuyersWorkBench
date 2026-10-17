import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import type { Session } from "./api";
import type {
  Exception,
  IssueEvent,
  IssueResolution,
  IssueResolutionInput,
  Note,
  WorkflowInput,
} from "./workbench-types";
import type { Mutate } from "./workbench";
import { Modal } from "./modal";
import {
  Badge,
  displayText,
  ErrorNotice,
  humanize,
  localTime,
  originLabel,
} from "./ui";

export const needsResolution = (status: string) =>
  ["open", "in_progress", "completed"].includes(status);
export const issueStatusLabel = (status: string) =>
  status === "completed" ? "Needs resolution details" : humanize(status);
const resolutionTypes = [
  ["confirmation_received", "Confirmation received"],
  ["delivery_updated", "Delivery date updated"],
  ["shipment_updated", "Shipment updated"],
  ["data_corrected", "Source data corrected"],
  ["other", "Other"],
] as const;
const eventNames: Record<string, string> = {
  resolution_submitted: "Resolution submitted",
  source_verified: "Source verification",
  reopened: "Issue reopened",
  workflow_updated: "Workflow updated",
};
function evidenceValue(key: string, value: string) {
  return key === "source_key"
    ? value
    : key === "origin"
      ? originLabel(value)
      : displayText(value) || "Unknown";
}
function retainedKey(
  ref: { current: { signature: string; key: string } | null },
  body: unknown,
) {
  const signature = JSON.stringify(body);
  if (ref.current?.signature !== signature)
    ref.current = { signature, key: crypto.randomUUID() };
  return ref.current!.key;
}
function workflowFrom(condition: Exception): WorkflowInput {
  return {
    acknowledged: condition.acknowledged,
    assignee_id: condition.assignee_id,
    follow_up_date: condition.follow_up_date,
    status: condition.status,
  };
}
function sameWorkflow(a: WorkflowInput, b: WorkflowInput) {
  return (
    a.acknowledged === b.acknowledged &&
    a.assignee_id === b.assignee_id &&
    a.follow_up_date === b.follow_up_date &&
    a.status === b.status
  );
}

export function IssueCard({
  condition,
  notes,
  session,
  mutate,
  busy,
  mutationError,
  title,
  severityTone,
  onPending,
}: {
  condition: Exception;
  notes: Note[];
  session: Session;
  mutate: Mutate;
  busy: boolean;
  mutationError: string | null;
  title: string;
  severityTone: string;
  onPending: (pending: boolean) => void;
}) {
  const [base, setBase] = useState(condition);
  const [workflow, setWorkflow] = useState(() => workflowFrom(condition));
  const [saving, setSaving] = useState(false);
  const [workflowAttempted, setWorkflowAttempted] = useState(false);
  const [note, setNote] = useState("");
  const [noteRevision, setNoteRevision] = useState(condition.revision);
  const [noteSaving, setNoteSaving] = useState(false);
  const [noteAttempted, setNoteAttempted] = useState(false);
  const [modal, setModal] = useState<"resolve" | "reopen" | null>(null);
  const dirty = !sameWorkflow(workflow, workflowFrom(base));
  const conflicted = condition.revision !== base.revision;
  useEffect(() => {
    if (condition.revision === base.revision) return;
    const onlyUnrelatedChange =
      sameWorkflow(workflowFrom(base), workflowFrom(condition)) &&
      base.material_revision === condition.material_revision &&
      base.active === condition.active;
    const matchesDraft = sameWorkflow(workflow, workflowFrom(condition));
    if (!dirty || onlyUnrelatedChange || matchesDraft) {
      setBase(condition);
      if (!dirty || matchesDraft) setWorkflow(workflowFrom(condition));
    }
  }, [condition, base, dirty, workflow]);
  useEffect(() => {
    if (!note) setNoteRevision(condition.revision);
  }, [condition.revision, note]);
  const noteConflicted = note.length > 0 && noteRevision !== condition.revision;
  const editableStatus = ["open", "in_progress"].includes(condition.status);
  async function saveWorkflow(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || saving || conflicted) return;
    setSaving(true);
    setWorkflowAttempted(true);
    onPending(true);
    const success = await mutate(
      `/exceptions/${encodeURIComponent(condition.id)}/workflow`,
      { revision: base.revision, ...workflow },
      "PATCH",
    );
    setSaving(false);
    onPending(false);
    if (success) setWorkflowAttempted(false);
  }
  async function addNote(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!note.trim() || busy || noteSaving || noteConflicted) return;
    setNoteSaving(true);
    setNoteAttempted(true);
    onPending(true);
    const success = await mutate(
      `/exceptions/${encodeURIComponent(condition.id)}/notes`,
      { revision: noteRevision, body: note.trim() },
    );
    setNoteSaving(false);
    onPending(false);
    if (success) {
      setNote("");
      setNoteAttempted(false);
    }
  }
  return (
    <section
      className="condition-card issue-card"
      id={`issue-${condition.id}`}
      data-condition-id={condition.id}
      tabIndex={-1}
      aria-label={`${condition.rule} ${title}`}
    >
      <header>
        <div>
          <Badge tone={severityTone}>{humanize(condition.severity)}</Badge>
          <h4>
            {condition.rule} · {title}
          </h4>
        </div>
        <Badge tone={condition.active ? "attention" : "fictional"}>
          {condition.active
            ? "Source condition active"
            : "Source condition cleared"}
        </Badge>
      </header>
      <p>
        <span className="issue-evidence-label">
          {condition.active
            ? "Current condition evidence"
            : "Last detected condition evidence · Source condition is now cleared"}
        </span>
        {displayText(condition.explanation)}
      </p>
      <div className="issue-state-strip">
        <div>
          <span className="eyebrow">Buyer workflow</span>
          <strong>{issueStatusLabel(condition.status)}</strong>
          {condition.status === "completed" ? (
            <p className="cell-meta">
              The earlier task completion has no closeout record. Add resolution
              details to finish this issue.
            </p>
          ) : condition.status === "awaiting_verification" ? (
            <p className="cell-meta">
              Closeout was submitted. The source condition still needs
              verification.
            </p>
          ) : condition.status === "resolved" ? (
            <p className="cell-meta">
              {condition.resolved_at
                ? `Resolved ${localTime(condition.resolved_at)}.`
                : "The issue is resolved."}{" "}
              A source recurrence may reopen it.
            </p>
          ) : null}
        </div>
        <div className="form-actions">
          {needsResolution(condition.status) ? (
            <button
              type="button"
              className="button primary compact"
              disabled={busy || saving || noteSaving}
              onClick={() => setModal("resolve")}
            >
              Resolve issue
            </button>
          ) : (
            <button
              type="button"
              className="button secondary compact"
              disabled={busy || saving || noteSaving}
              onClick={() => setModal("reopen")}
            >
              Reopen issue
            </button>
          )}
        </div>
      </div>
      <dl className="evidence-grid">
        {Object.entries(condition.evidence ?? {}).map(([key, value]) => (
          <div key={key}>
            <dt>{humanize(key)}</dt>
            <dd>{evidenceValue(key, value)}</dd>
          </div>
        ))}
      </dl>
      <p className="cell-meta">
        First detected {localTime(condition.first_seen_at)} · Last verified{" "}
        {localTime(condition.last_seen_at)}
      </p>
      {condition.resolution ? (
        <div className="saved-resolution">
          <h5>Saved resolution</h5>
          <ResolutionDetails resolution={condition.resolution} />
        </div>
      ) : null}
      <form onSubmit={saveWorkflow} className="issue-workflow-form">
        {workflowAttempted && !saving && mutationError ? (
          <ErrorNotice message={mutationError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              Current workflow or source evidence changed. Your unsaved workflow
              inputs remain here. Reload to replace them with the current
              values.
            </p>
            <button
              type="button"
              className="button secondary compact"
              disabled={busy || saving}
              onClick={() => {
                setBase(condition);
                setWorkflow(workflowFrom(condition));
                setWorkflowAttempted(false);
              }}
            >
              Reload workflow
            </button>
          </div>
        ) : null}
        <fieldset
          disabled={busy || saving || noteSaving}
          className="workflow-form"
        >
          <label className="check-label">
            <input
              type="checkbox"
              checked={workflow.acknowledged}
              onChange={(e) =>
                setWorkflow((current) => ({
                  ...current,
                  acknowledged: e.target.checked,
                }))
              }
            />{" "}
            Acknowledged
          </label>
          <label>
            Assignee
            <select
              value={workflow.assignee_id ?? ""}
              onChange={(e) =>
                setWorkflow((current) => ({
                  ...current,
                  assignee_id: e.target.value || null,
                }))
              }
            >
              <option value="">Unassigned</option>
              <option value={session.user.id}>
                {session.user.display_name} (me)
              </option>
              {workflow.assignee_id &&
              workflow.assignee_id !== session.user.id ? (
                <option value={workflow.assignee_id}>
                  Current workspace assignee
                </option>
              ) : null}
            </select>
          </label>
          <label>
            Follow-up due
            <input
              type="date"
              value={workflow.follow_up_date ?? ""}
              onChange={(e) =>
                setWorkflow((current) => ({
                  ...current,
                  follow_up_date: e.target.value || null,
                }))
              }
            />
          </label>
          {editableStatus ? (
            <label>
              Work status
              <select
                aria-label="Work status"
                value={workflow.status}
                onChange={(e) =>
                  setWorkflow((current) => ({
                    ...current,
                    status: e.target.value,
                  }))
                }
              >
                <option value="open">Open</option>
                <option value="in_progress">In progress</option>
              </select>
            </label>
          ) : (
            <div className="workflow-readonly">
              <span>Work status</span>
              <strong>{issueStatusLabel(condition.status)}</strong>
            </div>
          )}
          <button
            type="submit"
            className="button secondary compact"
            disabled={conflicted || !dirty}
          >
            {saving ? "Saving workflow…" : "Save workflow"}
          </button>
        </fieldset>
      </form>
      <div className="notes-section">
        <h5>Buyer notes</h5>
        {notes.length ? (
          <ol className="notes-list">
            {notes.map((item) => (
              <li key={item.id}>
                <p>{item.body}</p>
                <span>
                  {item.author_name} · {localTime(item.created_at)}
                </span>
              </li>
            ))}
          </ol>
        ) : (
          <p className="cell-meta">
            No notes yet. Notes do not change supplier facts or resolve an
            issue.
          </p>
        )}
        <form onSubmit={addNote}>
          {noteAttempted && !noteSaving && mutationError ? (
            <ErrorNotice message={mutationError} />
          ) : null}
          {noteConflicted ? (
            <div className="notice" role="alert">
              <p>
                The issue changed. Your note is retained. Review the current
                issue before appending it.
              </p>
              <button
                type="button"
                className="button secondary compact"
                disabled={busy || noteSaving}
                onClick={() => {
                  setNoteRevision(condition.revision);
                  setNoteAttempted(false);
                }}
              >
                Use current issue revision
              </button>
            </div>
          ) : null}
          <fieldset disabled={busy || saving || noteSaving}>
            <label htmlFor={`note-${condition.id}`}>Add a note</label>
            <textarea
              id={`note-${condition.id}`}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              rows={2}
              required
              maxLength={4000}
            />
            <button
              type="submit"
              className="button secondary compact"
              disabled={!note.trim() || noteConflicted}
            >
              {noteSaving ? "Saving note…" : "Save note"}
            </button>
          </fieldset>
        </form>
      </div>
      {modal === "resolve" ? (
        <ResolveIssue
          condition={condition}
          title={title}
          mutate={mutate}
          busy={busy}
          serverError={mutationError}
          onClose={() => setModal(null)}
          onPending={onPending}
        />
      ) : modal === "reopen" ? (
        <ReopenIssue
          condition={condition}
          title={title}
          mutate={mutate}
          busy={busy}
          serverError={mutationError}
          onClose={() => setModal(null)}
          onPending={onPending}
        />
      ) : null}
    </section>
  );
}

function ResolutionDetails({ resolution }: { resolution: IssueResolution }) {
  return (
    <>
      <p className="cell-meta">
        {resolution.submitted_by} · {localTime(resolution.submitted_at)} ·
        Source condition {resolution.condition_active ? "active" : "cleared"} at
        submission
      </p>
      <dl className="resolution-facts">
        <div>
          <dt>Resolution type</dt>
          <dd>
            {resolutionTypes.find(
              ([value]) => value === resolution.resolution_type,
            )?.[1] ?? humanize(resolution.resolution_type)}
          </dd>
        </div>
        <div>
          <dt>Action taken</dt>
          <dd className="plain-text">{resolution.action_taken}</dd>
        </div>
        <div>
          <dt>Result</dt>
          <dd className="plain-text">{resolution.result}</dd>
        </div>
        <div>
          <dt>Reference</dt>
          <dd>{resolution.reference}</dd>
        </div>
        {resolution.supplier_contact ? (
          <div>
            <dt>Supplier contact</dt>
            <dd>{resolution.supplier_contact}</dd>
          </div>
        ) : null}
        {resolution.revised_date ? (
          <div>
            <dt>Revised date</dt>
            <dd>{resolution.revised_date}</dd>
          </div>
        ) : null}
        {resolution.confirmed_quantity ? (
          <div>
            <dt>Confirmed quantity</dt>
            <dd>
              {resolution.confirmed_quantity} {resolution.quantity_uom}
            </dd>
          </div>
        ) : null}
        {resolution.follow_up_date ? (
          <div>
            <dt>Follow-up due</dt>
            <dd>{resolution.follow_up_date}</dd>
          </div>
        ) : null}
      </dl>
    </>
  );
}

function ResolveIssue({
  condition,
  title,
  mutate,
  busy,
  serverError,
  onClose,
  onPending,
}: {
  condition: Exception;
  title: string;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onClose: () => void;
  onPending: (pending: boolean) => void;
}) {
  const [baseRevision, setBaseRevision] = useState(condition.revision);
  const [draft, setDraft] = useState<IssueResolutionInput>({
    resolution_type: "",
    action_taken: "",
    result: "",
    reference: "",
    supplier_contact: "",
    revised_date: "",
    confirmed_quantity: "",
    follow_up_date: condition.follow_up_date ?? "",
  });
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const key = useRef<{ signature: string; key: string } | null>(null);
  const conflicted = condition.revision !== baseRevision;
  const allowed = needsResolution(condition.status);
  function set(field: keyof IssueResolutionInput, value: string) {
    setDraft((current) => ({ ...current, [field]: value }));
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || saving || conflicted || !allowed) return;
    setSaving(true);
    setAttempted(true);
    onPending(true);
    const body = { revision: baseRevision, ...draft };
    const success = await mutate(
      `/exceptions/${encodeURIComponent(condition.id)}/resolve`,
      { ...body, idempotency_key: retainedKey(key, body) },
    );
    setSaving(false);
    onPending(false);
    if (success) onClose();
  }
  return (
    <Modal
      title={`Resolve issue · ${title}`}
      eyebrow={`${condition.rule} · Resolution details`}
      onClose={onClose}
      pending={saving}
      className="issue-resolution-dialog"
    >
      <form
        className="dialog-content issue-resolution-form"
        onSubmit={submit}
        aria-busy={saving}
      >
        <div className="coverage-note">
          <strong>
            {condition.active
              ? "This submission will await source verification."
              : "This submission will resolve the cleared issue."}
          </strong>
          <p>
            Record what you did and the outcome. This saves buyer closeout
            details; it does not change supplier confirmation, shipment, dates,
            or recorded order receipts.
          </p>
        </div>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <div>
              <p>
                The issue changed. Your resolution draft is retained. Review the
                current condition below and use its latest revision before
                retrying.
              </p>
              <p>
                {displayText(condition.explanation)} ·{" "}
                {issueStatusLabel(condition.status)} · Source{" "}
                {condition.active ? "active" : "cleared"}
              </p>
            </div>
            {allowed ? (
              <button
                type="button"
                className="button secondary compact"
                disabled={busy || saving}
                onClick={() => {
                  setBaseRevision(condition.revision);
                  setAttempted(false);
                }}
              >
                Use current issue revision
              </button>
            ) : null}
          </div>
        ) : null}
        {!allowed ? (
          <ErrorNotice message="A resolution is already submitted for this issue. Close this form to review it; reopen the issue before submitting another resolution." />
        ) : null}
        <fieldset
          disabled={busy || saving || !allowed}
          className="resolution-form-grid"
        >
          <label className="resolution-wide" htmlFor="resolution-type">
            Resolution type
            <select
              data-initial-focus
              id="resolution-type"
              aria-label="Resolution type"
              required
              value={draft.resolution_type}
              onChange={(e) => set("resolution_type", e.target.value)}
            >
              <option value="">Choose how this issue was addressed</option>
              {resolutionTypes.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label className="resolution-wide" htmlFor="resolution-action">
            Action taken
            <textarea
              id="resolution-action"
              aria-label="Action taken"
              required
              maxLength={4000}
              rows={3}
              value={draft.action_taken}
              onChange={(e) => set("action_taken", e.target.value)}
            />
          </label>
          <label className="resolution-wide" htmlFor="resolution-result">
            Result
            <textarea
              id="resolution-result"
              aria-label="Result"
              required
              maxLength={4000}
              rows={3}
              value={draft.result}
              onChange={(e) => set("result", e.target.value)}
            />
          </label>
          <label htmlFor="resolution-reference">
            Reference
            <input
              id="resolution-reference"
              aria-label="Reference"
              required
              maxLength={500}
              value={draft.reference}
              onChange={(e) => set("reference", e.target.value)}
            />
          </label>
          <label htmlFor="resolution-contact">
            Supplier contact (optional)
            <input
              id="resolution-contact"
              aria-label="Supplier contact (optional)"
              maxLength={200}
              value={draft.supplier_contact}
              onChange={(e) => set("supplier_contact", e.target.value)}
            />
          </label>
          <label htmlFor="resolution-date">
            Revised date (optional)
            <input
              id="resolution-date"
              aria-label="Revised date (optional)"
              type="date"
              value={draft.revised_date}
              onChange={(e) => set("revised_date", e.target.value)}
            />
          </label>
          <label htmlFor="resolution-quantity">
            Confirmed quantity (optional)
            <input
              id="resolution-quantity"
              aria-label="Confirmed quantity (optional)"
              type="text"
              inputMode="decimal"
              pattern="[0-9]{1,18}([.][0-9]{1,4})?"
              value={draft.confirmed_quantity}
              onChange={(e) => set("confirmed_quantity", e.target.value)}
            />
          </label>
          <label className="resolution-wide" htmlFor="resolution-followup">
            {condition.active
              ? "Follow-up date (required while source condition is active)"
              : "Follow-up date (optional)"}
            <input
              id="resolution-followup"
              aria-label={
                condition.active
                  ? "Follow-up date (required while source condition is active)"
                  : "Follow-up date (optional)"
              }
              type="date"
              required={condition.active}
              value={draft.follow_up_date}
              onChange={(e) => set("follow_up_date", e.target.value)}
            />
          </label>
          <div className="resolution-wide form-actions">
            <button
              type="submit"
              className="button primary"
              disabled={
                conflicted ||
                !draft.resolution_type ||
                !draft.action_taken.trim() ||
                !draft.result.trim() ||
                !draft.reference.trim() ||
                (condition.active && !draft.follow_up_date)
              }
            >
              {saving
                ? "Saving resolution…"
                : condition.active
                  ? "Submit for verification"
                  : "Resolve cleared issue"}
            </button>
            <button
              type="button"
              className="button secondary"
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

function ReopenIssue({
  condition,
  title,
  mutate,
  busy,
  serverError,
  onClose,
  onPending,
}: {
  condition: Exception;
  title: string;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onClose: () => void;
  onPending: (pending: boolean) => void;
}) {
  const [reason, setReason] = useState("");
  const [baseRevision, setBaseRevision] = useState(condition.revision);
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const key = useRef<{ signature: string; key: string } | null>(null);
  const conflicted = condition.revision !== baseRevision;
  const allowed = !needsResolution(condition.status);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || saving || conflicted || !allowed || !reason.trim()) return;
    setSaving(true);
    setAttempted(true);
    onPending(true);
    const body = { revision: baseRevision, reason };
    const success = await mutate(
      `/exceptions/${encodeURIComponent(condition.id)}/reopen`,
      { ...body, idempotency_key: retainedKey(key, body) },
    );
    setSaving(false);
    onPending(false);
    if (success) onClose();
  }
  return (
    <Modal
      title={`Reopen issue · ${title}`}
      eyebrow={condition.rule}
      onClose={onClose}
      pending={saving}
      className="issue-resolution-dialog"
    >
      <form className="dialog-content issue-resolution-form" onSubmit={submit}>
        <p className="muted">
          Reopening returns buyer work to Open and preserves earlier resolution
          details and activity.
        </p>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              The issue changed. Review its current state before retrying; your
              reason is retained.
            </p>
            {allowed ? (
              <button
                type="button"
                className="button secondary compact"
                disabled={busy || saving}
                onClick={() => {
                  setBaseRevision(condition.revision);
                  setAttempted(false);
                }}
              >
                Use current issue revision
              </button>
            ) : null}
          </div>
        ) : null}
        {!allowed ? (
          <ErrorNotice message="The issue is already open for buyer action. Close this form to review its current workflow." />
        ) : null}
        <fieldset disabled={busy || saving || !allowed}>
          <label htmlFor="issue-reopen-reason">
            Reason for reopening
            <textarea
              data-initial-focus
              id="issue-reopen-reason"
              aria-label="Reason for reopening"
              required
              maxLength={4000}
              rows={4}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
          <div className="form-actions">
            <button
              type="submit"
              className="button primary"
              disabled={conflicted || !reason.trim()}
            >
              {saving ? "Reopening issue…" : "Confirm reopen"}
            </button>
            <button
              type="button"
              className="button secondary"
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

export function IssueActivity({ events }: { events: IssueEvent[] }) {
  return (
    <section className="issue-activity">
      <h3 className="section-title">Issue activity</h3>
      <p className="muted">
        Buyer workflow, closeout submissions, reopening, and source verification
        are recorded separately from supplier changes.
      </p>
      {events.length ? (
        <ol className="issue-event-list">
          {[...events]
            .sort((a, b) => b.created_at.localeCompare(a.created_at))
            .map((event) => (
              <li key={event.id}>
                <div className="issue-event-heading">
                  <strong>
                    {eventNames[event.action] ?? humanize(event.action)}
                  </strong>
                  <Badge>
                    {issueStatusLabel(event.from_status)} →{" "}
                    {issueStatusLabel(event.to_status)}
                  </Badge>
                  <span className="cell-meta">
                    {event.rule} · {event.actor_name} ·{" "}
                    {localTime(event.created_at)}
                  </span>
                </div>
                {event.reason ? (
                  <p className="plain-text">{event.reason}</p>
                ) : null}
                {event.resolution ? (
                  <details>
                    <summary>Resolution submitted</summary>
                    <ResolutionDetails resolution={event.resolution} />
                  </details>
                ) : null}
                {event.after_workflow ? (
                  <p className="cell-meta">
                    Acknowledged:{" "}
                    {event.after_workflow.acknowledged ? "Yes" : "No"} ·
                    Follow-up: {event.after_workflow.follow_up_date || "None"} ·{" "}
                    {event.after_workflow.assignee_id
                      ? "Assigned"
                      : "Unassigned"}
                  </p>
                ) : null}
                <details>
                  <summary>Source condition at this event</summary>
                  <p className="cell-meta">
                    {event.condition_active ? "Active" : "Cleared"} · Material
                    revision {event.material_revision}
                  </p>
                  <dl className="evidence-grid">
                    {Object.entries(event.evidence ?? {}).map(
                      ([key, value]) => (
                        <div key={key}>
                          <dt>{humanize(key)}</dt>
                          <dd>{evidenceValue(key, value)}</dd>
                        </div>
                      ),
                    )}
                  </dl>
                </details>
              </li>
            ))}
        </ol>
      ) : (
        <p className="empty-issue-activity">
          No closeout or workflow activity recorded yet.
        </p>
      )}
    </section>
  );
}
