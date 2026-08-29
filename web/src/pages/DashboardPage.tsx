import { A } from "@solidjs/router";
import { ArrowRight, RefreshCw } from "lucide-solid";
import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { useI18n } from "../app/i18n";
import { ProviderIdentity } from "../components/ProviderIdentity";
import { Button } from "../components/ui/Button";
import { Alert, Badge, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiErrorMessage } from "../lib/api";
import {
  listAuditEvents,
  listProviderAccounts,
  listProviderTypes,
  listZones,
  type AuditEvent,
  type ProviderAccount,
  type ProviderTypeDefinition,
  type Zone,
} from "../lib/dns";

export default function DashboardPage() {
  const { t } = useI18n();
  const [accounts, setAccounts] = createSignal<ProviderAccount[]>([]);
  const [providers, setProviders] = createSignal<ProviderTypeDefinition[]>([]);
  const [zones, setZones] = createSignal<Zone[]>([]);
  const [events, setEvents] = createSignal<AuditEvent[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<{ message: string; requestId?: string }>();
  const [zoneTotal, setZoneTotal] = createSignal(0);

  const unhealthy = createMemo(() =>
    accounts().filter(
      (account) =>
        !account.enabled ||
        account.validation_status === "invalid" ||
        account.validation_status === "error" ||
        events().some(
          (event) =>
            event.action === "zone.sync" &&
            event.result === "failed" &&
            event.provider_account_id === account.id,
        ),
    ),
  );
  const staleAccounts = createMemo(() => {
    const threshold = Date.now() - 15 * 60 * 1000;
    return accounts().filter(
      (account) =>
        account.enabled &&
        (account.last_zone_sync_at === undefined ||
          new Date(account.last_zone_sync_at).getTime() < threshold),
    );
  });
  const mutations = createMemo(() =>
    events().filter((event) => event.action.startsWith("recordset.")),
  );
  const failures = createMemo(() => events().filter((event) => event.result === "failed"));

  const load = async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const [catalog, providerResult, zoneResult, auditResult] = await Promise.all([
        listProviderTypes(signal),
        listProviderAccounts(signal),
        listZones({ limit: 200 }, signal),
        listAuditEvents({ limit: 20 }, signal),
      ]);
      setProviders(catalog.provider_types);
      setAccounts(providerResult.provider_accounts);
      setZones(zoneResult.zones);
      setZoneTotal(zoneResult.total);
      setEvents(auditResult.audit_events);
      setError(undefined);
    } catch (caught) {
      setError(errorState(caught, t));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(controller.signal);
    onCleanup(() => controller.abort());
  });

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow={t("dashboard.eyebrow")}
        title={t("dashboard.title")}
        description={t("dashboard.description")}
        actions={
          <Button icon={RefreshCw} disabled={loading()} onClick={() => void load()}>
            {t("dashboard.refresh")}
          </Button>
        }
      />

      <Show when={error()}>
        {(value) => (
          <Alert variant="danger" role="alert">
            {value().message}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">
                {t("dashboard.request", { id: value().requestId ?? "" })}
              </span>
            </Show>
          </Alert>
        )}
      </Show>

      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label={t("dashboard.metrics")}>
        <Metric
          label={t("dashboard.providerAccounts")}
          value={accounts().length}
          hint={t("dashboard.enabled", {
            count: accounts().filter((account) => account.enabled).length,
          })}
        />
        <Metric
          label={t("dashboard.indexedZones")}
          value={zoneTotal()}
          hint={t("dashboard.providers", {
            count: new Set(zones().map((zone) => zone.provider_type)).size,
          })}
        />
        <Metric
          label={t("dashboard.staleSyncs")}
          value={staleAccounts().length}
          hint={t("dashboard.olderThan")}
          danger={staleAccounts().length > 0}
        />
        <Metric
          label={t("dashboard.recentFailures")}
          value={failures().length}
          hint={t("dashboard.lastAudit")}
          danger={failures().length > 0}
        />
      </section>

      <div class="grid gap-5 xl:grid-cols-2">
        <Panel
          title={t("dashboard.providerHealth")}
          description={t("dashboard.providerHealthDescription")}
        >
          <div class="space-y-3">
            <For
              each={accounts()}
              fallback={<p class="text-sm text-muted-foreground">{t("dashboard.noAccounts")}</p>}
            >
              {(account) => {
                const stale = () =>
                  staleAccounts().some((candidate) => candidate.id === account.id);
                const recentSyncFailure = () =>
                  events().find(
                    (event) =>
                      event.action === "zone.sync" &&
                      event.result === "failed" &&
                      event.provider_account_id === account.id,
                  );
                const healthy = () =>
                  account.enabled &&
                  account.validation_status === "valid" &&
                  recentSyncFailure() === undefined;
                return (
                  <article class="rounded-md border border-border bg-surface-subtle p-4">
                    <div class="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div class="flex flex-wrap items-center gap-2">
                          <p class="font-medium">{account.name}</p>
                          <Badge tone="primary">
                            <ProviderIdentity
                              provider={providers().find(
                                (provider) => provider.type === account.provider_type,
                              )}
                              providerType={account.provider_type}
                              iconClass="h-4 w-4"
                            />
                          </Badge>
                          <Badge tone={healthy() ? "success" : "danger"}>
                            {healthy() ? t("dashboard.healthy") : t("dashboard.needsAttention")}
                          </Badge>
                          <Show when={stale()}>
                            <Badge tone="warning">{t("dashboard.staleSync")}</Badge>
                          </Show>
                        </div>
                        <p class="mt-2 text-xs text-muted-foreground">
                          {t("dashboard.zoneCount", { count: account.zone_count })} ·{" "}
                          {account.last_zone_sync_at
                            ? t("dashboard.synced", { date: formatDate(account.last_zone_sync_at) })
                            : t("dashboard.neverSynced")}
                        </p>
                        <Show when={recentSyncFailure()}>
                          {(failure) => (
                            <p class="mt-2 text-sm text-danger-foreground">
                              {t("dashboard.latestSyncFailed")}{" "}
                              {failure().error_code ?? t("dashboard.providerError")}.{" "}
                              {t("dashboard.request", { id: failure().request_id })}
                            </p>
                          )}
                        </Show>
                      </div>
                      <A
                        class="inline-flex items-center gap-1.5 text-sm font-semibold text-primary hover:underline"
                        href={`/accounts/${account.id}`}
                      >
                        {t("dashboard.manage")}
                        <ArrowRight size={15} strokeWidth={1.8} aria-hidden="true" />
                      </A>
                    </div>
                  </article>
                );
              }}
            </For>
          </div>
        </Panel>

        <Panel
          title={t("dashboard.recentMutations")}
          description={t("dashboard.recentMutationsDescription")}
        >
          <div class="space-y-3">
            <For
              each={mutations().slice(0, 8)}
              fallback={<p class="text-sm text-muted-foreground">{t("dashboard.noMutations")}</p>}
            >
              {(event) => (
                <article class="flex flex-col gap-2 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <p class="font-medium">{event.action}</p>
                      <Badge tone={event.result === "succeeded" ? "success" : "danger"}>
                        {event.result === "succeeded"
                          ? t("dashboard.succeeded")
                          : t("dashboard.failed")}
                      </Badge>
                    </div>
                    <p class="mt-1 truncate text-xs text-muted-foreground">
                      {event.zone_id || event.resource_id || t("dashboard.dnsRecord")} ·{" "}
                      {event.actor_username || t("dashboard.system")} ·{" "}
                      {formatDate(event.occurred_at)}
                    </p>
                  </div>
                  <code class="text-xs text-muted-foreground">{event.request_id}</code>
                </article>
              )}
            </For>
          </div>
          <A
            class="mt-4 inline-flex items-center gap-1.5 text-sm font-semibold text-primary hover:underline"
            href="/audit"
          >
            {t("dashboard.viewAudit")}
            <ArrowRight size={15} strokeWidth={1.8} aria-hidden="true" />
          </A>
        </Panel>
      </div>

      <Panel title={t("dashboard.quickEntry")} description={t("dashboard.quickEntryDescription")}>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <For
            each={zones().slice(0, 6)}
            fallback={<p class="text-sm text-muted-foreground">{t("dashboard.syncToIndex")}</p>}
          >
            {(zone) => (
              <A
                class="rounded-md border border-border bg-surface-subtle p-4 transition hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                href={`/zones/${zone.id}/records`}
              >
                <div class="flex items-center justify-between gap-3">
                  <p class="truncate font-medium">{zone.name}</p>
                  <Badge>
                    <ProviderIdentity
                      provider={providers().find(
                        (provider) => provider.type === zone.provider_type,
                      )}
                      providerType={zone.provider_type}
                      iconClass="h-4 w-4"
                    />
                  </Badge>
                </div>
                <p class="mt-2 truncate text-xs text-muted-foreground">
                  {zone.provider_account_name}
                </p>
              </A>
            )}
          </For>
        </div>
      </Panel>

      <Show when={unhealthy().length > 0}>
        <Alert variant="warning">
          {t("dashboard.unhealthy", { count: unhealthy().length })} {t("dashboard.unhealthyDetail")}
        </Alert>
      </Show>
    </div>
  );
}

function Metric(props: { label: string; value: number; hint: string; danger?: boolean }) {
  return (
    <article class="rounded-lg border border-border bg-surface p-5 shadow-sm">
      <p class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {props.label}
      </p>
      <p
        class={`mt-2 text-3xl font-semibold ${props.danger ? "text-danger-foreground" : "text-foreground"}`}
      >
        {props.value}
      </p>
      <p class="mt-1 text-xs text-muted-foreground">{props.hint}</p>
    </article>
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "short", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorState(
  error: unknown,
  t: (key: string, values?: Record<string, string | number>) => string,
): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return {
      message: apiErrorMessage(error, t("dashboard.requestFailedMessage")),
      ...(error.requestId ? { requestId: error.requestId } : {}),
    };
  return { message: error instanceof Error ? error.message : t("dashboard.requestFailedMessage") };
}
