import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { ApiError, errorMessage, isAborted, request } from "./api";
import { ErrorNotice, Panel } from "./ui";

type Preferences = {
  status_changes: boolean;
  supplier_conditions: boolean;
  revision: number;
};
type Draft = Pick<Preferences, "status_changes" | "supplier_conditions">;
type Props = {
  base: string;
  generation: number;
  csrfToken: string;
  busy: boolean;
  onExpired?: () => void;
};

// A workspace switch discards that workspace's form; refreshes and failed saves
// retain the active workspace's choices until the user saves or reloads them.
export function NotificationPreferences(props: Props) {
  return <PreferencesForm key={props.base} {...props} />;
}

function PreferencesForm({ base, generation, csrfToken, busy, onExpired }: Props) {
  const [saved, setSaved] = useState<Preferences | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [reload, setReload] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [status, setStatus] = useState("");
  const current = useRef({ base, generation });
  current.current = { base, generation };
  const expired = useRef(onExpired);
  expired.current = onExpired;

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    request<Preferences>(`${base}/notification-preferences`, {
      signal: controller.signal,
    })
      .then((preferences) => {
        if (controller.signal.aborted) return;
        setSaved(preferences);
        setDraft((existing) => existing ?? preferences);
      })
      .catch((failure: unknown) => {
        if (isAborted(failure)) return;
        setError(errorMessage(failure));
        if (failure instanceof ApiError && failure.status === 401)
          expired.current?.();
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [base, generation, reload]);

  const dirty =
    !!saved &&
    !!draft &&
    (draft.status_changes !== saved.status_changes ||
      draft.supplier_conditions !== saved.supplier_conditions);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!saved || !draft || busy || saving || loading || conflict || !dirty) return;
    setSaving(true);
    setError(null);
    setStatus("");
    try {
      const preferences = await request<Preferences>(
        `${base}/notification-preferences`,
        {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": csrfToken,
          },
          body: JSON.stringify({ ...draft, generation, revision: saved.revision }),
        },
      );
      if (current.current.base !== base || current.current.generation !== generation)
        return;
      setSaved(preferences);
      setDraft(preferences);
      setStatus("Notification preferences saved.");
    } catch (failure) {
      if (current.current.base !== base || current.current.generation !== generation)
        return;
      if (failure instanceof ApiError && failure.status === 409) {
        setConflict(true);
        setReload((value) => value + 1);
      } else {
        setError(errorMessage(failure));
      }
      if (failure instanceof ApiError && failure.status === 401)
        expired.current?.();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Panel
      title="Notification preferences"
      description="Your in-app notifications for this workspace. Changes apply to future notices; order history, buyer priorities and existing notices stay available."
    >
    <form className="panel-content notification-preferences" onSubmit={save} aria-busy={loading || saving}>
      {loading ? <p role="status">Loading notification preferences…</p> : null}
      {error ? <ErrorNotice message={error} /> : null}
      {conflict ? (
        <div role="alert">
          <p>
            Your preferences changed in another request, or the workspace was
            reset. Your choices are retained. Review them before saving again.
          </p>
          <button
            className="button secondary"
            type="button"
            disabled={busy || saving || loading || !!error || !saved}
            onClick={() => setConflict(false)}
          >
            Keep my choices with the latest revision
          </button>
        </div>
      ) : null}
      {draft ? (
        <fieldset disabled={busy || saving || loading}>
          <legend>Notify me about</legend>
          <label>
            <input
              type="checkbox"
              checked={draft.status_changes}
              onChange={(event) => {
                setDraft({ ...draft, status_changes: event.target.checked });
                setStatus("");
              }}
            />{" "}
            Order status changes
          </label>
          <label>
            <input
              type="checkbox"
              checked={draft.supplier_conditions}
              onChange={(event) => {
                setDraft({ ...draft, supplier_conditions: event.target.checked });
                setStatus("");
              }}
            />{" "}
            New, recurring or worsening supplier conditions
          </label>
        </fieldset>
      ) : null}
      <p className="muted">
        Assigned buyer routing applies first. Turning notices off does not send
        them to other buyers. These preferences survive a demo reset.
      </p>
      <div className="form-actions">
        <button className="button primary" disabled={busy || saving || loading || conflict || !dirty}>
          {saving ? "Saving preferences…" : "Save preferences"}
        </button>
        {error ? (
          <button
            className="button secondary"
            type="button"
            disabled={busy || saving || loading}
            onClick={() => setReload((value) => value + 1)}
          >
            Retry loading preferences
          </button>
        ) : null}
      </div>
      {status ? <p role="status">{status}</p> : null}
    </form>
    </Panel>
  );
}
