import {
  For,
  Show,
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
  untrack,
} from "solid-js";

import { useAuth } from "../app/AuthContext";
import { DescriptorFields, type FieldValues } from "../components/ProviderFields";
import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import {
  createProviderAccount,
  deleteProviderAccount,
  listProviderAccounts,
  listProviderTypes,
  replaceProviderCredentials,
  syncProviderZones,
  updateProviderAccount,
  validateProviderAccount,
  type ProviderAccount,
  type ProviderTypeDefinition,
} from "../lib/dns";

type EditorMode = "create" | "edit" | "credentials";

interface EditorState {
  mode: EditorMode;
  account?: ProviderAccount;
}

export default function ProviderAccountsPage() {
  const auth = useAuth();
  const [providers, setProviders] = createSignal<ProviderTypeDefinition[]>([]);
  const [accounts, setAccounts] = createSignal<ProviderAccount[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<{ message: string; requestId?: string } | null>(null);
  const [notice, setNotice] = createSignal<string | null>(null);
  const [editor, setEditor] = createSignal<EditorState | null>(null);
  const [dialog, setDialog] = createSignal<HTMLDialogElement>();
  const isAdmin = createMemo(() => {
    const state = auth.state();
    return state.kind === "authenticated" && state.session.user.role === "admin";
  });

  const load = async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const [catalog, accountList] = await Promise.all([
        listProviderTypes(signal),
        listProviderAccounts(signal),
      ]);
      setProviders(catalog.provider_types);
      setAccounts(accountList.provider_accounts);
      setError(null);
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

  createEffect(() => {
    const element = dialog();
    if (element === undefined) return;
    if (editor() !== null && !element.open) element.showModal();
    if (editor() === null && element.open) element.close();
  });

  const run = async (operation: () => Promise<void>, success: string) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await operation();
      setNotice(success);
    } catch (caught) {
      setError(errorState(caught));
    } finally {
      await load();
      setBusy(false);
    }
  };

  const handleEditorSaved = async (message: string) => {
    setEditor(null);
    setNotice(message);
    await load();
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="DNS providers"
        title="Provider accounts"
        description="Accounts expose safe health and sync state. Credentials are accepted only by dedicated admin mutations and are never returned."
        actions={
          <Show when={isAdmin()}>
            <Button variant="primary" onClick={() => setEditor({ mode: "create" })}>
              Add provider account
            </Button>
          </Show>
        }
      />

      <Show when={error()}>
        {(value) => (
          <Alert variant="danger" title="Provider request failed" role="alert">
            {value().message}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">Request {value().requestId}</span>
            </Show>
          </Alert>
        )}
      </Show>
      <Show when={notice()}>{(value) => <Alert variant="success">{value()}</Alert>}</Show>
      <Show when={!isAdmin()}>
        <Alert title="Read-only account access">
          Your role can inspect provider health and zone coverage. Credential management is
          restricted to administrators by the API.
        </Alert>
      </Show>

      <Show
        when={!loading()}
        fallback={
          <Panel>
            <p class="text-sm text-muted-foreground" aria-live="polite">
              Loading provider accounts…
            </p>
          </Panel>
        }
      >
        <div class="grid gap-4 xl:grid-cols-2">
          <For
            each={accounts()}
            fallback={
              <Panel>
                <p class="text-sm text-muted-foreground">No provider accounts configured.</p>
              </Panel>
            }
          >
            {(account) => (
              <AccountCard
                account={account}
                provider={providers().find((item) => item.type === account.provider_type)}
                admin={isAdmin()}
                busy={busy()}
                edit={() => setEditor({ mode: "edit", account })}
                replaceCredentials={() => setEditor({ mode: "credentials", account })}
                validate={() =>
                  void run(async () => {
                    await validateProviderAccount(account.id);
                  }, `${account.name} validated.`)
                }
                sync={() =>
                  void run(async () => {
                    const result = await syncProviderZones(account.id);
                    setNotice(`${account.name} synchronized ${result.zone_count} zones.`);
                  }, `${account.name} zones synchronized.`)
                }
                toggle={() =>
                  void run(
                    async () => {
                      await updateProviderAccount(account.id, { enabled: !account.enabled });
                    },
                    `${account.name} ${account.enabled ? "disabled" : "enabled"}.`,
                  )
                }
                remove={() => {
                  if (!window.confirm(`Delete provider account “${account.name}”?`)) return;
                  void run(async () => {
                    await deleteProviderAccount(account.id);
                  }, `${account.name} deleted.`);
                }}
              />
            )}
          </For>
        </div>
      </Show>

      <dialog
        ref={(element) => setDialog(element)}
        class="m-auto max-h-[92dvh] w-[min(48rem,calc(100vw-2rem))] overflow-y-auto rounded-lg border border-border bg-surface p-0 text-foreground shadow-2xl backdrop:bg-foreground/35"
        aria-labelledby="provider-account-editor-title"
        onClose={() => setEditor(null)}
      >
        <Show when={editor()}>
          {(state) => (
            <ProviderAccountEditor
              state={state()}
              providers={providers()}
              busy={busy()}
              close={() => setEditor(null)}
              saved={handleEditorSaved}
              failed={(caught) => setError(errorState(caught))}
              setBusy={setBusy}
            />
          )}
        </Show>
      </dialog>
    </div>
  );
}

