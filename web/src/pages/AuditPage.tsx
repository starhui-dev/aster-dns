import { For, Show, createSignal, onCleanup, onMount } from "solid-js";

import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import { getAuditEvent, listAuditEvents, type AuditEvent } from "../lib/dns";

export default function AuditPage() {
  const [events, setEvents] = createSignal<AuditEvent[]>([]);
  const [selected, setSelected] = createSignal<AuditEvent>();
  const [actor, setActor] = createSignal("");
  const [action, setAction] = createSignal("");
  const [result, setResult] = createSignal("");
  const [from, setFrom] = createSignal("");
  const [to, setTo] = createSignal("");
  const [cursor, setCursor] = createSignal("");
  const [nextCursor, setNextCursor] = createSignal("");
  const [total, setTotal] = createSignal(0);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<{ message: string; requestId?: string }>();

  const load = async (signal?: AbortSignal, requestedCursor = cursor()) => {
    setLoading(true);
    try {
      const response = await listAuditEvents(
        {
          actor: actor(),
          action: action(),
          result: result(),
          from: from() === "" ? undefined : new Date(from()).toISOString(),
          to: to() === "" ? undefined : new Date(to()).toISOString(),
          cursor: requestedCursor,
          limit: 50,
        },
        signal,
      );
      setEvents(response.audit_events);
      setNextCursor(response.next_cursor ?? "");
      setTotal(response.total);
      setError(undefined);
    } catch (caught) {
      setError(errorState(caught));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(controller.signal, "");
    onCleanup(() => controller.abort());
  });

  const applyFilters = (event: SubmitEvent) => {
    event.preventDefault();
    setCursor("");
    void load(undefined, "");
  };

  const openDetail = (event: AuditEvent) => {
    setError(undefined);
    void getAuditEvent(event.id)
      .then((response) => setSelected(response.audit_event))
      .catch((caught) => setError(errorState(caught)));
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="Append-only history"
        title="Audit events"
        description={`${total()} safe audit events. Provider credentials, session tokens, and TOTP material are excluded.`}
        actions={<Button onClick={() => void load()}>Reload</Button>}
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
        <form class="grid gap-3 md:grid-cols-2 xl:grid-cols-6" onSubmit={applyFilters}>
          <Field label="Actor" for="audit-actor">
            <input
              id="audit-actor"
              class="text-input"
              value={actor()}
              onInput={(event) => setActor(event.currentTarget.value)}
            />
          </Field>
          <Field label="Action" for="audit-action">
            <input
              id="audit-action"
              class="text-input"
              placeholder="recordset.update"
              value={action()}
              onInput={(event) => setAction(event.currentTarget.value)}
            />
          </Field>
          <Field label="Result" for="audit-result">
            <select
              id="audit-result"
              class="text-input"
              value={result()}
              onChange={(event) => setResult(event.currentTarget.value)}
            >
              <option value="">All results</option>
              <option value="succeeded">Succeeded</option>
              <option value="failed">Failed</option>
            </select>
          </Field>
          <Field label="From" for="audit-from">
            <input
              id="audit-from"
              class="text-input"
              type="datetime-local"
              value={from()}
              onInput={(event) => setFrom(event.currentTarget.value)}
            />
          </Field>
          <Field label="To" for="audit-to">
            <input
              id="audit-to"
              class="text-input"
              type="datetime-local"
              value={to()}
              onInput={(event) => setTo(event.currentTarget.value)}
            />
          </Field>
          <div class="flex items-end gap-2">
            <Button type="submit" variant="primary" disabled={loading()}>
              Apply
            </Button>
            <Button
              disabled={loading()}
              onClick={() => {
                setActor("");
                setAction("");
                setResult("");
                setFrom("");
                setTo("");
                setCursor("");
                void load(undefined, "");
              }}
            >
              Reset
            </Button>
          </div>
        </form>
      </Panel>

      <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <Panel compact>
          <Show
            when={!loading()}
            fallback={<p class="p-3 text-sm text-muted-foreground">Loading audit events…</p>}
          >
            <div class="overflow-x-auto">
              <table class="w-full min-w-[52rem] border-collapse text-left text-sm">
                <thead>
                  <tr class="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                    <th class="px-3 py-3 font-semibold">Time</th>
                    <th class="px-3 py-3 font-semibold">Actor</th>
                    <th class="px-3 py-3 font-semibold">Action</th>
                    <th class="px-3 py-3 font-semibold">Resource</th>
                    <th class="px-3 py-3 font-semibold">Result</th>
                    <th class="px-3 py-3 font-semibold">Request ID</th>
                  </tr>
                </thead>
                <tbody>
                  <For
                    each={events()}
                    fallback={
                      <tr>
                        <td class="px-3 py-8 text-center text-muted-foreground" colSpan={6}>
                          No audit events match.
                        </td>
                      </tr>
                    }
                  >
                    {(event) => (
                      <tr
                        class="cursor-pointer border-b border-border hover:bg-muted/50"
                        tabIndex={0}
                        onClick={() => openDetail(event)}
                        onKeyDown={(keyboardEvent) => {
                          if (keyboardEvent.key === "Enter" || keyboardEvent.key === " ")
                            openDetail(event);
                        }}
                      >
                        <td class="whitespace-nowrap px-3 py-4 text-xs">
                          {formatDate(event.occurred_at)}
                        </td>
                        <td class="px-3 py-4">{event.actor_username || "System"}</td>
                        <td class="px-3 py-4 font-medium">{event.action}</td>
                        <td class="px-3 py-4">
                          <p>{event.resource_type}</p>
                          <p class="max-w-44 truncate text-xs text-muted-foreground">
                            {event.resource_id}
                          </p>
                        </td>
                        <td class="px-3 py-4">
                          <Badge tone={event.result === "succeeded" ? "success" : "danger"}>
                            {event.result}
                          </Badge>
                        </td>
                        <td class="px-3 py-4 font-mono text-xs">{event.request_id}</td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </Show>
          <div class="mt-4 flex justify-end">
            <Button
              disabled={loading() || nextCursor() === ""}
              onClick={() => {
                const next = nextCursor();
                setCursor(next);
                void load(undefined, next);
              }}
            >
              Next page
            </Button>
          </div>
        </Panel>

        <Panel
          title="Audit detail"
          description="Select an event to inspect its safe before/after data."
        >
          <Show
            when={selected()}
            fallback={<p class="text-sm text-muted-foreground">No event selected.</p>}
          >
            {(event) => (
              <div class="space-y-4 text-sm">
                <Detail label="Action" value={event().action} />
                <Detail label="Result" value={event().result} />
                <Detail label="Actor" value={event().actor_username || "System"} />
                <Detail label="Request ID" value={event().request_id} mono />
                <Detail
                  label="Client"
                  value={
                    [event().ip, event().user_agent].filter(Boolean).join(" · ") || "Unavailable"
                  }
                />
                <SafeJSON title="Before" value={event().before} />
                <SafeJSON title="After" value={event().after} />
                <SafeJSON title="Metadata" value={event().metadata} />
              </div>
            )}
          </Show>
        </Panel>
      </div>
    </div>
  );
}

function Detail(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <p class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {props.label}
      </p>
      <p class={`mt-1 break-words ${props.mono ? "font-mono text-xs" : ""}`}>{props.value}</p>
    </div>
  );
}
function SafeJSON(props: { title: string; value?: Record<string, unknown> | undefined }) {
  return (
    <Show when={props.value && Object.keys(props.value).length > 0}>
      <div>
        <p class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {props.title}
        </p>
        <pre class="mt-1 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-surface-subtle p-3 text-xs">
          {JSON.stringify(props.value, null, 2)}
        </pre>
      </div>
    </Show>
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "short", timeStyle: "medium" }).format(
    new Date(value),
  );
}

function errorState(error: unknown): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  return { message: error instanceof Error ? error.message : "Audit request failed." };
}
