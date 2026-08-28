import { Show, createEffect, createSignal } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { useI18n } from "../app/i18n";
import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert, Field } from "../components/ui/Layout";
import { ApiError, apiErrorMessage } from "../lib/api";
import { bootstrapAdmin, bootstrapAdminWithPassword, type BootstrapStatus } from "../lib/auth";

type BootstrapMethod = "password" | "passkey";

export default function BootstrapPage(props: { status: BootstrapStatus }) {
  const auth = useAuth();
  const { t } = useI18n();
  const [bootstrapToken, setBootstrapToken] = createSignal("");
  const [username, setUsername] = createSignal("admin");
  const [displayName, setDisplayName] = createSignal("Administrator");
  const [password, setPassword] = createSignal("");
  const [passwordConfirmation, setPasswordConfirmation] = createSignal("");
  const [passkeyName, setPasskeyName] = createSignal("Primary passkey");
  const [method, setMethod] = createSignal<BootstrapMethod>("password");
  createEffect(() => {
    if (!props.status.password_login_enabled) setMethod("passkey");
  });
  const [submitting, setSubmitting] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const response = await createAdministrator();
      setBootstrapToken("");
      setPassword("");
      setPasswordConfirmation("");
      await auth.acceptLogin(response);
    } catch (caught) {
      setError(authErrorMessage(caught, t));
    } finally {
      setSubmitting(false);
    }
  };

  const createAdministrator = async () => {
    if (method() === "password") {
      if (password() !== passwordConfirmation()) {
        throw new Error(t("auth.passwordMismatch"));
      }
      return bootstrapAdminWithPassword({
        bootstrap_token: bootstrapToken(),
        username: username(),
        display_name: displayName(),
        password: password(),
      });
    }
    return bootstrapAdmin({
      bootstrap_token: bootstrapToken(),
      username: username(),
      display_name: displayName(),
      passkey_name: passkeyName(),
    });
  };

  return (
    <AuthLayout
      eyebrow={t("auth.bootstrap.eyebrow")}
      title={t("auth.bootstrap.title")}
      description={
        props.status.password_login_enabled
          ? t("auth.bootstrap.description")
          : t("auth.bootstrap.descriptionPasskeyOnly")
      }
      wide
    >
      <Show
        when={props.status.configured}
        fallback={
          <Alert variant="warning" title={t("auth.bootstrap.locked")}>
            {t("auth.bootstrap.lockedMessage")}
          </Alert>
        }
      >
        <form class="space-y-5" onSubmit={(event) => void submit(event)}>
          <Field
            label={t("auth.bootstrap.token")}
            for="bootstrap-token"
            hint={t("auth.bootstrap.tokenHint")}
          >
            <input
              id="bootstrap-token"
              class="text-input"
              type="password"
              autocomplete="off"
              required
              value={bootstrapToken()}
              onInput={(event) => setBootstrapToken(event.currentTarget.value)}
            />
          </Field>
          <div class="grid gap-4 sm:grid-cols-2">
            <Field label={t("auth.username")} for="bootstrap-username">
              <input
                id="bootstrap-username"
                class="text-input"
                autocomplete="username"
                required
                value={username()}
                onInput={(event) => setUsername(event.currentTarget.value)}
              />
            </Field>
            <Field label={t("auth.displayName")} for="bootstrap-display-name">
              <input
                id="bootstrap-display-name"
                class="text-input"
                required
                value={displayName()}
                onInput={(event) => setDisplayName(event.currentTarget.value)}
              />
            </Field>
          </div>

          <fieldset class="space-y-3">
            <legend class="text-sm font-medium text-foreground">{t("auth.initialMethod")}</legend>
            <Show when={props.status.password_login_enabled}>
              <label class="flex cursor-pointer gap-3 rounded-md border border-border bg-surface-subtle p-3">
                <input
                  class="mt-1"
                  type="radio"
                  name="bootstrap-method"
                  value="password"
                  aria-label={t("auth.passwordMethod")}
                  checked={method() === "password"}
                  disabled={submitting()}
                  onChange={() => setMethod("password")}
                />
                <span>
                  <span class="block text-sm font-semibold text-foreground">
                    {t("auth.passwordMethod")}
                  </span>
                  <span class="mt-1 block text-xs leading-5 text-muted-foreground">
                    {t("auth.passwordMethodHint")}
                  </span>
                </span>
              </label>
            </Show>
            <label class="flex cursor-pointer gap-3 rounded-md border border-border bg-surface-subtle p-3">
              <input
                class="mt-1"
                type="radio"
                name="bootstrap-method"
                value="passkey"
                aria-label={t("auth.passkeyMethod")}
                checked={method() === "passkey"}
                disabled={submitting()}
                onChange={() => setMethod("passkey")}
              />
              <span>
                <span class="block text-sm font-semibold text-foreground">
                  {t("auth.passkeyMethod")}
                </span>
                <span class="mt-1 block text-xs leading-5 text-muted-foreground">
                  {t("auth.passkeyMethodHint")}
                </span>
              </span>
            </label>
          </fieldset>

          <Show when={method() === "password"}>
            <div class="grid gap-4 sm:grid-cols-2">
              <Field
                label={t("auth.password")}
                for="bootstrap-password"
                hint={t("auth.passwordHint")}
              >
                <input
                  id="bootstrap-password"
                  class="text-input"
                  type="password"
                  autocomplete="new-password"
                  required={method() === "password"}
                  value={password()}
                  onInput={(event) => setPassword(event.currentTarget.value)}
                />
              </Field>
              <Field label={t("auth.confirmPassword")} for="bootstrap-password-confirmation">
                <input
                  id="bootstrap-password-confirmation"
                  class="text-input"
                  type="password"
                  autocomplete="new-password"
                  required={method() === "password"}
                  value={passwordConfirmation()}
                  onInput={(event) => setPasswordConfirmation(event.currentTarget.value)}
                />
              </Field>
            </div>
          </Show>

          <Show when={method() === "passkey"}>
            <Field label={t("auth.passkeyName")} for="bootstrap-passkey-name">
              <input
                id="bootstrap-passkey-name"
                class="text-input"
                required={method() === "passkey"}
                value={passkeyName()}
                onInput={(event) => setPasskeyName(event.currentTarget.value)}
              />
            </Field>
          </Show>

          {error() !== null && <Alert variant="danger">{error()}</Alert>}
          <Button class="w-full" type="submit" variant="primary" disabled={submitting()}>
            {submitting()
              ? method() === "passkey"
                ? t("auth.waitingPasskey")
                : t("auth.creating")
              : method() === "passkey"
                ? t("auth.createWithPasskey")
                : t("auth.createWithPassword")}
          </Button>
        </form>
      </Show>
    </AuthLayout>
  );
}

function authErrorMessage(error: unknown, translate: (key: string) => string): string {
  if (error instanceof ApiError) {
    const message =
      error.code === "authentication_failed"
        ? translate("auth.authenticationFailed")
        : error.code === "origin_denied"
          ? translate("auth.originDenied")
          : apiErrorMessage(error, translate("auth.requestFailedMessage"));
    return error.requestId === null
      ? message
      : `${message} ${translate("auth.requestId")}: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : translate("auth.genericFailure");
}