function AccountCard(props: {
  account: ProviderAccount;
  provider?: ProviderTypeDefinition | undefined;
  admin: boolean;
  busy: boolean;
  edit: () => void;
  replaceCredentials: () => void;
  validate: () => void;
  sync: () => void;
  toggle: () => void;
  remove: () => void;
}) {
  const validationTone = () => {
    if (props.account.validation_status === "valid") return "success" as const;
    if (
      props.account.validation_status === "invalid" ||
      props.account.validation_status === "error"
    )
      return "danger" as const;
    return "warning" as const;
  };
  return (
    <Panel class="h-full">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">
            {props.provider?.display_name ?? props.account.provider_type}
          </p>
          <h2 class="mt-1 text-lg font-semibold text-foreground">{props.account.name}</h2>
          <Show when={props.account.description}>
            <p class="mt-1 text-sm text-muted-foreground">{props.account.description}</p>
          </Show>
        </div>
        <div class="flex flex-wrap gap-2">
          <Badge tone={props.account.enabled ? "success" : "neutral"}>
            {props.account.enabled ? "Enabled" : "Disabled"}
          </Badge>
          <Badge tone={validationTone()}>{props.account.validation_status}</Badge>
        </div>
      </div>
      <dl class="mt-5 grid gap-3 text-sm sm:grid-cols-2">
        <Metric label="Zones" value={String(props.account.zone_count)} />
        <Metric
          label="Credentials"
          value={
            props.account.credential_configured
              ? `Configured · revision ${props.account.credential_revision}`
              : "Not configured"
          }
        />
        <Metric label="Last validation" value={formatDate(props.account.last_validated_at)} />
        <Metric label="Last zone sync" value={formatDate(props.account.last_zone_sync_at)} />
      </dl>
      <Show when={props.account.last_validation_error_code}>
        <Alert class="mt-4" variant="warning">
          Validation state: {props.account.last_validation_error_code}
        </Alert>
      </Show>
      <Show when={props.admin}>
        <div class="mt-5 flex flex-wrap gap-2 border-t border-border pt-4">
          <Button size="sm" disabled={props.busy} onClick={props.edit}>
            Edit
          </Button>
          <Button size="sm" disabled={props.busy} onClick={props.replaceCredentials}>
            Replace credentials
          </Button>
          <Button
            size="sm"
            disabled={props.busy || !props.account.credential_configured}
            onClick={props.validate}
          >
            Validate
          </Button>
          <Button size="sm" disabled={props.busy || !props.account.enabled} onClick={props.sync}>
            Sync zones
          </Button>
          <Button size="sm" disabled={props.busy} onClick={props.toggle}>
            {props.account.enabled ? "Disable" : "Enable"}
          </Button>
          <Button size="sm" variant="danger" disabled={props.busy} onClick={props.remove}>
            Delete
          </Button>
        </div>
      </Show>
    </Panel>
  );
}

