import { A, RouterContext, useLocation } from "@solidjs/router";
import {
  For,
  Show,
  createMemo,
  createResource,
  createSignal,
  useContext,
  type JSX,
  type ParentProps,
} from "solid-js";
import { Dynamic } from "solid-js/web";

import {
  ClipboardList,
  Cloud,
  ExternalLink,
  Globe2,
  LayoutDashboard,
  Menu,
  RefreshCw,
  Settings,
  Users,
  X,
  type LucideIcon,
} from "lucide-solid";

import { Brand } from "../components/Brand";
import { UserMenu, type LayoutMode } from "../components/UserMenu";
import { checkForUpdates, getApiOverview } from "../lib/api";
import { useAuth } from "./AuthContext";
import { useI18n } from "./i18n";

const primaryNavigation = [
  { href: "/", key: "nav.dashboard", end: true, icon: LayoutDashboard },
  { href: "/zones", key: "nav.zones", end: false, icon: Globe2 },
  { href: "/accounts", key: "nav.accounts", end: false, icon: Cloud },
  { href: "/audit", key: "nav.audit", end: false, icon: ClipboardList },
] as const;

const settingsNavigation = {
  href: "/settings",
  key: "nav.settings",
  end: false,
  icon: Settings,
} as const;
type NavigationItem = {
  href: string;
  key: string;
  end: boolean;
  icon: LucideIcon;
};
const layoutStorageKey = "aster-dns-layout";

function readLayoutMode(): LayoutMode {
  try {
    const saved = window.localStorage.getItem(layoutStorageKey);
    if (saved === "sidebar" || saved === "top") return saved;
  } catch {
    // Layout persistence is optional when storage is unavailable.
  }
  return "top";
}

type ShellLayoutProps = {
  children: JSX.Element;
  navigation: readonly NavigationItem[];
  pageTitle: string;
  displayName: string;
  username: string;
  email: string | undefined;
  role: string;
  layout: LayoutMode;
  onLayoutChange: (layout: LayoutMode) => void;
  onSignOut: () => void;
};
type UpdateState =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "current" }
  | { kind: "available"; version: string; url: string }
  | { kind: "error" };

export default function AppShell(props: ParentProps) {
  const location = useLocation();
  const auth = useAuth();
  const { t } = useI18n();
  const [apiOverview] = createResource(getApiOverview);
  const [mobileNavOpen, setMobileNavOpen] = createSignal(false);
  const [layoutMode, setLayoutMode] = createSignal<LayoutMode>(readLayoutMode());
  const copyrightYear = new Date().getFullYear();
  const versionLabel = () => formatVersion(apiOverview()?.version);

  const session = createMemo(() => {
    const state = auth.state();
    return state.kind === "authenticated" ? state.session : undefined;
  });
  const navigation = createMemo(() => [
    ...primaryNavigation,
    ...(session()?.user.role === "admin"
      ? [{ href: "/users", key: "nav.users", end: false, icon: Users } as const]
      : []),
    settingsNavigation,
  ]);
  const pageTitle = () =>
    t(
      navigation().find((item) =>
        item.end ? location.pathname === item.href : location.pathname.startsWith(item.href),
      )?.key ?? "nav.controlPlane",
    );
  const displayName = () =>
    session()?.user.display_name || session()?.user.username || t("nav.unknownUser");
  const username = () => session()?.user.username || "";
  const email = () => session()?.user.email;
  const role = () => session()?.user.role || "viewer";
  const signOut = () => void auth.signOut().catch(() => auth.refresh());
  const closeMobileNav = () => setMobileNavOpen(false);
  const changeLayout = (next: LayoutMode) => {
    setLayoutMode(next);
    try {
      window.localStorage.setItem(layoutStorageKey, next);
    } catch {
      // Layout persistence is optional when storage is unavailable.
    }
  };

  return (
    <div class="relative min-h-screen overflow-x-clip bg-background text-foreground">
      <div class="pointer-events-none fixed inset-0 overflow-hidden" aria-hidden="true">
        <div class="absolute left-[-18rem] top-[-18rem] h-[42rem] w-[42rem] rounded-full bg-primary/10 blur-[150px]" />
        <div class="absolute right-[-18rem] top-1/3 h-[38rem] w-[38rem] rounded-full bg-sky-400/8 blur-[150px]" />
      </div>

      <Show
        when={layoutMode() === "sidebar"}
        fallback={
          <TopNavigationLayout
            navigation={navigation()}
            pageTitle={pageTitle()}
            displayName={displayName()}
            username={username()}
            email={email()}
            role={role()}
            layout={layoutMode()}
            onLayoutChange={changeLayout}
            onSignOut={signOut}
            copyrightYear={copyrightYear}
            version={versionLabel()}
            mobileNavOpen={mobileNavOpen()}
            onToggleMobileNav={() => setMobileNavOpen((open) => !open)}
          >
            {props.children}
          </TopNavigationLayout>
        }
      >
        <SidebarLayout
          navigation={navigation()}
          pageTitle={pageTitle()}
          displayName={displayName()}
          username={username()}
          email={email()}
          role={role()}
          layout={layoutMode()}
          onLayoutChange={changeLayout}
          onSignOut={signOut}
          copyrightYear={copyrightYear}
          version={versionLabel()}
          mobileNavOpen={mobileNavOpen()}
          onToggleMobileNav={() => setMobileNavOpen((open) => !open)}
        >
          {props.children}
        </SidebarLayout>
      </Show>

      <Show when={mobileNavOpen()}>
        <MobileNavigation
          items={navigation()}
          pageTitle={pageTitle()}
          onNavigate={closeMobileNav}
        />
      </Show>
    </div>
  );
}

