import {
  startAuthentication,
  startRegistration,
  type AuthenticationResponseJSON,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
  type RegistrationResponseJSON,
} from "@simplewebauthn/browser";

import { apiRequest } from "./api";

export type Role = "admin" | "operator" | "viewer";

export interface AuthUser {
  id: string;
  username: string;
  display_name: string;
  email?: string;
  role: Role;
  password_enabled: boolean;
  totp_required: boolean;
  disabled_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BootstrapStatus {
  required: boolean;
  configured: boolean;
  password_login_enabled: boolean;
}

export interface AuthSessionResponse {
  authenticated: true;
  user: AuthUser;
  password_login_enabled: boolean;
}

export interface LoginResponse {
  authenticated: boolean;
  user?: AuthUser;
  totp_required?: boolean;
  totp_token?: string;
}

interface RegistrationOptionsResponse {
  ceremony_token: string;
  options: PublicKeyCredentialCreationOptionsJSON;
}

interface LoginOptionsResponse {
  ceremony_token: string;
  options: PublicKeyCredentialRequestOptionsJSON;
}

export interface Passkey {
  id: string;
  name: string;
  transports: string[];
  created_at: string;
  last_used_at?: string;
}

export interface SessionInfo {
  id: string;
  ip: string;
  user_agent: string;
  auth_method: "passkey" | "password";
  created_at: string;
  last_seen_at: string;
  idle_expires_at: string;
  absolute_expires_at: string;
  current: boolean;
}

export interface CreateUserInput {
  username: string;
  display_name: string;
  email?: string;
  role: Role;
  initial_password?: string;
}

export interface UserEnrollmentResult {
  user: AuthUser;
  enrollment_token: string;
  enrollment_expires_at: string;
}

export function getBootstrapStatus(signal?: AbortSignal): Promise<BootstrapStatus> {
  return apiRequest<BootstrapStatus>("/auth/bootstrap", withSignal(signal));
}

export function getCurrentSession(signal?: AbortSignal): Promise<AuthSessionResponse> {
  return apiRequest<AuthSessionResponse>("/auth/session", withSignal(signal));
}

export async function bootstrapAdmin(input: {
  bootstrap_token: string;
  username: string;
  display_name: string;
  passkey_name: string;
}): Promise<LoginResponse> {
  const begin = await apiRequest<RegistrationOptionsResponse>("/auth/bootstrap/passkey/options", {
    method: "POST",
    body: JSON.stringify(input),
  });
  const credential = await startRegistration({ optionsJSON: begin.options });
  return finishRegistration("/auth/bootstrap/passkey/verify", begin.ceremony_token, credential, {
    bootstrap_token: input.bootstrap_token,
  });
}

export function bootstrapAdminWithPassword(input: {
  bootstrap_token: string;
  username: string;
  display_name: string;
  password: string;
}): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/bootstrap/password", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function enrollPasskey(input: {
  enrollment_token: string;
  passkey_name: string;
}): Promise<LoginResponse> {
  const begin = await apiRequest<RegistrationOptionsResponse>("/auth/passkeys/enroll/options", {
    method: "POST",
    body: JSON.stringify(input),
  });
  const credential = await startRegistration({ optionsJSON: begin.options });
  return finishRegistration("/auth/passkeys/enroll/verify", begin.ceremony_token, credential, {
    enrollment_token: input.enrollment_token,
  });
}

export async function loginWithPasskey(): Promise<LoginResponse> {
  const begin = await apiRequest<LoginOptionsResponse>("/auth/passkeys/login/options", {
    method: "POST",
    body: JSON.stringify({}),
  });
  const credential = await startAuthentication({ optionsJSON: begin.options });
  return apiRequest<LoginResponse>("/auth/passkeys/login/verify", {
    method: "POST",
    body: JSON.stringify({ ceremony_token: begin.ceremony_token, credential }),
  });
}

export function loginWithPassword(username: string, password: string): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/login/password", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
}

export function completeTOTPLogin(totpToken: string, code: string): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/login/totp", {
    method: "POST",
    body: JSON.stringify({ totp_token: totpToken, code }),
  });
}

export function logout(all = false): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(all ? "/auth/logout-all" : "/auth/logout", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function registerPasskey(name: string): Promise<LoginResponse> {
  const begin = await apiRequest<RegistrationOptionsResponse>("/auth/passkeys/register/options", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
  const credential = await startRegistration({ optionsJSON: begin.options });
  return finishRegistration("/auth/passkeys/register/verify", begin.ceremony_token, credential);
}

export function listPasskeys(signal?: AbortSignal): Promise<{ passkeys: Passkey[] }> {
  return apiRequest<{ passkeys: Passkey[] }>("/auth/passkeys", withSignal(signal));
}

export function deletePasskey(id: string): Promise<LoginResponse> {
  return apiRequest<LoginResponse>(`/auth/passkeys/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function setPassword(password: string): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/password", {
    method: "PUT",
    body: JSON.stringify({ password }),
  });
}

export function deletePassword(): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/password", { method: "DELETE" });
}

export function setupTOTP(): Promise<{ provisioning_uri: string }> {
  return apiRequest<{ provisioning_uri: string }>("/auth/totp/setup", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function confirmTOTP(code: string): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/totp/confirm", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

export function deleteTOTP(): Promise<LoginResponse> {
  return apiRequest<LoginResponse>("/auth/totp", { method: "DELETE" });
}

export function listSessions(signal?: AbortSignal): Promise<{ sessions: SessionInfo[] }> {
  return apiRequest<{ sessions: SessionInfo[] }>("/auth/sessions", withSignal(signal));
}

export function revokeSession(id: string): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(`/auth/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function revokeOtherSessions(): Promise<{ status: string }> {
  return apiRequest<{ status: string }>("/auth/sessions/revoke-others", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function listUsers(signal?: AbortSignal): Promise<{ users: AuthUser[] }> {
  return apiRequest<{ users: AuthUser[] }>("/users", withSignal(signal));
}

export function createUser(input: CreateUserInput): Promise<UserEnrollmentResult> {
  return apiRequest<UserEnrollmentResult>("/users", {
    method: "POST",
    body: JSON.stringify(input),
  });
}
export function updateUser(
  id: string,
  input: {
    display_name?: string;
    email?: string;
    role?: Role;
    password?: string;
    password_enabled?: boolean;
  },
): Promise<{ user: AuthUser }> {
  return apiRequest<{ user: AuthUser }>(`/users/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}

export function setUserDisabled(id: string, disabled: boolean): Promise<{ user: AuthUser }> {
  return apiRequest<{ user: AuthUser }>(
    `/users/${encodeURIComponent(id)}/${disabled ? "disable" : "enable"}`,
    { method: "POST", body: JSON.stringify({}) },
  );
}

export function issueEnrollmentToken(id: string): Promise<{
  enrollment_token: string;
  enrollment_expires_at: string;
}> {
  return apiRequest(`/users/${encodeURIComponent(id)}/enrollment-token`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

async function finishRegistration(
  path: string,
  ceremonyToken: string,
  credential: RegistrationResponseJSON,
  extra: Record<string, unknown> = {},
): Promise<LoginResponse> {
  return apiRequest<LoginResponse>(path, {
    method: "POST",
    body: JSON.stringify({ ...extra, ceremony_token: ceremonyToken, credential }),
  });
}

function withSignal(signal?: AbortSignal): RequestInit {
  return signal === undefined ? {} : { signal };
}

export type { AuthenticationResponseJSON };
