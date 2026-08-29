import { type ParentProps } from "solid-js";

import { useI18n } from "../app/i18n";
import { AppearanceControls } from "./AppearanceControls";
import { Brand } from "./Brand";

type AuthLayoutProps = ParentProps<{
  eyebrow: string;
  title: string;
  description: string;
  wide?: boolean | undefined;
}>;

export function AuthLayout(props: AuthLayoutProps) {
  const { t } = useI18n();

  return (
    <main
      class={`auth-login-shell min-h-screen bg-background text-foreground ${props.wide ? "auth-login-shell-wide" : ""}`}
    >
      <div class="auth-login-art" aria-hidden="true">
        <span class="auth-login-orbit auth-login-orbit-one" />
        <span class="auth-login-orbit auth-login-orbit-two" />
        <span class="auth-login-glow auth-login-glow-one" />
        <span class="auth-login-glow auth-login-glow-two" />
      </div>

      <header class="auth-login-header">
        <Brand />
        <div class="flex items-center gap-3">
          <span class="auth-login-header-note hidden text-xs font-semibold text-muted-foreground sm:inline">
            {t("auth.login.title")}
          </span>
          <AppearanceControls idPrefix="auth-login" />
        </div>
      </header>

      <div class="auth-login-content">
        <section class="auth-login-hero" aria-labelledby="auth-login-hero-title">
          <p class="auth-login-kicker">{t("brand.eyebrow")}</p>
          <h2 id="auth-login-hero-title">{t("auth.login.heroTitle")}</h2>
          <p class="auth-login-hero-description">{t("auth.login.heroDescription")}</p>

          <div class="auth-login-stats" aria-label={t("auth.login.title")}>
            <div class="auth-login-stat">
              <strong>4</strong>
              <span>{t("auth.login.stats.providers")}</span>
            </div>
            <div class="auth-login-stat">
              <strong>1</strong>
              <span>{t("auth.login.stats.controlPlane")}</span>
            </div>
            <div class="auth-login-stat">
              <strong>0</strong>
              <span>{t("auth.login.stats.browserSecrets")}</span>
            </div>
          </div>
        </section>

        <section class="auth-login-card" aria-labelledby="auth-login-title">
          <div class="mb-6">
            <p class="auth-login-card-eyebrow">{props.eyebrow}</p>
            <h1
              id="auth-login-title"
              class="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl"
            >
              {props.title}
            </h1>
            <p class="mt-2 text-sm leading-6 text-muted-foreground">{props.description}</p>
          </div>
          {props.children}
        </section>
      </div>

      <footer class="auth-login-footer">
        <span>Aster DNS</span>
        <span aria-hidden="true">·</span>
        <span>{t("brand.product")}</span>
      </footer>
    </main>
  );
}