type RenderLayoutProps = ShellLayoutProps & {
  copyrightYear: number;
  version: string;
  mobileNavOpen: boolean;
  onToggleMobileNav: () => void;
};

function TopNavigationLayout(props: RenderLayoutProps) {
  const { t } = useI18n();
  return (
    <div class="relative z-10 flex min-h-screen flex-col">
      <header class="sticky top-0 z-30 border-b border-border/60 bg-background/80 backdrop-blur-xl">
        <div class="mx-auto flex min-h-14 w-full max-w-[1600px] items-center gap-4 px-4 md:px-6 lg:px-8">
          <div class="flex min-w-0 items-center gap-3">
            <button
              class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground lg:hidden"
              type="button"
              aria-label={props.mobileNavOpen ? t("nav.close") : t("nav.open")}
              aria-controls="mobile-navigation"
              aria-expanded={props.mobileNavOpen}
              onClick={() => props.onToggleMobileNav()}
            >
              <Dynamic
                component={props.mobileNavOpen ? X : Menu}
                size={18}
                strokeWidth={1.9}
                aria-hidden="true"
              />
            </button>
            <Brand />
            <div class="hidden min-w-0 border-l border-border pl-4 sm:block">
              <p class="text-xs font-medium text-muted-foreground">{props.pageTitle}</p>
            </div>
          </div>
          <div class="hidden min-w-0 flex-1 justify-center lg:flex">
            <NavigationLinks items={props.navigation} />
          </div>
          <div class="ml-auto">
            <UserMenu
              displayName={props.displayName}
              username={props.username}
              email={props.email}
              role={props.role}
              layout={props.layout}
              onLayoutChange={props.onLayoutChange}
              onSignOut={props.onSignOut}
            />
          </div>
        </div>
      </header>
      <main class="relative z-10 mx-auto w-full max-w-[1180px] flex-1 px-4 py-8 md:px-8 lg:py-10">
        {props.children}
      </main>
      <div class="relative z-10 mx-auto w-full max-w-[1180px] px-4 md:px-8">
        <AppFooter year={props.copyrightYear} version={props.version} />
      </div>
    </div>
  );
}

