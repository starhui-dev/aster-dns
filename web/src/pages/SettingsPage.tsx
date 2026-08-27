import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { Button } from "../components/ui/Button";
import { Alert, Field, PageHeader, Panel } from "../components/ui/Layout";
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
      await load();
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
      await load();
    }, "TOTP enabled and other sessions revoked.");
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="Security"
        title="Authentication settings"
        description="Manage Passkeys, password fallback, TOTP, and active server-side sessions."
      />

      <Show when={error() !== null}>
        <Alert variant="danger">{error()}</Alert>
      </Show>
      <Show when={notice() !== null}>
        <Alert variant="success">{notice()}</Alert>
      </Show>

      <Panel
        title="Passkeys"
        description="Multiple Passkeys are supported. Private key material never reaches this server."
      >
        <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitPasskey}>
          <Field class="min-w-0 flex-1" label="New Passkey name" for="new-passkey-name">
            <input
              id="new-passkey-name"
              class="text-input"
              required
              value={passkeyName()}
              onInput={(event) => setPasskeyName(event.currentTarget.value)}
            />
          </Field>
          <Button type="submit" variant="primary" disabled={busy()}>
            Register Passkey
          </Button>
        </form>
        <div class="mt-5 space-y-3">
          <Show
            when={!loading()}
            fallback={<p class="text-sm text-muted-foreground">Loading Passkeys…</p>}
          >
            <For
              each={passkeys()}
              fallback={<p class="text-sm text-muted-foreground">No Passkeys registered.</p>}
            >
              {(passkey) => (
                <article class="flex flex-col gap-3 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <p class="font-medium text-foreground">{passkey.name}</p>
                    <p class="mt-1 text-xs text-muted-foreground">
                      Created {formatDate(passkey.created_at)}
                      {passkey.last_used_at === undefined
                        ? " · Never used"
                        : ` · Last used ${formatDate(passkey.last_used_at)}`}
                    </p>
                  </div>
                  <Button
                    variant="danger"
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
                  </Button>
                </article>
              )}
            </For>
          </Show>
        </div>
      </Panel>

      <Show when={session()?.password_login_enabled}>
        <Panel
          title="Password fallback"
          description="Passwords are hashed with Argon2id. Passkey remains the preferred method."
        >
          <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitPassword}>
            <Field class="min-w-0 flex-1" label="New password" for="new-password">
              <input
                id="new-password"
                class="text-input"
                type="password"
                minlength={12}
                maxlength={1024}
                autocomplete="new-password"
                required
                value={newPassword()}
                onInput={(event) => setNewPassword(event.currentTarget.value)}
              />
            </Field>
            <Button type="submit" disabled={busy()}>
              {session()?.user.password_enabled ? "Replace password" : "Enable password"}
            </Button>
          </form>
          <Show when={session()?.user.password_enabled}>
            <Button
              class="mt-4"
              variant="danger"
              disabled={busy()}
              onClick={() => {
                if (!window.confirm("Disable password fallback and revoke other sessions?")) return;
                void run(async () => {
                  const response = await deletePassword();
                  await auth.acceptLogin(response);
                  await load();
                }, "Password fallback disabled and other sessions revoked.");
              }}
            >
              Disable password fallback
            </Button>
          </Show>
        </Panel>
      </Show>

      <Panel
        title="Authenticator app (TOTP)"
        description="The seed is encrypted at rest. Setup is enabled only after a valid confirmation code."
      >
        <Show
          when={!session()?.user.totp_required}
          fallback={
            <Button
              variant="danger"
              disabled={busy()}
              onClick={() => {
                if (!window.confirm("Disable TOTP and revoke other sessions?")) return;
                void run(async () => {
                  const response = await deleteTOTP();
                  await auth.acceptLogin(response);
                  setProvisioningURI(null);
                  await load();
                }, "TOTP disabled and other sessions revoked.");
              }}
            >
              Disable TOTP
            </Button>
          }
        >
          <Show
            when={provisioningURI() !== null}
            fallback={
              <Button
                disabled={busy()}
                onClick={() =>
                  void run(async () => {
                    const result = await setupTOTP();
                    setProvisioningURI(result.provisioning_uri);
                  }, "TOTP setup created. Confirm it before leaving this page.")
                }
              >
                Start TOTP setup
              </Button>
            }
          >
            <div class="space-y-4">
              <Alert variant="warning" title="Provisioning URI — shown once">
                <code class="block break-all text-xs">{provisioningURI()}</code>
              </Alert>
              <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitTOTP}>
                <Field
                  class="min-w-0 flex-1"
                  label="Six-digit confirmation code"
                  for="totp-confirmation-code"
                >
                  <input
                    id="totp-confirmation-code"
                    class="text-input"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    pattern="[0-9]{6}"
                    maxlength={6}
                    required
                    value={totpCode()}
                    onInput={(event) => setTOTPCode(event.currentTarget.value)}
                  />
                </Field>
                <Button type="submit" variant="primary" disabled={busy()}>
                  Confirm TOTP
                </Button>
              </form>
            </div>
          </Show>
        </Show>
      </Panel>

      <Panel
        title="Active sessions"
        description="Session tokens are opaque; only hashes are stored in PostgreSQL."
      >
        <div class="mb-4 flex justify-end">
          <Button
            disabled={busy()}
            onClick={() => {
              if (!window.confirm("Revoke all other sessions?")) return;
              void run(async () => {
                await revokeOtherSessions();
                await load();
              }, "Other sessions revoked.");
            }}
          >
            Revoke other sessions
          </Button>
        </div>
        <div class="space-y-3">
          <For
            each={sessions()}
            fallback={<p class="text-sm text-muted-foreground">No active sessions.</p>}
          >
            {(item) => (
              <article class="flex flex-col gap-3 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                <div class="min-w-0">
                  <p class="font-medium text-foreground">
                    {item.current ? "Current session" : item.user_agent || "Unknown client"}
                  </p>
                  <p class="mt-1 break-words text-xs text-muted-foreground">
                    {item.auth_method} · {item.ip || "IP unavailable"} · Last seen{" "}
                    {formatDate(item.last_seen_at)}
                  </p>
                </div>
                <Show when={!item.current}>
                  <Button
                    variant="danger"
                    disabled={busy()}
                    onClick={() => {
                      if (!window.confirm("Revoke this session?")) return;
                      void run(async () => {
                        await revokeSession(item.id);
                        await load();
                      }, "Session revoked.");
                    }}
                  >
                    Revoke
                  </Button>
                </Show>
              </article>
            )}
          </For>
        </div>
      </Panel>
    </div>
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
