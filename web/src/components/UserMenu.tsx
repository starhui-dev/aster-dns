import md5 from "md5";
import { ChevronDown, ExternalLink, LayoutDashboard, LogOut, Menu } from "lucide-solid";
import { createEffect, createSignal, onCleanup } from "solid-js";

import { AppearanceControls } from "./AppearanceControls";
import { useI18n } from "../app/i18n";

export type LayoutMode = "top" | "sidebar";

const repositoryURL = "https://github.com/starhui-dev/aster-dns";

export function UserMenu(props: {
  displayName: string;
  username: string;
  email: string | undefined;
  role: string;
  layout: LayoutMode;
  onLayoutChange: (layout: LayoutMode) => void;
  onSignOut: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = createSignal(false);
  const [imageFailed, setImageFailed] = createSignal(false);
  let root: HTMLDivElement | undefined;
  const assignRoot = (element: HTMLDivElement) => {
    root = element;
  };
  const email = () => props.email?.trim() || emailFromUsername(props.username);
  const avatarURL = () => gravatarURL(email());
  const initials = () => getInitials(props.displayName || props.username);

  createEffect(() => {
    if (!open()) return;
    const handlePointerDown = (event: PointerEvent) => {
      if (!root || !root.contains(event.target as Node)) setOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    onCleanup(() => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    });
  });

  const selectLayout = (layout: LayoutMode) => {
    props.onLayoutChange(layout);
    setOpen(false);
  };

  return (
    <div ref={assignRoot} class="relative">
      <button
        class="inline-flex min-w-0 items-center gap-2 rounded-lg px-1.5 py-1.5 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus"
        type="button"
        aria-label={t("user.menu")}
        aria-haspopup="menu"
        aria-expanded={open()}
        onClick={() => setOpen((value) => !value)}
      >
        <Avatar
          src={avatarURL()}
          initials={initials()}
          onError={() => setImageFailed(true)}
          hidden={imageFailed()}
        />
        <span class="hidden min-w-0 max-w-36 sm:block">
          <span class="block truncate text-xs font-semibold text-foreground">
            {props.displayName}
          </span>
          <span class="block truncate text-[11px] text-muted-foreground">
            {email() || props.username}
          </span>
        </span>
        <ChevronDown
          class="hidden shrink-0 text-muted-foreground sm:block"
          size={14}
          strokeWidth={1.8}
          aria-hidden="true"
        />
      </button>

      {open() && (
        <div
          class="absolute right-0 top-[calc(100%+0.5rem)] z-50 w-[min(20rem,calc(100vw-1.5rem))] overflow-hidden rounded-xl border border-border bg-surface shadow-2xl"
          role="menu"
          aria-label={t("user.menu")}
        >
          <div class="flex items-center gap-3 border-b border-border/80 p-4">
            <Avatar
              src={avatarURL()}
              initials={initials()}
              onError={() => setImageFailed(true)}
              hidden={imageFailed()}
              large
            />
            <div class="min-w-0">
              <p class="truncate font-semibold text-foreground">{props.displayName}</p>
              <p class="truncate text-xs text-muted-foreground">
                {email() || t("user.emailMissing")}
              </p>
              <p class="mt-0.5 text-xs text-muted-foreground">{t(`role.${props.role}`)}</p>
            </div>
          </div>
          <div class="space-y-3 border-b border-border/80 p-3">
            <div>
              <p class="mb-2 text-xs font-semibold text-muted-foreground">{t("user.layout")}</p>
              <div class="grid grid-cols-2 gap-1 rounded-lg bg-muted p-1">
                <LayoutOption
                  icon={LayoutDashboard}
                  label={t("layout.top")}
                  selected={props.layout === "top"}
                  onClick={() => selectLayout("top")}
                />
                <LayoutOption
                  icon={Menu}
                  label={t("layout.sidebar")}
                  selected={props.layout === "sidebar"}
                  onClick={() => selectLayout("sidebar")}
                />
              </div>
            </div>
            <AppearanceControls idPrefix="user-menu" />
          </div>
          <div class="p-2">
            <a
              class="flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              href={repositoryURL}
              target="_blank"
              rel="noreferrer"
              role="menuitem"
              onClick={() => setOpen(false)}
            >
              <ExternalLink size={16} strokeWidth={1.8} aria-hidden="true" />
              {t("user.github")}
            </a>
            <button
              class="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                props.onSignOut();
              }}
            >
              <LogOut size={16} strokeWidth={1.8} aria-hidden="true" />
              {t("user.signOut")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function Avatar(props: {
  src: string | undefined;
  initials: string;
  onError: () => void;
  hidden: boolean;
  large?: boolean;
}) {
  return (
    <span
      class={`inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-primary-subtle font-semibold text-primary-subtle-foreground ${props.large ? "h-11 w-11 text-sm" : "h-8 w-8 text-xs"}`}
    >
      {props.src && !props.hidden ? (
        <img
          class="h-full w-full object-cover"
          src={props.src}
          alt=""
          referrerPolicy="no-referrer"
          onError={props.onError}
        />
      ) : (
        props.initials
      )}
    </span>
  );
}

function LayoutOption(props: {
  icon: typeof LayoutDashboard;
  label: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      class={`flex min-w-0 items-center justify-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium transition-colors ${props.selected ? "bg-surface text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
      type="button"
      aria-pressed={props.selected}
      onClick={() => props.onClick()}
    >
      <props.icon size={14} strokeWidth={1.8} aria-hidden="true" />
      <span class="truncate">{props.label}</span>
    </button>
  );
}

export function gravatarURL(email: string): string | undefined {
  const normalized = email.trim().toLowerCase();
  if (normalized === "") return undefined;
  return `https://www.gravatar.com/avatar/${md5(normalized)}?s=96&d=identicon`;
}

function emailFromUsername(username: string): string {
  return username.includes("@") ? username : "";
}

function getInitials(value: string): string {
  const parts = value.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "A";
  return parts
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase();
}
