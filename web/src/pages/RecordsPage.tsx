import { Dialog as KobalteDialog } from "@kobalte/core/dialog";
import {
  ArrowLeft,
  ChevronDown,
  ChevronUp,
  Clock3,
  Filter,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Trash2,
  X,
} from "lucide-solid";
import { A, useParams } from "@solidjs/router";
import {
  For,
  Match,
  Show,
  Switch,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
  untrack,
} from "solid-js";

import { useI18n } from "../app/i18n";
import { ProviderIdentity } from "../components/ProviderIdentity";
import { useAuth } from "../app/AuthContext";
import {
  ExtensionFields,
  ExtensionSummary,
  extensionContainerFromValues,
  extensionValuesFromContainer,
  type FieldValues,
} from "../components/ProviderFields";
import { Button } from "../components/ui/Button";
import { ModalDialog } from "../components/ui/Dialog";
import { SelectField } from "../components/ui/Select";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, apiErrorMessage, redactClientValue } from "../lib/api";
import {
  batchRecordSets,
  createRecordSet,
  deleteRecordSet,
  getZone,
  listProviderTypes,
  listRecordSets,
  updateRecordSet,
  type BatchResult,
  type ProviderCapabilities,
  type ProviderTypeDefinition,
  type RecordEntry,
  type RecordSet,
  type RecordSetInput,
  type Zone,
} from "../lib/dns";

interface EditorState {
  mode: "create" | "edit";
  record?: RecordSet | undefined;
}

interface ConflictState {
  kind: "update" | "delete";
  recordID: string;
  current: RecordSet;
  pending?: RecordSetInput | undefined;
}

