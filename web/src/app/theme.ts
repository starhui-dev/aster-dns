import { createEffect, createSignal } from "solid-js";

export type Theme = "light" | "dark";

const storageKey = "aster-dns-theme";

export function createThemeController() {
  const [theme, setTheme] = createSignal<Theme>(readInitialTheme());

  createEffect(() => {
    const current = theme();
    document.documentElement.dataset.theme = current;
    document.documentElement.style.colorScheme = current;
    try {
      window.localStorage.setItem(storageKey, current);
    } catch {
      // Theme persistence is optional when storage is unavailable.
    }
  });

  return {
    theme,
    toggle: () => setTheme((current) => (current === "light" ? "dark" : "light")),
  };
}

function readInitialTheme(): Theme {
  try {
    const saved = window.localStorage.getItem(storageKey);
    if (saved === "light" || saved === "dark") {
      return saved;
    }
  } catch {
    // Fall back to the operating system preference.
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}
