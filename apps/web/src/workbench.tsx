import { NotificationPreferences } from "./notification-preferences";
import { useEffect, useRef, useState } from "react";
import { ApiError, errorMessage, isAborted, request } from "./api";
import type { Session, Workspace } from "./api";
import type { Overview, Run } from "./workbench-types";
import {
  Badge,
  displayWorkspaceName,
  Empty,
  ErrorNotice,
  humanize,
  localTime,
  Panel,
} from "./ui";
import {
  Priorities,
  ObservedOrders,
  OrderInspector,
  SupplierPerformance,
} from "./purchasing";
import { PurchaseOrders } from "./purchase-orders";
import {
  QuoteComparison,
  DemoControls,
  DataHealth,
  Notifications,
} from "./workspace-tools";

export type Mutate = (
  path: string,
  body: Record<string, unknown>,
  method?: string,
) => Promise<boolean>;
export const activeRun = (run: Run) =>
  [
    "queued",
    "running",
    "validating",
    "publishing",
    "processing",
    "pending",
    "retrying",
  ].includes(run.state);

const views = [
  { key: "priorities", label: "Buyer Priorities", short: "Priorities" },
  { key: "orders", label: "Orders", short: "Orders" },
  { key: "suppliers", label: "Supplier Performance", short: "Suppliers" },
  { key: "quotes", label: "Quote Comparison", short: "Quotes" },
  { key: "health", label: "Connections / Data Health", short: "Data Health" },
  { key: "demo", label: "Demo Controls", short: "Demo Controls" },
  { key: "notifications", label: "Notifications", short: "Notifications" },
] as const;
type View = (typeof views)[number]["key"];

export function Workbench({
  session,
  onSignedOut,
  onExpired,
}: {
  session: Session;
  onSignedOut: () => void;
  onExpired: () => void;
}) {
  const [workspaces, setWorkspaces] = useState<Workspace[] | null>(null);
  const [selectedId, setSelectedId] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [view, setView] = useState<View>("priorities");
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const logoutController = useRef<AbortController | null>(null);
  const accountFooter = useRef<HTMLDivElement | null>(null);
  const expired = useRef(onExpired);
  expired.current = onExpired;
  useEffect(() => () => logoutController.current?.abort(), []);
  useEffect(() => {
    const footer = accountFooter.current;
    if (!footer) return;
    const updateSpace = () => {
      document.documentElement.style.setProperty(
        "--bwb-account-height",
        `${footer.getBoundingClientRect().height}px`,
      );
    };
    updateSpace();
    const observer = new ResizeObserver(updateSpace);
    observer.observe(footer);
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty("--bwb-account-height");
    };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    request<{ workspaces: Workspace[] }>("/api/workspaces", {
      signal: controller.signal,
    })
      .then(({ workspaces: list }) => {
        setWorkspaces(list);
        setError(null);
        setSelectedId((current) =>
          list.some((w) => w.id === current) ? current : (list[0]?.id ?? ""),
        );
      })
      .catch((cause) => {
        if (isAborted(cause)) return;
        if (cause instanceof ApiError && cause.status === 401)
          expired.current();
        else setError(errorMessage(cause));
      });
    return () => controller.abort();
  }, [attempt]);
  const workspace = workspaces?.find((w) => w.id === selectedId);
  async function logout() {
    if (loggingOut) return;
    setLoggingOut(true);
    setLogoutError(null);
    const controller = new AbortController();
    logoutController.current = controller;
    try {
      await request<void>("/api/logout", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
        },
        body: "{}",
        signal: controller.signal,
      });
      onSignedOut();
    } catch (cause) {
      if (isAborted(cause)) return;
      if (cause instanceof ApiError && cause.status === 401) onExpired();
      else setLogoutError(errorMessage(cause));
    } finally {
      if (!controller.signal.aborted) setLoggingOut(false);
    }
  }
  function changeWorkspace(id: string) {
    setSelectedId(id);
    if (
      view === "demo" &&
      workspaces?.find((w) => w.id === id)?.kind === "private"
    )
      setView("health");
  }
  return (
    <div className="app-layout">
      <a className="skip-link" href="#main-content">
        Skip to main content
      </a>
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            BW
          </span>
          <span>BuyersWorkBench</span>
        </div>
        <div className="workspace-switch sidebar-workspace">
          <label htmlFor="workspace">Workspace</label>
          <select
            id="workspace"
            value={selectedId}
            onChange={(e) => changeWorkspace(e.target.value)}
            disabled={!workspaces?.length || loggingOut}
          >
            {workspaces?.map((w) => (
              <option key={w.id} value={w.id}>
                {displayWorkspaceName(w.name)}
              </option>
            ))}
          </select>
        </div>
        <nav className="work-nav" aria-label="Main navigation">
          {views
            .filter((item) => item.key !== "demo")
            .map((item) => (
              <button
                type="button"
                key={item.key}
                className={view === item.key ? "current-view" : "nav-link"}
                aria-current={view === item.key ? "page" : undefined}
                onClick={() => setView(item.key)}
              >
                {item.short}
              </button>
            ))}
        </nav>
        <div className="sidebar-bottom" ref={accountFooter}>
          {logoutError ? (
            <ErrorNotice message={`Sign out failed. ${logoutError}`} />
          ) : null}
          <div className="account sidebar-account">
            <div>
              <strong title={session.user.display_name}>
                {session.user.display_name}
              </strong>
              <span>{humanize(session.user.role)}</span>
            </div>
            <div className="profile-actions">
              {workspace && workspace.kind !== "private" ? (
                <button
                  className={`button secondary compact${view === "demo" ? " profile-demo-current" : ""}`}
                  type="button"
                  aria-current={view === "demo" ? "page" : undefined}
                  disabled={loggingOut}
                  onClick={() => {
                    setView("demo");
                    window.scrollTo(0, 0);
                  }}
                >
                  Demo Controls
                </button>
              ) : null}
              <button
                className="button secondary compact"
                type="button"
                disabled={loggingOut}
                onClick={logout}
              >
                {loggingOut ? "Signing out…" : "Sign out"}
              </button>
            </div>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <main id="main-content" className="workspace-main">
          {error ? (
            <ErrorNotice
              message={error}
              retry={() => setAttempt((n) => n + 1)}
            />
          ) : null}
          {workspaces === null && !error ? (
            <p role="status">Loading your workspaces…</p>
          ) : null}
          {workspaces?.length === 0 ? (
            <Panel title="Your workspaces">
              <Empty title="No workspace access yet">
                Ask the project owner to provision access.
              </Empty>
            </Panel>
          ) : null}
          {workspace ? (
            <WorkspaceContent
              key={workspace.id}
              workspace={workspace}
              session={session}
              view={view}
              onNavigate={setView}
              onExpired={onExpired}
            />
          ) : null}
        </main>
      </div>
    </div>
  );
}