export default function RecordsPage() {
  const params = useParams<{ zoneId: string }>();
  const { t } = useI18n();
  const auth = useAuth();
  const [zone, setZone] = createSignal<Zone>();
  const [providers, setProviders] = createSignal<ProviderTypeDefinition[]>([]);
  const [records, setRecords] = createSignal<RecordSet[]>([]);
  const [query, setQuery] = createSignal("");
  const [typeFilter, setTypeFilter] = createSignal("");
  const [fetchedAt, setFetchedAt] = createSignal<string>();
  const [stale, setStale] = createSignal(true);
  const [warning, setWarning] = createSignal<{ message: string; requestId?: string }>();
  const [loading, setLoading] = createSignal(true);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<{ message: string; requestId?: string } | null>(null);
  const [notice, setNotice] = createSignal<string>();
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set());
  const [selected, setSelected] = createSignal<Set<string>>(new Set());
  const [editor, setEditor] = createSignal<EditorState>();
  const [conflict, setConflict] = createSignal<ConflictState>();
  const [batchMode, setBatchMode] = createSignal<"delete" | "ttl_update">();
  const [batchTTL, setBatchTTL] = createSignal(300);
  const [batchConfirmation, setBatchConfirmation] = createSignal("");
  const [batchResult, setBatchResult] = createSignal<BatchResult>();

  let loadGeneration = 0;
  const canMutate = createMemo(() => {
    const state = auth.state();
    return (
      state.kind === "authenticated" &&
      (state.session.user.role === "admin" || state.session.user.role === "operator")
    );
  });
  const providerDefinition = createMemo(() =>
    providers().find((provider) => provider.type === zone()?.provider_type),
  );
  const selectedRecords = createMemo(() => records().filter((record) => selected().has(record.id)));

  const load = async (refresh = false, signal?: AbortSignal) => {
    const generation = ++loadGeneration;
    setStale(true);
    setLoading(true);
    try {
      const [zoneResult, catalog, recordResult] = await Promise.all([
        getZone(params.zoneId, signal),
        listProviderTypes(signal),
        (async () => {
          let result = await listRecordSets(
            params.zoneId,
            { q: query(), type: typeFilter(), limit: 200, refresh },
            signal,
          );
          const all = [...result.recordsets];
          for (
            let page = 1;
            result.next_cursor !== undefined && result.next_cursor !== "";
            page += 1
          ) {
            if (page >= 1_000) throw new Error("Record pagination exceeded the safety limit.");
            result = await listRecordSets(
              params.zoneId,
              { q: query(), type: typeFilter(), cursor: result.next_cursor, limit: 200, refresh },
              signal,
            );
            all.push(...result.recordsets);
          }
          return { ...result, recordsets: all };
        })(),
      ]);
      if (generation !== loadGeneration) return;
      setZone(zoneResult.zone);
      setProviders(catalog.provider_types);
      setRecords(recordResult.recordsets);
      setFetchedAt(recordResult.fetched_at);
      setStale(recordResult.stale);
      setWarning(
        recordResult.warning
          ? { message: recordResult.warning.message, requestId: recordResult.warning.request_id }
          : undefined,
      );
      setError(null);
    } catch (caught) {
      if (generation !== loadGeneration) return;
      setStale(true);
      setWarning({ message: t("records.refreshFailed") });
      setError(errorState(caught, t));
    } finally {
      if (generation === loadGeneration) setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(false, controller.signal);
    onCleanup(() => controller.abort());
  });

  const submitFilters = (event: SubmitEvent) => {
    event.preventDefault();
    void load();
  };

  const handleMutationError = (
    caught: unknown,
    kind: "update" | "delete",
    recordID: string,
    pending?: RecordSetInput,
  ) => {
    if (caught instanceof ApiError && caught.code === "conflict") {
      const current = caught.details?.current as RecordSet | undefined;
      if (current !== undefined) {
        setConflict({ kind, recordID, current, pending });
        setEditor(undefined);
        return;
      }
    }
    setError(errorState(caught, t));
  };

  const executeDelete = async (record: RecordSet) => {
    setBusy(true);
    setError(null);
    try {
      await deleteRecordSet(params.zoneId, record.id, record.fingerprint);
      setNotice(t("records.deleted", { name: record.name, type: record.type }));
      await load(true);
    } catch (caught) {
      handleMutationError(caught, "delete", record.id);
    } finally {
      setBusy(false);
    }
  };

  const remove = (record: RecordSet) => {
    const summary = record.entries.map(entryLabel).join("; ");
    if (
      !window.confirm(t("records.confirmDelete", { name: record.name, type: record.type, summary }))
    )
      return;
    void executeDelete(record);
  };

  const executeBatch = async (mode: "delete" | "ttl_update") => {
    setBusy(true);
    setError(null);
    try {
      const result = await batchRecordSets(params.zoneId, {
        operation: mode,
        ...(mode === "delete" ? { confirmation: batchConfirmation() } : {}),
        items: selectedRecords().map((record) => ({
          recordset_id: record.id,
          expected_fingerprint: record.fingerprint,
          provider_version: record.provider_version,
          ...(mode === "ttl_update" ? { ttl: batchTTL() } : {}),
        })),
      });
      setBatchResult(result);
      setBatchMode(undefined);
      setSelected(new Set<string>());
      setBatchConfirmation("");
      await load(true);
    } catch (caught) {
      setError(errorState(caught, t));
    } finally {
      setBusy(false);
    }
  };

  const submitBatch = (event: SubmitEvent) => {
    event.preventDefault();
    const mode = batchMode();
    if (mode === undefined || zone() === undefined) return;
    void executeBatch(mode);
  };

  const executeConflictReapply = async (state: ConflictState) => {
    setBusy(true);
    try {
      if (state.kind === "delete") {
        await deleteRecordSet(params.zoneId, state.recordID, state.current.fingerprint);
        setConflict(undefined);
        setNotice(t("records.deletedWithFingerprint"));
        await load(true);
        return;
      }
      if (state.pending === undefined) return;
      await updateRecordSet(params.zoneId, state.recordID, {
        ...state.pending,
        expected_fingerprint: state.current.fingerprint,
        provider_version: state.current.provider_version,
      });
      setConflict(undefined);
      setNotice(t("records.reapplied"));
      await load(true);
    } catch (caught) {
      handleMutationError(caught, state.kind, state.recordID, state.pending);
    } finally {
      setBusy(false);
    }
  };

  const reapplyConflict = () => {
    const state = conflict();
    if (state !== undefined) void executeConflictReapply(state);
  };

  const executeSave = async (state: EditorState, input: RecordSetInput) => {
    setBusy(true);
    try {
      if (state.mode === "create") {
        await createRecordSet(params.zoneId, input);
      } else if (state.record !== undefined) {
        await updateRecordSet(params.zoneId, state.record.id, {
          ...input,
          expected_fingerprint: state.record.fingerprint,
          provider_version: state.record.provider_version,
        });
      }
      setEditor(undefined);
      setNotice(state.mode === "create" ? "Record created." : "Record updated.");
      await load(true);
    } catch (caught) {
      if (state.mode === "edit" && state.record !== undefined) {
        handleMutationError(caught, "update", state.record.id, input);
      } else {
        setError(errorState(caught, t));
      }
      queueMicrotask(() => document.getElementById("record-name")?.focus());
    } finally {
      setBusy(false);
    }
  };

  const saveRecord = (input: RecordSetInput) => {
    const state = editor();
    if (state !== undefined) void executeSave(state, input);
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow={
          zone() ? (
            <ProviderIdentity
              class="inline-flex min-w-0 items-center gap-1.5 text-xs font-semibold text-primary"
              iconClass="h-5 w-5"
              provider={providerDefinition()}
              providerType={zone()?.provider_type ?? ""}
            />
          ) : (
            t("records.eyebrow")
          )
        }
        title={zone()?.name ?? t("records.title")}
        description={
          fetchedAt()
            ? `${t("records.fetched", { date: formatDate(fetchedAt() as string) })}${stale() ? ` · ${t("records.staleCache")}` : ""}`
            : t("records.loadingProvider")
        }
        actions={
          <>
            <A
              class="inline-flex items-center gap-1.5 text-sm font-semibold text-primary hover:underline"
              href="/zones"
            >
              <ArrowLeft size={15} strokeWidth={1.8} aria-hidden="true" />
              {t("records.allZones")}
            </A>
            <Show when={canMutate()}>
              <Button
                icon={RefreshCw}
                disabled={loading() || busy()}
                onClick={() => void load(true)}
              >
                {t("records.forceRefresh")}
              </Button>
            </Show>
            <Show when={canMutate()}>
              <Button
                icon={Plus}
                variant="primary"
                disabled={loading() || busy() || stale()}
                onClick={() => setEditor({ mode: "create" })}
              >
                {t("records.addRecord")}
              </Button>
            </Show>
          </>
        }
      />

      <Show when={error()}>
        {(value) => (
          <Alert variant="danger" role="alert">
            {value().message}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">
                {t("records.request", { id: value().requestId ?? "" })}
              </span>
            </Show>
          </Alert>
        )}
      </Show>
      <Show when={notice()}>{(value) => <Alert variant="success">{value()}</Alert>}</Show>
      <Show when={warning()}>
        {(value) => (
          <Alert variant="warning">
            {value().message} {t("records.cachedStale")}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">
                {t("records.request", { id: value().requestId ?? "" })}
              </span>
            </Show>
          </Alert>
        )}
      </Show>
      <Show when={stale() && warning() === undefined}>
        <Alert variant="warning">{t("records.staleBeforeEditing")}</Alert>
      </Show>

      <Show when={zone()?.metadata.extensions}>
        <Panel compact>
          <ExtensionSummary
            descriptors={providerDefinition()?.capabilities.extension_fields ?? []}
            scope="zone"
            recordType=""
            extensions={zone()?.metadata.extensions}
          />
        </Panel>
      </Show>

      <Show when={conflict()}>
        {(state) => (
          <Panel title={t("records.conflictTitle")} description={t("records.conflictDescription")}>
            <div class="grid gap-4 lg:grid-cols-2">
              <RecordSnapshot title={t("records.currentProviderValue")} value={state().current} />
              <RecordSnapshot
                title={
                  state().kind === "delete"
                    ? t("records.pendingOperation")
                    : t("records.pendingChanges")
                }
                value={state().pending ?? { operation: "delete" }}
              />
            </div>
            <div class="mt-4 flex flex-wrap gap-2">
              <Button
                icon={RefreshCw}
                onClick={() => {
                  setConflict(undefined);
                  void load(true);
                }}
              >
                {t("records.reload")}
              </Button>
              <Button
                variant="primary"
                icon={RotateCcw}
                disabled={busy()}
                onClick={reapplyConflict}
              >
                {t("records.reapply")}
              </Button>
            </div>
          </Panel>
        )}
      </Show>

      <Show when={batchResult()}>
        {(result) => (
          <BatchResultPanel result={result()} dismiss={() => setBatchResult(undefined)} />
        )}
      </Show>

      <Panel compact>
        <form class="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submitFilters}>
          <Field class="min-w-0 flex-1" label={t("records.filter")} for="record-search">
            <input
              id="record-search"
              class="text-input"
              type="search"
              value={query()}
              onInput={(event) => setQuery(event.currentTarget.value)}
            />
          </Field>
          <SelectField
            id="record-type-filter"
            label={t("records.type")}
            value={typeFilter()}
            options={[
              { value: "", label: t("records.allTypes") },
              ...(providerDefinition()?.capabilities.supported_record_types ?? []).map((type) => ({
                value: type,
                label: type,
              })),
            ]}
            class="min-w-36"
            onChange={setTypeFilter}
          />
          <Button type="submit" variant="primary" icon={Filter} disabled={loading()}>
            {t("records.apply")}
          </Button>
        </form>
      </Panel>

      <Show when={canMutate() && selectedRecords().length > 0}>
        <Panel compact>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="text-sm font-medium">
              {t("records.selected", { count: selectedRecords().length })}
            </p>
            <div class="flex gap-2">
              <Button
                icon={Clock3}
                disabled={loading() || busy() || stale() || selectedRecords().length > 100}
                onClick={() => setBatchMode("ttl_update")}
              >
                {t("records.updateTTL")}
              </Button>
              <Button
                icon={Trash2}
                variant="danger"
                disabled={loading() || busy() || stale() || selectedRecords().length > 100}
                onClick={() => setBatchMode("delete")}
              >
                {t("records.batchDelete")}
              </Button>
            </div>
          </div>
          <Show when={selectedRecords().length > 100}>
            <p class="mt-2 text-sm text-danger" role="alert">
              {t("records.batchLimit")}
            </p>
          </Show>
        </Panel>
      </Show>

      <Panel compact>
        <Show
          when={!loading()}
          fallback={<p class="p-3 text-sm text-muted-foreground">{t("records.loadingSets")}</p>}
        >
          <div class="overflow-x-auto">
            <table class="w-full min-w-[68rem] border-collapse text-left text-sm">
              <thead>
                <tr class="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                  <Show when={canMutate()}>
                    <th class="w-10 px-3 py-3">
                      <span class="sr-only">{t("records.select")}</span>
                    </th>
                  </Show>
                  <th class="px-3 py-3 font-semibold">{t("records.column.name")}</th>
                  <th class="px-3 py-3 font-semibold">{t("records.column.type")}</th>
                  <th class="px-3 py-3 font-semibold">{t("records.column.values")}</th>
                  <th class="px-3 py-3 font-semibold">{t("records.column.ttl")}</th>
                  <th class="px-3 py-3 font-semibold">{t("records.column.metadata")}</th>
                  <th class="px-3 py-3 text-right font-semibold">{t("records.column.actions")}</th>
                </tr>
              </thead>
              <tbody>
                <For
                  each={records()}
                  fallback={
                    <tr>
                      <td class="px-3 py-8 text-center text-muted-foreground" colSpan={7}>
                        {t("records.empty")}
                      </td>
                    </tr>
                  }
                >
                  {(record) => (
                    <>
                      <tr class="border-b border-border">
                        <Show when={canMutate()}>
                          <td class="px-3 py-4">
                            <input
                              type="checkbox"
                              aria-label={t("records.selectRecord", {
                                name: record.name,
                                type: record.type,
                              })}
                              checked={selected().has(record.id)}
                              onChange={(event) =>
                                setSelected((current) => {
                                  const next = new Set(current);
                                  if (event.currentTarget.checked) next.add(record.id);
                                  else next.delete(record.id);
                                  return next;
                                })
                              }
                            />
                          </td>
                        </Show>
                        <td class="px-3 py-4 font-medium">{record.name}</td>
                        <td class="px-3 py-4">
                          <Badge tone="primary">{record.type}</Badge>
                        </td>
                        <td class="max-w-md px-3 py-4">
                          <p class="truncate">{record.entries.map(entryLabel).join(", ")}</p>
                          <Show
                            when={
                              record.entries.length > 1 ||
                              entryLabel(record.entries[0] ?? {}).length > 48
                            }
                          >
                            <button
                              class="mt-1 inline-flex items-center gap-1.5 text-xs font-semibold text-primary hover:underline"
                              type="button"
                              aria-expanded={expanded().has(record.id)}
                              aria-controls={`record-entries-${record.id}`}
                              onClick={() =>
                                setExpanded((current) => {
                                  const next = new Set(current);
                                  if (next.has(record.id)) next.delete(record.id);
                                  else next.add(record.id);
                                  return next;
                                })
                              }
                            >
                              <Show
                                when={expanded().has(record.id)}
                                fallback={
                                  <ChevronDown size={14} strokeWidth={1.8} aria-hidden="true" />
                                }
                              >
                                <ChevronUp size={14} strokeWidth={1.8} aria-hidden="true" />
                              </Show>
                              {expanded().has(record.id)
                                ? t("records.collapseEntries")
                                : t("records.expandEntries", { count: record.entries.length })}
                            </button>
                          </Show>
                        </td>
                        <td class="px-3 py-4 font-mono text-xs">{record.ttl}s</td>
                        <td class="px-3 py-4">
                          <ExtensionSummary
                            descriptors={providerDefinition()?.capabilities.extension_fields ?? []}
                            scope="record_set"
                            recordType={record.type}
                            extensions={record.extensions}
                          />
                        </td>
                        <td class="px-3 py-4 text-right">
                          <Show
                            when={canMutate()}
                            fallback={
                              <span class="text-xs text-muted-foreground">
                                {t("records.readOnly")}
                              </span>
                            }
                          >
                            <div class="flex justify-end gap-2">
                              <Button
                                icon={Pencil}
                                size="sm"
                                disabled={loading() || busy() || stale()}
                                onClick={() => setEditor({ mode: "edit", record })}
                              >
                                {t("records.edit")}
                              </Button>
                              <Button
                                icon={Trash2}
                                size="sm"
                                variant="danger"
                                disabled={loading() || busy() || stale()}
                                onClick={() => remove(record)}
                              >
                                {t("records.delete")}
                              </Button>
                            </div>
                          </Show>
                        </td>
                      </tr>
                      <Show when={expanded().has(record.id)}>
                        <tr
                          id={`record-entries-${record.id}`}
                          class="border-b border-border bg-surface-subtle"
                        >
                          <td class="px-3 py-4" colSpan={canMutate() ? 7 : 6}>
                            <ol class="space-y-3">
                              <For each={record.entries}>
                                {(entry, index) => (
                                  <li class="rounded-md border border-border bg-surface p-3">
                                    <p class="font-mono text-xs break-all">
                                      {index() + 1}. {entryLabel(entry)}
                                    </p>
                                    <ExtensionSummary
                                      descriptors={
                                        providerDefinition()?.capabilities.extension_fields ?? []
                                      }
                                      scope="record_entry"
                                      recordType={record.type}
                                      extensions={entry.extensions}
                                    />
                                  </li>
                                )}
                              </For>
                            </ol>
                          </td>
                        </tr>
                      </Show>
                    </>
                  )}
                </For>
              </tbody>
            </table>
          </div>
        </Show>
      </Panel>

      <ModalDialog
        open={editor() !== undefined}
        onOpenChange={(open) => {
          if (!open) setEditor(undefined);
        }}
      >
        <Show when={editor() && providerDefinition()}>
          <RecordEditor
            state={editor() as EditorState}
            capabilities={providerDefinition()?.capabilities as ProviderCapabilities}
            busy={busy()}
            close={() => setEditor(undefined)}
            save={saveRecord}
          />
        </Show>
      </ModalDialog>
      <ModalDialog
        open={batchMode() !== undefined}
        onOpenChange={(open) => {
          if (!open) setBatchMode(undefined);
        }}
        class="!w-[min(34rem,calc(100vw-2rem))]"
      >
        <form onSubmit={submitBatch}>
          <header class="border-b border-border p-5">
            <KobalteDialog.Title class="text-lg font-semibold">
              {batchMode() === "delete"
                ? t("records.batchDeleteTitle")
                : t("records.batchTTLTitle")}
            </KobalteDialog.Title>
            <KobalteDialog.Description class="mt-1 text-sm text-muted-foreground">
              {t("records.batchDescription", {
                count: selectedRecords().length,
                name: zone()?.name ?? "",
              })}
            </KobalteDialog.Description>
          </header>
          <div class="space-y-4 p-5">
            <Show when={batchMode() === "ttl_update"}>
              <Field label={t("records.newTTL")} for="batch-ttl">
                <input
                  id="batch-ttl"
                  class="text-input"
                  type="number"
                  required
                  min={providerDefinition()?.capabilities.min_ttl ?? 1}
                  max={providerDefinition()?.capabilities.max_ttl}
                  value={batchTTL()}
                  onInput={(event) => setBatchTTL(Number(event.currentTarget.value))}
                />
              </Field>
            </Show>
            <Show when={batchMode() === "delete"}>
              <Alert variant="warning">{t("records.batchWarning")}</Alert>
              <Field
                label={t("records.confirmZone", { name: zone()?.name ?? "" })}
                for="batch-confirmation"
              >
                <input
                  id="batch-confirmation"
                  class="text-input"
                  required
                  autocomplete="off"
                  value={batchConfirmation()}
                  onInput={(event) => setBatchConfirmation(event.currentTarget.value)}
                />
              </Field>
            </Show>
          </div>
          <footer class="flex justify-end gap-2 border-t border-border p-5">
            <Button icon={X} disabled={busy()} onClick={() => setBatchMode(undefined)}>
              {t("records.cancel")}
            </Button>
            <Button
              icon={batchMode() === "delete" ? Trash2 : Save}
              type="submit"
              variant={batchMode() === "delete" ? "danger" : "primary"}
              disabled={
                busy() || (batchMode() === "delete" && batchConfirmation() !== zone()?.name)
              }
            >
              {t("records.applyItems", { count: selectedRecords().length })}
            </Button>
          </footer>
        </form>
      </ModalDialog>
    </div>
  );
}

