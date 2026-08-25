import { Route, Router } from "@solidjs/router";
import { ErrorBoundary } from "solid-js";

import { AuthLayout } from "../components/AuthLayout";
import { Button } from "../components/ui/Button";
import { Alert, PageHeader, Panel } from "../components/ui/Layout";
import AuditPage from "../pages/AuditPage";
import DashboardPage from "../pages/DashboardPage";
import ProviderAccountsPage from "../pages/ProviderAccountsPage";
import RecordsPage from "../pages/RecordsPage";
import SettingsPage from "../pages/SettingsPage";
import UsersPage from "../pages/UsersPage";
import ZonesPage from "../pages/ZonesPage";
import AppShell from "./AppShell";
import { AuthProvider } from "./AuthContext";
import AuthGate from "./AuthGate";

export default function App() {
  return (
    <ErrorBoundary fallback={(error, reset) => <AppError error={error} reset={reset} />}>
      <AuthProvider>
        <AuthGate>
          <Router root={AppShell}>
            <Route path="/" component={DashboardPage} />
            <Route path="/zones" component={ZonesPage} />
            <Route path="/zones/:zoneId/records" component={RecordsPage} />
            <Route path="/accounts" component={ProviderAccountsPage} />
            <Route path="/accounts/:accountId" component={ProviderAccountsPage} />
            <Route path="/audit" component={AuditPage} />
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
