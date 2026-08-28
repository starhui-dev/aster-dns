import { For } from "solid-js";

import { availableLanguages, useI18n, type Language } from "../app/i18n";
import { useTheme, type ThemeMode } from "../app/theme";

export function AppearanceControls(props: { idPrefix?: string } = {}) {
  const { language, setLanguage, t } = useI18n();
  const theme = useTheme();
  const prefix = props.idPrefix ?? "appearance";
  const languageID = `${prefix}-language-select`;
  const themeID = `${prefix}-theme-select`;

  return (
    <div class="flex items-center gap-2">
      <label class="sr-only" for={languageID}>
        {t("language")}
      </label>
      <select
        id={languageID}
        class="rounded-md border border-input bg-surface px-2 py-1.5 text-xs text-foreground shadow-sm outline-none focus:border-primary focus:ring-2 focus:ring-focus/20"
        aria-label={t("language")}
        value={language()}
        onChange={(event) => setLanguage(event.currentTarget.value as Language)}
      >
        <For each={availableLanguages()}>
          {(item) => <option value={item}>{t(`language.${languageKey(item)}`)}</option>}
        </For>
      </select>
      <label class="sr-only" for={themeID}>
        {t("theme")}
      </label>
      <select
        id={themeID}
        class="rounded-md border border-input bg-surface px-2 py-1.5 text-xs text-foreground shadow-sm outline-none focus:border-primary focus:ring-2 focus:ring-focus/20"
        aria-label={t("theme")}
        value={theme.mode()}
        onChange={(event) => theme.setMode(event.currentTarget.value as ThemeMode)}
      >
        <option value="system">{t("theme.system")}</option>
        <option value="light">{t("theme.light")}</option>
        <option value="dark">{t("theme.dark")}</option>
      </select>
    </div>
  );
}

function languageKey(language: Language): string {
  if (language === "zh-CN") return "zh-CN";
  if (language === "ja") return "ja";
  return "en";
}