function ProviderAccountEditor(props: {
  state: EditorState;
  providers: ProviderTypeDefinition[];
  busy: boolean;
  close: () => void;
  saved: (message: string) => Promise<void>;
  failed: (error: unknown) => void;
  setBusy: (busy: boolean) => void;
}) {
  const initial = untrack(() => {
    const providerType = props.state.account?.provider_type ?? props.providers[0]?.type ?? "";
    const definition = props.providers.find((provider) => provider.type === providerType);
    return {
      providerType,
      name: props.state.account?.name ?? "",
      description: props.state.account?.description ?? "",
      enabled: props.state.account?.enabled ?? true,
      options: allowlistedValues(
        (props.state.account?.options as FieldValues | undefined) ?? {},
        definition?.account_options.map((field) => field.key) ?? [],
      ),
    };
  });
  const [providerType, setProviderType] = createSignal(initial.providerType);
  const [name, setName] = createSignal(initial.name);
  const [description, setDescription] = createSignal(initial.description);
  const [enabled, setEnabled] = createSignal(initial.enabled);
  const [options, setOptions] = createSignal<FieldValues>(initial.options);
  const [credentials, setCredentials] = createSignal<FieldValues>({});
  const selectedProvider = createMemo(() =>
    props.providers.find((provider) => provider.type === providerType()),
  );
  const title = () => {
    if (props.state.mode === "credentials")
      return `Replace credentials · ${props.state.account?.name}`;
    if (props.state.mode === "edit") return `Edit ${props.state.account?.name}`;
    return "Add provider account";
  };
  onCleanup(() => setCredentials({}));
  const closeEditor = () => {
    setCredentials({});
    props.close();
  };

  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    props.setBusy(true);
    const complete = async () => {
      try {
        if (props.state.mode === "credentials" && props.state.account !== undefined) {
          await replaceProviderCredentials(props.state.account.id, compactValues(credentials()));
          setCredentials({});
          await props.saved(`${props.state.account.name} credentials replaced.`);
          return;
        }
        if (props.state.mode === "edit" && props.state.account !== undefined) {
          await updateProviderAccount(props.state.account.id, {
            name: name(),
            description: description(),
            enabled: enabled(),
            options: compactValues(options()),
          });
          await props.saved(`${name()} updated.`);
          return;
        }
        await createProviderAccount({
          provider_type: providerType(),
          name: name(),
          description: description(),
          enabled: enabled(),
          options: compactValues(options()),
          credentials: compactValues(credentials()),
        });
        setCredentials({});
        await props.saved(`${name()} created.`);
      } catch (caught) {
        props.failed(caught);
      } finally {
        props.setBusy(false);
      }
    };
    void complete();
  };

  return (
    <form onSubmit={submit}>
      <header class="flex items-start justify-between gap-4 border-b border-border p-5 sm:p-6">
        <div>
          <p class="text-xs font-semibold text-primary">Provider configuration</p>
          <h2 id="provider-account-editor-title" class="mt-1 text-xl font-semibold">
            {title()}
          </h2>
        </div>
        <Button
          size="sm"
          variant="ghost"
          aria-label="Close provider editor"
          disabled={props.busy}
          onClick={closeEditor}
        >
          Close
        </Button>
      </header>
      <div class="space-y-5 p-5 sm:p-6">
        <Show when={props.state.mode === "create"}>
          <Field label="Provider" for="provider-type">
            <select
              id="provider-type"
              class="text-input"
              required
              value={providerType()}
              onChange={(event) => {
                setProviderType(event.currentTarget.value);
                setOptions({});
                setCredentials({});
              }}
            >
              <For each={props.providers}>
                {(provider) => <option value={provider.type}>{provider.display_name}</option>}
              </For>
            </select>
          </Field>
        </Show>

        <Show when={props.state.mode !== "credentials"}>
          <div class="grid gap-4 sm:grid-cols-2">
            <Field label="Account name" for="provider-account-name">
              <input
                id="provider-account-name"
                class="text-input"
                required
                maxlength={128}
                value={name()}
                onInput={(event) => setName(event.currentTarget.value)}
              />
            </Field>
            <Field label="Enabled" for="provider-account-enabled">
              <label class="flex min-h-10 items-center gap-3 rounded-md border border-input bg-surface px-3">
                <input
                  id="provider-account-enabled"
                  type="checkbox"
                  checked={enabled()}
                  onChange={(event) => setEnabled(event.currentTarget.checked)}
                />
                <span>{enabled() ? "Provider calls enabled" : "Account disabled"}</span>
              </label>
            </Field>
          </div>
          <Field label="Description" for="provider-account-description">
            <textarea
              id="provider-account-description"
              class="text-input min-h-24"
              maxlength={2048}
              value={description()}
              onInput={(event) => setDescription(event.currentTarget.value)}
            />
          </Field>
          <Show when={(selectedProvider()?.account_options.length ?? 0) > 0}>
            <div>
              <h3 class="mb-3 text-sm font-semibold">Account options</h3>
              <DescriptorFields
                fields={selectedProvider()?.account_options ?? []}
                values={options()}
                idPrefix="provider-option"
                onChange={(key, value) => setOptions((current) => ({ ...current, [key]: value }))}
              />
            </div>
          </Show>
        </Show>

        <Show when={props.state.mode === "create" || props.state.mode === "credentials"}>
          <div>
            <h3 class="mb-1 text-sm font-semibold">Credentials</h3>
            <p class="mb-3 text-xs text-muted-foreground">
              Values are sent once to the server. Saved secrets are never refilled into this form.
            </p>
            <DescriptorFields
              fields={selectedProvider()?.credential_fields ?? []}
              values={credentials()}
              idPrefix="provider-credential"
              onChange={(key, value) => setCredentials((current) => ({ ...current, [key]: value }))}
            />
          </div>
        </Show>
      </div>
      <footer class="flex flex-wrap justify-end gap-2 border-t border-border p-5 sm:p-6">
        <Button disabled={props.busy} onClick={closeEditor}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={props.busy}>
          {props.state.mode === "credentials" ? "Replace credentials" : "Save account"}
        </Button>
      </footer>
    </form>
  );
}

function Metric(props: { label: string; value: string }) {
  return (
    <div>
      <dt class="text-muted-foreground">{props.label}</dt>
      <dd class="mt-1 font-medium text-foreground">{props.value}</dd>
    </div>
  );
}

function allowlistedValues(values: FieldValues, allowedKeys: string[]): FieldValues {
  const allowed = new Set(allowedKeys);
  const result: FieldValues = {};
  for (const [key, value] of Object.entries(values)) {
    if (allowed.has(key)) result[key] = value;
  }
  return result;
}

function compactValues(values: FieldValues): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== "" && (!Array.isArray(value) || value.length > 0)) {
      result[key] = value;
    }
  }
  return result;
}

function formatDate(value?: string): string {
  if (value === undefined) return "Never";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorState(error: unknown): { message: string; requestId?: string } {
  if (error instanceof ApiError) {
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  }
  return { message: error instanceof Error ? error.message : "Provider request failed." };
}
