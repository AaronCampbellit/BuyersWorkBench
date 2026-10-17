import { useEffect, useRef, useState } from "react";
import type { FormEvent, KeyboardEvent } from "react";
import type { Supplier } from "./workbench-types";
import type { Mutate } from "./workbench";
import { displayText, ErrorNotice } from "./ui";

type SupplierDraft = Pick<
  Supplier,
  "name" | "contact_name" | "email" | "phone" | "website" | "notes" | "status"
>;
const emptyDraft: SupplierDraft = {
  name: "",
  contact_name: "",
  email: "",
  phone: "",
  website: "",
  notes: "",
  status: "active",
};

function draftFrom(supplier: Supplier | null): SupplierDraft {
  if (!supplier) return { ...emptyDraft };
  return {
    name: displayText(supplier.name),
    contact_name: supplier.contact_name ?? "",
    email: supplier.email ?? "",
    phone: supplier.phone ?? "",
    website: supplier.website ?? "",
    notes: supplier.notes ?? "",
    status: supplier.status ?? "active",
  };
}

export function SupplierEditor({
  supplier,
  mutate,
  busy,
  serverError,
  onClose,
  onSaved,
}: {
  supplier: Supplier | null;
  mutate: Mutate;
  busy: boolean;
  serverError: string | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState<SupplierDraft>(() => draftFrom(supplier));
  const [baseRevision, setBaseRevision] = useState(supplier?.revision ?? 0);
  const [saving, setSaving] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const dialog = useRef<HTMLDialogElement | null>(null);
  const nameInput = useRef<HTMLInputElement | null>(null);
  const conflicted = supplier !== null && supplier.revision !== baseRevision;
  useEffect(() => {
    const current = dialog.current;
    const opener =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    current?.showModal();
    nameInput.current?.focus();
    return () => {
      current?.close();
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  useEffect(() => {
    const current = dialog.current;
    if (current?.open && !current.contains(document.activeElement)) {
      const control = current.querySelector<HTMLButtonElement>(
        "button:not(:disabled)",
      );
      if (control) control.focus();
      else current.focus();
    }
  }, [busy, saving]);
  function trapFocus(event: KeyboardEvent<HTMLDialogElement>) {
    if (event.key !== "Tab") return;
    const current = event.currentTarget;
    const controls = Array.from(
      current.querySelectorAll<HTMLElement>(
        "button,a[href],input,select,textarea,[tabindex]",
      ),
    ).filter(
      (el) =>
        !el.matches(":disabled") &&
        el.tabIndex >= 0 &&
        el.getClientRects().length > 0,
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
  function update<K extends keyof SupplierDraft>(
    key: K,
    value: SupplierDraft[K],
  ) {
    setDraft((current) => ({ ...current, [key]: value }));
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || busy || conflicted || !draft.name.trim()) return;
    setSaving(true);
    setAttempted(true);
    const fields = {
      ...draft,
      name:
        supplier && draft.name.trim() === displayText(supplier.name)
          ? supplier.name
          : draft.name.trim(),
      contact_name: draft.contact_name.trim(),
      email: draft.email.trim(),
      phone: draft.phone.trim(),
      website: draft.website.trim(),
      notes: draft.notes.trim(),
    };
    const success = await mutate(
      supplier ? `/suppliers/${encodeURIComponent(supplier.id)}` : "/suppliers",
      supplier ? { ...fields, revision: baseRevision } : fields,
      supplier ? "PATCH" : "POST",
    );
    setSaving(false);
    if (success) onSaved();
  }
  function reloadDraft() {
    setDraft(draftFrom(supplier));
    setBaseRevision(supplier?.revision ?? 0);
    setAttempted(false);
    nameInput.current?.focus();
  }
  const disabled = busy || saving;
  return (
    <dialog
      ref={dialog}
      tabIndex={-1}
      className="detail-dialog supplier-dialog"
      aria-labelledby="supplier-editor-title"
      onKeyDown={trapFocus}
      onCancel={(event) => {
        if (saving) event.preventDefault();
        else onClose();
      }}
    >
      <div className="dialog-header">
        <div>
          <p className="eyebrow">Supplier directory</p>
          <h2 id="supplier-editor-title">
            {supplier ? "Edit supplier" : "Add supplier"}
          </h2>
        </div>
        <button
          className="button secondary compact"
          type="button"
          disabled={saving}
          onClick={onClose}
        >
          Close
        </button>
      </div>
      <form
        className="dialog-content supplier-profile-form"
        onSubmit={submit}
        aria-busy={saving}
      >
        <p className="muted supplier-form-intro">
          Save contact and profile details in this workspace. Supplier
          observations and order facts stay unchanged.
        </p>
        {attempted && !saving && serverError ? (
          <ErrorNotice message={serverError} />
        ) : null}
        {conflicted ? (
          <div className="notice" role="alert">
            <p>
              A newer supplier revision is available. Reload its details before
              saving; this replaces your unsaved edits.
            </p>
            <button
              type="button"
              className="button secondary compact"
              disabled={disabled}
              onClick={reloadDraft}
            >
              Reload supplier details
            </button>
          </div>
        ) : null}
        <fieldset disabled={disabled} className="supplier-form-grid">
          <div className="supplier-wide-field">
            <label htmlFor="supplier-name">
              Supplier name <span className="required-label">(required)</span>
            </label>
            <input
              ref={nameInput}
              id="supplier-name"
              name="name"
              autoComplete="organization"
              required
              maxLength={100}
              value={draft.name}
              onChange={(e) => update("name", e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="supplier-contact">Contact name</label>
            <input
              id="supplier-contact"
              name="contact_name"
              autoComplete="name"
              maxLength={100}
              value={draft.contact_name}
              onChange={(e) => update("contact_name", e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="supplier-email">Email</label>
            <input
              id="supplier-email"
              name="email"
              type="email"
              autoComplete="email"
              maxLength={254}
              value={draft.email}
              onChange={(e) => update("email", e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="supplier-phone">Phone</label>
            <input
              id="supplier-phone"
              name="phone"
              type="tel"
              autoComplete="tel"
              maxLength={50}
              value={draft.phone}
              onChange={(e) => update("phone", e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="supplier-status">Status</label>
            <select
              id="supplier-status"
              name="status"
              value={draft.status}
              onChange={(e) =>
                update("status", e.target.value as SupplierDraft["status"])
              }
            >
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
            </select>
          </div>
          <div className="supplier-wide-field">
            <label htmlFor="supplier-website">Website</label>
            <input
              id="supplier-website"
              name="website"
              type="url"
              autoComplete="url"
              placeholder="https://example.com"
              maxLength={2048}
              value={draft.website}
              onChange={(e) => update("website", e.target.value)}
              aria-describedby="supplier-website-help"
            />
            <p className="cell-meta" id="supplier-website-help">
              Use a full HTTP or HTTPS address.
            </p>
          </div>
          <div className="supplier-wide-field">
            <label htmlFor="supplier-notes">Notes</label>
            <textarea
              id="supplier-notes"
              name="notes"
              rows={4}
              maxLength={4000}
              value={draft.notes}
              onChange={(e) => update("notes", e.target.value)}
            />
          </div>
          <div className="form-actions supplier-wide-field">
            <button
              className="button primary"
              type="submit"
              disabled={conflicted || !draft.name.trim()}
            >
              {saving
                ? "Saving supplier…"
                : supplier
                  ? "Save supplier"
                  : "Create supplier"}
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
    </dialog>
  );
}
