import { render, screen, fireEvent } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.removeItem("aster-dns-language");
    window.localStorage.removeItem("aster-dns-theme");
    window.history.replaceState({}, "", "/");
  });

  it("renders authenticated security state and admin navigation", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "DNS control plane" })).toBeInTheDocument();
    expect(screen.getByText("Indexed zones")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Users" })).toBeInTheDocument();
  });

  it("switches the authenticated shell language", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByRole("link", { name: "Dashboard" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Language"), { target: { value: "zh-CN" } });
    expect(await screen.findByRole("link", { name: "仪表盘" })).toBeInTheDocument();
  });

  it("applies an explicit theme selection to the document", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    await screen.findByRole("heading", { name: "DNS control plane" });
    const selector = screen.getByLabelText("Theme");
    fireEvent.change(selector, { target: { value: "dark" } });
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(selector).toHaveValue("dark");
  });

  it("renders Passkey-first login with optional password fallback", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/auth/session")) {
          return jsonResponse(
            {
              error: {
                code: "authentication_failed",
                message: "Authentication failed.",
                request_id: "req_unauthenticated",
              },
            },
            401,
          );
        }
        if (path.endsWith("/auth/bootstrap")) {
          return jsonResponse({
            required: false,
            configured: false,
            password_login_enabled: true,
          });
        }
        throw new Error(`Unexpected request: ${path}`);
      }),
    );

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue with Passkey" })).toBeInTheDocument();
    expect(screen.getByText("Password fallback")).toBeInTheDocument();
  });

  it("renders password-first bootstrap choices when password login is enabled", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/auth/session")) {
          return jsonResponse(
            {
              error: {
                code: "authentication_failed",
                message: "Authentication failed.",
                request_id: "req_unauthenticated",
              },
            },
            401,
          );
        }
        if (path.endsWith("/auth/bootstrap")) {
          return jsonResponse({
            required: true,
            configured: true,
            password_login_enabled: true,
          });
        }
        throw new Error(`Unexpected request: ${path}`);
      }),
    );

    render(() => <App />);

    expect(
      await screen.findByRole("heading", { name: "Create the first administrator" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Create with password" })).toBeChecked();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.getByLabelText("Confirm password")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Register a Passkey" })).toBeInTheDocument();
  });
  it("localizes authentication failures", async () => {
    window.localStorage.setItem("aster-dns-language", "zh-CN");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/auth/session")) {
          return jsonResponse(
            {
              error: {
                code: "authentication_failed",
                message: "Authentication failed.",
                request_id: "req_unauthenticated",
              },
            },
            401,
          );
        }
        if (path.endsWith("/auth/bootstrap")) {
          return jsonResponse({
            required: false,
            configured: false,
            password_login_enabled: true,
          });
        }
        if (path.endsWith("/auth/login/password")) {
          return jsonResponse(
            {
              error: {
                code: "authentication_failed",
                message: "Authentication failed.",
                request_id: "req_password_failed",
              },
            },
            401,
          );
        }
        throw new Error(`Unexpected request: ${path}`);
      }),
    );

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "登录" })).toBeInTheDocument();
    fireEvent.input(screen.getByLabelText("用户名"), { target: { value: "zhenxin" } });
    fireEvent.input(screen.getByLabelText("密码"), { target: { value: "wrong-password" } });
    fireEvent.click(screen.getByRole("button", { name: "使用密码登录" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("登录失败，请检查登录信息后重试。");
    expect(alert).toHaveTextContent("请求编号: req_password_failed");
  });

  it("links cross-account zones to their record management routes", async () => {
    window.history.replaceState({}, "", "/zones");
    vi.stubGlobal("fetch", vi.fn(zonesFetch));

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "Zones", level: 2 })).toBeInTheDocument();
    const cloudflareZone = await screen.findByRole("link", { name: "example.com" });
    const huaweiZone = await screen.findByRole("link", { name: "example.cn" });
    expect(cloudflareZone).toHaveAttribute("href", `/zones/${cloudflareZoneID}/records`);
    expect(huaweiZone).toHaveAttribute("href", `/zones/${huaweiZoneID}/records`);
  });

  it("opens the account selected by the detail route", async () => {
    window.history.replaceState({}, "", "/accounts/01900000-0000-7000-8000-000000000102");
    vi.stubGlobal("fetch", vi.fn(zonesFetch));

    render(() => <App />);

    expect(
      await screen.findByRole("heading", { name: "Edit Huawei production" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Account name")).toHaveValue("Huawei production");
  });
});

const cloudflareZoneID = "01900000-0000-7000-8000-000000000201";
const huaweiZoneID = "01900000-0000-7000-8000-000000000202";

async function zonesFetch(input: RequestInfo | URL): Promise<Response> {
  const path = String(input);
  if (path.endsWith("/auth/session")) return authenticatedFetch(input);
  if (path.endsWith("/provider-types")) {
    return jsonResponse({
      provider_types: [
        providerType("cloudflare", "Cloudflare DNS"),
        providerType("huawei", "Huawei Cloud DNS"),
      ],
    });
  }
  if (path.endsWith("/provider-accounts")) {
    return jsonResponse({
      provider_accounts: [
        providerAccount(
          "01900000-0000-7000-8000-000000000101",
          "cloudflare",
          "Cloudflare production",
        ),
        providerAccount("01900000-0000-7000-8000-000000000102", "huawei", "Huawei production"),
      ],
    });
  }
  if (path.includes("/zones?")) {
    return jsonResponse({
      zones: [
        zone(
          cloudflareZoneID,
          "01900000-0000-7000-8000-000000000101",
          "cloudflare",
          "Cloudflare production",
          "example.com",
        ),
        zone(
          huaweiZoneID,
          "01900000-0000-7000-8000-000000000102",
          "huawei",
          "Huawei production",
          "example.cn",
        ),
      ],
      total: 2,
    });
  }
  throw new Error(`Unexpected request: ${path}`);
}

function providerType(type: string, displayName: string) {
  return {
    type,
    display_name: displayName,
    credential_fields: [],
    account_options: [],
    capabilities: {
      supported_record_types: ["A"],
      native_record_granularity: "record_set",
      supports_proxy: false,
      supports_routing_line: false,
      supports_weight: false,
      supports_record_status: false,
      supports_dnssec: false,
      supports_native_batch: false,
      supports_comments: false,
      extension_fields: [],
    },
  };
}

function providerAccount(id: string, providerTypeName: string, name: string) {
  return {
    id,
    provider_type: providerTypeName,
    name,
    description: "",
    enabled: true,
    options: {},
    credential_configured: true,
    credential_revision: 1,
    validation_status: "valid",
    zone_count: 1,
    created_at: "2026-08-24T00:00:00Z",
    updated_at: "2026-08-26T00:00:00Z",
  };
}

function zone(
  id: string,
  providerAccountID: string,
  providerTypeName: string,
  providerAccountName: string,
  name: string,
) {
  return {
    id,
    provider_account_id: providerAccountID,
    provider_type: providerTypeName,
    provider_account_name: providerAccountName,
    account_enabled: true,
    validation_status: "valid",
    name,
    status: "active",
    metadata: { nameservers: [] },
    fetched_at: "2026-08-26T00:00:00Z",
    stale: false,
  };
}

async function authenticatedFetch(input: RequestInfo | URL): Promise<Response> {
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
  if (path.endsWith("/provider-accounts")) {
    return jsonResponse({ provider_accounts: [] });
  }
  if (path.includes("/zones?limit=200")) {
    return jsonResponse({ zones: [], total: 0 });
  }
  if (path.includes("/audit-events?limit=20")) {
    return jsonResponse({ audit_events: [], total: 0 });
  }
  throw new Error(`Unexpected request: ${path}`);
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_test" },
  });
}
