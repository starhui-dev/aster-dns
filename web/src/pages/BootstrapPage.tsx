import { createSignal } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { ApiError } from "../lib/api";
import { bootstrapAdmin, type BootstrapStatus } from "../lib/auth";

export default function BootstrapPage(props: { status: BootstrapStatus }) {
  const auth = useAuth();
  const [bootstrapToken, setBootstrapToken] = createSignal("");
  const [username, setUsername] = createSignal("admin");
  const [displayName, setDisplayName] = createSignal("Administrator");
  const [passkeyName, setPasskeyName] = createSignal("Primary passkey");
  const [submitting, setSubmitting] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const response = await bootstrapAdmin({
        bootstrap_token: bootstrapToken(),
        username: username(),
        display_name: displayName(),
        passkey_name: passkeyName(),
      });
      setBootstrapToken("");
      await auth.acceptLogin(response);
    } catch (caught) {
      setError(authErrorMessage(caught));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main class="grid min-h-screen place-items-center bg-slate-100 p-6 text-slate-950 dark:bg-slate-950 dark:text-slate-100">
      <section class="w-full max-w-xl rounded-3xl border border-slate-200 bg-white p-8 shadow-xl shadow-slate-950/5 dark:border-slate-800 dark:bg-slate-900">
        <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">Aster DNS setup</p>
        <h1 class="mt-2 text-3xl font-semibold">Create the first administrator</h1>
        <p class="mt-3 text-sm leading-6 text-slate-600 dark:text-slate-300">
          The first account is created only after the server-provided one-time bootstrap token and a
          Passkey ceremony both succeed. No default password exists.
        </p>

        {!props.status.configured ? (
          <div class="mt-6 rounded-2xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
            Bootstrap is locked. Configure <code>APP_BOOTSTRAP_TOKEN</code> with 32 random bytes
            encoded as unpadded base64url, then restart the server.
          </div>
        ) : (
          <form class="mt-8 space-y-5" onSubmit={(event) => void submit(event)}>
            <Field label="Bootstrap token" for="bootstrap-token">
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
            <Field label="Username" for="bootstrap-username">
              <input
                id="bootstrap-username"
                class="text-input"
                autocomplete="username"
                required
                value={username()}
                onInput={(event) => setUsername(event.currentTarget.value)}
              />
            </Field>
            <Field label="Display name" for="bootstrap-display-name">
              <input
                id="bootstrap-display-name"
                class="text-input"
                required
                value={displayName()}
                onInput={(event) => setDisplayName(event.currentTarget.value)}
              />
            </Field>
            <Field label="Passkey name" for="bootstrap-passkey-name">
              <input
                id="bootstrap-passkey-name"
                class="text-input"
                required
                value={passkeyName()}
                onInput={(event) => setPasskeyName(event.currentTarget.value)}
              />
            </Field>
            {error() !== null && (
              <p class="text-sm text-rose-700 dark:text-rose-300" role="alert">
                {error()}
              </p>
            )}
            <button class="primary-button w-full" type="submit" disabled={submitting()}>
              {submitting() ? "Waiting for Passkey…" : "Create administrator with Passkey"}
            </button>
          </form>
        )}
      </section>
    </main>
  );
}

function Field(props: { label: string; for: string; children: unknown }) {
  return (
    <div>
      <label class="field-label" for={props.for}>
        {props.label}
      </label>
      {props.children as never}
    </div>
  );
}

function authErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId === null
      ? error.message
      : `${error.message} Request ID: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : "Authentication failed.";
}
