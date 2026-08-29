import { Languages, Palette } from "lucide-solid";

import { availableLanguages, useI18n, type Language } from "../app/i18n";
import { useTheme, type ThemeMode } from "../app/theme";
import { SelectField } from "./ui/Select";

export function AppearanceControls(props: { idPrefix?: string } = {}) {
  const { language, setLanguage, t } = useI18n();
  const theme = useTheme();
  const prefix = props.idPrefix ?? "appearance";
  const languageID = `${prefix}-language-select`;
  const themeID = `${prefix}-theme-select`;

  return (
    <div class="flex items-center gap-2">
      <SelectField
        id={languageID}
        label={t("language")}
        labelClass="sr-only"
        value={language()}
        options={availableLanguages().map((item) => ({
          value: item,
          label: t(`language.${languageKey(item)}`),
        }))}
        onChange={(value) => setLanguage(value as Language)}
        icon={
          <Languages
            class="shrink-0 text-muted-foreground"
            size={14}
            strokeWidth={1.8}
            aria-hidden="true"
          />
        }
        triggerClass="min-h-8 w-auto max-w-32 py-1.5 pl-2.5 pr-2 text-xs"
      />
      <SelectField
        id={themeID}
        label={t("theme")}
        labelClass="sr-only"
        value={theme.mode()}
        options={[
          { value: "system", label: t("theme.system") },
          { value: "light", label: t("theme.light") },
          { value: "dark", label: t("theme.dark") },
        ]}
        onChange={(value) => theme.setMode(value as ThemeMode)}
        icon={
          <Palette
            class="shrink-0 text-muted-foreground"
            size={14}
            strokeWidth={1.8}
            aria-hidden="true"
          />
        }
        triggerClass="min-h-8 w-auto max-w-40 py-1.5 pl-2.5 pr-2 text-xs"
      />
    </div>
  );
}

function languageKey(language: Language): string {
  if (language === "zh-CN") return "zh-CN";
  if (language === "ja") return "ja";
  return "en";
}
