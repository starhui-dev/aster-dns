import { For, Show, createSignal, onCleanup, onMount } from "solid-js";

import { useI18n } from "../app/i18n";
import { useAuth } from "../app/AuthContext";
import { Button } from "../components/ui/Button";
import { Alert, Badge, Field, PageHeader, Panel } from "../components/ui/Layout";
import { ApiError } from "../lib/api";
import {
  createUser,
  issueEnrollmentToken,
  listUsers,
  setUserDisabled,
  updateUser,
  type AuthUser,
  type Role,
} from "../lib/auth";

export default function UsersPage() {
  const { t } = useI18n();
  const auth = useAuth();
  const [users, setUsers] = createSignal<AuthUser[]>([]);
  const [username, setUsername] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [role, setRole] = createSignal<Role>("viewer");
  const [initialPassword, setInitialPassword] = createSignal("");
  const [enrollment, setEnrollment] = createSignal<{
    token: string;
    expiresAt: string;
    username: string;
  } | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const currentSession = () => {
    const state = auth.state();
    return state.kind === "authenticated" ? state.session : undefined;
  };

  const load = async (signal?: AbortSignal) => {
    try {
      setUsers((await listUsers(signal)).users);
      setError(null);
    } catch (caught) {
      setError(errorMessage(caught, t));
    }
  };

  onMount(() => {
    const controller = new AbortController();
    void load(controller.signal);
    onCleanup(() => controller.abort());
  });

  const run = async (operation: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await operation();
    } catch (caught) {
      setError(errorMessage(caught, t));
    } finally {
      setBusy(false);
    }
  };
  const submitCreateUser = (event: SubmitEvent) => {
    event.preventDefault();
    const input = {
      username: username(),
      display_name: displayName(),
      role: role(),
      ...(initialPassword() === "" ? {} : { initial_password: initialPassword() }),
    };
    void run(async () => {
      const result = await createUser(input);
      setEnrollment({
        token: result.enrollment_token,
        expiresAt: result.enrollment_expires_at,
        username: result.user.username,
      });
      setUsername("");
      setDisplayName("");
      setInitialPassword("");
      await load();
    });
  };

  return (
    <div class="space-y-6">
      <PageHeader
        eyebrow={t("users.eyebrow")}
        title={t("users.title")}
        description={t("users.description")}
      />

      <Show when={error() !== null}>
        <Alert variant="danger">{error()}</Alert>
      </Show>

      <Show when={enrollment()}>
        {(value) => (
          <Alert
            variant="warning"
            title={t("users.enrollmentTitle", { username: value().username })}
          >
            <code class="block break-all rounded-md border border-warning/20 bg-surface/70 p-3 text-xs">
              {value().token}
            </code>
            <p class="mt-2 text-xs">
              {t("users.expires", { date: formatDate(value().expiresAt) })}
            </p>
            <Button class="mt-3" size="sm" onClick={() => setEnrollment(null)}>
              {t("users.dismiss")}
            </Button>
          </Alert>
        )}
      </Show>

      <Panel title={t("users.create")} description={t("users.createDescription")}>
        <form class="grid gap-4 md:grid-cols-2" onSubmit={submitCreateUser}>
          <Field label={t("users.username")} for="create-username">
            <input
              id="create-username"
              class="text-input"
              required
              value={username()}
              onInput={(event) => setUsername(event.currentTarget.value)}
            />
          </Field>
          <Field label={t("users.displayName")} for="create-display-name">
            <input
              id="create-display-name"
              class="text-input"
              value={displayName()}
              onInput={(event) => setDisplayName(event.currentTarget.value)}
            />
          </Field>
          <Field label={t("users.role")} for="create-role">
            <select
              id="create-role"
              class="text-input"
              value={role()}
              onChange={(event) => setRole(event.currentTarget.value as Role)}
            >
              <option value="viewer">{t("role.viewer")}</option>
              <option value="operator">{t("role.operator")}</option>
              <option value="admin">{t("role.admin")}</option>
            </select>
          </Field>
          <Show when={currentSession()?.password_login_enabled}>
            <Field label={t("users.initialPassword")} for="create-password">
              <input
                id="create-password"
                class="text-input"
                type="password"
                minlength={12}
                maxlength={1024}
                autocomplete="new-password"
                value={initialPassword()}
                onInput={(event) => setInitialPassword(event.currentTarget.value)}
              />
            </Field>
          </Show>
          <div class="md:col-span-2">
            <Button type="submit" variant="primary" disabled={busy()}>
              {t("users.createButton")}
            </Button>
          </div>
        </form>
      </Panel>

      <Panel title={t("users.existing")} description={t("users.count", { count: users().length })}>
        <div class="space-y-3">
          <For
            each={users()}
            fallback={<p class="text-sm text-muted-foreground">{t("users.none")}</p>}
          >
            {(user) => (
              <UserRow
                user={user}
                currentUserID={currentSession()?.user.id ?? ""}
                busy={busy()}
                run={run}
                reload={() => load()}
                showEnrollment={(token, expiresAt) =>
                  setEnrollment({ token, expiresAt, username: user.username })
                }
              />
            )}
          </For>
        </div>
      </Panel>
    </div>
  );
}

