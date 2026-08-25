import { A, useParams } from "@solidjs/router";
import {
  For,
  Match,
  Show,
  Switch,
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
  untrack,
} from "solid-js";

import { useAuth } from "../app/AuthContext";
import {
  ExtensionFields,
  ExtensionSummary,
  extensionContainerFromValues,
  extensionValuesFromContainer,
  type FieldValues,
} from "../components/ProviderFields";
import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
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
  const auth = useAuth();
  const [zone, setZone] = createSignal<Zone>();
  const [providers, setProviders] = createSignal<ProviderTypeDefinition[]>([]);
  const [records, setRecords] = createSignal<RecordSet[]>([]);
  const [query, setQuery] = createSignal("");
  const [typeFilter, setTypeFilter] = createSignal("");
  const [fetchedAt, setFetchedAt] = createSignal<string>();
  const [stale, setStale] = createSignal(false);
  const [warning, setWarning] = createSignal<string>();
  const [loading, setLoading] = createSignal(true);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<{ message: string; requestId?: string } | null>(null);
  const [notice, setNotice] = createSignal<string>();
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set());
  const [selected, setSelected] = createSignal<Set<string>>(new Set());
  const [editor, setEditor] = createSignal<EditorState>();
  const [editorDialog, setEditorDialog] = createSignal<HTMLDialogElement>();
  const [conflict, setConflict] = createSignal<ConflictState>();
  const [batchMode, setBatchMode] = createSignal<"delete" | "ttl_update">();
  const [batchDialog, setBatchDialog] = createSignal<HTMLDialogElement>();
  const [batchTTL, setBatchTTL] = createSignal(300);
  const [batchConfirmation, setBatchConfirmation] = createSignal("");
  const [batchResult, setBatchResult] = createSignal<BatchResult>();

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
    setLoading(true);
    try {
      const [zoneResult, catalog, recordResult] = await Promise.all([
        getZone(params.zoneId, signal),
        listProviderTypes(signal),
        listRecordSets(
          params.zoneId,
          { q: query(), type: typeFilter(), limit: 200, refresh },
          signal,
        ),
      ]);
      setZone(zoneResult.zone);
      setProviders(catalog.provider_types);
      setRecords(recordResult.recordsets);
      setFetchedAt(recordResult.fetched_at);
      setStale(recordResult.stale);
      setWarning(recordResult.warning?.message);
      setError(null);
    } catch (caught) {
      setError(errorState(caught));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(false, controller.signal);
    onCleanup(() => controller.abort());
  });

  createEffect(() => {
    const dialog = editorDialog();
    if (dialog === undefined) return;
    if (editor() !== undefined && !dialog.open) dialog.showModal();
    if (editor() === undefined && dialog.open) dialog.close();
  });
  createEffect(() => {
    const dialog = batchDialog();
    if (dialog === undefined) return;
    if (batchMode() !== undefined && !dialog.open) dialog.showModal();
    if (batchMode() === undefined && dialog.open) dialog.close();
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
    setError(errorState(caught));
  };

  const executeDelete = async (record: RecordSet) => {
    setBusy(true);
    setError(null);
    try {
      await deleteRecordSet(params.zoneId, record.id, record.fingerprint);
      setNotice(`${record.name} ${record.type} deleted.`);
      await load(true);
    } catch (caught) {
      handleMutationError(caught, "delete", record.id);
    } finally {
      setBusy(false);
    }
  };

  const remove = (record: RecordSet) => {
    const summary = record.entries.map(entryLabel).join("; ");
    if (!window.confirm(`Delete ${record.name} ${record.type} (${summary})?`)) return;
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
      setError(errorState(caught));
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
        setNotice("Record deleted using the refreshed provider fingerprint.");
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
      setNotice("Changes reapplied against the current provider state.");
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
        setError(errorState(caught));
      }
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
          zone() ? `${zone()?.provider_account_name} · ${zone()?.provider_type}` : "DNS records"
        }
        title={zone()?.name ?? "Zone records"}
        description={
          fetchedAt()
            ? `Provider state fetched ${formatDate(fetchedAt() as string)}${stale() ? " · stale cache" : ""}`
            : "Loading Provider state…"
        }
        actions={
          <>
            <A class="text-sm font-semibold text-primary hover:underline" href="/zones">
              All zones
            </A>
            <Button disabled={loading()} onClick={() => void load(true)}>
              Force refresh
            </Button>
            <Show when={canMutate()}>
              <Button variant="primary" onClick={() => setEditor({ mode: "create" })}>
                Add record
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
              <span class="mt-2 block font-mono text-xs">Request {value().requestId}</span>
            </Show>
          </Alert>
        )}
      </Show>
      <Show when={notice()}>{(value) => <Alert variant="success">{value()}</Alert>}</Show>
      <Show when={warning()}>
        {(value) => <Alert variant="warning">{value()} Cached data is marked stale.</Alert>}
      </Show>
      <Show when={stale() && warning() === undefined}>
        <Alert variant="warning">
          This record snapshot is stale. Force refresh before editing.
        </Alert>
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
          <Panel
            title="Provider conflict"
            description="The record changed after this page loaded. No overwrite occurred."
          >
            <div class="grid gap-4 lg:grid-cols-2">
              <RecordSnapshot title="Current Provider value" value={state().current} />
              <RecordSnapshot
                title={state().kind === "delete" ? "Pending operation" : "Pending changes"}
                value={state().pending ?? { operation: "delete" }}
              />
            </div>
            <div class="mt-4 flex flex-wrap gap-2">
              <Button
                onClick={() => {
                  setConflict(undefined);
                  void load(true);
                }}
              >
                Reload
              </Button>
              <Button variant="primary" disabled={busy()} onClick={reapplyConflict}>
                Reapply against current
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
          <Field class="min-w-0 flex-1" label="Filter name or value" for="record-search">
            <input
              id="record-search"
              class="text-input"
              type="search"
              value={query()}
              onInput={(event) => setQuery(event.currentTarget.value)}
            />
          </Field>
          <Field label="Type" for="record-type-filter">
            <select
              id="record-type-filter"
              class="text-input min-w-36"
              value={typeFilter()}
              onChange={(event) => setTypeFilter(event.currentTarget.value)}
            >
              <option value="">All types</option>
              <For each={providerDefinition()?.capabilities.supported_record_types ?? []}>
                {(type) => <option value={type}>{type}</option>}
              </For>
            </select>
          </Field>
          <Button type="submit" variant="primary" disabled={loading()}>
            Apply
          </Button>
        </form>
      </Panel>

      <Show when={canMutate() && selectedRecords().length > 0}>
        <Panel compact>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="text-sm font-medium">{selectedRecords().length} record sets selected</p>
            <div class="flex gap-2">
              <Button onClick={() => setBatchMode("ttl_update")}>Update TTL</Button>
              <Button variant="danger" onClick={() => setBatchMode("delete")}>
                Batch delete
              </Button>
            </div>
          </div>
        </Panel>
      </Show>

      <Panel compact>
        <Show
          when={!loading()}
          fallback={<p class="p-3 text-sm text-muted-foreground">Loading record sets…</p>}
        >
          <div class="overflow-x-auto">
            <table class="w-full min-w-[68rem] border-collapse text-left text-sm">
              <thead>
                <tr class="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                  <Show when={canMutate()}>
                    <th class="w-10 px-3 py-3">
                      <span class="sr-only">Select</span>
                    </th>
                  </Show>
                  <th class="px-3 py-3 font-semibold">Name</th>
                  <th class="px-3 py-3 font-semibold">Type</th>
                  <th class="px-3 py-3 font-semibold">Value / entries</th>
                  <th class="px-3 py-3 font-semibold">TTL</th>
                  <th class="px-3 py-3 font-semibold">Provider metadata</th>
                  <th class="px-3 py-3 text-right font-semibold">Actions</th>
                </tr>
              </thead>
              <tbody>
                <For
                  each={records()}
                  fallback={
                    <tr>
                      <td class="px-3 py-8 text-center text-muted-foreground" colSpan={7}>
                        No record sets match.
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
                              aria-label={`Select ${record.name} ${record.type}`}
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
                              class="mt-1 text-xs font-semibold text-primary hover:underline"
                              type="button"
                              onClick={() =>
                                setExpanded((current) => {
                                  const next = new Set(current);
                                  if (next.has(record.id)) next.delete(record.id);
                                  else next.add(record.id);
                                  return next;
                                })
                              }
                            >
                              {expanded().has(record.id)
                                ? "Collapse entries"
                                : `Expand ${record.entries.length} entries`}
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
                            fallback={<span class="text-xs text-muted-foreground">Read only</span>}
                          >
                            <div class="flex justify-end gap-2">
                              <Button
                                size="sm"
                                disabled={busy() || stale()}
                                onClick={() => setEditor({ mode: "edit", record })}
                              >
                                Edit
                              </Button>
                              <Button
                                size="sm"
                                variant="danger"
                                disabled={busy() || stale()}
                                onClick={() => remove(record)}
                              >
                                Delete
                              </Button>
                            </div>
                          </Show>
                        </td>
                      </tr>
                      <Show when={expanded().has(record.id)}>
                        <tr class="border-b border-border bg-surface-subtle">
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

      <dialog
        ref={(element) => setEditorDialog(element)}
        class="m-auto max-h-[94dvh] w-[min(56rem,calc(100vw-2rem))] overflow-y-auto rounded-lg border border-border bg-surface p-0 text-foreground shadow-2xl backdrop:bg-foreground/35"
        aria-label="Record editor"
        onClose={() => setEditor(undefined)}
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
      </dialog>

      <dialog
        ref={(element) => setBatchDialog(element)}
        class="m-auto w-[min(34rem,calc(100vw-2rem))] rounded-lg border border-border bg-surface p-0 text-foreground shadow-2xl backdrop:bg-foreground/35"
        aria-label="Batch record operation"
        onClose={() => setBatchMode(undefined)}
      >
        <form onSubmit={submitBatch}>
          <header class="border-b border-border p-5">
            <h2 class="text-lg font-semibold">
              {batchMode() === "delete" ? "Batch delete record sets" : "Batch update TTL"}
            </h2>
            <p class="mt-1 text-sm text-muted-foreground">
              {selectedRecords().length} record sets in {zone()?.name}. Results are reported item by
              item.
            </p>
          </header>
          <div class="space-y-4 p-5">
            <Show when={batchMode() === "ttl_update"}>
              <Field label="New TTL (seconds)" for="batch-ttl">
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
              <Alert variant="warning">
                This deletes real Provider records. The operation is not transactional across items.
              </Alert>
              <Show when={selectedRecords().length > 10}>
                <Field label={`Type ${zone()?.name} to confirm`} for="batch-confirmation">
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
            </Show>
          </div>
          <footer class="flex justify-end gap-2 border-t border-border p-5">
            <Button disabled={busy()} onClick={() => setBatchMode(undefined)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={batchMode() === "delete" ? "danger" : "primary"}
              disabled={
                busy() ||
                (batchMode() === "delete" &&
                  selectedRecords().length > 10 &&
                  batchConfirmation() !== zone()?.name)
              }
            >
              Apply to {selectedRecords().length} items
            </Button>
          </footer>
        </form>
      </dialog>
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
    if (
      hasValues &&
      !window.confirm(
        "Changing record type clears incompatible entry and Provider fields. Continue?",
      )
    )
      return;
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
          <p class="text-xs font-semibold text-primary">DNS semantic editor</p>
          <h2 class="mt-1 text-xl font-semibold">
            {props.state.mode === "create" ? "Create record set" : "Edit record set"}
          </h2>
        </div>
        <Button size="sm" variant="ghost" aria-label="Close record editor" onClick={props.close}>
          Close
        </Button>
      </header>
      <div class="space-y-5 p-5 sm:p-6">
        <div class="grid gap-4 sm:grid-cols-3">
          <Field label="Name" for="record-name" hint="Use @ for the zone apex.">
            <input
              id="record-name"
              class="text-input"
              required
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </Field>
          <Field label="Type" for="record-type">
            <select
              id="record-type"
              class="text-input"
              value={recordType()}
              onChange={(event) => changeType(event.currentTarget.value)}
            >
              <For each={props.capabilities.supported_record_types}>
                {(type) => <option value={type}>{type}</option>}
              </For>
            </select>
          </Field>
          <Field label="TTL (seconds)" for="record-ttl">
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
            <h3 class="text-sm font-semibold">Entries</h3>
            <Button
              size="sm"
              onClick={() => {
                setEntries((current) => [...current, {}]);
                setEntryExtensions((current) => [...current, {}]);
              }}
            >
              Add entry
            </Button>
          </div>
          <div class="space-y-4">
            <For each={entries()}>
              {(entry, index) => (
                <div class="rounded-md border border-border bg-surface-subtle p-4">
                  <div class="flex items-start justify-between gap-3">
                    <p class="text-xs font-semibold text-muted-foreground">Entry {index() + 1}</p>
                    <Button
                      size="sm"
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
                      Remove
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
        <Button disabled={props.busy} onClick={props.close}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={props.busy}>
          Save record set
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
  return (
    <Switch
      fallback={
        <div class="mt-3">
          <TextField
            id={`${props.idPrefix}-value`}
            label={props.type === "TXT" ? "Text value" : "Value"}
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
            label="Priority"
            value={props.entry.priority}
            update={(priority) => props.update({ priority })}
          />
          <TextField
            id={`${props.idPrefix}-target`}
            label="Mail server"
            value={props.entry.target}
            update={(target) => props.update({ target })}
          />
        </div>
      </Match>
      <Match when={props.type === "SRV"}>
        <div class="mt-3 grid gap-3 sm:grid-cols-4">
          <NumberField
            id={`${props.idPrefix}-priority`}
            label="Priority"
            value={props.entry.priority}
            update={(priority) => props.update({ priority })}
          />
          <NumberField
            id={`${props.idPrefix}-weight`}
            label="Weight"
            value={props.entry.weight}
            update={(weight) => props.update({ weight })}
          />
          <NumberField
            id={`${props.idPrefix}-port`}
            label="Port"
            value={props.entry.port}
            update={(port) => props.update({ port })}
          />
          <TextField
            id={`${props.idPrefix}-target`}
            label="Target"
            value={props.entry.target}
            update={(target) => props.update({ target })}
          />
        </div>
      </Match>
      <Match when={props.type === "CAA"}>
        <div class="mt-3 grid gap-3 sm:grid-cols-[7rem_10rem_1fr]">
          <NumberField
            id={`${props.idPrefix}-flags`}
            label="Flags"
            value={props.entry.flags}
            maximum={255}
            update={(flags) => props.update({ flags })}
          />
          <TextField
            id={`${props.idPrefix}-tag`}
            label="Tag"
            value={props.entry.tag}
            update={(tag) => props.update({ tag })}
          />
          <TextField
            id={`${props.idPrefix}-value`}
            label="Value"
            value={props.entry.value}
            update={(value) => props.update({ value })}
          />
        </div>
      </Match>
      <Match when={props.type === "CNAME" || props.type === "NS"}>
        <div class="mt-3">
          <TextField
            id={`${props.idPrefix}-target`}
            label="Target"
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
  return (
    <Panel
      title="Batch result"
      description={`${props.result.succeeded} succeeded · ${props.result.failed} failed`}
    >
      <div class="space-y-2">
        <For each={props.result.items}>
          {(item) => (
            <div class="flex flex-col gap-1 rounded-md border border-border p-3 sm:flex-row sm:items-center sm:justify-between">
              <code class="break-all text-xs">{item.id}</code>
              <div class="text-sm">
                <Badge tone={item.status === "succeeded" ? "success" : "danger"}>
                  {item.status}
                </Badge>
                <Show when={item.error}>
                  {(error) => (
                    <p class="mt-1 text-xs text-danger-foreground">
                      {error().message} · Request {error().request_id}
                    </p>
                  )}
                </Show>
              </div>
            </div>
          )}
        </For>
      </div>
      <Button class="mt-4" onClick={props.dismiss}>
        Dismiss
      </Button>
    </Panel>
  );
}

function RecordSnapshot(props: { title: string; value: unknown }) {
  return (
    <div class="rounded-md border border-border bg-surface-subtle p-4">
      <h3 class="text-sm font-semibold">{props.title}</h3>
      <pre class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-words text-xs">
        {JSON.stringify(props.value, null, 2)}
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

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorState(error: unknown): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  return { message: error instanceof Error ? error.message : "Record request failed." };
}
