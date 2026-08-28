import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { useI18n } from "../app/i18n";
import { useAuth } from "../app/AuthContext";
import { Button } from "../components/ui/Button";
import { Alert, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiErrorMessage } from "../lib/api";
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
  const { t } = useI18n();
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
      setError(errorMessage(caught, t));
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
      setError(errorMessage(caught, t));
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
    }, t("settings.passkeyRegistered"));
  };

  const submitPassword = (event: SubmitEvent) => {
    event.preventDefault();
    const password = newPassword();
    void run(async () => {
      const response = await setPassword(password);
      setNewPassword("");
      await auth.acceptLogin(response);
      await load();
    }, t("settings.passwordUpdated"));
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
    }, t("settings.totpEnabled"));
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow={t("settings.eyebrow")}
        title={t("settings.title")}
        description={t("settings.description")}
      />

      <Show when={error() !== null}>
        <Alert variant="danger">{error()}</Alert>
      </Show>
      <Show when={notice() !== null}>
        <Alert variant="success">{notice()}</Alert>
      </Show>

      <Panel title={t("settings.passkeys")} description={t("settings.passkeysDescription")}>
        <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitPasskey}>
          <Field class="min-w-0 flex-1" label={t("settings.newPasskeyName")} for="new-passkey-name">
            <input
              id="new-passkey-name"
              class="text-input"
              required
              value={passkeyName()}
              onInput={(event) => setPasskeyName(event.currentTarget.value)}
            />
          </Field>
          <Button type="submit" variant="primary" disabled={busy()}>
            {t("settings.registerPasskey")}
          </Button>
        </form>
        <div class="mt-5 space-y-3">
          <Show
            when={!loading()}
            fallback={<p class="text-sm text-muted-foreground">{t("settings.loadingPasskeys")}</p>}
          >
            <For
              each={passkeys()}
              fallback={<p class="text-sm text-muted-foreground">{t("settings.noPasskeys")}</p>}
            >
              {(passkey) => (
                <article class="flex flex-col gap-3 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <p class="font-medium text-foreground">{passkey.name}</p>
                    <p class="mt-1 text-xs text-muted-foreground">
                      {t("settings.created", { date: formatDate(passkey.created_at) })}
                      {passkey.last_used_at === undefined
                        ? ` · ${t("settings.neverUsed")}`
                        : ` · ${t("settings.lastUsed", { date: formatDate(passkey.last_used_at) })}`}
                    </p>
                  </div>
                  <Button
                    variant="danger"
                    disabled={busy()}
                    onClick={() => {
                      if (
                        !window.confirm(t("settings.confirmDeletePasskey", { name: passkey.name }))
                      )
                        return;
                      void run(async () => {
                        const response = await deletePasskey(passkey.id);
                        await auth.acceptLogin(response);
                        await load();
                      }, t("settings.passkeyDeleted"));
                    }}
                  >
                    {t("settings.delete")}
                  </Button>
                </article>
              )}
            </For>
          </Show>
        </div>
      </Panel>

      <Show when={session()?.password_login_enabled}>
        <Panel
          title={t("settings.passwordFallback")}
          description={t("settings.passwordDescription")}
        >
          <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitPassword}>
            <Field class="min-w-0 flex-1" label={t("settings.newPassword")} for="new-password">
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
              {session()?.user.password_enabled
                ? t("settings.replacePassword")
                : t("settings.enablePassword")}
            </Button>
          </form>
          <Show when={session()?.user.password_enabled}>
            <Button
              class="mt-4"
              variant="danger"
              disabled={busy()}
              onClick={() => {
                if (!window.confirm(t("settings.confirmDisablePassword"))) return;
                void run(async () => {
                  const response = await deletePassword();
                  await auth.acceptLogin(response);
                  await load();
                }, t("settings.passwordDisabled"));
              }}
            >
              {t("settings.disablePassword")}
            </Button>
          </Show>
        </Panel>
      </Show>

      <Panel title={t("settings.totp")} description={t("settings.totpDescription")}>
        <Show
          when={!session()?.user.totp_required}
          fallback={
            <Button
              variant="danger"
              disabled={busy()}
              onClick={() => {
                if (!window.confirm(t("settings.confirmDisableTOTP"))) return;
                void run(async () => {
                  const response = await deleteTOTP();
                  await auth.acceptLogin(response);
                  setProvisioningURI(null);
                  await load();
                }, t("settings.totpDisabled"));
              }}
            >
              {t("settings.disableTOTP")}
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
                  }, t("settings.totpSetupCreated"))
                }
              >
                {t("settings.startTOTP")}
              </Button>
            }
          >
            <div class="space-y-4">
              <Alert variant="warning" title={t("settings.provisioningURI")}>
                <code class="block break-all text-xs">{provisioningURI()}</code>
              </Alert>
              <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitTOTP}>
                <Field
                  class="min-w-0 flex-1"
                  label={t("settings.confirmationCode")}
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
                  {t("settings.confirmTOTP")}
                </Button>
              </form>
            </div>
          </Show>
        </Show>
      </Panel>

      <Panel title={t("settings.sessions")} description={t("settings.sessionsDescription")}>
        <div class="mb-4 flex justify-end">
          <Button
            disabled={busy()}
            onClick={() => {
              if (!window.confirm(t("settings.confirmRevokeOther"))) return;
              void run(async () => {
                await revokeOtherSessions();
                await load();
              }, t("settings.otherSessionsRevoked"));
            }}
          >
            {t("settings.revokeOther")}
          </Button>
        </div>
        <div class="space-y-3">
          <For
            each={sessions()}
            fallback={<p class="text-sm text-muted-foreground">{t("settings.noSessions")}</p>}
          >
            {(item) => (
              <article class="flex flex-col gap-3 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                <div class="min-w-0">
                  <p class="font-medium text-foreground">
                    {item.current
                      ? t("settings.currentSession")
                      : item.user_agent || t("settings.unknownClient")}
                  </p>
                  <p class="mt-1 break-words text-xs text-muted-foreground">
                    {item.auth_method} · {item.ip || t("settings.ipUnavailable")} ·{" "}
                    {t("settings.lastSeen", { date: formatDate(item.last_seen_at) })}
                  </p>
                </div>
                <Show when={!item.current}>
                  <Button
                    variant="danger"
                    disabled={busy()}
                    onClick={() => {
                      if (!window.confirm(t("settings.confirmRevokeSession"))) return;
                      void run(async () => {
                        await revokeSession(item.id);
                        await load();
                      }, t("settings.sessionRevoked"));
                    }}
                  >
                    {t("settings.revoke")}
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

function errorMessage(
  error: unknown,
  t: (key: string, values?: Record<string, string | number>) => string,
): string {
  if (error instanceof ApiError) {
    const message = apiErrorMessage(error, t("auth.requestFailedMessage"));
    return error.requestId === null
      ? message
      : `${message} ${t("settings.requestId")}: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : t("settings.requestFailed");
}
