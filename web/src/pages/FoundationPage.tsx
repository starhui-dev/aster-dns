import { Match, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { useI18n } from "../app/i18n";

import { Alert, Badge, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiErrorMessage, apiRequest } from "../lib/api";

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
  const { t } = useI18n();
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
          setConnection({
            kind: "error",
            message: apiErrorMessage(error, t("foundation.apiUnavailableLabel")),
            requestId: error.requestId,
          });
          return;
        }
        setConnection({
          kind: "error",
          message: t("foundation.apiUnavailableLabel"),
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
        eyebrow={t("foundation.eyebrow")}
        title={t("foundation.title")}
        description={t("foundation.description")}
      />

      <div class="grid gap-4 lg:grid-cols-3">
        <Panel title={t("foundation.apiConnection")} compact>
          <Switch>
            <Match when={connection().kind === "loading"}>
              <p class="text-sm text-muted-foreground" aria-live="polite">
                {t("foundation.checking")}
              </p>
            </Match>
            <Match when={connected()}>
              <Badge tone="success">{t("foundation.apiConnected")}</Badge>
              <p class="mt-3 text-xs text-muted-foreground">
                {connected()?.overview.api_version} · {connected()?.overview.version}
              </p>
            </Match>
            <Match when={failed()}>
              <Badge tone="danger">{t("foundation.apiUnavailableLabel")}</Badge>
              <p class="mt-3 text-sm text-danger-foreground">{failed()?.message}</p>
              {failed()?.requestId && (
                <p class="mt-2 font-mono text-xs text-muted-foreground">
                  {t("foundation.request", { id: failed()?.requestId ?? "" })}
                </p>
              )}
            </Match>
          </Switch>
        </Panel>

        <Panel title={t("foundation.authentication")} compact>
          <Badge tone="success">{t("foundation.serverEnforced")}</Badge>
          <p class="mt-3 text-sm leading-6 text-muted-foreground">
            {t("foundation.authDescription")}
          </p>
        </Panel>

        <Panel title={t("foundation.providerIntegration")} compact>
          <Badge tone="success">{t("foundation.capabilityDriven")}</Badge>
          <p class="mt-3 text-sm leading-6 text-muted-foreground">
            {t("foundation.providerDescription")}
          </p>
        </Panel>
      </div>

      <Alert variant="success" title={t("foundation.securityActive")}>
        {t("foundation.securityDescription")}
      </Alert>
    </div>
  );
}
