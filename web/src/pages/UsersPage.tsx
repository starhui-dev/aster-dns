import { For, Show, createSignal, onCleanup, onMount } from "solid-js";

import { useAuth } from "../app/AuthContext";
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
      setError(errorMessage(caught));
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
      setError(errorMessage(caught));
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
      <header>
        <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">Administration</p>
        <h2 class="mt-1 text-3xl font-semibold">Users and roles</h2>
        <p class="mt-2 text-sm text-slate-600 dark:text-slate-300">
          Authorization is enforced by the API for admin, operator, and viewer roles.
        </p>
      </header>

      <Show when={error() !== null}>
        <p
          class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
          role="alert"
        >
          {error()}
        </p>
      </Show>

      <Show when={enrollment()}>
        {(value) => (
          <section class="rounded-2xl border border-amber-300 bg-amber-50 p-5 text-amber-950 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
            <h3 class="font-semibold">Enrollment token for {value().username} — shown once</h3>
            <code class="mt-3 block break-all rounded-lg bg-white/70 p-3 text-xs dark:bg-slate-950/60">
              {value().token}
            </code>
            <p class="mt-2 text-xs">Expires {formatDate(value().expiresAt)}.</p>
            <button class="secondary-button mt-4" type="button" onClick={() => setEnrollment(null)}>
              Dismiss
            </button>
          </section>
        )}
      </Show>

      <section class="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h3 class="text-xl font-semibold">Create user</h3>
        <form class="mt-5 grid gap-4 md:grid-cols-2" onSubmit={submitCreateUser}>
          <label>
            <span class="field-label">Username</span>
            <input
              class="text-input"
              required
              value={username()}
              onInput={(event) => setUsername(event.currentTarget.value)}
            />
          </label>
          <label>
            <span class="field-label">Display name</span>
            <input
              class="text-input"
              value={displayName()}
              onInput={(event) => setDisplayName(event.currentTarget.value)}
            />
          </label>
          <label>
            <span class="field-label">Role</span>
            <select
              class="text-input"
              value={role()}
              onChange={(event) => setRole(event.currentTarget.value as Role)}
            >
              <option value="viewer">Viewer</option>
              <option value="operator">Operator</option>
              <option value="admin">Admin</option>
            </select>
          </label>
          <Show when={currentSession()?.password_login_enabled}>
            <label>
              <span class="field-label">Initial password (optional)</span>
              <input
                class="text-input"
                type="password"
                minlength={12}
                maxlength={1024}
                autocomplete="new-password"
                value={initialPassword()}
                onInput={(event) => setInitialPassword(event.currentTarget.value)}
              />
            </label>
          </Show>
          <div class="md:col-span-2">
            <button class="primary-button" type="submit" disabled={busy()}>
              Create user
            </button>
          </div>
        </form>
      </section>

      <section class="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h3 class="text-xl font-semibold">Existing users</h3>
        <div class="mt-5 space-y-3">
          <For each={users()} fallback={<p class="text-sm text-slate-500">No users found.</p>}>
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
      </section>
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
    const reload = props.reload;
    const run = props.run;
    void run(async () => {
      await setUserDisabled(id, disabled);
      await reload();
    });
  };

  return (
    <article class="rounded-xl border border-slate-200 p-4 dark:border-slate-700">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <p class="font-medium">{props.user.display_name || props.user.username}</p>
            <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-semibold dark:bg-slate-800">
              {props.user.role}
            </span>
            <Show when={props.user.disabled_at !== undefined}>
              <span class="rounded-full bg-rose-100 px-2 py-0.5 text-xs font-semibold text-rose-800 dark:bg-rose-950 dark:text-rose-200">
                Disabled
              </span>
            </Show>
          </div>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{props.user.username}</p>
        </div>
        <div class="flex flex-wrap items-end gap-2">
          <label>
            <span class="field-label">Role</span>
            <select
              class="text-input min-w-32"
              value={role()}
              disabled={props.busy || isCurrent()}
              onChange={(event) => setSelectedRole(event.currentTarget.value as Role)}
            >
              <option value="viewer">Viewer</option>
              <option value="operator">Operator</option>
              <option value="admin">Admin</option>
            </select>
          </label>
          <button
            class="secondary-button"
            type="button"
            disabled={props.busy || isCurrent() || role() === props.user.role}
            onClick={saveRole}
          >
            Save role
          </button>
          <button
            class="secondary-button"
            type="button"
            disabled={props.busy}
            onClick={createEnrollment}
          >
            New enrollment token
          </button>
          <button
            class={props.user.disabled_at === undefined ? "danger-button" : "secondary-button"}
            type="button"
            disabled={props.busy || isCurrent()}
            onClick={toggleDisabled}
          >
            {props.user.disabled_at === undefined ? "Disable" : "Enable"}
          </button>
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

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId === null
      ? error.message
      : `${error.message} Request ID: ${error.requestId}`;
  }
  return error instanceof Error ? error.message : "The user operation failed.";
}
