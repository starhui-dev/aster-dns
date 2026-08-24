import { Route, Router } from "@solidjs/router";
import { ErrorBoundary } from "solid-js";

import FoundationPage from "../pages/FoundationPage";
import PlaceholderPage from "../pages/PlaceholderPage";
import SettingsPage from "../pages/SettingsPage";
import UsersPage from "../pages/UsersPage";
import AppShell from "./AppShell";
import { AuthProvider } from "./AuthContext";
import AuthGate from "./AuthGate";

export default function App() {
  return (
    <ErrorBoundary fallback={(error, reset) => <AppError error={error} reset={reset} />}>
      <AuthProvider>
        <AuthGate>
          <Router root={AppShell}>
            <Route path="/" component={FoundationPage} />
            <Route
              path="/zones"
              component={() => (
                <PlaceholderPage
                  title="Zones"
                  phase="Future provider phase"
                  description="The zone index route exists, but it will remain empty until real provider account sync is implemented."
                />
              )}
            />
            <Route
              path="/accounts"
              component={() => (
                <PlaceholderPage
                  title="Provider accounts"
                  phase="Future provider phase"
                  description="Credential capture and validation will be added with the official provider adapters."
                />
              )}
            />
            <Route
              path="/audit"
              component={() => (
                <PlaceholderPage
                  title="Audit"
                  phase="Future audit query phase"
                  description="Authentication events are append-only in PostgreSQL; the query UI is not implemented yet."
                />
              )}
            />
            <Route path="/settings" component={SettingsPage} />
            <Route path="/users" component={UsersPage} />
            <Route path="*404" component={NotFoundPage} />
          </Router>
        </AuthGate>
      </AuthProvider>
    </ErrorBoundary>
  );
}

function NotFoundPage() {
  return (
    <section class="rounded-3xl border border-slate-200 bg-white p-8 dark:border-slate-800 dark:bg-slate-900">
      <p class="text-sm font-semibold text-rose-700 dark:text-rose-300">404</p>
      <h2 class="mt-2 text-3xl font-semibold">Page not found</h2>
      <p class="mt-3 text-sm text-slate-600 dark:text-slate-300">
        This route is not part of the current application shell.
      </p>
    </section>
  );
}

function AppError(props: { error: unknown; reset: () => void }) {
  return (
    <main class="grid min-h-screen place-items-center bg-slate-100 p-6 dark:bg-slate-950">
      <section class="w-full max-w-lg rounded-2xl border border-rose-200 bg-white p-6 shadow-sm dark:border-rose-900 dark:bg-slate-900">
        <p class="text-sm font-semibold text-rose-700 dark:text-rose-300">Application error</p>
        <h1 class="mt-2 text-2xl font-semibold text-slate-950 dark:text-white">
          The console could not render.
        </h1>
        <p class="mt-3 text-sm text-slate-600 dark:text-slate-300" role="alert">
          {props.error instanceof Error ? props.error.message : "An unexpected UI error occurred."}
        </p>
        <button
          type="button"
          class="mt-6 rounded-xl bg-slate-950 px-4 py-2 text-sm font-semibold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-600 dark:bg-white dark:text-slate-950"
          onClick={() => props.reset()}
        >
          Try again
        </button>
      </section>
    </main>
  );
}
