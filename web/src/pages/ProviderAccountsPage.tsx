import { For, Match, Show, Switch, createMemo, createSignal, onCleanup, onMount } from "solid-js";

import { Alert, Badge, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiRequest } from "../lib/api";

interface DescriptorField {
  key: string;
  label: string;
  secret?: boolean;
  required?: boolean;
}

interface ProviderCapabilities {
  supported_record_types: string[];
  min_ttl?: number;
  max_ttl?: number;
  native_record_granularity: "record_set" | "record_entry";
  supports_routing_line: boolean;
  supports_weight: boolean;
  supports_record_status: boolean;
}

interface ProviderTypeDefinition {
  type: string;
  display_name: string;
  documentation_url?: string;
  credential_fields: DescriptorField[];
  account_options: DescriptorField[];
  capabilities: ProviderCapabilities;
}

interface ProviderTypesResponse {
  provider_types: ProviderTypeDefinition[];
}

type CatalogState =
  | { kind: "loading" }
  | { kind: "ready"; providers: ProviderTypeDefinition[] }
  | { kind: "error"; message: string; requestId: string | null };

export default function ProviderAccountsPage() {
  const [catalog, setCatalog] = createSignal<CatalogState>({ kind: "loading" });
  const ready = createMemo(() => {
    const current = catalog();
    return current.kind === "ready" ? current : undefined;
  });
  const failed = createMemo(() => {
    const current = catalog();
    return current.kind === "error" ? current : undefined;
  });

  onMount(() => {
    const controller = new AbortController();
    const loadCatalog = async () => {
      try {
        const response = await apiRequest<ProviderTypesResponse>("/provider-types", {
          signal: controller.signal,
        });
        setCatalog({ kind: "ready", providers: response.provider_types });
      } catch (error) {
        if (controller.signal.aborted) return;
        setCatalog({
          kind: "error",
          message: error instanceof Error ? error.message : "Provider catalog could not be loaded.",
          requestId: error instanceof ApiError ? error.requestId : null,
        });
      }
    };

    void loadCatalog();
    onCleanup(() => controller.abort());
  });

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="DNS providers"
        title="Provider accounts"
        description="Factory metadata and capabilities are loaded from the live server registry. Account creation and credential replacement remain server-authorized operations."
      />

      <Switch>
        <Match when={catalog().kind === "loading"}>
          <Panel>
            <p class="text-sm text-muted-foreground" aria-live="polite">
              Loading provider capabilities…
            </p>
          </Panel>
        </Match>
        <Match when={failed()}>
          <Alert variant="danger" title="Provider catalog unavailable" role="alert">
            {failed()?.message}
            <Show when={failed()?.requestId}>
              <span class="mt-2 block font-mono text-xs">Request {failed()?.requestId}</span>
            </Show>
          </Alert>
        </Match>
        <Match when={ready()}>
          <Show
            when={(ready()?.providers.length ?? 0) > 0}
            fallback={
              <Alert title="No production adapters registered">
                The server returned an empty provider catalog. No cloud capability is implied.
              </Alert>
            }
          >
            <div class="grid gap-4 xl:grid-cols-2">
              <For each={ready()?.providers ?? []}>
                {(provider) => <ProviderCapabilityCard provider={provider} />}
              </For>
            </div>
          </Show>
        </Match>
      </Switch>
    </div>
  );
}

function ProviderCapabilityCard(props: { provider: ProviderTypeDefinition }) {
  const capabilities = () => props.provider.capabilities;
  return (
    <Panel class="h-full">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
            {props.provider.type}
          </p>
          <h2 class="mt-1 text-lg font-semibold text-foreground">{props.provider.display_name}</h2>
        </div>
        <Badge tone="success">Registered</Badge>
      </div>

      <div class="mt-5">
        <p class="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
          Record types
        </p>
        <div class="mt-2 flex flex-wrap gap-2">
          <For each={capabilities().supported_record_types}>
            {(recordType) => <Badge tone="neutral">{recordType}</Badge>}
          </For>
        </div>
      </div>

      <dl class="mt-5 grid gap-3 text-sm sm:grid-cols-2">
        <Capability label="Native model" value={nativeModelLabel(capabilities())} />
        <Capability label="TTL range" value={ttlRangeLabel(capabilities())} />
        <Capability label="Routing lines" value={yesNo(capabilities().supports_routing_line)} />
        <Capability label="Weighted routing" value={yesNo(capabilities().supports_weight)} />
        <Capability label="Record status" value={yesNo(capabilities().supports_record_status)} />
      </dl>

      <div class="mt-5 border-t border-border pt-4">
        <p class="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
          Credential schema
        </p>
        <ul class="mt-2 space-y-2 text-sm text-foreground">
          <For each={props.provider.credential_fields}>
            {(field) => (
              <li class="flex flex-wrap items-center justify-between gap-2">
                <span>{field.label}</span>
                <span class="flex gap-2">
                  <Show when={field.required}>
                    <Badge tone="primary">Required</Badge>
                  </Show>
                  <Show when={field.secret}>
                    <Badge tone="neutral">Secret</Badge>
                  </Show>
                </span>
              </li>
            )}
          </For>
        </ul>
      </div>

      <Show when={props.provider.account_options.length > 0}>
        <div class="mt-5 border-t border-border pt-4">
          <p class="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
            Account options
          </p>
          <ul class="mt-2 space-y-2 text-sm text-foreground">
            <For each={props.provider.account_options}>
              {(field) => (
                <li class="flex flex-wrap items-center justify-between gap-2">
                  <span>{field.label}</span>
                  <Show when={field.required}>
                    <Badge tone="primary">Required</Badge>
                  </Show>
                </li>
              )}
            </For>
          </ul>
        </div>
      </Show>

      <Show when={props.provider.documentation_url}>
        <a
          class="mt-5 inline-flex text-sm font-medium text-primary hover:underline"
          href={props.provider.documentation_url}
          target="_blank"
          rel="noreferrer"
        >
          Official documentation
        </a>
      </Show>
    </Panel>
  );
}

function Capability(props: { label: string; value: string }) {
  return (
    <div>
      <dt class="text-muted-foreground">{props.label}</dt>
      <dd class="mt-1 font-medium text-foreground">{props.value}</dd>
    </div>
  );
}

function nativeModelLabel(capabilities: ProviderCapabilities): string {
  return capabilities.native_record_granularity === "record_set" ? "RRSet" : "Record entry";
}

function ttlRangeLabel(capabilities: ProviderCapabilities): string {
  if (capabilities.min_ttl === undefined && capabilities.max_ttl === undefined)
    return "Provider-defined";
  return `${capabilities.min_ttl ?? "Provider min"}–${capabilities.max_ttl ?? "Provider max"} seconds`;
}

function yesNo(value: boolean): string {
  return value ? "Supported" : "Not supported";
}
