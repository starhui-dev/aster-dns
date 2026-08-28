import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import { I18nProvider } from "../app/i18n";
import { AuthProvider } from "../app/AuthContext";
import ProviderAccountsPage from "./ProviderAccountsPage";

describe("ProviderAccountsPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders safe account state and capability-driven credential fields", async () => {
    vi.stubGlobal("fetch", vi.fn(providerAccountsFetch));

    render(() => (
      <I18nProvider>
        <AuthProvider>
          <ProviderAccountsPage />
        </AuthProvider>
      </I18nProvider>
    ));

    expect(await screen.findByRole("heading", { name: "Production DNS" })).toBeInTheDocument();
    expect(screen.getByText("Configured · revision 3")).toBeInTheDocument();
    expect(screen.queryByText("fixture-secret-key")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add provider account" }));

    expect(await screen.findByLabelText("Access key (AK)")).toHaveValue("");
    expect(screen.getByLabelText("Secret key (SK)")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("DNS region")).toBeInTheDocument();

    const canary = "frontend-credential-canary-random-long-550195f2";
    fireEvent.input(screen.getByLabelText("Access key (AK)"), { target: { value: canary } });
    expect(document.body.textContent).not.toContain(canary);
    expect(JSON.stringify(window.localStorage)).not.toContain(canary);
    expect(JSON.stringify(window.sessionStorage)).not.toContain(canary);

    fireEvent.click(screen.getByRole("button", { name: "Close provider editor" }));
    await waitFor(() => expect(screen.queryByLabelText("Access key (AK)")).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Add provider account" }));
    expect(await screen.findByLabelText("Access key (AK)")).toHaveValue("");
  });
});

async function providerAccountsFetch(input: RequestInfo | URL): Promise<Response> {
  const path = String(input);
  if (path.endsWith("/auth/session")) {
    return jsonResponse({
      authenticated: true,
      password_login_enabled: true,
      user: {
        id: "01900000-0000-7000-8000-000000000001",
        username: "admin",
        display_name: "Administrator",
        role: "admin",
        password_enabled: false,
        totp_required: false,
        created_at: "2026-08-24T00:00:00Z",
        updated_at: "2026-08-24T00:00:00Z",
      },
    });
  }
  if (path.endsWith("/provider-types")) {
    return jsonResponse({ provider_types: [huaweiDefinition] });
  }
  if (path.endsWith("/provider-accounts")) {
    return jsonResponse({
      provider_accounts: [
        {
          id: "01900000-0000-7000-8000-000000000101",
          provider_type: "huawei",
          name: "Production DNS",
          description: "Primary account",
          enabled: true,
          options: { region: "ap-southeast-1" },
          credential_configured: true,
          credential_revision: 3,
          validation_status: "valid",
          last_validated_at: "2026-08-26T00:00:00Z",
          last_zone_sync_at: "2026-08-26T00:01:00Z",
          zone_count: 4,
          created_at: "2026-08-24T00:00:00Z",
          updated_at: "2026-08-26T00:01:00Z",
        },
      ],
    });
  }
  throw new Error(`Unexpected request: ${path}`);
}

const huaweiDefinition = {
  type: "huawei",
  display_name: "Huawei Cloud DNS",
  documentation_url: "https://support.huaweicloud.com/intl/en-us/dns/index.html",
  credential_fields: [
    { key: "access_key", label: "Access key (AK)", type: "string", secret: true, required: true },
    { key: "secret_key", label: "Secret key (SK)", type: "string", secret: true, required: true },
  ],
  account_options: [
    { key: "region", label: "DNS region", type: "string", secret: false, required: true },
  ],
  capabilities: {
    supported_record_types: ["A", "AAAA", "CNAME", "TXT", "MX"],
    min_ttl: 1,
    max_ttl: 2147483647,
    native_record_granularity: "record_set",
    supports_proxy: false,
    supports_routing_line: true,
    supports_weight: true,
    supports_record_status: true,
    supports_dnssec: false,
    supports_native_batch: false,
    supports_comments: false,
    extension_fields: [],
  },
};

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_provider_accounts" },
  });
}
