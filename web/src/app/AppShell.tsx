import { A, useLocation } from "@solidjs/router";
import { For, createEffect, createMemo, createSignal, onCleanup, type ParentProps } from "solid-js";

import { AppearanceControls } from "../components/AppearanceControls";
import { Brand } from "../components/Brand";
import { Button } from "../components/ui/Button";
import { useI18n } from "./i18n";
import { useAuth } from "./AuthContext";

const primaryNavigation = [
  { href: "/", key: "nav.dashboard", end: true },
  { href: "/zones", key: "nav.zones", end: false },
  { href: "/accounts", key: "nav.accounts", end: false },
  { href: "/audit", key: "nav.audit", end: false },
] as const;

const settingsNavigation = { href: "/settings", key: "nav.settings", end: false } as const;

export default function AppShell(props: ParentProps) {
  const location = useLocation();
  const auth = useAuth();
  const { t } = useI18n();
  const [mobileOpen, setMobileOpen] = createSignal(false);
  const [mobileDialog, setMobileDialog] = createSignal<HTMLDialogElement>();

  createEffect(() => {
    const dialog = mobileDialog();
    if (dialog === undefined) return;
    if (mobileOpen() && !dialog.open) {
      dialog.showModal();
    } else if (!mobileOpen() && dialog.open) {
      dialog.close();
    }
  });
  onCleanup(() => {
    const dialog = mobileDialog();
    if (dialog?.open) dialog.close();
  });

  const session = createMemo(() => {
    const state = auth.state();
    return state.kind === "authenticated" ? state.session : undefined;
  });
  const navigation = createMemo(() => [
    ...primaryNavigation,
    ...(session()?.user.role === "admin"
      ? [{ href: "/users", key: "nav.users", end: false } as const]
      : []),
    settingsNavigation,
  ]);
  const pageTitle = () =>
    t(
      navigation().find((item) =>
        item.end ? location.pathname === item.href : location.pathname.startsWith(item.href),
      )?.key ?? "nav.controlPlane",
    );

  return (
    <div class="min-h-screen bg-background text-foreground">
      <div class="mx-auto flex min-h-screen max-w-[1680px]">
        <aside class="hidden w-60 shrink-0 border-r border-sidebar-border bg-sidebar p-5 md:flex md:flex-col">
          <Brand />
          <NavigationLinks items={navigation()} onNavigate={() => setMobileOpen(false)} />
          <AccountSummary
            displayName={
              session()?.user.display_name || session()?.user.username || t("nav.unknownUser")
            }
            role={session()?.user.role || "viewer"}
            onSignOut={() => void auth.signOut().catch(() => auth.refresh())}
          />
        </aside>

        <dialog
          ref={(element) => setMobileDialog(element)}
          class="m-0 h-dvh max-h-none w-screen max-w-none bg-transparent p-0 backdrop:bg-foreground/30"
          aria-label={t("nav.controlPlane")}
          onClick={(event) => {
            if (event.target === event.currentTarget) setMobileOpen(false);
          }}
          onClose={() => setMobileOpen(false)}
        >
          <aside class="flex h-full w-[min(18rem,86vw)] flex-col border-r border-sidebar-border bg-sidebar p-5 shadow-xl">
            <div class="flex items-center justify-between gap-4">
              <Brand />
              <Button
                variant="ghost"
                size="sm"
                aria-label={t("nav.close")}
                onClick={() => setMobileOpen(false)}
              >
                {t("nav.close")}
              </Button>
            </div>
            <NavigationLinks items={navigation()} onNavigate={() => setMobileOpen(false)} />
            <AccountSummary
              displayName={
                session()?.user.display_name || session()?.user.username || t("nav.unknownUser")
              }
              role={session()?.user.role || "viewer"}
              onSignOut={() => void auth.signOut().catch(() => auth.refresh())}
            />
          </aside>
        </dialog>

        <div class="min-w-0 flex-1">
          <header class="sticky top-0 z-30 border-b border-border bg-surface/95 px-4 py-3 backdrop-blur md:px-6 lg:px-8">
            <div class="flex items-center justify-between gap-4">
              <div class="flex min-w-0 items-center gap-3">
                <Button
                  class="md:hidden"
                  variant="ghost"
                  size="sm"
                  aria-label={t("nav.open")}
                  onClick={() => setMobileOpen(true)}
                >
                  {t("nav.open")}
                </Button>
                <div class="min-w-0">
                  <p class="text-xs font-medium text-muted-foreground">{t("nav.controlPlane")}</p>
                  <h1 class="truncate text-base font-semibold text-foreground">{pageTitle()}</h1>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <span class="hidden max-w-48 truncate text-sm text-muted-foreground sm:block">
                  {session()?.user.display_name || session()?.user.username}
                </span>
                <AppearanceControls />
              </div>
            </div>
          </header>

          <main class="px-4 py-6 md:px-6 lg:px-8 lg:py-8">{props.children}</main>
        </div>
      </div>
    </div>
  );
}

function NavigationLinks(props: {
  items: readonly { href: string; key: string; end: boolean }[];
  onNavigate?: () => void;
}) {
  const { t } = useI18n();
  return (
    <nav aria-label={t("nav.controlPlane")} class="mt-8 space-y-1">
      <For each={props.items}>
        {(item) => (
          <A
            href={item.href}
            end={item.end}
            class="block rounded-md border-l-2 px-3 py-2 text-sm font-medium transition-colors"
            activeClass="border-primary bg-primary-subtle text-primary-subtle-foreground"
            inactiveClass="border-transparent text-sidebar-foreground hover:bg-muted hover:text-foreground"
            onClick={props.onNavigate}
          >
            {t(item.key)}
          </A>
        )}
      </For>
    </nav>
  );
}

function AccountSummary(props: { displayName: string; role: string; onSignOut: () => void }) {
  const { t } = useI18n();
  return (
    <div class="mt-auto border-t border-sidebar-border pt-4 text-xs leading-5">
      <p class="truncate font-semibold text-foreground">{props.displayName}</p>
      <p class="text-muted-foreground">{t(`role.${props.role}`)}</p>
      <button
        class="mt-2 font-semibold text-primary hover:text-primary-hover hover:underline"
        type="button"
        onClick={() => props.onSignOut()}
      >
        {t("auth.signOut")}
      </button>
    </div>
  );
}
