import { For, Show, createSignal, onCleanup, onMount } from "solid-js";
import { useI18n } from "../app/i18n";

import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError, redactClientValue } from "../lib/api";
import { getAuditEvent, listAuditEvents, type AuditEvent } from "../lib/dns";

export default function AuditPage() {
  const { t } = useI18n();
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
      setError(errorState(caught, t));
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
      .catch((caught) => setError(errorState(caught, t)));
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow={t("audit.eyebrow")}
        title={t("audit.title")}
        description={t("audit.description", { total: total() })}
        actions={<Button onClick={() => void load()}>{t("audit.reload")}</Button>}
      />

      <Show when={error()}>
        {(value) => (
          <Alert variant="danger" role="alert">
            {value().message}
            <Show when={value().requestId}>
              <span class="mt-2 block font-mono text-xs">
                {t("audit.request", { id: value().requestId ?? "" })}
              </span>
            </Show>
          </Alert>
        )}
      </Show>

      <Panel compact>
        <form class="grid gap-3 md:grid-cols-2 xl:grid-cols-6" onSubmit={applyFilters}>
          <Field label={t("audit.actor")} for="audit-actor">
            <input
              id="audit-actor"
              class="text-input"
              value={actor()}
              onInput={(event) => setActor(event.currentTarget.value)}
            />
          </Field>
          <Field label={t("audit.action")} for="audit-action">
            <input
              id="audit-action"
              class="text-input"
              placeholder="recordset.update"
              value={action()}
              onInput={(event) => setAction(event.currentTarget.value)}
            />
          </Field>
          <Field label={t("audit.result")} for="audit-result">
            <select
              id="audit-result"
              class="text-input"
              value={result()}
              onChange={(event) => setResult(event.currentTarget.value)}
            >
              <option value="">{t("audit.allResults")}</option>
              <option value="succeeded">{t("audit.succeeded")}</option>
              <option value="failed">{t("audit.failed")}</option>
            </select>
          </Field>
          <Field label={t("audit.from")} for="audit-from">
            <input
              id="audit-from"
              class="text-input"
              type="datetime-local"
              value={from()}
              onInput={(event) => setFrom(event.currentTarget.value)}
            />
          </Field>
          <Field label={t("audit.to")} for="audit-to">
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
              {t("audit.apply")}
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
              {t("audit.reset")}
            </Button>
          </div>
        </form>
      </Panel>

      <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <Panel compact>
          <Show
            when={!loading()}
            fallback={<p class="p-3 text-sm text-muted-foreground">{t("audit.loading")}</p>}
          >
            <div class="overflow-x-auto">
              <table class="w-full min-w-[52rem] border-collapse text-left text-sm">
                <thead>
                  <tr class="border-b border-border text-xs uppercase tracking-wide text-muted-foreground">
                    <th class="px-3 py-3 font-semibold">{t("audit.column.time")}</th>
                    <th class="px-3 py-3 font-semibold">{t("audit.column.actor")}</th>
                    <th class="px-3 py-3 font-semibold">{t("audit.column.action")}</th>
                    <th class="px-3 py-3 font-semibold">{t("audit.column.resource")}</th>
                    <th class="px-3 py-3 font-semibold">{t("audit.column.result")}</th>
                    <th class="px-3 py-3 font-semibold">{t("audit.column.request")}</th>
                  </tr>
                </thead>
                <tbody>
                  <For
                    each={events()}
                    fallback={
                      <tr>
                        <td class="px-3 py-8 text-center text-muted-foreground" colSpan={6}>
                          {t("audit.empty")}
                        </td>
                      </tr>
                    }
                  >
                    {(event) => (
                      <tr class="border-b border-border hover:bg-muted/50">
                        <td class="whitespace-nowrap px-3 py-4 text-xs">
                          {formatDate(event.occurred_at)}
                        </td>
                        <td class="px-3 py-4">{event.actor_username || t("audit.system")}</td>
                        <td class="px-3 py-4 font-medium">
                          <button
                            type="button"
                            class="text-left text-primary hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
                            aria-pressed={selected()?.id === event.id}
                            onClick={() => openDetail(event)}
                          >
                            {event.action}
                          </button>
                        </td>
                        <td class="px-3 py-4">
                          <p>{event.resource_type}</p>
                          <p class="max-w-44 truncate text-xs text-muted-foreground">
                            {event.resource_id}
                          </p>
                        </td>
                        <td class="px-3 py-4">
                          <Badge tone={event.result === "succeeded" ? "success" : "danger"}>
                            {auditResultLabel(event.result, t)}
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
              {t("audit.nextPage")}
            </Button>
          </div>
        </Panel>

        <Panel
          title={t("audit.detailTitle")}
          description={t("audit.detailDescription")}
          class="[&>div:last-child]:focus-visible:outline-2"
        >
          <Show
            when={selected()}
            fallback={<p class="text-sm text-muted-foreground">{t("audit.noSelection")}</p>}
          >
            {(event) => (
              <div class="space-y-4 text-sm">
                <Detail label={t("audit.action")} value={event().action} />
                <Detail label={t("audit.result")} value={auditResultLabel(event().result, t)} />
                <Detail
                  label={t("audit.actor")}
                  value={event().actor_username || t("audit.system")}
                />
                <Detail label={t("audit.requestId")} value={event().request_id} mono />
                <Detail
                  label={t("audit.client")}
                  value={
                    [event().ip, event().user_agent].filter(Boolean).join(" · ") ||
                    t("audit.unavailable")
                  }
                />
                <SafeJSON title={t("audit.before")} value={event().before} />
                <SafeJSON title={t("audit.after")} value={event().after} />
                <SafeJSON title={t("audit.metadata")} value={event().metadata} />
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
          {JSON.stringify(redactClientValue(props.value), null, 2)}
        </pre>
      </div>
    </Show>
  );
}

function auditResultLabel(
  result: string,
  t: (key: string, values?: Record<string, string | number>) => string,
): string {
  const key = `audit.status.${result}`;
  const translated = t(key);
  return translated === key ? result : translated;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "short", timeStyle: "medium" }).format(
    new Date(value),
  );
}

function errorState(
  error: unknown,
  t: (key: string, values?: Record<string, string | number>) => string,
): { message: string; requestId?: string } {
  if (error instanceof ApiError)
    return { message: error.message, ...(error.requestId ? { requestId: error.requestId } : {}) };
  return { message: error instanceof Error ? error.message : t("audit.requestFailedMessage") };
}
