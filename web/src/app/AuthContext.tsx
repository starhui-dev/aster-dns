import {
  createContext,
  createSignal,
  onCleanup,
  onMount,
  useContext,
  type Accessor,
  type ParentProps,
} from "solid-js";

import { ApiError } from "../lib/api";
import {
  getBootstrapStatus,
  getCurrentSession,
  logout as logoutRequest,
  type AuthSessionResponse,
  type BootstrapStatus,
  type LoginResponse,
} from "../lib/auth";

type AuthState =
  | { kind: "loading" }
  | { kind: "authenticated"; session: AuthSessionResponse }
  | { kind: "unauthenticated"; bootstrap: BootstrapStatus }
  | { kind: "error"; message: string; requestId: string | null };

interface AuthContextValue {
  state: Accessor<AuthState>;
  refresh: () => Promise<void>;
  acceptLogin: (response: LoginResponse) => Promise<void>;
  signOut: (all?: boolean) => Promise<void>;
}

const AuthContext = createContext<AuthContextValue>();

export function AuthProvider(props: ParentProps) {
  const [state, setState] = createSignal<AuthState>({ kind: "loading" });

  const refresh = async (signal?: AbortSignal) => {
    try {
      const session = await getCurrentSession(signal);
      setState({ kind: "authenticated", session });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        try {
          const bootstrap = await getBootstrapStatus(signal);
          setState({ kind: "unauthenticated", bootstrap });
          return;
        } catch (bootstrapError) {
          setState(errorState(bootstrapError));
          return;
        }
      }
      setState(errorState(error));
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    onCleanup(() => controller.abort());
  });

  const value: AuthContextValue = {
    state,
    refresh: () => refresh(),
    acceptLogin: async (response) => {
      if (!response.authenticated || response.user === undefined) {
        throw new Error("The authentication response did not create a session.");
      }
      await refresh();
    },
    signOut: async (all = false) => {
      await logoutRequest(all);
      const bootstrap = await getBootstrapStatus();
      setState({ kind: "unauthenticated", bootstrap });
    },
  };

  return <AuthContext.Provider value={value}>{props.children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("AuthProvider is missing.");
  }
  return context;
}

function errorState(error: unknown): AuthState {
  if (error instanceof ApiError) {
    return { kind: "error", message: error.message, requestId: error.requestId };
  }
  return {
    kind: "error",
    message: error instanceof Error ? error.message : "Authentication initialization failed.",
    requestId: null,
  };
}

export type { AuthState };
