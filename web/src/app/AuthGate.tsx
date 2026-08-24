import { Match, Switch, type ParentProps } from "solid-js";

import BootstrapPage from "../pages/BootstrapPage";
import LoginPage from "../pages/LoginPage";
import { useAuth } from "./AuthContext";

export default function AuthGate(props: ParentProps) {
  const auth = useAuth();
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
        <AuthScreen>
          <p class="text-sm font-medium text-slate-600 dark:text-slate-300">
            Loading authentication…
          </p>
        </AuthScreen>
      </Match>
      <Match when={errorState()}>
        {(state) => (
          <AuthScreen>
            <p class="text-sm font-semibold text-rose-700 dark:text-rose-300" role="alert">
              {state().message}
            </p>
            {state().requestId !== null && (
              <p class="mt-2 text-xs text-slate-500">Request ID: {state().requestId}</p>
            )}
            <button class="primary-button mt-6" type="button" onClick={() => void auth.refresh()}>
              Retry
            </button>
          </AuthScreen>
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

function AuthScreen(props: ParentProps) {
  return (
    <main class="grid min-h-screen place-items-center bg-slate-100 p-6 text-slate-950 dark:bg-slate-950 dark:text-slate-100">
      <section class="w-full max-w-lg rounded-3xl border border-slate-200 bg-white p-8 shadow-xl shadow-slate-950/5 dark:border-slate-800 dark:bg-slate-900">
        {props.children}
      </section>
    </main>
  );
}
