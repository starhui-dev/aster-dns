import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { Show, createSignal } from "solid-js";

import { useAuth } from "../app/AuthContext";
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
    <main class="grid min-h-screen place-items-center bg-slate-100 p-6 text-slate-950 dark:bg-slate-950 dark:text-slate-100">
      <section class="w-full max-w-xl rounded-3xl border border-slate-200 bg-white p-8 shadow-xl shadow-slate-950/5 dark:border-slate-800 dark:bg-slate-900">
        <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">Aster DNS</p>
        <h1 class="mt-2 text-3xl font-semibold">Sign in</h1>
        <p class="mt-3 text-sm text-slate-600 dark:text-slate-300">
          Passkey is the primary authentication method.
        </p>

        <Show when={totpToken() === null} fallback={<TOTPForm />}>
          <button
            class="primary-button mt-8 w-full"
            type="button"
            disabled={busy() || !browserSupportsWebAuthn()}
            onClick={() => void run(loginWithPasskey)}
          >
            {busy() ? "Waiting…" : "Continue with Passkey"}
          </button>
          {!browserSupportsWebAuthn() && (
            <p class="mt-2 text-sm text-amber-700 dark:text-amber-300" role="status">
              This browser does not support WebAuthn.
            </p>
          )}

          <Show when={props.status.password_login_enabled}>
            <div class="my-7 flex items-center gap-3 text-xs font-semibold tracking-wide text-slate-400 uppercase">
              <span class="h-px flex-1 bg-slate-200 dark:bg-slate-700" /> Password fallback
              <span class="h-px flex-1 bg-slate-200 dark:bg-slate-700" />
            </div>
            <form class="space-y-4" onSubmit={submitPassword}>
              <label class="block">
                <span class="field-label">Username</span>
                <input
                  class="text-input"
                  autocomplete="username webauthn"
                  required
                  value={username()}
                  onInput={(event) => setUsername(event.currentTarget.value)}
                />
              </label>
              <label class="block">
                <span class="field-label">Password</span>
                <input
                  class="text-input"
                  type="password"
                  autocomplete="current-password"
                  required
                  value={password()}
                  onInput={(event) => setPassword(event.currentTarget.value)}
                />
              </label>
              <button class="secondary-button w-full" type="submit" disabled={busy()}>
                Sign in with password
              </button>
            </form>
          </Show>

          <details class="mt-8 rounded-2xl border border-slate-200 p-4 dark:border-slate-700">
            <summary class="cursor-pointer text-sm font-semibold">
              Register from an enrollment token
            </summary>
            <form class="mt-4 space-y-4" onSubmit={submitEnrollment}>
              <label class="block">
                <span class="field-label">Enrollment token</span>
                <input
                  class="text-input"
                  type="password"
                  autocomplete="off"
                  required
                  value={enrollmentToken()}
                  onInput={(event) => setEnrollmentToken(event.currentTarget.value)}
                />
              </label>
              <label class="block">
                <span class="field-label">Passkey name</span>
                <input
                  class="text-input"
                  required
                  value={passkeyName()}
                  onInput={(event) => setPasskeyName(event.currentTarget.value)}
                />
              </label>
              <button class="secondary-button w-full" type="submit" disabled={busy()}>
                Register Passkey
              </button>
            </form>
          </details>
        </Show>

        {error() !== null && (
          <p class="mt-5 text-sm text-rose-700 dark:text-rose-300" role="alert">
            {error()}
          </p>
        )}
      </section>
    </main>
  );

  function TOTPForm() {
    return (
      <form class="mt-8 space-y-5" onSubmit={submitTOTP}>
        <div>
          <p class="text-sm font-semibold">Two-factor verification</p>
          <p class="mt-1 text-sm text-slate-600 dark:text-slate-300">
            Enter the six-digit code from your authenticator app.
          </p>
        </div>
        <label class="block">
          <span class="field-label">Authentication code</span>
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
        <button class="primary-button w-full" type="submit" disabled={busy()}>
          Verify code
        </button>
        <button class="secondary-button w-full" type="button" onClick={() => setTOTPToken(null)}>
          Start over
        </button>
      </form>
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
