import { A, useLocation } from "@solidjs/router";
import { For, type ParentProps } from "solid-js";

import { createThemeController } from "./theme";

const navigation = [
  { href: "/", label: "Overview", end: true },
  { href: "/zones", label: "Zones", end: false },
  { href: "/accounts", label: "Accounts", end: false },
  { href: "/audit", label: "Audit", end: false },
  { href: "/settings", label: "Settings", end: false },
] as const;

export default function AppShell(props: ParentProps) {
  const location = useLocation();
  const theme = createThemeController();
  const pageTitle = () =>
    navigation.find((item) =>
      item.end ? location.pathname === item.href : location.pathname.startsWith(item.href),
    )?.label ?? "Aster DNS";

  return (
    <div class="min-h-screen bg-slate-100 text-slate-950 transition-colors dark:bg-slate-950 dark:text-slate-100">
      <div class="mx-auto flex min-h-screen max-w-[1600px]">
        <aside class="hidden w-64 shrink-0 border-r border-slate-200 bg-white/90 px-5 py-6 backdrop-blur md:flex md:flex-col dark:border-slate-800 dark:bg-slate-950/90">
          <Brand />
          <nav aria-label="Primary navigation" class="mt-10 space-y-1">
            <For each={navigation}>
              {(item) => (
                <A
                  href={item.href}
                  end={item.end}
                  class="block rounded-xl px-3 py-2.5 text-sm font-medium transition-colors"
                  activeClass="bg-cyan-50 text-cyan-800 dark:bg-cyan-950/60 dark:text-cyan-200"
                  inactiveClass="text-slate-600 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-400 dark:hover:bg-slate-900 dark:hover:text-white"
                >
                  {item.label}
                </A>
              )}
            </For>
          </nav>
          <div class="mt-auto rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-200">
            Phase 1 foundation only. Authentication and DNS provider operations are not available.
          </div>
        </aside>

        <div class="min-w-0 flex-1">
          <header class="sticky top-0 z-10 border-b border-slate-200 bg-white/85 px-4 py-3 backdrop-blur md:px-8 dark:border-slate-800 dark:bg-slate-950/85">
            <div class="flex items-center justify-between gap-4">
              <div class="flex min-w-0 items-center gap-3">
                <div class="md:hidden">
                  <Brand compact />
                </div>
                <div class="hidden md:block">
                  <p class="text-xs font-semibold tracking-[0.16em] text-slate-500 uppercase dark:text-slate-400">
                    Console
                  </p>
                  <h1 class="truncate text-lg font-semibold">{pageTitle()}</h1>
                </div>
              </div>
              <button
                type="button"
                class="rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium shadow-sm transition hover:border-cyan-300 hover:text-cyan-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-600 dark:border-slate-700 dark:bg-slate-900 dark:hover:border-cyan-700 dark:hover:text-cyan-300"
                aria-label={`Switch to ${theme.theme() === "light" ? "dark" : "light"} theme`}
                onClick={theme.toggle}
              >
                {theme.theme() === "light" ? "Dark" : "Light"}
              </button>
            </div>
            <nav
              aria-label="Mobile navigation"
              class="mt-3 flex gap-2 overflow-x-auto pb-1 md:hidden"
            >
              <For each={navigation}>
                {(item) => (
                  <A
                    href={item.href}
                    end={item.end}
                    class="shrink-0 rounded-lg px-3 py-1.5 text-sm font-medium"
                    activeClass="bg-cyan-100 text-cyan-900 dark:bg-cyan-950 dark:text-cyan-100"
                    inactiveClass="text-slate-600 dark:text-slate-400"
                  >
                    {item.label}
                  </A>
                )}
              </For>
            </nav>
          </header>

          <main class="px-4 py-6 md:px-8 md:py-8">{props.children}</main>
        </div>
      </div>
    </div>
  );
}

function Brand(props: { compact?: boolean }) {
  return (
    <div class="flex items-center gap-3" aria-label="Aster DNS">
      <div class="grid size-9 place-items-center rounded-xl bg-cyan-600 font-bold text-white shadow-sm shadow-cyan-900/20">
        A
      </div>
      {!props.compact && (
        <div>
          <p class="text-sm font-semibold tracking-wide">Aster DNS</p>
          <p class="text-xs text-slate-500 dark:text-slate-400">Unified DNS control</p>
        </div>
      )}
    </div>
  );
}
