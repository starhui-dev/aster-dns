import { createSignal } from "solid-js";

import { useAuth } from "../app/AuthContext";
import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert, Field } from "../components/ui/Layout";
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
    <AuthLayout
      eyebrow="Secure bootstrap"
      title="Create the first administrator"
      description="The first account is created only after the one-time server token and a Passkey ceremony both succeed. No default password exists."
      wide
    >
      {!props.status.configured ? (
        <Alert variant="warning" title="Bootstrap is locked">
          Configure <code>APP_BOOTSTRAP_TOKEN</code> with 32 random bytes encoded as unpadded
          base64url, then restart the server.
        </Alert>
      ) : (
        <form class="space-y-5" onSubmit={(event) => void submit(event)}>
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
          {error() !== null && <Alert variant="danger">{error()}</Alert>}
          <Button class="w-full" type="submit" variant="primary" disabled={submitting()}>
            {submitting() ? "Waiting for Passkey…" : "Create administrator with Passkey"}
          </Button>
        </form>
      )}
    </AuthLayout>
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
