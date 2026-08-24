import { Match, Switch, type ParentProps } from "solid-js";

import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert } from "../components/ui/Layout";
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
        <AuthLayout
          eyebrow="Security"
          title="Loading authentication"
          description="Establishing the server-side session and checking bootstrap state."
        >
          <Alert>Loading authentication…</Alert>
        </AuthLayout>
      </Match>
      <Match when={errorState()}>
        {(state) => (
          <AuthLayout
            eyebrow="Security"
            title="Authentication unavailable"
            description="The console could not establish authentication state with the server."
          >
            <Alert variant="danger" title="Connection failed">
              <p>{state().message}</p>
              {state().requestId !== null && (
                <p class="mt-2 font-mono text-xs">Request ID: {state().requestId}</p>
              )}
            </Alert>
            <Button class="mt-5" variant="primary" onClick={() => void auth.refresh()}>
              Retry
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
