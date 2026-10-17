import { StrictMode, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { ApiError, errorMessage, isAborted, request } from "./api";
import type { Session } from "./api";
import { Workbench } from "./workbench";
import { displayText } from "./ui";
import "./styles.css";

type SessionState =
  | { status: "checking" }
  | { status: "signed_out"; expired?: boolean }
  | { status: "error"; message: string }
  | { status: "authenticated"; session: Session };

function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark" aria-hidden="true">
        BW
      </span>
      <span>BuyersWorkBench</span>
    </div>
  );
}

function Notice({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div className="notice" role="alert">
      <p>{displayText(message)}</p>
      {onRetry ? (
        <button type="button" className="button secondary" onClick={onRetry}>
          Retry
        </button>
      ) : null}
    </div>
  );
}

function App() {
  const [state, setState] = useState<SessionState>({ status: "checking" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    request<Session>("/api/session", { signal: controller.signal })
      .then((session) => setState({ status: "authenticated", session }))
      .catch((error: unknown) => {
        if (isAborted(error)) return;
        if (error instanceof ApiError && error.status === 401)
          setState({ status: "signed_out" });
        else setState({ status: "error", message: errorMessage(error) });
      });
    return () => controller.abort();
  }, [attempt]);

  function retrySession() {
    setState({ status: "checking" });
    setAttempt((value) => value + 1);
  }

  if (state.status === "authenticated") {
    if (state.session.user.must_change_password)
      return (
        <PasswordChange
          session={state.session}
          onSignedOut={() => setState({ status: "signed_out" })}
          onUpdated={(session) =>
            setState({ status: "authenticated", session })
          }
        />
      );
    return (
      <Workbench
        session={state.session}
        onSignedOut={() => setState({ status: "signed_out" })}
        onExpired={() => setState({ status: "signed_out", expired: true })}
      />
    );
  }

  return (
    <div className="login-layout">
      <header className="login-header">
        <Brand />
        <span className="environment-label">Purchasing workspace</span>
      </header>
      <main className="login-main" id="main-content">
        <div className="login-intro">
          <p className="eyebrow">Purchasing, with context</p>
          <h1>A foundation for better buying decisions.</h1>
          <p>
            Track supplier changes, review purchasing priorities, and
            record buyer follow-up in your authorized workspace.
          </p>
          <div className="scope-note">
            Orders, quotes, and supplier follow-up in one workspace.
          </div>
        </div>
        <section className="login-panel" aria-labelledby="sign-in-title">
          <p className="eyebrow">BuyersWorkBench</p>
          <h2 id="sign-in-title">Sign in to your workspace</h2>
          {state.status === "checking" ? (
            <p className="muted" role="status">
              Checking your session…
            </p>
          ) : null}
          {state.status === "error" ? (
            <Notice message={state.message} onRetry={retrySession} />
          ) : null}
          {state.status === "signed_out" ? (
            <LoginForm
              expired={state.expired ?? false}
              onSignedIn={(session) =>
                setState({ status: "authenticated", session })
              }
            />
          ) : null}
        </section>
      </main>
      <footer className="login-footer">
        BuyersWorkBench
      </footer>
    </div>
  );
}

function LoginForm({
  expired,
  onSignedIn,
}: {
  expired: boolean;
  onSignedIn: (session: Session) => void;
}) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const controllerRef = useRef<AbortController | null>(null);
  useEffect(() => () => controllerRef.current?.abort(), []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError(null);
    const controller = new AbortController();
    controllerRef.current = controller;
    try {
      const session = await request<Session>("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: email.trim(), password }),
        signal: controller.signal,
      });
      onSignedIn(session);
    } catch (cause) {
      if (!isAborted(cause)) setError(errorMessage(cause));
    } finally {
      if (!controller.signal.aborted) {
        setPassword("");
        setPending(false);
      }
    }
  }

  return (
    <>
      <p className="muted">Use an account provisioned by the project owner.</p>
      {expired ? (
        <Notice message="Your session expired. Sign in again to continue." />
      ) : null}
      {error ? <Notice message={error} /> : null}
      <form onSubmit={submit} aria-busy={pending}>
        <fieldset disabled={pending}>
          <label htmlFor="email">Email address</label>
          <input
            id="email"
            name="email"
            type="email"
            autoComplete="username"
            required
            maxLength={254}
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
          <label htmlFor="password">Password</label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            maxLength={1024}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          <button className="button primary full-width" type="submit">
            {pending ? "Signing in…" : "Sign in"}
          </button>
        </fieldset>
      </form>
      <p className="login-help">
        Accounts are created locally by the owner. Public registration is
        unavailable.
      </p>
    </>
  );
}

function PasswordChange({
  session,
  onUpdated,
  onSignedOut,
}: {
  session: Session;
  onUpdated: (session: Session) => void;
  onSignedOut: () => void;
}) {
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    if (password !== confirmation) {
      setError("New passwords do not match.");
      return;
    }
    setPending(true);
    setError(null);
    const abort = new AbortController();
    controller.current = abort;
    try {
      const updated = await request<Session>("/api/password", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
        },
        body: JSON.stringify({
          current_password: current,
          new_password: password,
        }),
        signal: abort.signal,
      });
      onUpdated(updated);
    } catch (cause) {
      if (
        cause instanceof ApiError &&
        cause.status === 401 &&
        cause.message === "Authentication required."
      )
        onSignedOut();
      else if (!isAborted(cause)) setError(errorMessage(cause));
    } finally {
      if (!abort.signal.aborted) {
        setCurrent("");
        setPassword("");
        setConfirmation("");
        setPending(false);
      }
    }
  }
  async function logout() {
    if (pending) return;
    setPending(true);
    setError(null);
    const abort = new AbortController();
    controller.current = abort;
    try {
      await request<void>("/api/logout", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
        },
        body: "{}",
        signal: abort.signal,
      });
      onSignedOut();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onSignedOut();
      else if (!isAborted(cause)) setError(errorMessage(cause));
    } finally {
      if (!abort.signal.aborted) setPending(false);
    }
  }
  return (
    <div className="login-layout">
      <header className="login-header">
        <Brand />
        <button
          className="button secondary compact"
          type="button"
          disabled={pending}
          onClick={logout}
        >
          Sign out
        </button>
      </header>
      <main className="password-main">
        <section className="login-panel">
          <h1>Choose your password</h1>
          <p className="muted">
            Replace your initial password before opening your workspace. Use at
            least 14 characters.
          </p>
          {error ? <Notice message={error} /> : null}
          <form onSubmit={submit} aria-busy={pending}>
            <fieldset disabled={pending}>
              <label htmlFor="current-password">Initial password</label>
              <input
                id="current-password"
                type="password"
                autoComplete="current-password"
                required
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
              <label htmlFor="new-password">New password</label>
              <input
                id="new-password"
                type="password"
                autoComplete="new-password"
                required
                minLength={14}
                maxLength={1024}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <label htmlFor="confirm-password">Confirm new password</label>
              <input
                id="confirm-password"
                type="password"
                autoComplete="new-password"
                required
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
              />
              <button className="button primary full-width" type="submit">
                {pending ? "Saving password…" : "Save password and continue"}
              </button>
            </fieldset>
          </form>
        </section>
      </main>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
