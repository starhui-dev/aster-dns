import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import { I18nProvider } from "../app/i18n";
import { AuthProvider } from "../app/AuthContext";
import ProviderAccountsPage from "./ProviderAccountsPage";

describe("ProviderAccountsPage", () => {
  afterEach(() => {
    window.localStorage.removeItem("aster-dns-language");
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

    const addButton = screen.getByRole("button", { name: "Add provider account" });
    addButton.focus();
    fireEvent.click(addButton);
    expect(await screen.findByLabelText("Access key (AK)")).toHaveValue("");
    expect(screen.getByLabelText("Secret key (SK)")).toHaveAttribute("type", "password");
    expect(document.querySelector("#provider-option-region-native")).toHaveValue("");
    expect(document.querySelector("#provider-option-region")).toHaveTextContent("Select…");

    const canary = "frontend-credential-canary-random-long-550195f2";
    fireEvent.input(screen.getByLabelText("Access key (AK)"), { target: { value: canary } });
    expect(document.body.textContent).not.toContain(canary);
    expect(JSON.stringify(window.localStorage)).not.toContain(canary);
    expect(JSON.stringify(window.sessionStorage)).not.toContain(canary);

    fireEvent.click(screen.getByRole("button", { name: "Close provider editor" }));
    await waitFor(() => expect(screen.queryByLabelText("Access key (AK)")).not.toBeInTheDocument());
    expect(document.activeElement).toBe(addButton);
    fireEvent.click(screen.getByRole("button", { name: "Add provider account" }));
    expect(await screen.findByLabelText("Access key (AK)")).toHaveValue("");
    expect(
      document.querySelector('#provider-type img[data-provider-icon="huawei"]'),
    ).toBeInTheDocument();
  });
  it("submits the initial provider selection without requiring a manual reselect", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith("/provider-accounts") && init?.method === "POST") {
        return jsonResponse({ provider_account: {} });
      }
      return providerAccountsFetch(input);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(() => (
      <I18nProvider>
        <AuthProvider>
          <ProviderAccountsPage />
        </AuthProvider>
      </I18nProvider>
    ));

    await screen.findByRole("heading", { name: "Production DNS" });
    fireEvent.click(screen.getByRole("button", { name: "Add provider account" }));
    fireEvent.input(await screen.findByLabelText("Account name"), {
      target: { value: "Huawei test" },
    });
    fireEvent.change(
      document.querySelector("#provider-option-region-native") as HTMLSelectElement,
      {
        target: { value: "ap-southeast-3" },
      },
    );
    fireEvent.input(screen.getByLabelText("Access key (AK)"), {
      target: { value: "fixture-access-key" },
    });
    fireEvent.input(screen.getByLabelText("Secret key (SK)"), {
      target: { value: "fixture-secret-key" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save account" }));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input).endsWith("/provider-accounts") && init?.method === "POST",
        ),
      ).toBe(true),
    );
  });
  it("renders localized Chinese Huawei metadata and Lobe icon", async () => {
    window.localStorage.setItem("aster-dns-language", "zh-CN");
    vi.stubGlobal("fetch", vi.fn(providerAccountsFetch));

    render(() => (
      <I18nProvider>
        <AuthProvider>
          <ProviderAccountsPage />
        </AuthProvider>
      </I18nProvider>
    ));

    expect(await screen.findByText("华为云 DNS")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "添加服务商账号" }));
    expect(document.querySelector("#provider-type")).toHaveTextContent("华为云 DNS");
    expect(document.querySelector('img[data-provider-icon="huawei"]')).toBeInTheDocument();
    expect(await screen.findByText("DNS 区域")).toBeInTheDocument();
    expect(document.querySelector("#provider-option-region")).toHaveTextContent("请选择…");
    fireEvent.change(
      document.querySelector("#provider-option-region-native") as HTMLSelectElement,
      {
        target: { value: "ap-southeast-3" },
      },
    );
    expect(document.querySelector("#provider-option-region")).toHaveTextContent(
      "ap-southeast-3 — 国际站",
    );
  });

  it("renders Japanese Huawei region metadata", async () => {
    window.localStorage.setItem("aster-dns-language", "ja");
    vi.stubGlobal("fetch", vi.fn(providerAccountsFetch));

    render(() => (
      <I18nProvider>
        <AuthProvider>
          <ProviderAccountsPage />
        </AuthProvider>
      </I18nProvider>
    ));

    await screen.findByRole("heading", { name: "Production DNS" });
    fireEvent.click(screen.getByRole("button", { name: "Provider アカウントを追加" }));
    expect(await screen.findByText("DNS リージョン")).toBeInTheDocument();
    expect(document.querySelector("#provider-option-region")).toHaveTextContent("選択…");
    fireEvent.change(
      document.querySelector("#provider-option-region-native") as HTMLSelectElement,
      {
        target: { value: "cn-north-4" },
      },
    );
    expect(document.querySelector("#provider-option-region")).toHaveTextContent(
      "cn-north-4 — 中国サイト",
    );
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
  display_names: { "zh-CN": "华为云 DNS", en: "Huawei Cloud DNS", ja: "Huawei Cloud DNS" },
  documentation_url: "https://support.huaweicloud.com/intl/en-us/dns/index.html",
  credential_fields: [
    { key: "access_key", label: "Access key (AK)", type: "string", secret: true, required: true },
    { key: "secret_key", label: "Secret key (SK)", type: "string", secret: true, required: true },
  ],
  account_options: [
    {
      key: "region",
      label: "DNS region",
      labels: { "zh-CN": "DNS 区域", en: "DNS region", ja: "DNS リージョン" },
      type: "enum",
      secret: false,
      required: true,
      options: [
        {
          value: "ap-southeast-3",
          label: "ap-southeast-3",
          labels: {
            "zh-CN": "ap-southeast-3 — 国际站",
            en: "ap-southeast-3 — International site",
            ja: "ap-southeast-3 — 国際サイト",
          },
        },
        {
          value: "cn-north-4",
          label: "cn-north-4",
          labels: {
            "zh-CN": "cn-north-4 — 中国站",
            en: "cn-north-4 — China site",
            ja: "cn-north-4 — 中国サイト",
          },
        },
      ],
    },
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
