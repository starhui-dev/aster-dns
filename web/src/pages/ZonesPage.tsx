import { A } from "@solidjs/router";
import { For, Show, createSignal, onCleanup, onMount } from "solid-js";

import { useAuth } from "../app/AuthContext";

import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import {
  listProviderAccounts,
  listProviderTypes,
  listZones,
  refreshZone,
  type ProviderAccount,
  type ProviderTypeDefinition,
  type Zone,
} from "../lib/dns";

export default function ZonesPage() {
  const auth = useAuth();
  const canMutate = () => {
    const state = auth.state();
    return (
      state.kind === "authenticated" &&
      (state.session.user.role === "admin" || state.session.user.role === "operator")
    );
  };
  const [zones, setZones] = createSignal<Zone[]>([]);
  const [providers, setProviders] = createSignal<ProviderTypeDefinition[]>([]);
  const [accounts, setAccounts] = createSignal<ProviderAccount[]>([]);
  const [query, setQuery] = createSignal("");
  const [providerType, setProviderType] = createSignal("");
  const [accountID, setAccountID] = createSignal("");
  const [status, setStatus] = createSignal("");
  const [cursor, setCursor] = createSignal("");
  const [nextCursor, setNextCursor] = createSignal("");
  const [total, setTotal] = createSignal(0);
  const [loading, setLoading] = createSignal(true);
  const [busyZone, setBusyZone] = createSignal<string>();
  const [error, setError] = createSignal<{ message: string; requestId?: string } | null>(null);

  const loadCatalog = async (signal?: AbortSignal) => {
    const [catalog, accountList] = await Promise.all([
      listProviderTypes(signal),
      listProviderAccounts(signal),
    ]);
    setProviders(catalog.provider_types);
    setAccounts(accountList.provider_accounts);
  };

  const loadZones = async (signal?: AbortSignal, requestedCursor = cursor()) => {
    setLoading(true);
    try {
      const result = await listZones(
        {
          q: query(),
          provider_type: providerType(),
          provider_account_id: accountID(),
          status: status(),
          cursor: requestedCursor,
          limit: 50,
        },
        signal,
      );
      setZones(result.zones);
      setNextCursor(result.next_cursor ?? "");
      setTotal(result.total);
      setError(null);
    } catch (caught) {
      setError(errorState(caught));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    const initialize = async () => {
      try {
        await loadCatalog(controller.signal);
        await loadZones(controller.signal, "");
      } catch (caught) {
        setError(errorState(caught));
        setLoading(false);
      }
    };
    void initialize();
    onCleanup(() => controller.abort());
  });

  const applyFilters = (event: SubmitEvent) => {
    event.preventDefault();
    setCursor("");
    void loadZones(undefined, "");
  };

  const refresh = (zone: Zone) => {
    setBusyZone(zone.id);
    setError(null);
    void refreshZone(zone.id)
      .then((result) =>
        setZones((current) => current.map((item) => (item.id === zone.id ? result.zone : item))),
      )
      .catch((caught) => setError(errorState(caught)))
      .finally(() => setBusyZone(undefined));
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="Global DNS index"
        title="Zones"
        description={`${total()} indexed zones across all enabled and disabled provider accounts.`}
        actions={<Button onClick={() => void loadZones()}>Reload index</Button>}
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

      <Panel compact>
        <form class="grid gap-3 md:grid-cols-2 xl:grid-cols-5" onSubmit={applyFilters}>
          <Field label="Search" for="zone-search">
            <input
              id="zone-search"
              class="text-input"
              type="search"
              placeholder="example.com"
              value={query()}
              onInput={(event) => setQuery(event.currentTarget.value)}
            />
          </Field>
          <Field label="Provider" for="zone-provider-filter">
            <select
              id="zone-provider-filter"
              class="text-input"
              value={providerType()}
              onChange={(event) => setProviderType(event.currentTarget.value)}
            >
              <option value="">All providers</option>
              <For each={providers()}>
                {(provider) => <option value={provider.type}>{provider.display_name}</option>}
              </For>
            </select>
          </Field>
          <Field label="Account" for="zone-account-filter">
            <select
              id="zone-account-filter"
              class="text-input"
              value={accountID()}
              onChange={(event) => setAccountID(event.currentTarget.value)}
            >
              <option value="">All accounts</option>
              <For each={accounts()}>
                {(account) => <option value={account.id}>{account.name}</option>}
              </For>
            </select>
          </Field>
          <Field label="Status" for="zone-status-filter">
            <input
              id="zone-status-filter"
              class="text-input"
              placeholder="Provider status"
              value={status()}
              onInput={(event) => setStatus(event.currentTarget.value)}
            />
          </Field>
          <div class="flex items-end gap-2">
            <Button type="submit" variant="primary" disabled={loading()}>
              Apply filters
            </Button>
            <Button
              disabled={loading()}
              onClick={() => {
                setQuery("");
                setProviderType("");
                setAccountID("");
                setStatus("");
                setCursor("");
                void loadZones(undefined, "");
              }}
            >
              Reset
            </Button>
          </div>
        </form>
      </Panel>

      <Panel compact>
        <Show
          when={!loading()}
          fallback={
            <p class="p-2 text-sm text-muted-foreground" aria-live="polite">
              Loading zones…
            </p>
          }
        >
          <div class="overflow-x-auto">
            <table class="w-full min-w-[56rem] border-collapse text-left text-sm">
              <thead>
                <tr class="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                  <th class="px-3 py-3 font-semibold">Zone</th>
                  <th class="px-3 py-3 font-semibold">Provider account</th>
                  <th class="px-3 py-3 font-semibold">Status</th>
                  <th class="px-3 py-3 font-semibold">Freshness</th>
                  <th class="px-3 py-3 text-right font-semibold">Actions</th>
                </tr>
              </thead>
              <tbody>
                <For
                  each={zones()}
                  fallback={
                    <tr>
                      <td class="px-3 py-8 text-center text-muted-foreground" colSpan={5}>
                        No zones match the current filters.
                      </td>
                    </tr>
                  }
                >
                  {(zone) => (
                    <tr class="border-b border-border last:border-0">
                      <td class="px-3 py-4">
                        <A
                          class="font-semibold text-primary hover:underline"
                          href={`/zones/${zone.id}/records`}
                        >
                          {zone.name}
                        </A>
                        <Show when={(zone.metadata.nameservers?.length ?? 0) > 0}>
                          <p class="mt-1 max-w-sm truncate text-xs text-muted-foreground">
                            {zone.metadata.nameservers?.join(", ")}
                          </p>
                        </Show>
                      </td>
                      <td class="px-3 py-4">
                        <p class="font-medium">{zone.provider_account_name}</p>
                        <p class="text-xs text-muted-foreground">
                          {providerLabel(providers(), zone.provider_type)}
                        </p>
                      </td>
                      <td class="px-3 py-4">
                        <div class="flex flex-wrap gap-2">
                          <Badge tone={zone.account_enabled ? "success" : "neutral"}>
                            {zone.account_enabled ? zone.status || "Active" : "Account disabled"}
                          </Badge>
                          <Show when={zone.validation_status !== "valid"}>
                            <Badge tone="warning">{zone.validation_status}</Badge>
                          </Show>
                        </div>
                      </td>
                      <td class="px-3 py-4">
                        <Badge tone={zone.stale ? "warning" : "success"}>
                          {zone.stale ? "Stale" : "Fresh"}
                        </Badge>
                        <p class="mt-1 text-xs text-muted-foreground">
                          {formatDate(zone.fetched_at)}
                        </p>
                      </td>
                      <td class="px-3 py-4 text-right">
                        <div class="flex justify-end gap-2">
                          <Show when={canMutate()}>
                            <Button
                              size="sm"
                              disabled={busyZone() === zone.id || !zone.account_enabled}
                              onClick={() => refresh(zone)}
                            >
                              Refresh
                            </Button>
                          </Show>
                          <A
                            class="inline-flex min-h-8 items-center rounded-md border border-primary bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary-hover"
                            href={`/zones/${zone.id}/records`}
                          >
                            Records
                          </A>
                        </div>
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </div>
        </Show>
      </Panel>

      <div class="flex justify-end">
        <Button
          disabled={loading() || nextCursor() === ""}
          onClick={() => {
            const next = nextCursor();
            setCursor(next);
            void loadZones(undefined, next);
          }}
        >
          Next page
        </Button>
      </div>
    </div>
  );
}

function providerLabel(providers: ProviderTypeDefinition[], type: string): string {
  return providers.find((provider) => provider.type === type)?.display_name ?? type;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorState(error: unknown): { message: string; requestId?: string } {
  if (error instanceof ApiError) {
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  }
  return { message: error instanceof Error ? error.message : "Zone request failed." };
}
