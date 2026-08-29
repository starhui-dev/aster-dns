import { apiRequest } from "./api";

export type DescriptorFieldType = "string" | "integer" | "boolean" | "enum" | "string_list";
export type ExtensionScope = "zone" | "record_set" | "record_entry";

export interface DescriptorOption {
  value: string;
  label: string;
}

export interface DescriptorCondition {
  field: string;
  values: string[];
}

export interface DescriptorField {
  key: string;
  label: string;
  type: DescriptorFieldType;
  secret: boolean;
  required: boolean;
  placeholder?: string | undefined;
  description?: string | undefined;
  options?: DescriptorOption[] | undefined;
  minimum?: number | undefined;
  maximum?: number | undefined;
}

export interface ExtensionFieldDescriptor {
  namespace: string;
  scope: ExtensionScope;
  key: string;
  label: string;
  type: DescriptorFieldType;
  read_only: boolean;
  required: boolean;
  applicable_when?: DescriptorCondition[] | undefined;
  required_when?: DescriptorCondition[] | undefined;
  options?: DescriptorOption[] | undefined;
  minimum?: number | undefined;
  maximum?: number | undefined;
}

export interface ProviderCapabilities {
  supported_record_types: string[];
  min_ttl?: number | undefined;
  max_ttl?: number | undefined;
  native_record_granularity: "record_set" | "record_entry";
  supports_proxy: boolean;
  supports_routing_line: boolean;
  supports_weight: boolean;
  supports_record_status: boolean;
  supports_dnssec: boolean;
  supports_native_batch: boolean;
  supports_comments: boolean;
  extension_fields: ExtensionFieldDescriptor[];
}

export interface ProviderTypeDefinition {
  type: string;
  display_name: string;
  display_names?: Record<string, string> | undefined;
  documentation_url?: string | undefined;
  credential_fields: DescriptorField[];
  account_options: DescriptorField[];
  capabilities: ProviderCapabilities;
}

export interface ProviderAccount {
  id: string;
  provider_type: string;
  name: string;
  description: string;
  enabled: boolean;
  options: Record<string, unknown>;
  credential_configured: boolean;
  credential_revision: number;
  validation_status: "unconfigured" | "pending" | "valid" | "invalid" | "error";
  last_validated_at?: string | undefined;
  last_validation_error_code?: string | undefined;
  last_zone_sync_at?: string | undefined;
  zone_count: number;
  created_at: string;
  updated_at: string;
}

export interface Zone {
  id: string;
  provider_account_id: string;
  provider_type: string;
  provider_account_name: string;
  account_enabled: boolean;
  validation_status: ProviderAccount["validation_status"];
  name: string;
  status?: string | undefined;
  metadata: {
    nameservers?: string[] | undefined;
    extensions?: ExtensionContainer | undefined;
  };
  fetched_at: string;
  stale: boolean;
}

export interface RecordEntry {
  id?: string | undefined;
  value?: string | undefined;
  priority?: number | undefined;
  weight?: number | undefined;
  port?: number | undefined;
  target?: string | undefined;
  flags?: number | undefined;
  tag?: string | undefined;
  extensions?: ExtensionContainer | undefined;
}

export type ExtensionValue = string | number | boolean | string[];
export type ExtensionNamespace = Record<string, ExtensionValue | undefined>;
export type ExtensionContainer = Record<string, ExtensionNamespace | undefined>;

export interface RecordSet {
  id: string;
  name: string;
  type: string;
  ttl: number;
  entries: RecordEntry[];
  extensions?: ExtensionContainer | undefined;
  provider_version?: string | undefined;
  fingerprint: string;
}

export interface RecordSetInput {
  name: string;
  type: string;
  ttl: number;
  entries: RecordEntry[];
  extensions?: ExtensionContainer | undefined;
}

export interface SafeAPIError {
  code: string;
  message: string;
  request_id: string;
  details?: Record<string, unknown> | undefined;
}

export interface BatchItemResult {
  id: string;
  status: "succeeded" | "failed";
  recordset?: RecordSet | undefined;
  error?: SafeAPIError | undefined;
}

export interface BatchResult {
  total: number;
  succeeded: number;
  failed: number;
  items: BatchItemResult[];
}

export interface AuditEvent {
  id: string;
  occurred_at: string;
  actor_user_id?: string | undefined;
  actor_username: string;
  action: string;
  resource_type: string;
  resource_id?: string | undefined;
  provider_account_id?: string | undefined;
  zone_id?: string | undefined;
  request_id: string;
  ip?: string | undefined;
  user_agent?: string | undefined;
  result: "succeeded" | "failed";
  error_code?: string | undefined;
  before?: Record<string, unknown> | undefined;
  after?: Record<string, unknown> | undefined;
  metadata: Record<string, unknown>;
}

export function listProviderTypes(
  signal?: AbortSignal,
): Promise<{ provider_types: ProviderTypeDefinition[] }> {
  return apiRequest("/provider-types", withSignal(signal));
}

export function listProviderAccounts(
  signal?: AbortSignal,
): Promise<{ provider_accounts: ProviderAccount[] }> {
  return apiRequest("/provider-accounts", withSignal(signal));
}