function RecordEditor(props: {
  state: EditorState;
  capabilities: ProviderCapabilities;
  busy: boolean;
  close: () => void;
  save: (input: RecordSetInput) => void;
}) {
  const { t } = useI18n();
  const initial = untrack(() => {
    const record = props.state.record;
    const descriptors = props.capabilities.extension_fields;
    const initialEntries = record?.entries.map((entry) => ({ ...entry })) ?? [{}];
    return {
      type: record?.type ?? props.capabilities.supported_record_types[0] ?? "A",
      name: record?.name ?? "@",
      ttl: record?.ttl ?? props.capabilities.min_ttl ?? 300,
      entries: initialEntries,
      setExtensions: extensionValuesFromContainer(descriptors, "record_set", record?.extensions),
      entryExtensions: initialEntries.map((entry) =>
        extensionValuesFromContainer(descriptors, "record_entry", entry.extensions),
      ),
    };
  });
  const [name, setName] = createSignal(initial.name);
  const [recordType, setRecordType] = createSignal(initial.type);
  const [ttl, setTTL] = createSignal(initial.ttl);
  const [entries, setEntries] = createSignal<RecordEntry[]>(initial.entries);
  const [setExtensions, setSetExtensions] = createSignal<FieldValues>(initial.setExtensions);
  const [entryExtensions, setEntryExtensions] = createSignal<FieldValues[]>(
    initial.entryExtensions,
  );

  const changeType = (nextType: string) => {
    if (nextType === recordType()) return;
    const hasValues = entries().some((entry) => entryLabel(entry) !== "");
    if (hasValues && !window.confirm(t("records.confirmTypeChange"))) return;
    setRecordType(nextType);
    setEntries([{}]);
    setEntryExtensions([{}]);
    setSetExtensions({});
  };

  const updateEntry = (index: number, changes: Partial<RecordEntry>) => {
    setEntries((current) =>
      current.map((entry, entryIndex) => (entryIndex === index ? { ...entry, ...changes } : entry)),
    );
  };

  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    const descriptors = props.capabilities.extension_fields;
    const desiredEntries = entries().map((entry, index) => ({
      ...entry,
      extensions: extensionContainerFromValues(
        descriptors,
        "record_entry",
        recordType(),
        entryExtensions()[index] ?? {},
      ),
    }));
    props.save({
      name: name(),
      type: recordType(),
      ttl: ttl(),
      entries: desiredEntries,
      extensions: extensionContainerFromValues(
        descriptors,
        "record_set",
        recordType(),
        setExtensions(),
      ),
    });
  };

  return (
    <form onSubmit={submit}>
      <header class="flex items-start justify-between gap-4 border-b border-border p-5 sm:p-6">
        <div>
          <p class="text-xs font-semibold text-primary">{t("records.semanticEditor")}</p>
          <KobalteDialog.Title class="mt-1 text-xl font-semibold">
            {props.state.mode === "create" ? t("records.createSet") : t("records.editSet")}
          </KobalteDialog.Title>
        </div>
        <Button
          size="sm"
          variant="ghost"
          icon={X}
          aria-label={t("records.closeEditor")}
          onClick={props.close}
        >
          {t("records.close")}
        </Button>
      </header>
      <div class="space-y-5 p-5 sm:p-6">
        <div class="grid gap-4 sm:grid-cols-3">
          <Field label={t("records.name")} for="record-name" hint={t("records.nameHint")}>
            <input
              id="record-name"
              class="text-input"
              required
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </Field>
          <SelectField
            id="record-type"
            label={t("records.type")}
            value={recordType()}
            options={props.capabilities.supported_record_types.map((type) => ({
              value: type,
              label: type,
            }))}
            onChange={changeType}
          />
          <Field label={t("records.ttl")} for="record-ttl">
            <input
              id="record-ttl"
              class="text-input"
              type="number"
              required
              min={props.capabilities.min_ttl ?? 1}
              max={props.capabilities.max_ttl}
              value={ttl()}
              onInput={(event) => setTTL(Number(event.currentTarget.value))}
            />
          </Field>
        </div>

        <div>
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold">{t("records.entries")}</h3>
            <Button
              size="sm"
              icon={Plus}
              onClick={() => {
                setEntries((current) => [...current, {}]);
                setEntryExtensions((current) => [...current, {}]);
              }}
            >
              {t("records.addEntry")}
            </Button>
          </div>
          <div class="space-y-4">
            <For each={entries()}>
              {(entry, index) => (
                <div class="rounded-md border border-border bg-surface-subtle p-4">
                  <div class="flex items-start justify-between gap-3">
                    <p class="text-xs font-semibold text-muted-foreground">
                      {t("records.entry", { index: index() + 1 })}
                    </p>
                    <Button
                      size="sm"
                      icon={X}
                      variant="ghost"
                      disabled={entries().length === 1}
                      onClick={() => {
                        setEntries((current) =>
                          current.filter((_, itemIndex) => itemIndex !== index()),
                        );
                        setEntryExtensions((current) =>
                          current.filter((_, itemIndex) => itemIndex !== index()),
                        );
                      }}
                    >
                      {t("records.remove")}
                    </Button>
                  </div>
                  <EntryFields
                    type={recordType()}
                    entry={entry}
                    idPrefix={`entry-${index()}`}
                    update={(changes) => updateEntry(index(), changes)}
                  />
                  <ExtensionFields
                    descriptors={props.capabilities.extension_fields}
                    scope="record_entry"
                    recordType={recordType()}
                    values={entryExtensions()[index()] ?? {}}
                    idPrefix={`entry-extension-${index()}`}
                    onChange={(key, value) =>
                      setEntryExtensions((current) =>
                        current.map((item, itemIndex) =>
                          itemIndex === index() ? { ...item, [key]: value } : item,
                        ),
                      )
                    }
                  />
                </div>
              )}
            </For>
          </div>
        </div>

        <ExtensionFields
          descriptors={props.capabilities.extension_fields}
          scope="record_set"
          recordType={recordType()}
          values={setExtensions()}
          idPrefix="record-set-extension"
          onChange={(key, value) => setSetExtensions((current) => ({ ...current, [key]: value }))}
        />
      </div>
      <footer class="flex justify-end gap-2 border-t border-border p-5 sm:p-6">
        <Button icon={X} disabled={props.busy} onClick={props.close}>
          {t("records.cancel")}
        </Button>
        <Button type="submit" variant="primary" icon={Save} disabled={props.busy}>
          {t("records.saveSet")}
        </Button>
      </footer>
    </form>
  );
}

