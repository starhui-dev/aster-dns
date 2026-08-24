import { Match, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { Alert, Badge, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiRequest } from "../lib/api";

interface APIOverview {
  name: string;
  api_version: string;
  version: string;
  commit: string;
  status: string;
}

type ConnectionState =
  | { kind: "loading" }
  | { kind: "connected"; overview: APIOverview }
  | { kind: "error"; message: string; requestId: string | null };

export default function FoundationPage() {
  const [connection, setConnection] = createSignal<ConnectionState>({ kind: "loading" });
  const connected = createMemo(() => {
    const current = connection();
    return current.kind === "connected" ? current : undefined;
  });
  const failed = createMemo(() => {
    const current = connection();
    return current.kind === "error" ? current : undefined;
  });

  onMount(() => {
    const controller = new AbortController();
    const loadOverview = async () => {
      try {
        const overview = await apiRequest<APIOverview>("", { signal: controller.signal });
        setConnection({ kind: "connected", overview });
      } catch (error: unknown) {
        if (error instanceof DOMException && error.name === "AbortError") {
          return;
        }
        if (error instanceof ApiError) {
          setConnection({ kind: "error", message: error.message, requestId: error.requestId });
          return;
        }
        setConnection({
          kind: "error",
          message: "The API could not be reached.",
          requestId: null,
        });
      }
    };

    void loadOverview();
    onCleanup(() => {
      controller.abort();
    });
  });

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="System"
        title="Control plane status"
        description="Live platform state. Provider capabilities come from the server registry; account, zone, and record metrics appear only after real account synchronization."
      />

      <div class="grid gap-4 lg:grid-cols-3">
        <Panel title="API connection" compact>
          <Switch>
            <Match when={connection().kind === "loading"}>
              <p class="text-sm text-muted-foreground" aria-live="polite">
                Checking the same-origin API…
              </p>
            </Match>
            <Match when={connected()}>
              <Badge tone="success">API connected</Badge>
              <p class="mt-3 text-xs text-muted-foreground">
                {connected()?.overview.api_version} · {connected()?.overview.version}
              </p>
            </Match>
            <Match when={failed()}>
              <Badge tone="danger">API unavailable</Badge>
              <p class="mt-3 text-sm text-danger-foreground">{failed()?.message}</p>
              {failed()?.requestId && (
                <p class="mt-2 font-mono text-xs text-muted-foreground">
                  Request {failed()?.requestId}
                </p>
              )}
            </Match>
          </Switch>
        </Panel>

        <Panel title="Authentication" compact>
          <Badge tone="success">Server enforced</Badge>
          <p class="mt-3 text-sm leading-6 text-muted-foreground">
            Opaque sessions, Passkeys, optional password and TOTP, RBAC, CSRF protection, and
            security audit events are active.
          </p>
        </Panel>

        <Panel title="Provider integration" compact>
          <Badge tone="success">Capability driven</Badge>
          <p class="mt-3 text-sm leading-6 text-muted-foreground">
            Production adapters publish credential schemas, RRSet granularity, record types, and
            vendor extensions through the authenticated provider catalog.
          </p>
        </Panel>
      </div>

      <Alert variant="success" title="Authentication security active">
        The API—not hidden UI controls—is the authorization authority. Configure another
        authentication method before removing the last usable Passkey or password.
      </Alert>
    </div>
  );
}
