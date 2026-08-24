import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { ApiError } from "../lib/api";
import {
  confirmTOTP,
  deletePasskey,
  deletePassword,
  deleteTOTP,
  listPasskeys,
  listSessions,
  registerPasskey,
  revokeOtherSessions,
  revokeSession,
  setPassword,
  setupTOTP,
  type Passkey,
  type SessionInfo,
} from "../lib/auth";

export default function SettingsPage() {
  const auth = useAuth();
  const [passkeys, setPasskeys] = createSignal<Passkey[]>([]);
  const [sessions, setSessions] = createSignal<SessionInfo[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [notice, setNotice] = createSignal<string | null>(null);
  const [passkeyName, setPasskeyName] = createSignal("");
  const [newPassword, setNewPassword] = createSignal("");
  const [provisioningURI, setProvisioningURI] = createSignal<string | null>(null);
  const [totpCode, setTOTPCode] = createSignal("");

  const session = createMemo(() => {
    const state = auth.state();
    return state.kind === "authenticated" ? state.session : undefined;
  });

  const load = async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const [passkeyResult, sessionResult] = await Promise.all([
        listPasskeys(signal),
        listSessions(signal),
      ]);
      setPasskeys(passkeyResult.passkeys);
      setSessions(sessionResult.sessions);
      setError(null);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(controller.signal);
    onCleanup(() => controller.abort());
  });

  const run = async (operation: () => Promise<void>, success: string) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await operation();
      setNotice(success);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  };
  const submitPasskey = (event: SubmitEvent) => {
    event.preventDefault();
    const name = passkeyName();
    void run(async () => {
      const response = await registerPasskey(name);
      setPasskeyName("");
      await auth.acceptLogin(response);
      await load();
    }, "Passkey registered.");
  };

  const submitPassword = (event: SubmitEvent) => {
    event.preventDefault();
    const password = newPassword();
    void run(async () => {
      const response = await setPassword(password);
      setNewPassword("");
      await auth.acceptLogin(response);
    }, "Password fallback updated and other sessions revoked.");
  };

  const submitTOTP = (event: SubmitEvent) => {
    event.preventDefault();
    const code = totpCode();
    void run(async () => {
      const response = await confirmTOTP(code);
      setTOTPCode("");
      setProvisioningURI(null);
      await auth.acceptLogin(response);
    }, "TOTP enabled and other sessions revoked.");
  };

  return (
    <div class="space-y-6">
      <header>
        <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">Security</p>
        <h2 class="mt-1 text-3xl font-semibold">Authentication settings</h2>
        <p class="mt-2 text-sm text-slate-600 dark:text-slate-300">
          Manage Passkeys, password fallback, TOTP, and active server-side sessions.
        </p>
      </header>

      <Show when={error() !== null}>
        <p
          class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
          role="alert"
        >
          {error()}
        </p>
      </Show>
      <Show when={notice() !== null}>
        <p
          class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200"
          role="status"
        >
          {notice()}
        </p>
      </Show>

      <SecurityCard
        title="Passkeys"
        description="Multiple Passkeys are supported. Private key material never reaches this server."
      >
        <form class="flex flex-col gap-3 sm:flex-row" onSubmit={submitPasskey}>
          <label class="min-w-0 flex-1">
            <span class="field-label">New Passkey name</span>
            <input
              class="text-input"
              required
              value={passkeyName()}
              onInput={(event) => setPasskeyName(event.currentTarget.value)}
            />
          </label>
          <button class="primary-button self-end" type="submit" disabled={busy()}>
            Register Passkey
          </button>
        </form>
        <div class="mt-5 space-y-3">
          <Show
            when={!loading()}
            fallback={<p class="text-sm text-slate-500">Loading Passkeys…</p>}
          >
            <For
              each={passkeys()}
              fallback={<p class="text-sm text-slate-500">No Passkeys registered.</p>}
            >
              {(passkey) => (
                <article class="flex flex-col gap-3 rounded-xl border border-slate-200 p-4 sm:flex-row sm:items-center sm:justify-between dark:border-slate-700">
                  <div>
                    <p class="font-medium">{passkey.name}</p>
                    <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                      Created {formatDate(passkey.created_at)}
                      {passkey.last_used_at === undefined
                        ? " · Never used"
                        : ` · Last used ${formatDate(passkey.last_used_at)}`}
                    </p>
                  </div>
                  <button
                    class="danger-button"
                    type="button"
                    disabled={busy()}
                    onClick={() => {
                      if (!window.confirm(`Delete Passkey “${passkey.name}”?`)) return;
                      void run(async () => {
                        const response = await deletePasskey(passkey.id);
                        await auth.acceptLogin(response);
                        await load();
                      }, "Passkey deleted and other sessions revoked.");
                    }}
                  >
                    Delete
                  </button>
                </article>
              )}
            </For>
          </Show>
        </div>
      </SecurityCard>

      <Show when={session()?.password_login_enabled}>
        <SecurityCard
          title="Password fallback"
          description="Passwords are hashed with Argon2id. Passkey remains the preferred method."
        >
          <form class="flex flex-col gap-3 sm:flex-row" onSubmit={submitPassword}>
            <label class="min-w-0 flex-1">
              <span class="field-label">New password</span>
              <input
                class="text-input"
                type="password"
                minlength={12}
                maxlength={1024}
                autocomplete="new-password"
                required
                value={newPassword()}
                onInput={(event) => setNewPassword(event.currentTarget.value)}
              />
            </label>
            <button class="secondary-button self-end" type="submit" disabled={busy()}>
              {session()?.user.password_enabled ? "Replace password" : "Enable password"}
            </button>
          </form>
          <Show when={session()?.user.password_enabled}>
            <button
              class="danger-button mt-4"
              type="button"
              disabled={busy()}
              onClick={() =>
                void run(async () => {
                  const response = await deletePassword();
                  await auth.acceptLogin(response);
                }, "Password fallback disabled and other sessions revoked.")
              }
            >
              Disable password fallback
            </button>
          </Show>
        </SecurityCard>
      </Show>

      <SecurityCard
        title="Authenticator app (TOTP)"
        description="The seed is encrypted at rest. Setup is enabled only after a valid confirmation code."
      >
        <Show
          when={!session()?.user.totp_required}
          fallback={
            <button
              class="danger-button"
              type="button"
              disabled={busy()}
              onClick={() =>
                void run(async () => {
                  const response = await deleteTOTP();
                  await auth.acceptLogin(response);
                  setProvisioningURI(null);
                }, "TOTP disabled and other sessions revoked.")
              }
            >
              Disable TOTP
            </button>
          }
        >
          <Show
            when={provisioningURI() !== null}
            fallback={
              <button
                class="secondary-button"
                type="button"
                disabled={busy()}
                onClick={() =>
                  void run(async () => {
                    const result = await setupTOTP();
                    setProvisioningURI(result.provisioning_uri);
                  }, "TOTP setup created. Confirm it before leaving this page.")
                }
              >
                Start TOTP setup
              </button>
            }
          >
            <div class="space-y-4">
              <div class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-100">
                <p class="font-semibold">Provisioning URI — shown once</p>
                <code class="mt-2 block break-all text-xs">{provisioningURI()}</code>
              </div>
              <form class="flex flex-col gap-3 sm:flex-row" onSubmit={submitTOTP}>
                <label class="min-w-0 flex-1">
                  <span class="field-label">Six-digit confirmation code</span>
                  <input
                    class="text-input"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    pattern="[0-9]{6}"
                    maxlength={6}
                    required
                    value={totpCode()}
                    onInput={(event) => setTOTPCode(event.currentTarget.value)}
                  />
                </label>
                <button class="primary-button self-end" type="submit" disabled={busy()}>
                  Confirm TOTP
                </button>
              </form>
            </div>
          </Show>
        </Show>
      </SecurityCard>

      <SecurityCard
        title="Active sessions"
        description="Session tokens are opaque; only hashes are stored in PostgreSQL."
      >
        <div class="mb-4 flex justify-end">
          <button
            class="secondary-button"
            type="button"
            disabled={busy()}
            onClick={() =>
              void run(async () => {
                await revokeOtherSessions();
                await load();
              }, "Other sessions revoked.")
            }
          >
            Revoke other sessions
          </button>
        </div>
        <div class="space-y-3">
          <For
            each={sessions()}
            fallback={<p class="text-sm text-slate-500">No active sessions.</p>}
          >
            {(item) => (
              <article class="flex flex-col gap-3 rounded-xl border border-slate-200 p-4 sm:flex-row sm:items-center sm:justify-between dark:border-slate-700">
                <div class="min-w-0">
                  <p class="font-medium">
                    {item.current ? "Current session" : item.user_agent || "Unknown client"}
                  </p>
                  <p class="mt-1 break-words text-xs text-slate-500 dark:text-slate-400">
                    {item.auth_method} · {item.ip || "IP unavailable"} · Last seen{" "}
                    {formatDate(item.last_seen_at)}
                  </p>
                </div>
                <Show when={!item.current}>
                  <button
                    class="danger-button"
                    type="button"
                    disabled={busy()}
                    onClick={() =>
                      void run(async () => {
                        await revokeSession(item.id);
                        await load();
                      }, "Session revoked.")
                    }
                  >
                    Revoke
                  </button>
                </Show>
              </article>
            )}
          </For>
        </div>
      </SecurityCard>
    </div>
  );
}

function SecurityCard(props: { title: string; description: string; children: unknown }) {
  return (
    <section class="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <h3 class="text-xl font-semibold">{props.title}</h3>
      <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">{props.description}</p>
      <div class="mt-5">{props.children as never}</div>
    </section>
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId === null
      ? error.message
      : `${error.message} Request ID: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : "The security operation failed.";
}