function SidebarLayout(props: RenderLayoutProps) {
  const { t } = useI18n();
  return (
    <div class="relative z-10 flex min-h-screen">
      <aside
        class="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-border/70 bg-surface/45 backdrop-blur-xl lg:flex"
        aria-label={props.pageTitle}
      >
        <div class="flex h-14 items-center border-b border-border/60 px-4">
          <Brand />
        </div>
        <div class="flex-1 overflow-y-auto px-3 py-4">
          <NavigationLinks items={props.navigation} vertical />
        </div>
      </aside>
      <div class="flex min-h-screen min-w-0 flex-1 flex-col">
        <header class="flex min-h-14 items-center border-b border-border/60 px-4 md:px-6">
          <div class="flex min-w-0 items-center gap-3">
            <button
              class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground lg:hidden"
              type="button"
              aria-label={props.mobileNavOpen ? t("nav.close") : t("nav.open")}
              aria-controls="mobile-navigation"
              aria-expanded={props.mobileNavOpen}
              onClick={() => props.onToggleMobileNav()}
            >
              <Dynamic
                component={props.mobileNavOpen ? X : Menu}
                size={18}
                strokeWidth={1.9}
                aria-hidden="true"
              />
            </button>
            <div class="lg:hidden">
              <Brand />
            </div>
            <div class="hidden min-w-0 items-center gap-2 sm:flex">
              <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" aria-hidden="true" />
              <h1 class="truncate text-sm font-semibold text-foreground">{props.pageTitle}</h1>
            </div>
          </div>
          <div class="ml-auto">
            <UserMenu
              displayName={props.displayName}
              username={props.username}
              email={props.email}
              role={props.role}
              layout={props.layout}
              onLayoutChange={props.onLayoutChange}
              onSignOut={props.onSignOut}
            />
          </div>
        </header>
        <main class="relative z-10 mx-auto w-full max-w-[1180px] flex-1 px-4 py-8 md:px-8 lg:py-10">
          {props.children}
        </main>
        <div class="relative z-10 mx-auto w-full max-w-[1180px] px-4 md:px-8">
          <AppFooter year={props.copyrightYear} version={props.version} />
        </div>
      </div>
    </div>
  );
}

function MobileNavigation(props: {
  items: readonly NavigationItem[];
  pageTitle: string;
  onNavigate: () => void;
}) {
  const { t } = useI18n();
  return (
    <>
      <div
        class="fixed inset-0 z-40 bg-slate-950/60 lg:hidden"
        aria-hidden="true"
        onClick={() => props.onNavigate()}
      />
      <aside
        id="mobile-navigation"
        class="fixed inset-x-3 top-16 z-50 overflow-hidden rounded-xl border border-border bg-surface shadow-2xl lg:hidden"
        aria-label={t("nav.controlPlane")}
        role="dialog"
        aria-modal="true"
      >
        <div class="flex items-center justify-between border-b border-border/80 p-4">
          <div>
            <p class="text-xs font-medium text-muted-foreground">{t("nav.controlPlane")}</p>
            <p class="font-semibold text-foreground">{props.pageTitle}</p>
          </div>
          <button
            class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            type="button"
            aria-label={t("nav.close")}
            onClick={() => props.onNavigate()}
          >
            <X size={18} strokeWidth={1.9} aria-hidden="true" />
          </button>
        </div>
        <div class="max-h-[calc(100vh-9rem)] overflow-y-auto px-3 py-4">
          <NavigationLinks items={props.items} vertical onNavigate={props.onNavigate} />
        </div>
      </aside>
    </>
  );
}

