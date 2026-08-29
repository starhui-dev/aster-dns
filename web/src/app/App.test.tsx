import { render, screen, fireEvent } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import App, { AppError } from "./App";
import { I18nProvider } from "./i18n";
import { ThemeProvider } from "./theme";

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.removeItem("aster-dns-language");
    window.localStorage.removeItem("aster-dns-theme");
    window.localStorage.removeItem("aster-dns-layout");
    window.history.replaceState({}, "", "/");
  });

  it("renders authenticated security state and admin navigation", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "DNS control plane" })).toBeInTheDocument();
    expect(screen.getByText("Indexed zones")).toBeInTheDocument();
    expect(await screen.findByText("Version vtest")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Starhui Technology/ })).toHaveAttribute(
      "href",
      "https://starhui.com",
    );
    const checkUpdates = screen.getByRole("button", { name: "Check for updates" });
    fireEvent.click(checkUpdates);
    expect(await screen.findByText("You're up to date")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Users" })).toBeInTheDocument();
  });

  it("switches the authenticated shell language", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByRole("link", { name: "Dashboard" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open user menu" }));
    const languageSelector = screen.getByRole("button", { name: /^Language/ });
    fireEvent.pointerDown(languageSelector, { pointerType: "mouse", button: 0 });
    fireEvent.click(await screen.findByRole("option", { name: "中文" }));
    expect(await screen.findByRole("link", { name: "仪表盘" })).toBeInTheDocument();
    expect(screen.getAllByText("星绘开源项目").length).toBeGreaterThan(0);
  });

  it("applies an explicit theme selection to the document", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    await screen.findByRole("heading", { name: "DNS control plane" });
    fireEvent.click(screen.getByRole("button", { name: "Open user menu" }));
    const selector = screen.getByRole("button", { name: /^Theme/ });
    fireEvent.pointerDown(selector, { pointerType: "mouse", button: 0 });
    fireEvent.click(await screen.findByRole("option", { name: "Dark" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(selector).toHaveTextContent("Dark");
  });

  it("opens the profile menu and switches the shell layout", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByRole("button", { name: "Open user menu" })).toBeInTheDocument();
    expect(document.querySelector('img[src*="gravatar.com/avatar/"]')).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open user menu" }));
    expect(screen.getByRole("menu", { name: "Open user menu" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Sidebar navigation" }));
    expect(window.localStorage.getItem("aster-dns-layout")).toBe("sidebar");
    expect(await screen.findByRole("link", { name: "Dashboard" })).toBeInTheDocument();
  });

  it("updates the current user's profile from settings", async () => {
    let user = {
      id: "01900000-0000-7000-8000-000000000001",
      username: "admin",
      display_name: "Administrator",
      email: "admin@example.com",
      role: "admin" as const,
      password_enabled: false,
      totp_required: false,
      created_at: "2026-08-24T00:00:00Z",
      updated_at: "2026-08-24T00:00:00Z",
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith("/auth/session")) {
        return jsonResponse({ authenticated: true, password_login_enabled: true, user });
      }
      if (path.endsWith("/auth/passkeys")) return jsonResponse({ passkeys: [] });
      if (path.endsWith("/auth/sessions")) return jsonResponse({ sessions: [] });
      if (path.endsWith("/auth/profile") && init?.method === "PATCH") {
        const profile = JSON.parse(String(init.body)) as {
          display_name: string;
          email: string;
        };
        user = { ...user, ...profile, updated_at: "2026-08-29T00:00:00Z" };
        return jsonResponse({ user });
      }
      return authenticatedFetch(input);
    });
    vi.stubGlobal("fetch", fetchMock);
    window.history.replaceState({}, "", "/settings");

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "Profile" })).toBeInTheDocument();
    fireEvent.input(screen.getByLabelText("Display name"), {
      target: { value: "Aster Administrator" },
    });
    fireEvent.input(screen.getByLabelText("Email"), {
      target: { value: "aster-admin@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));

    expect(await screen.findByText("Profile updated.")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/auth\/profile$/),
      expect.objectContaining({ method: "PATCH" }),
    );
    expect(screen.getByLabelText("Display name")).toHaveValue("Aster Administrator");
    expect(screen.getByLabelText("Email")).toHaveValue("aster-admin@example.com");
  });

  it("renders application errors with the login layout", () => {
    render(() => (
      <ThemeProvider>
        <I18nProvider>
          <AppError error={new Error("render failure")} reset={vi.fn()} />
        </I18nProvider>
      </ThemeProvider>
    ));

    expect(screen.getByRole("main")).toHaveClass("auth-login-shell");
    expect(
      screen.getByRole("heading", { name: "The console could not render" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("render failure");
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
    expect(
      screen.getByRole("heading", {
        name: "Manage every DNS account in one place.",
        level: 2,
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue with Passkey" })).toBeInTheDocument();
    expect(screen.getByText("Password fallback")).toBeInTheDocument();
  });

  it("renders the app shell after a successful password login", async () => {
    let authenticated = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/auth/session")) {
          if (authenticated) return authenticatedFetch(input);
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
          authenticated = true;
          return jsonResponse({
            authenticated: true,
            user: {
              id: "01900000-0000-7000-8000-000000000001",
              username: "admin",
              display_name: "Administrator",
              role: "admin",
            },
          });
        }
        return authenticatedFetch(input);
      }),
    );

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    fireEvent.input(screen.getByLabelText("Username"), { target: { value: "admin" } });
    fireEvent.input(screen.getByLabelText("Password"), { target: { value: "correct-password" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in with password" }));

    expect(await screen.findByRole("heading", { name: "DNS control plane" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Dashboard" })).toBeInTheDocument();
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
  if (path.endsWith("/api/v1")) {
    return jsonResponse({
      name: "Aster DNS",
      api_version: "v1",
      version: "test",
      commit: "test",
      status: "available",
    });
  }
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
  if (path.endsWith("/api/v1")) {
    return jsonResponse({
      name: "Aster DNS",
      api_version: "v1",
      version: "test",
      commit: "test",
      status: "available",
    });
  }
  if (path.endsWith("/api/v1/updates")) {
    return jsonResponse({
      current_version: "test",
      latest_version: "v0.1.0",
      update_available: false,
      release_url: "https://github.com/starhui-dev/aster-dns/releases/latest",
    });
  }
  if (path.endsWith("/auth/session")) {
    return jsonResponse({
      authenticated: true,
      password_login_enabled: true,
      user: {
        id: "01900000-0000-7000-8000-000000000001",
        username: "admin",
        display_name: "Administrator",
        email: "admin@example.com",
        role: "admin",
        password_enabled: false,
        totp_required: false,
        created_at: "2026-08-24T00:00:00Z",
        updated_at: "2026-08-24T00:00:00Z",
      },
    });
  }
  if (path.endsWith("/provider-types")) {
    return jsonResponse({ provider_types: [] });
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
