import type { ReactNode } from "react";

export function ErrorNotice({
  message,
  retry,
}: {
  message: string;
  retry?: () => void;
}) {
  return (
    <div className="notice" role="alert">
      <p>{displayText(message)}</p>
      {retry ? (
        <button
          className="button secondary compact"
          type="button"
          onClick={retry}
        >
          Retry
        </button>
      ) : null}
    </div>
  );
}

export function Panel({
  title,
  description,
  actions,
  children,
  className = "",
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      <div className="panel-heading">
        <div>
          <h2>{title}</h2>
          {description ? <p>{description}</p> : null}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}

export function Empty({
  title,
  children,
}: {
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <h3>{title}</h3>
      {children ? <p>{children}</p> : null}
    </div>
  );
}

export function localTime(value: string | null | undefined): string {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return (
    new Intl.DateTimeFormat("en-US", {
      dateStyle: "medium",
      timeStyle: "short",
      timeZone: "America/Chicago",
    }).format(date) + " CT"
  );
}

export function dateOnly(value: string | null | undefined): string {
  return value || "Unknown";
}

export function humanize(value: string | null | undefined): string {
  if (!value) return "Unknown";
  if (value === "fictional") return "Presentation";
  return displayText(value.replace(/\bfict_/gi, "").replaceAll("_", " "))
    .replace(/^./, (char) => char.toUpperCase());
}

// Apply only when rendering source labels: aliases, origins and snapshots stay
// unchanged for matching, import round trips and purchasing history.
export function displayText(value: string | null | undefined): string {
  if (!value) return "";
  const text = value
    .replace(/\bfictional(?:[\s_-]+|\b)|\bfict_/gi, "")
    .replace(/; not actual supplier offers\./gi, ".")
    .replace(/ {2,}/g, " ")
    .trim();
  return /^\s*(?:fictional(?:[\s_-]+|\b)|fict_)/i.test(value)
    ? text.replace(/^./, (char) => char.toUpperCase())
    : text;
}

export function displayWorkspaceName(name: string): string {
  return name === "Fictional presentation" ? "Presentation" : name;
}

export function originLabel(origin: string): string {
  if (origin === "fictional") return "Presentation";
  if (origin === "manual_private" || origin === "live_private") return "Private";
  if (origin === "sanitized_snapshot") return "Approved snapshot";
  return humanize(origin);
}

export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: string;
}) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
