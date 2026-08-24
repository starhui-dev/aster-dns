import {
  Match,
  Switch,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
  type ParentProps,
} from "solid-js";

import { ApiError, apiRequest } from "../lib/api";

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
          setConnection({ kind: "error", message: error.message, requestId: error.requestId });
          return;
        }
        setConnection({
          kind: "error",
          message: "The API could not be reached.",
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
      <section class="overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <div class="border-b border-slate-200 bg-gradient-to-r from-cyan-50 to-white px-6 py-8 dark:border-slate-800 dark:from-cyan-950/50 dark:to-slate-900 md:px-8">
          <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">
            Engineering foundation
          </p>
          <h2 class="mt-2 max-w-3xl text-3xl font-semibold tracking-tight md:text-4xl">
            A runnable base without invented DNS data.
          </h2>
          <p class="mt-3 max-w-2xl text-sm leading-6 text-slate-600 md:text-base dark:text-slate-300">
            The server, PostgreSQL migrations, API contract, frontend shell, and delivery workflow
            are wired. Provider integrations remain intentionally absent.
          </p>
        </div>

        <div class="grid gap-4 p-6 md:grid-cols-3 md:p-8">
          <StatusCard title="API connection">
            <Switch>
              <Match when={connection().kind === "loading"}>
                <p class="text-sm text-slate-500 dark:text-slate-400" aria-live="polite">
                  Checking the same-origin API…
                </p>
              </Match>
              <Match when={connected()}>
                <p class="font-semibold text-emerald-700 dark:text-emerald-300">API connected</p>
                <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                  {connected()?.overview.api_version} · {connected()?.overview.version}
                </p>
              </Match>
              <Match when={failed()}>
                <p class="font-semibold text-rose-700 dark:text-rose-300">API unavailable</p>
                <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">{failed()?.message}</p>
                {failed()?.requestId && (
                  <p class="mt-2 font-mono text-[11px] text-slate-500">
                    Request {failed()?.requestId}
                  </p>
                )}
              </Match>
            </Switch>
          </StatusCard>

          <StatusCard title="Database contract">
            <p class="font-semibold">Seven initial data areas</p>
            <p class="mt-1 text-xs leading-5 text-slate-500 dark:text-slate-400">
              Users, sessions, passkeys, TOTP, provider accounts, zone index, and append-only audit
              events.
            </p>
          </StatusCard>

          <StatusCard title="Provider integration">
            <p class="font-semibold text-amber-700 dark:text-amber-300">Not implemented</p>
            <p class="mt-1 text-xs leading-5 text-slate-500 dark:text-slate-400">
              No mock zones, records, credentials, or false provider success responses are exposed.
            </p>
          </StatusCard>
        </div>
      </section>

      <section class="rounded-2xl border border-amber-200 bg-amber-50 p-5 dark:border-amber-900/70 dark:bg-amber-950/30">
        <h2 class="font-semibold text-amber-950 dark:text-amber-100">
          Security initialization pending
        </h2>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-amber-900 dark:text-amber-200">
          Authentication, first-admin enrollment, RBAC, CSRF, credential encryption, and provider
          adapters are not implemented in Phase 1. Do not expose this build to untrusted networks.
        </p>
      </section>
    </div>
  );
}

function StatusCard(props: ParentProps<{ title: string }>) {
  return (
    <article class="rounded-2xl border border-slate-200 bg-slate-50 p-5 dark:border-slate-800 dark:bg-slate-950/60">
      <h3 class="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase dark:text-slate-400">
        {props.title}
      </h3>
      <div class="mt-3">{props.children}</div>
    </article>
  );
}