function EntryFields(props: {
  type: string;
  entry: RecordEntry;
  idPrefix: string;
  update: (changes: Partial<RecordEntry>) => void;
}) {
  const { t } = useI18n();
  return (
    <Switch
      fallback={
        <div class="mt-3">
          <TextField
            id={`${props.idPrefix}-value`}
            label={props.type === "TXT" ? t("records.textValue") : t("records.value")}
            value={props.entry.value}
            update={(value) => props.update({ value })}
          />
        </div>
      }
    >
      <Match when={props.type === "MX"}>
        <div class="mt-3 grid gap-3 sm:grid-cols-[9rem_1fr]">
          <NumberField
            id={`${props.idPrefix}-priority`}
            label={t("records.priority")}
            value={props.entry.priority}
            update={(priority) => props.update({ priority })}
          />
          <TextField
            id={`${props.idPrefix}-target`}
            label={t("records.mailServer")}
            value={props.entry.target}
            update={(target) => props.update({ target })}
          />
        </div>
      </Match>
      <Match when={props.type === "SRV"}>
        <div class="mt-3 grid gap-3 sm:grid-cols-4">
          <NumberField
            id={`${props.idPrefix}-priority`}
            label={t("records.priority")}
            value={props.entry.priority}
            update={(priority) => props.update({ priority })}
          />
          <NumberField
            id={`${props.idPrefix}-weight`}
            label={t("records.weight")}
            value={props.entry.weight}
            update={(weight) => props.update({ weight })}
          />
          <NumberField
            id={`${props.idPrefix}-port`}
            label={t("records.port")}
            value={props.entry.port}
            update={(port) => props.update({ port })}
          />
          <TextField
            id={`${props.idPrefix}-target`}
            label={t("records.target")}
            value={props.entry.target}
            update={(target) => props.update({ target })}
          />
        </div>
      </Match>
      <Match when={props.type === "CAA"}>
        <div class="mt-3 grid gap-3 sm:grid-cols-[7rem_10rem_1fr]">
          <NumberField
            id={`${props.idPrefix}-flags`}
            label={t("records.flags")}
            value={props.entry.flags}
            maximum={255}
            update={(flags) => props.update({ flags })}
          />
          <TextField
            id={`${props.idPrefix}-tag`}
            label={t("records.tag")}
            value={props.entry.tag}
            update={(tag) => props.update({ tag })}
          />
          <TextField
            id={`${props.idPrefix}-value`}
            label={t("records.value")}
            value={props.entry.value}
            update={(value) => props.update({ value })}
          />
        </div>
      </Match>
      <Match when={props.type === "CNAME" || props.type === "NS"}>
        <div class="mt-3">
          <TextField
            id={`${props.idPrefix}-target`}
            label={t("records.target")}
            value={props.entry.target}
            update={(target) => props.update({ target })}
          />
        </div>
      </Match>
    </Switch>
  );
}

