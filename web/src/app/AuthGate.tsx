import { Match, Switch, type ParentProps } from "solid-js";

import { useI18n } from "./i18n";
import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert } from "../components/ui/Layout";
import BootstrapPage from "../pages/BootstrapPage";
import LoginPage from "../pages/LoginPage";
import { useAuth } from "./AuthContext";
function authErrorMessage(message: string, t: (key: string) => string): string {
  if (message === "The API request failed.") return t("auth.requestFailedMessage");
  if (message === "Authentication initialization failed.") return t("auth.genericFailure");
  return message;
}

export default function AuthGate(props: ParentProps) {
  const auth = useAuth();
  const { t } = useI18n();
  const errorState = () => {
    const state = auth.state();
    return state.kind === "error" ? state : undefined;
  };
  const unauthenticatedState = () => {
    const state = auth.state();
    return state.kind === "unauthenticated" ? state : undefined;
  };

  return (
    <Switch>
      <Match when={auth.state().kind === "loading"}>
        <AuthLayout
          eyebrow={t("auth.loading.title")}
          title={t("auth.loading.title")}
          description={t("auth.loading.description")}
        >
          <Alert>{t("auth.loading.message")}</Alert>
        </AuthLayout>
      </Match>
      <Match when={errorState()}>
        {(state) => (
          <AuthLayout
            eyebrow={t("auth.unavailable.title")}
            title={t("auth.unavailable.title")}
            description={t("auth.unavailable.description")}
          >
            <Alert variant="danger" title={t("auth.connectionFailed")}>
              <p>{authErrorMessage(state().message, t)}</p>
              {state().requestId !== null && (
                <p class="mt-2 font-mono text-xs">
                  {t("auth.requestId")}: {state().requestId}
                </p>
              )}
            </Alert>
            <Button class="mt-5" variant="primary" onClick={() => void auth.refresh()}>
              {t("auth.retry")}
            </Button>
          </AuthLayout>
        )}
      </Match>
      <Match when={unauthenticatedState()}>
        {(state) =>
          state().bootstrap.required ? (
            <BootstrapPage status={state().bootstrap} />
          ) : (
            <LoginPage status={state().bootstrap} />
          )
        }
      </Match>
      <Match when={auth.state().kind === "authenticated"}>{props.children}</Match>
    </Switch>
  );
}