function AppFooter(props: { year: number; version: string }) {
  const { t } = useI18n();
  const [updateState, setUpdateState] = createSignal<UpdateState>({ kind: "idle" });
  const checking = () => updateState().kind === "checking";
  const latestVersion = () => {
    const state = updateState();
    return state.kind === "available" ? state.version : "";
  };
  const releaseURL = () => {
    const state = updateState();
    return state.kind === "available" ? state.url : "";
  };
  const checkUpdates = async () => {
    if (checking()) return;
    setUpdateState({ kind: "checking" });
    try {
      const result = await checkForUpdates();
      setUpdateState(
        result.update_available
          ? {
              kind: "available",
              version: formatVersion(result.latest_version),
              url: result.release_url,
            }
          : { kind: "current" },
      );
    } catch {
      setUpdateState({ kind: "error" });
    }
  };

  return (
    <footer class="flex flex-col items-center justify-between gap-3 border-t border-border/70 px-1 py-5 text-xs text-muted-foreground sm:flex-row">
      <p>
        {t("footer.copyrightPrefix", { year: props.year })}
        <a
          class="transition-colors hover:text-foreground hover:underline"
          href="https://starhui.com"
          target="_blank"
          rel="noreferrer"
        >
          {t("footer.copyrightHolder")}
        </a>
        {t("footer.copyrightSuffix")}
      </p>
      <div class="flex flex-wrap items-center justify-center gap-x-4 gap-y-2">
        <span>{t("footer.version", { version: props.version })}</span>
        <button
          class="inline-flex items-center gap-1 font-semibold text-primary transition-colors hover:text-primary-hover hover:underline disabled:cursor-wait disabled:opacity-70"
          type="button"
          disabled={checking()}
          onClick={() => void checkUpdates()}
        >
          <RefreshCw
            class={checking() ? "animate-spin" : ""}
            size={13}
            strokeWidth={1.9}
            aria-hidden="true"
          />
          {checking() ? t("footer.checkingUpdates") : t("footer.checkUpdates")}
        </button>
        <Show when={updateState().kind === "current"}>
          <span aria-live="polite">{t("footer.upToDate")}</span>
        </Show>
        <Show when={updateState().kind === "available"}>
          <span aria-live="polite">
            {t("footer.updateAvailable", { version: latestVersion() })}
          </span>
          <a
            class="inline-flex items-center gap-1 font-semibold text-primary transition-colors hover:text-primary-hover hover:underline"
            href={releaseURL()}
            target="_blank"
            rel="noreferrer"
          >
            {t("footer.viewRelease")}
            <ExternalLink size={13} strokeWidth={1.9} aria-hidden="true" />
          </a>
        </Show>
        <Show when={updateState().kind === "error"}>
          <span aria-live="polite">{t("footer.updateCheckFailed")}</span>
        </Show>
      </div>
    </footer>
  );
}

function formatVersion(version: string | undefined): string {
  const normalized = version?.trim() || "dev";
  return normalized === "dev" || normalized.startsWith("v") ? normalized : `v${normalized}`;
}

function NavigationLinks(props: {
  items: readonly { href: string; key: string; end: boolean; icon: LucideIcon }[];
  onNavigate?: () => void;
  vertical?: boolean;
}) {
  const { t } = useI18n();
  const router = useContext(RouterContext);
  const navClass = () =>
    props.vertical ? "flex flex-col gap-1" : "flex flex-wrap items-center justify-center gap-1.5";
  const linkClass = () =>
    props.vertical
      ? "inline-flex min-h-10 w-full min-w-0 items-center justify-start gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors"
      : "inline-flex min-h-10 min-w-0 items-center justify-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors";
  const activeClass = () =>
    props.vertical
      ? "bg-muted text-foreground"
      : "bg-primary-subtle text-primary-subtle-foreground";
  const inactiveClass = () =>
    props.vertical
      ? "text-muted-foreground hover:bg-muted hover:text-foreground"
      : "text-muted-foreground hover:bg-muted hover:text-foreground";

  return (
    <nav aria-label={t("nav.controlPlane")} class={navClass()}>
      <RouterContext.Provider value={router}>
        <For each={props.items}>
          {(item) => (
            <A
              href={item.href}
              end={item.end}
              class={linkClass()}
              activeClass={activeClass()}
              inactiveClass={inactiveClass()}
              onClick={() => props.onNavigate?.()}
            >
              <Dynamic component={item.icon} size={16} strokeWidth={1.8} aria-hidden="true" />
              <span class="truncate">{t(item.key)}</span>
            </A>
          )}
        </For>
      </RouterContext.Provider>
    </nav>
  );
}