function TextField(props: {
  id: string;
  label: string;
  value?: string | undefined;
  update: (value: string) => void;
}) {
  return (
    <Field label={props.label} for={props.id}>
      <input
        id={props.id}
        class="text-input"
        required
        value={props.value ?? ""}
        onInput={(event) => props.update(event.currentTarget.value)}
      />
    </Field>
  );
}

function NumberField(props: {
  id: string;
  label: string;
  value?: number | undefined;
  maximum?: number | undefined;
  update: (value: number) => void;
}) {
  return (
    <Field label={props.label} for={props.id}>
      <input
        id={props.id}
        class="text-input"
        type="number"
        min={0}
        max={props.maximum ?? 65535}
        required
        value={props.value ?? ""}
        onInput={(event) => props.update(Number(event.currentTarget.value))}
      />
    </Field>
  );
}

function BatchResultPanel(props: { result: BatchResult; dismiss: () => void }) {
  const { t } = useI18n();
  return (
    <Panel
      title={t("records.batchResult")}
      description={t("records.batchSummary", {
        succeeded: props.result.succeeded,
        failed: props.result.failed,
      })}
    >
      <div class="space-y-2">
        <For each={props.result.items}>
          {(item) => (
            <div class="flex flex-col gap-1 rounded-md border border-border p-3 sm:flex-row sm:items-center sm:justify-between">
              <code class="break-all text-xs">{item.id}</code>
              <div class="text-sm">
                <Badge tone={item.status === "succeeded" ? "success" : "danger"}>
                  {recordStatusLabel(item.status, t)}
                </Badge>
                <Show when={item.error}>
                  {(error) => (
                    <p class="mt-1 text-xs text-danger-foreground">
                      {error().message} · {t("records.request", { id: error().request_id })}
                    </p>
                  )}
                </Show>
              </div>
            </div>
          )}
        </For>
      </div>
      <Button class="mt-4" icon={X} onClick={props.dismiss}>
        {t("records.dismiss")}
      </Button>
    </Panel>
  );
}

