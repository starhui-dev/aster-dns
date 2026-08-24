import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { Show, createSignal } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert, Field, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import {
  completeTOTPLogin,
  enrollPasskey,
  loginWithPasskey,
  loginWithPassword,
  type BootstrapStatus,
  type LoginResponse,
} from "../lib/auth";

export default function LoginPage(props: { status: BootstrapStatus }) {
  const auth = useAuth();
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [totpToken, setTOTPToken] = createSignal<string | null>(null);
  const [totpCode, setTOTPCode] = createSignal("");
  const [enrollmentToken, setEnrollmentToken] = createSignal("");
  const [passkeyName, setPasskeyName] = createSignal("My passkey");

  const handleResult = async (response: LoginResponse) => {
    if (response.totp_required && response.totp_token !== undefined) {
      setTOTPToken(response.totp_token);
      setPassword("");
      return;
    }
    await auth.acceptLogin(response);
  };

  const run = async (operation: () => Promise<LoginResponse>) => {
    setBusy(true);
    setError(null);
    try {
      await handleResult(await operation());
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  };
  const submitPassword = (event: SubmitEvent) => {
    event.preventDefault();
    const usernameValue = username();
    const passwordValue = password();
    void run(() => loginWithPassword(usernameValue, passwordValue));
  };

  const submitEnrollment = (event: SubmitEvent) => {
    event.preventDefault();
    const tokenValue = enrollmentToken();
    const nameValue = passkeyName();
    void run(() => enrollPasskey({ enrollment_token: tokenValue, passkey_name: nameValue }));
  };

  const submitTOTP = (event: SubmitEvent) => {
    event.preventDefault();
    const token = totpToken();
    const code = totpCode();
    if (token !== null) void run(() => completeTOTPLogin(token, code));
  };

  return (
    <AuthLayout
      eyebrow="Aster DNS"
      title="Sign in"
      description="Use a Passkey to access the DNS control plane. Password login appears only when enabled by an administrator."
      wide
    >
      <Show when={totpToken() === null} fallback={<TOTPForm />}>
        <Button
          class="w-full"
          variant="primary"
          disabled={busy() || !browserSupportsWebAuthn()}
          onClick={() => void run(loginWithPasskey)}
        >
          {busy() ? "Waiting…" : "Continue with Passkey"}
        </Button>
        {!browserSupportsWebAuthn() && (
          <Alert class="mt-3" variant="warning">
            This browser does not support WebAuthn.
          </Alert>
        )}

        <Show when={props.status.password_login_enabled}>
          <div class="my-6 flex items-center gap-3 text-xs font-semibold text-muted-foreground">
            <span class="h-px flex-1 bg-border" /> Password fallback
            <span class="h-px flex-1 bg-border" />
          </div>
          <form class="space-y-4" onSubmit={submitPassword}>
            <Field label="Username" for="login-username">
              <input
                id="login-username"
                class="text-input"
                autocomplete="username webauthn"
                required
                value={username()}
                onInput={(event) => setUsername(event.currentTarget.value)}
              />
            </Field>
            <Field label="Password" for="login-password">
              <input
                id="login-password"
                class="text-input"
                type="password"
                autocomplete="current-password"
                required
                value={password()}
                onInput={(event) => setPassword(event.currentTarget.value)}
              />
            </Field>
            <Button class="w-full" type="submit" disabled={busy()}>
              Sign in with password
            </Button>
          </form>
        </Show>

        <details class="mt-6 rounded-lg border border-border bg-surface-subtle p-4">
          <summary class="cursor-pointer text-sm font-semibold text-foreground">
            Register from an enrollment token
          </summary>
          <form class="mt-4 space-y-4" onSubmit={submitEnrollment}>
            <Field label="Enrollment token" for="enrollment-token">
              <input
                id="enrollment-token"
                class="text-input"
                type="password"
                autocomplete="off"
                required
                value={enrollmentToken()}
                onInput={(event) => setEnrollmentToken(event.currentTarget.value)}
              />
            </Field>
            <Field label="Passkey name" for="enrollment-passkey-name">
              <input
                id="enrollment-passkey-name"
                class="text-input"
                required
                value={passkeyName()}
                onInput={(event) => setPasskeyName(event.currentTarget.value)}
              />
            </Field>
            <Button class="w-full" type="submit" disabled={busy()}>
              Register Passkey
            </Button>
          </form>
        </details>
      </Show>

      {error() !== null && (
        <Alert class="mt-5" variant="danger">
          {error()}
        </Alert>
      )}
    </AuthLayout>
  );

  function TOTPForm() {
    return (
      <Panel
        title="Two-factor verification"
        description="Enter the six-digit code from your authenticator app."
      >
        <form class="space-y-4" onSubmit={submitTOTP}>
          <Field label="Authentication code" for="totp-code">
            <input
              id="totp-code"
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
          <div class="grid gap-2 sm:grid-cols-2">
            <Button type="submit" variant="primary" disabled={busy()}>
              Verify code
            </Button>
            <Button onClick={() => setTOTPToken(null)}>Start over</Button>
          </div>
        </form>
      </Panel>
    );
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId === null
      ? error.message
      : `${error.message} Request ID: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : "Authentication failed.";
}
