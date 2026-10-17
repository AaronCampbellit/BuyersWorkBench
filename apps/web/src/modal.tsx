import { useEffect, useRef } from "react";
import type { KeyboardEvent, ReactNode } from "react";

export function Modal({
  title,
  eyebrow,
  children,
  onClose,
  pending = false,
  className = "",
}: {
  title: string;
  eyebrow: string;
  children: ReactNode;
  onClose: () => void;
  pending?: boolean;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement | null>(null);
  useEffect(() => {
    const current = ref.current;
    const opener =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    current?.showModal();
    current?.querySelector<HTMLElement>("[data-initial-focus]")?.focus();
    return () => {
      current?.close();
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  useEffect(() => {
    const current = ref.current;
    if (current?.open && !current.contains(document.activeElement)) {
      const target = current.querySelector<HTMLElement>(
        "button:not(:disabled)",
      );
      if (target) target.focus();
      else current.focus();
    }
  });
  function trap(event: KeyboardEvent<HTMLDialogElement>) {
    event.stopPropagation();
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
  return (
    <dialog
      ref={ref}
      tabIndex={-1}
      className={`detail-dialog ${className}`}
      aria-label={title}
      onKeyDown={trap}
      onCancel={(event) => {
        event.stopPropagation();
        if (pending) event.preventDefault();
        else onClose();
      }}
    >
      <header className="dialog-header">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h2>{title}</h2>
        </div>
        <button
          className="button secondary compact"
          disabled={pending}
          type="button"
          onClick={onClose}
        >
          Close
        </button>
      </header>
      {children}
    </dialog>
  );
}