function UserRow(props: {
  user: AuthUser;
  currentUserID: string;
  busy: boolean;
  run: (operation: () => Promise<void>) => Promise<void>;
  reload: () => Promise<void>;
  showEnrollment: (token: string, expiresAt: string) => void;
}) {
  const { t } = useI18n();
  const [selectedRole, setSelectedRole] = createSignal<Role>();
  const role = () => selectedRole() ?? props.user.role;
  const isCurrent = () => props.user.id === props.currentUserID;

  const saveRole = () => {
    const id = props.user.id;
    const nextRole = role();
    const reload = props.reload;
    const run = props.run;
    void run(async () => {
      await updateUser(id, { role: nextRole });
      await reload();
    });
  };

  const createEnrollment = () => {
    const id = props.user.id;
    const showEnrollment = props.showEnrollment;
    const run = props.run;
    void run(async () => {
      const result = await issueEnrollmentToken(id);
      showEnrollment(result.enrollment_token, result.enrollment_expires_at);
    });
  };

  const toggleDisabled = () => {
    const id = props.user.id;
    const disabled = props.user.disabled_at === undefined;
    if (disabled && !window.confirm(t("users.confirmDisable", { username: props.user.username })))
      return;
    const reload = props.reload;
    const run = props.run;
    void run(async () => {
      await setUserDisabled(id, disabled);
      await reload();
    });
  };

  return (
    <article class="rounded-md border border-border bg-surface-subtle p-4">
      <div class="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
        <div class="min-w-0 pb-1">
          <div class="flex flex-wrap items-center gap-2">
            <p class="font-medium text-foreground">
              {props.user.display_name || props.user.username}
            </p>
            <Badge>{t(`role.${props.user.role}`)}</Badge>
            <Show when={props.user.disabled_at !== undefined}>
              <Badge tone="danger">{t("users.disabled")}</Badge>
            </Show>
          </div>
          <p class="mt-1 truncate text-sm text-muted-foreground">{props.user.username}</p>
        </div>
        <div class="flex flex-wrap items-end gap-2">
          <Field label={t("users.role")} for={`role-${props.user.id}`}>
            <select
              id={`role-${props.user.id}`}
              class="text-input min-w-32"
              value={role()}
              disabled={props.busy || isCurrent()}
              onChange={(event) => setSelectedRole(event.currentTarget.value as Role)}
            >
              <option value="viewer">{t("role.viewer")}</option>
              <option value="operator">{t("role.operator")}</option>
              <option value="admin">{t("role.admin")}</option>
            </select>
          </Field>
          <Button
            disabled={props.busy || isCurrent() || role() === props.user.role}
            onClick={saveRole}
          >
            {t("users.saveRole")}
          </Button>
          <Button disabled={props.busy} onClick={createEnrollment}>
            {t("users.newEnrollment")}
          </Button>
          <Button
            variant={props.user.disabled_at === undefined ? "danger" : "secondary"}
            disabled={props.busy || isCurrent()}
            onClick={toggleDisabled}
          >
            {props.user.disabled_at === undefined ? t("users.disable") : t("users.enable")}
          </Button>
        </div>
      </div>
    </article>
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

function errorMessage(
  error: unknown,
  t: (key: string, values?: Record<string, string | number>) => string,
): string {
  if (error instanceof ApiError) {
    return error.requestId === null
      ? error.message
      : `${error.message} ${t("users.requestId")}: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : t("users.requestFailed");
}
