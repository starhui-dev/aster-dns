import { type ParentProps } from "solid-js";

import { useI18n } from "../app/i18n";
import { AppearanceControls } from "./AppearanceControls";
import { Brand } from "./Brand";

export function AuthLayout(
  props: ParentProps<{ eyebrow: string; title: string; description: string; wide?: boolean }>,
) {
  const { t } = useI18n();

  return (
    <main class="grid min-h-screen bg-background text-foreground lg:grid-cols-[minmax(20rem,1fr)_minmax(30rem,42rem)]">
      <section class="relative hidden overflow-hidden border-r border-sidebar-border bg-sidebar p-10 lg:flex lg:flex-col">
        <div class="flex items-center justify-between gap-4">
          <Brand />
          <AppearanceControls idPrefix="auth-desktop" />
        </div>
        <div class="my-auto max-w-lg py-16">
          <p class="text-xs font-semibold text-primary">{t("brand.eyebrow")}</p>
          <h2 class="mt-3 text-3xl font-semibold leading-tight tracking-tight">
            {t("brand.headline")}
          </h2>
          <p class="mt-4 text-sm leading-7 text-muted-foreground">{t("brand.description")}</p>
        </div>
        <p class="text-xs text-muted-foreground">Aster DNS · {t("brand.product")}</p>
      </section>

      <section class="flex min-h-screen items-center justify-center p-5 sm:p-8 lg:p-12">
        <div class={props.wide ? "w-full max-w-xl" : "w-full max-w-md"}>
          <div class="mb-8 flex items-center justify-between gap-4 lg:hidden">
            <Brand />
            <AppearanceControls idPrefix="auth-mobile" />
          </div>
          <div class="mb-6">
            <p class="text-xs font-semibold text-primary">{props.eyebrow}</p>
            <h1 class="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">{props.title}</h1>
            <p class="mt-2 text-sm leading-6 text-muted-foreground">{props.description}</p>
          </div>
          {props.children}
        </div>
      </section>
    </main>
  );
}
