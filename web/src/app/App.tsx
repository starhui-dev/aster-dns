import { Route, Router } from "@solidjs/router";
import { ErrorBoundary } from "solid-js";

import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert, PageHeader, Panel } from "../components/ui/Layout";
import FoundationPage from "../pages/FoundationPage";
import PlaceholderPage from "../pages/PlaceholderPage";
import ProviderAccountsPage from "../pages/ProviderAccountsPage";
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
            <Route path="/accounts" component={ProviderAccountsPage} />
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
    <div class="space-y-6">
      <PageHeader
        eyebrow="404"
        title="Page not found"
        description="This route is not part of the current application shell."
      />
      <Panel>
        <Alert variant="warning">Use the primary navigation to return to an available page.</Alert>
      </Panel>
    </div>
  );
}

function AppError(props: { error: unknown; reset: () => void }) {
  return (
    <AuthLayout
      eyebrow="Application error"
      title="The console could not render"
      description="An unexpected client-side error interrupted the current view."
    >
      <Alert variant="danger">
        {props.error instanceof Error ? props.error.message : "An unexpected UI error occurred."}
      </Alert>
      <Button class="mt-5" variant="primary" onClick={() => props.reset()}>
        Try again
      </Button>
    </AuthLayout>
  );
}