export function createProviderAccount(input: {
  provider_type: string;
  name: string;
  description: string;
  enabled: boolean;
  options: Record<string, unknown>;
  credentials?: Record<string, unknown>;
}): Promise<{ provider_account: ProviderAccount }> {
  return apiRequest("/provider-accounts", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateProviderAccount(
  id: string,
  input: Partial<Pick<ProviderAccount, "name" | "description" | "enabled" | "options">>,
): Promise<{ provider_account: ProviderAccount }> {
  return apiRequest(`/provider-accounts/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}

export function deleteProviderAccount(id: string): Promise<void> {
  return apiRequest(`/provider-accounts/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function replaceProviderCredentials(
  id: string,
  credentials: Record<string, unknown>,
): Promise<{ provider_account: ProviderAccount }> {
  return apiRequest(`/provider-accounts/${encodeURIComponent(id)}/credentials`, {
    method: "POST",
    body: JSON.stringify({ credentials }),
  });
}

export function validateProviderAccount(
  id: string,
): Promise<{ provider_account: ProviderAccount }> {
  return apiRequest(`/provider-accounts/${encodeURIComponent(id)}/validate`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function syncProviderZones(id: string): Promise<{ status: string; zone_count: number }> {
  return apiRequest(`/provider-accounts/${encodeURIComponent(id)}/sync-zones`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function listZones(
  input: {
    q?: string | undefined;
    provider_type?: string | undefined;
    provider_account_id?: string | undefined;
    status?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
  } = {},
  signal?: AbortSignal,
): Promise<{ zones: Zone[]; next_cursor?: string | undefined; total: number }> {
  return apiRequest(`/zones${queryString(input)}`, withSignal(signal));
}

export function getZone(id: string, signal?: AbortSignal): Promise<{ zone: Zone }> {
  return apiRequest(`/zones/${encodeURIComponent(id)}`, withSignal(signal));
}

export function refreshZone(id: string): Promise<{ zone: Zone }> {
  return apiRequest(`/zones/${encodeURIComponent(id)}/refresh`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function listRecordSets(
  zoneID: string,
  input: {
    q?: string | undefined;
    type?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    refresh?: boolean | undefined;
  } = {},
  signal?: AbortSignal,
): Promise<{
  recordsets: RecordSet[];
  next_cursor?: string | undefined;
  total: number;
  fetched_at: string;
  stale: boolean;
  warning?: SafeAPIError | undefined;
}> {
  return apiRequest(
    `/zones/${encodeURIComponent(zoneID)}/recordsets${queryString(input)}`,
    withSignal(signal),
  );
}

export function getRecordSet(
  zoneID: string,
  recordSetID: string,
  signal?: AbortSignal,
): Promise<{ recordset: RecordSet }> {
  return apiRequest(
    `/zones/${encodeURIComponent(zoneID)}/recordsets/${encodeURIComponent(recordSetID)}`,
    withSignal(signal),
  );
}

export function createRecordSet(
  zoneID: string,
  input: RecordSetInput,
): Promise<{ recordset: RecordSet }> {
  return apiRequest(`/zones/${encodeURIComponent(zoneID)}/recordsets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateRecordSet(
  zoneID: string,
  recordSetID: string,
  input: RecordSetInput & { expected_fingerprint: string; provider_version?: string | undefined },
): Promise<{ recordset: RecordSet }> {
  return apiRequest(
    `/zones/${encodeURIComponent(zoneID)}/recordsets/${encodeURIComponent(recordSetID)}`,
    { method: "PATCH", body: JSON.stringify(input) },
  );
}

export function deleteRecordSet(
  zoneID: string,
  recordSetID: string,
  fingerprint: string,
): Promise<{ deleted: boolean; refetch_required: boolean }> {
  return apiRequest(
    `/zones/${encodeURIComponent(zoneID)}/recordsets/${encodeURIComponent(recordSetID)}`,
    { method: "DELETE", headers: { "If-Match": fingerprint } },
  );
}

export function batchRecordSets(
  zoneID: string,
  input: {
    operation: "delete" | "ttl_update";
    confirmation?: string | undefined;
    items: Array<{
      recordset_id: string;
      expected_fingerprint: string;
      provider_version?: string | undefined;
      ttl?: number | undefined;
    }>;
  },
): Promise<BatchResult> {
  return apiRequest(`/zones/${encodeURIComponent(zoneID)}/recordsets/batch`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function listAuditEvents(
  input: {
    actor?: string | undefined;
    action?: string | undefined;
    provider_account_id?: string | undefined;
    zone_id?: string | undefined;
    result?: string | undefined;
    from?: string | undefined;
    to?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
  } = {},
  signal?: AbortSignal,
): Promise<{ audit_events: AuditEvent[]; next_cursor?: string | undefined; total: number }> {
  return apiRequest(`/audit-events${queryString(input)}`, withSignal(signal));
}

export function getAuditEvent(
  id: string,
  signal?: AbortSignal,
): Promise<{ audit_event: AuditEvent }> {
  return apiRequest(`/audit-events/${encodeURIComponent(id)}`, withSignal(signal));
}

function queryString(input: Record<string, string | number | boolean | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(input)) {
    if (value !== undefined && value !== "") query.set(key, String(value));
  }
  const encoded = query.toString();
  return encoded === "" ? "" : `?${encoded}`;
}

function withSignal(signal?: AbortSignal): RequestInit {
  return signal === undefined ? {} : { signal };
}