function RecordSnapshot(props: { title: string; value: unknown }) {
  return (
    <div class="rounded-md border border-border bg-surface-subtle p-4">
      <h3 class="text-sm font-semibold">{props.title}</h3>
      <pre class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-words text-xs">
        {JSON.stringify(redactClientValue(props.value), null, 2)}
      </pre>
    </div>
  );
}

function entryLabel(entry: RecordEntry): string {
  if (entry.priority !== undefined && entry.weight !== undefined && entry.port !== undefined)
    return `${entry.priority} ${entry.weight} ${entry.port} ${entry.target ?? ""}`.trim();
  if (entry.priority !== undefined)
    return `${entry.priority} ${entry.target ?? entry.value ?? ""}`.trim();
  if (entry.flags !== undefined)
    return `${entry.flags} ${entry.tag ?? ""} ${entry.value ?? ""}`.trim();
  return entry.target ?? entry.value ?? "";
}
function recordStatusLabel(
  status: string,
  t: (key: string, values?: Record<string, string | number>) => string,
): string {
  const key = `records.status.${status}`;
  const translated = t(key);
  return translated === key ? status : translated;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
function errorState(
  error: unknown,
  t: (key: string, values?: Record<string, string | number>) => string,
): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return {
      message: apiErrorMessage(error, t("records.requestFailedMessage")),
      ...(error.requestId ? { requestId: error.requestId } : {}),
    };
  return { message: error instanceof Error ? error.message : t("records.requestFailedMessage") };
}
