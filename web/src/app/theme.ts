import {
  createComponent,
  createContext,
  createEffect,
  createSignal,
  onCleanup,
  onMount,
  useContext,
  type ParentProps,
} from "solid-js";

export type ThemeMode = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

type ThemeContextValue = {
  mode: () => ThemeMode;
  theme: () => ResolvedTheme;
  setMode: (mode: ThemeMode) => void;
};

const storageKey = "aster-dns-theme";
const systemPreference = "(prefers-color-scheme: dark)";
const ThemeContext = createContext<ThemeContextValue>();

export function ThemeProvider(props: ParentProps) {
  const initialMode = readInitialMode();
  const [mode, setMode] = createSignal<ThemeMode>(initialMode);
  const [theme, setTheme] = createSignal<ResolvedTheme>(resolveTheme(initialMode));

  const applyTheme = () => {
    const resolved = resolveTheme(mode());
    setTheme(resolved);
    document.documentElement.dataset.theme = resolved;
    document.documentElement.style.colorScheme = resolved;
    try {
      window.localStorage.setItem(storageKey, mode());
    } catch {
      // Theme persistence is optional when storage is unavailable.
    }
  };

  createEffect(() => {
    mode();
    applyTheme();
  });

  onMount(() => {
    const media = window.matchMedia?.(systemPreference);
    if (media === undefined) return;
    const handleChange = () => {
      if (mode() === "system") applyTheme();
    };
    media.addEventListener?.("change", handleChange);
    onCleanup(() => media.removeEventListener?.("change", handleChange));
  });
  const value: ThemeContextValue = { mode, theme, setMode };

  return createComponent(ThemeContext.Provider, {
    value,
    get children() {
      return props.children;
    },
  });
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (context === undefined) throw new Error("ThemeProvider is missing.");
  return context;
}

function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode !== "system") return mode;
  return window.matchMedia?.(systemPreference).matches ? "dark" : "light";
}

function readInitialMode(): ThemeMode {
  try {
    const saved = window.localStorage.getItem(storageKey);
    if (saved === "system" || saved === "light" || saved === "dark") return saved;
  } catch {
    // Fall back to system preference.
  }
  return "system";
}
