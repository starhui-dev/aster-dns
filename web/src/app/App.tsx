import { Route, Router, useParams } from "@solidjs/router";
import { RefreshCw } from "lucide-solid";
import { ErrorBoundary, Show } from "solid-js";

import { I18nProvider, useI18n } from "./i18n";
import { ThemeProvider } from "./theme";

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
import { AuthProvider, useAuth } from "./AuthContext";
import AuthGate from "./AuthGate";
export default function App() {
  return (
    <ThemeProvider>
      <I18nProvider>
        <ErrorBoundary fallback={(error, reset) => <AppError error={error} reset={reset} />}>
          <AuthProvider>
            <AuthGate>
              <Router>
                <Route path="/" component={AppShell}>
                  <Route path="/" component={DashboardPage} />
                  <Route path="/zones" component={ZonesPage} />
                  <Route path="/zones/:zoneId/records" component={RecordsPage} />
                  <Route path="/accounts" component={ProviderAccountsPage} />
                  <Route path="/accounts/:accountId" component={ProviderAccountDetailPage} />
                  <Route path="/audit" component={AuditPage} />
                  <Route path="/settings" component={SettingsPage} />
                  <Route path="/users" component={AdminUsersPage} />
                  <Route path="*404" component={NotFoundPage} />
                </Route>
              </Router>
            </AuthGate>
          </AuthProvider>
        </ErrorBoundary>
      </I18nProvider>
    </ThemeProvider>
  );
}

function ProviderAccountDetailPage() {
  const params = useParams<{ accountId: string }>();
  return <ProviderAccountsPage accountId={params.accountId} />;
}
function AdminUsersPage() {
  const auth = useAuth();
  const allowed = () => {
    const state = auth.state();
    return state.kind === "authenticated" && state.session.user.role === "admin";
  };
  return (
    <Show when={allowed()} fallback={<NotAuthorizedPage />}>
      <UsersPage />
    </Show>
  );
}

function NotAuthorizedPage() {
  const { t } = useI18n();
  return (
    <Panel>
      <Alert variant="danger">{t("auth.unauthorized")}</Alert>
    </Panel>
  );
}

function NotFoundPage() {
  const { t } = useI18n();
  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow="404"
        title={t("auth.notFound")}
        description={t("auth.notFoundDescription")}
      />
      <Panel>
        <Alert variant="warning">{t("auth.notFoundMessage")}</Alert>
      </Panel>
    </div>
  );
}

export function AppError(props: { error: unknown; reset: () => void }) {
  const { t } = useI18n();
  return (
    <AuthLayout
      eyebrow={t("app.error.eyebrow")}
      title={t("app.error.title")}
      description={t("app.error.description")}
    >
      <Alert variant="danger">
        {props.error instanceof Error ? props.error.message : t("app.error.message")}
      </Alert>
      <Button class="mt-5" variant="primary" icon={RefreshCw} onClick={() => props.reset()}>
        {t("app.tryAgain")}
      </Button>
    </AuthLayout>
  );
}
