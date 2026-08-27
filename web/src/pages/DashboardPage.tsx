import { A } from "@solidjs/router";
import { For, Show, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { Button } from "../components/ui/Button";
import { Alert, Badge, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import {
  listAuditEvents,
  listProviderAccounts,
  listZones,
  type AuditEvent,
  type ProviderAccount,
  type Zone,
} from "../lib/dns";

export default function DashboardPage() {
  const [accounts, setAccounts] = createSignal<ProviderAccount[]>([]);
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
      const [providerResult, zoneResult, auditResult] = await Promise.all([
        listProviderAccounts(signal),
        listZones({ limit: 200 }, signal),
        listAuditEvents({ limit: 20 }, signal),
      ]);
      setAccounts(providerResult.provider_accounts);
      setZones(zoneResult.zones);
      setZoneTotal(zoneResult.total);
      setEvents(auditResult.audit_events);
      setError(undefined);
    } catch (caught) {
      setError(errorState(caught));
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
        eyebrow="Operations overview"
        title="DNS control plane"
        description="Live Provider health, zone-index freshness, and recent DNS mutations."
        actions={
          <Button disabled={loading()} onClick={() => void load()}>
            Refresh dashboard
          </Button>
        }
      />

      <Show when={error()}>
        {(value) => (
          <Alert variant="danger" role="alert">
            {value().message}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">Request {value().requestId}</span>
            </Show>
          </Alert>
        )}
      </Show>

      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-label="DNS platform metrics">
        <Metric
          label="Provider accounts"
          value={accounts().length}
          hint={`${accounts().filter((account) => account.enabled).length} enabled`}
        />
        <Metric
          label="Indexed zones"
          value={zoneTotal()}
          hint={`${new Set(zones().map((zone) => zone.provider_type)).size} providers`}
        />
        <Metric
          label="Stale syncs"
          value={staleAccounts().length}
          hint="Older than 15 minutes"
          danger={staleAccounts().length > 0}
        />
        <Metric
          label="Recent failures"
          value={failures().length}
          hint="Last 20 audit events"
          danger={failures().length > 0}
        />
      </section>

      <div class="grid gap-5 xl:grid-cols-2">
        <Panel
          title="Provider health"
          description="Validation and latest zone-sync state for every account."
        >
          <div class="space-y-3">
            <For
              each={accounts()}
              fallback={
                <p class="text-sm text-muted-foreground">No Provider accounts configured.</p>
              }
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
                          <Badge tone="primary">{account.provider_type}</Badge>
                          <Badge tone={healthy() ? "success" : "danger"}>
                            {healthy() ? "Healthy" : "Needs attention"}
                          </Badge>
                          <Show when={stale()}>
                            <Badge tone="warning">Stale sync</Badge>
                          </Show>
                        </div>
                        <p class="mt-2 text-xs text-muted-foreground">
                          {account.zone_count} zones ·{" "}
                          {account.last_zone_sync_at
                            ? `Synced ${formatDate(account.last_zone_sync_at)}`
                            : "Never synced"}
                        </p>
                        <Show when={recentSyncFailure()}>
                          {(failure) => (
                            <p class="mt-2 text-sm text-danger-foreground">
                              Latest observed sync failed:{" "}
                              {failure().error_code ?? "provider error"}. Request{" "}
                              {failure().request_id}
                            </p>
                          )}
                        </Show>
                      </div>
                      <A
                        class="text-sm font-semibold text-primary hover:underline"
                        href={`/accounts/${account.id}`}
                      >
                        Manage
                      </A>
                    </div>
                  </article>
                );
              }}
            </For>
          </div>
        </Panel>

        <Panel
          title="Recent DNS mutations"
          description="Latest record create, update, delete, and batch audit events."
        >
          <div class="space-y-3">
            <For
              each={mutations().slice(0, 8)}
              fallback={<p class="text-sm text-muted-foreground">No recent DNS mutations.</p>}
            >
              {(event) => (
                <article class="flex flex-col gap-2 rounded-md border border-border bg-surface-subtle p-4 sm:flex-row sm:items-center sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <p class="font-medium">{event.action}</p>
                      <Badge tone={event.result === "succeeded" ? "success" : "danger"}>
                        {event.result}
                      </Badge>
                    </div>
                    <p class="mt-1 truncate text-xs text-muted-foreground">
                      {event.zone_id || event.resource_id || "DNS record"} ·{" "}
                      {event.actor_username || "System"} · {formatDate(event.occurred_at)}
                    </p>
                  </div>
                  <code class="text-xs text-muted-foreground">{event.request_id}</code>
                </article>
              )}
            </For>
          </div>
          <A
            class="mt-4 inline-block text-sm font-semibold text-primary hover:underline"
            href="/audit"
          >
            View audit history
          </A>
        </Panel>
      </div>

      <Panel title="Quick entry" description="Jump directly into the indexed zone inventory.">
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <For
            each={zones().slice(0, 6)}
            fallback={
              <p class="text-sm text-muted-foreground">Sync a Provider account to index zones.</p>
            }
          >
            {(zone) => (
              <A
                class="rounded-md border border-border bg-surface-subtle p-4 transition hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                href={`/zones/${zone.id}/records`}
              >
                <div class="flex items-center justify-between gap-3">
                  <p class="truncate font-medium">{zone.name}</p>
                  <Badge>{zone.provider_type}</Badge>
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
          {unhealthy().length} Provider account(s) are disabled or failed validation. Open Provider
          Accounts for safe details and request IDs.
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

function errorState(error: unknown): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  return { message: error instanceof Error ? error.message : "Dashboard request failed." };
}