function WorkspaceContent({
  workspace,
  session,
  view,
  onNavigate,
  onExpired,
}: {
  workspace: Workspace;
  session: Session;
  view: View;
  onNavigate: (view: View) => void;
  onExpired: () => void;
}) {
  const [data, setData] = useState<Overview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);
  const [orderId, setOrderId] = useState<string | null>(null);
  const [selectedConditionId, setSelectedConditionId] = useState<string | null>(
    null,
  );
  const [purchaseOrderId, setPurchaseOrderId] = useState<string | null>(null);
  const [supplier, setSupplier] = useState<string | null>(null);
  const mutation = useRef<AbortController | null>(null);
  const previousGeneration = useRef<number | null>(null);
  useEffect(() => {
    if (!data) return;
    if (
      previousGeneration.current !== null &&
      previousGeneration.current !== data.generation
    ) {
      setOrderId(null);
      setSelectedConditionId(null);
      setPurchaseOrderId(null);
      setSupplier(null);
    }
    previousGeneration.current = data.generation;
  }, [data?.generation]);
  const expired = useRef(onExpired);
  expired.current = onExpired;
  const base = `/api/v1/workspaces/${encodeURIComponent(workspace.id)}`;
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    request<Overview>(`${base}/overview`, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
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
  }, [base, revision]);
  useEffect(() => {
    if (loading || !data?.runs?.some(activeRun)) return;
    const timeout = setTimeout(() => setRevision((n) => n + 1), 1200);
    return () => clearTimeout(timeout);
  }, [data, loading]);

  const mutate: Mutate = async (path, body, method = "POST") => {
    if (mutation.current || !data) return false;
    const controller = new AbortController();
    mutation.current = controller;
    setPending(true);
    setActionError(null);
    setActionNotice(null);
    try {
      await request<unknown>(`${base}${path}`, {
        method,
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
        },
        body: JSON.stringify({ generation: data.generation, ...body }),
        signal: controller.signal,
      });
      if (controller.signal.aborted) return false;
      setActionNotice("Request accepted. Refreshing workspace state…");
      setRevision((n) => n + 1);
      return true;
    } catch (cause) {
      if (isAborted(cause)) return false;
      if (
        cause instanceof ApiError &&
        (cause.status === 401 || cause.code === "password_change_required")
      )
        expired.current();
      else {
        setActionError(
          cause instanceof ApiError && cause.status === 409
            ? "This workspace or record changed. Current values are being reloaded; review them before trying again."
            : errorMessage(cause),
        );
        if (cause instanceof ApiError && cause.status === 409)
          setRevision((n) => n + 1);
      }
      return false;
    } finally {
      mutation.current = null;
      if (!controller.signal.aborted) setPending(false);
    }
  };
  const active = data?.runs?.find(activeRun);
  const busy = pending || loading || !!active;
  const isFictional = workspace.kind !== "private";
  const latestConnection = data?.connections?.reduce<
    import("./workbench-types").Connection | undefined
  >(
    (latest, connection) =>
      !latest ||
      (connection.last_success_at &&
        (!latest.last_success_at ||
          connection.last_success_at > latest.last_success_at))
        ? connection
        : latest,
    undefined,
  );
  const sourceAttention =
    data?.connections?.filter((connection) => connection.last_failure_code) ??
    [];
  const title =
    views.find((item) => item.key === view)?.label ?? "Buyer Priorities";
  function openOrder(id: string, conditionId?: string) {
    setSelectedConditionId(conditionId ?? null);
    setOrderId(id);
  }
  function showSupplier(alias: string) {
    setSupplier(alias);
    setOrderId(null);
    setSelectedConditionId(null);
    onNavigate("suppliers");
  }
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">{displayWorkspaceName(workspace.name)}</p>
          <h1>{title}</h1>
          <p className="muted">
            {view === "priorities"
              ? "Calculated exceptions, evidence, and durable buyer follow-up."
              : view === "orders"
                ? "Order records and fulfillment evidence, separate from quote comparison and buyer tasks."
                : view === "suppliers"
                  ? "Supplier profiles, contact details, and observed order coverage."
                  : view === "quotes"
                    ? "Compare offers with server-calculated eligibility."
                    : view === "health"
                      ? "Collection attempts, accepted observations, and source freshness."
                      : view === "demo"
                        ? "Controlled source changes through the collection pipeline."
                        : "Material changes and follow-up updates for your workspace."}
          </p>
        </div>
        <div className="heading-actions">
          <Badge tone={isFictional ? "fictional" : "neutral"}>
            {isFictional ? "Presentation" : "Private"}
          </Badge>
          <button
            className="button secondary compact"
            disabled={loading || pending}
            onClick={() => setRevision((n) => n + 1)}
            type="button"
          >
            {loading ? "Updating…" : "Reload view"}
          </button>
        </div>
      </div>
      <div className="workspace-context">
        <span>
          {isFictional
            ? `Business date ${data?.business_date || workspace.business_date || "not set"}`
            : "Owner-only private area"}
        </span>
        <span>Generation {data?.generation ?? workspace.generation}</span>
        <span>
          Latest accepted collection:{" "}
          {localTime(latestConnection?.last_success_at)}
        </span>
      </div>
      {error ? (
        <ErrorNotice
          message={`${error}${data ? " Previously loaded data remains visible; it has not been refreshed." : ""}`}
          retry={() => setRevision((n) => n + 1)}
        />
      ) : null}
      {actionError ? <ErrorNotice message={actionError} /> : null}
      {actionNotice && !loading && !error ? (
        <p className="action-notice" role="status">
          Request accepted; workspace view refreshed. Collection results remain
          separate from saved buyer edits.
        </p>
      ) : actionNotice && loading ? (
        <p className="action-notice" role="status">
          {actionNotice}
        </p>
      ) : null}
      {active ? (
        <div className="run-banner" role="status">
          <strong>{humanize(active.state)}</strong>
          <span>
            {data?.seeded && !data.ready
              ? "Initializing the workspace. Results are unavailable until the baseline is accepted."
              : "Collection is in progress. Previous accepted data remains visible."}
          </span>
        </div>
      ) : null}
      {sourceAttention.length ? (
        <div className="notice">
          <p>
            <strong>
              Source attention required: {sourceAttention.length} connection
              {sourceAttention.length === 1 ? "" : "s"}.
            </strong>{" "}
            Previous accepted observations are preserved. Latest accepted
            collection: {localTime(latestConnection?.last_success_at)}.
          </p>
        </div>
      ) : null}
      {!data && loading ? (
        <p className="loading-state" role="status">
          Loading workspace data…
        </p>
      ) : null}
      {data ? (
        <>
          {isFictional && !data.seeded ? (
            <Panel
              title="Initialize this workspace"
              description="Load the pinned source and enqueue its baseline collection."
            >
              <div className="panel-content">
                <p className="muted">
                  The workspace has not been seeded. No supplier observations or
                  calculated priorities are available yet.
                </p>
                <button
                  className="button primary"
                  type="button"
                  disabled={busy || !!error}
                  onClick={() => mutate("/seed", {})}
                >
                  {pending ? "Initializing…" : "Initialize workbench"}
                </button>
              </div>
            </Panel>
          ) : null}
          {view === "priorities" ? (
            <Priorities
              data={data}
              userId={session.user.id}
              onOpenOrder={openOrder}
              onSupplier={showSupplier}
              onViewOrders={() => onNavigate("orders")}
            />
          ) : null}
          {view === "orders" ? (
            <>
              <PurchaseOrders
                key={data.generation}
                data={data}
                base={base}
                selectedId={purchaseOrderId}
                onSelect={setPurchaseOrderId}
                canManage={
                  session.user.role === "owner" && workspace.kind !== "reviewer"
                }
                mutate={mutate}
                busy={pending || loading || !!error}
                mutationError={actionError}
                revision={revision}
                onExpired={onExpired}
                onViewQuotes={() => onNavigate("quotes")}
                onManageSuppliers={() => onNavigate("suppliers")}
              />
              <ObservedOrders data={data} onOpenOrder={openOrder} />
            </>
          ) : null}
          {view === "suppliers" ? (
            <SupplierPerformance
              data={data}
              selected={supplier}
              onSelect={setSupplier}
              onOpenOrder={openOrder}
              canManage={
                session.user.role === "owner" && workspace.kind !== "reviewer"
              }
              mutate={mutate}
              busy={pending || loading || !!error}
              mutationError={actionError}
            />
          ) : null}
          {view === "quotes" ? (
            <QuoteComparison
              key={data.generation}
              quotes={data.quotes}
              mutate={mutate}
              busy={busy || !!error}
              actionError={actionError}
              purchaseOrders={data.purchase_orders ?? []}
              canCreateOrder={
                session.user.role === "owner" && workspace.kind !== "reviewer"
              }
              onViewOrders={() => {
                setPurchaseOrderId(null);
                onNavigate("orders");
              }}
              onOpenPurchaseOrder={(id) => {
                setPurchaseOrderId(id);
                onNavigate("orders");
              }}
            />
          ) : null}
          {view === "health" ? (
            <DataHealth
              data={data}
              fictional={isFictional}
              mutate={mutate}
              busy={pending || loading || !!error}
              base={base}
              session={session}
              onExpired={onExpired}
              serverError={actionError}
            />
          ) : null}
          {view === "demo" && isFictional ? (
            <DemoControls
              workspace={workspace}
              data={data}
              mutate={mutate}
              busy={busy || !!error}
              resetting={pending}
            />
          ) : null}
          {view === "notifications" ? (
            <>
              <NotificationPreferences
                base={base}
                generation={data.generation}
                csrfToken={session.csrf_token}
                busy={pending || !!error}
                onExpired={onExpired}
              />
              <Notifications
                items={data.notifications ?? []}
                priorities={data.priorities ?? []}
                mutate={mutate}
                busy={pending || !!error}
                onOpenOrder={openOrder}
              />
            </>
          ) : null}
          {orderId ? (
            <OrderInspector
              key={`${orderId}-${data.generation}`}
              base={base}
              id={orderId}
              selectedConditionId={selectedConditionId}
              connections={data.connections}
              generation={data.generation}
              revision={revision}
              session={session}
              busy={busy || !!error}
              mutationError={actionError}
              mutate={mutate}
              onClose={() => {
                setOrderId(null);
                setSelectedConditionId(null);
              }}
              onExpired={onExpired}
              onSupplier={showSupplier}
              suppliers={data.suppliers}
            />
          ) : null}
        </>
      ) : null}
    </>
  );
}
