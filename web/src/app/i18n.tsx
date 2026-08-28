import { createContext, createEffect, createSignal, useContext, type ParentProps } from "solid-js";

export type Language = "zh-CN" | "en" | "ja";

type TranslationValues = Record<string, string | number>;

type I18nContextValue = {
  language: () => Language;
  setLanguage: (language: Language) => void;
  t: (key: string, values?: TranslationValues) => string;
};

const languageStorageKey = "aster-dns-language";
const supportedLanguages: Language[] = ["zh-CN", "en", "ja"];

const translations: Record<Language, Record<string, string>> = {
  en: {
    language: "Language",
    theme: "Theme",
    "theme.system": "System",
    "theme.light": "Light",
    "theme.dark": "Dark",
    "language.zh-CN": "中文",
    "language.en": "English",
    "language.ja": "日本語",
    "brand.product": "A Starhui product",
    "brand.eyebrow": "Unified DNS operations",
    "brand.headline":
      "Manage provider accounts, zones, and records from one trusted control plane.",
    "brand.description":
      "Provider state remains authoritative. Credentials stay server-side, mutations are role-checked, and every DNS change is auditable.",
    "auth.loading.title": "Loading authentication",
    "auth.loading.description":
      "Establishing the server-side session and checking bootstrap state.",
    "auth.loading.message": "Loading authentication…",
    "auth.unavailable.title": "Authentication unavailable",
    "auth.unavailable.description":
      "The console could not establish authentication state with the server.",
    "auth.connectionFailed": "Connection failed",
    "auth.originDenied":
      "The browser origin does not match the configured public URL. Open the console from the configured URL.",
    "auth.requestId": "Request ID",
    "auth.genericFailure": "The operation failed.",
    "auth.retry": "Retry",
    "auth.unauthorized": "You are not authorized to view this page.",
    "auth.notFound": "Page not found",
    "auth.notFoundDescription": "This route is not part of the current application shell.",
    "auth.notFoundMessage": "Use the primary navigation to return to an available page.",
    "auth.bootstrap.eyebrow": "Secure bootstrap",
    "auth.bootstrap.title": "Create the first administrator",
    "auth.bootstrap.description":
      "Choose a password or Passkey for the first administrator. You can add the other sign-in method later in Settings.",
    "auth.bootstrap.descriptionPasskeyOnly":
      "Register a Passkey for the first administrator. Password bootstrap is disabled by server configuration.",
    "auth.bootstrap.locked": "Bootstrap is locked",
    "auth.bootstrap.lockedMessage":
      "Configure APP_BOOTSTRAP_TOKEN with 32 random bytes encoded as unpadded base64url, then restart the server.",
    "auth.bootstrap.token": "Bootstrap token",
    "auth.bootstrap.tokenHint":
      "This one-time token is removed from the server environment after bootstrap.",
    "auth.username": "Username",
    "auth.displayName": "Display name",
    "auth.initialMethod": "Initial sign-in method",
    "auth.passwordMethod": "Create with password",
    "auth.passwordMethodHint":
      "Use this when the browser cannot use WebAuthn. Add a Passkey later from Settings.",
    "auth.passkeyMethod": "Register a Passkey",
    "auth.passkeyMethodHint": "Phishing-resistant sign-in using this device or a security key.",
    "auth.password": "Password",
    "auth.passwordHint": "Use at least 12 characters.",
    "auth.confirmPassword": "Confirm password",
    "auth.passkeyName": "Passkey name",
    "auth.createWithPassword": "Create administrator with password",
    "auth.createWithPasskey": "Create administrator with Passkey",
    "auth.creating": "Creating administrator…",
    "auth.waitingPasskey": "Waiting for Passkey…",
    "auth.passwordMismatch": "Passwords do not match.",
    "auth.login.eyebrow": "Aster DNS",
    "auth.login.title": "Sign in",
    "auth.login.description":
      "Use a Passkey to access the DNS control plane. Password login appears when enabled by an administrator.",
    "auth.continuePasskey": "Continue with Passkey",
    "auth.passwordLogin": "Sign in with password",
    "auth.passwordFallback": "Password fallback",
    "auth.webAuthnUnsupported": "This browser does not support WebAuthn.",
    "auth.enrollment": "Register from an enrollment token",
    "auth.enrollmentToken": "Enrollment token",
    "auth.registerPasskey": "Register Passkey",
    "auth.totp.title": "Two-factor verification",
    "auth.totp.description": "Enter the six-digit code from your authenticator app.",
    "auth.totp.code": "Authentication code",
    "auth.totp.verify": "Verify code",
    "auth.startOver": "Start over",
    "auth.signOut": "Sign out",
    "nav.unknownUser": "Unknown user",
    "role.admin": "Admin",
    "role.operator": "Operator",
    "role.viewer": "Viewer",
    "nav.dashboard": "Dashboard",
    "nav.zones": "Zones",
    "nav.accounts": "Provider accounts",
    "nav.audit": "Audit logs",
    "nav.users": "Users",
    "nav.settings": "Settings",
    "nav.open": "Open navigation",
    "nav.close": "Close navigation",
    "nav.controlPlane": "DNS control plane",
    "app.error.eyebrow": "Application error",
    "app.error.title": "The console could not render",
    "app.error.description": "An unexpected client-side error interrupted the current view.",
    "app.error.message": "An unexpected UI error occurred.",
    "app.tryAgain": "Try again",
    "theme.switchToLight": "Switch to light theme",
    "theme.switchToDark": "Switch to dark theme",
    "theme.followSystem": "Theme follows system preference",
  },
  "zh-CN": {
    language: "语言",
    theme: "主题",
    "theme.system": "跟随系统",
    "theme.light": "浅色",
    "theme.dark": "深色",
    "language.zh-CN": "中文",
    "language.en": "English",
    "language.ja": "日本語",
    "brand.product": "Starhui 产品",
    "brand.eyebrow": "统一 DNS 运维",
    "brand.headline": "在一个可信控制平面中管理 Provider 账号、Zone 和 Record。",
    "brand.description":
      "Provider 状态保持权威，凭据只留在服务端，变更经过权限校验，每次 DNS 变更都有审计记录。",
    "auth.loading.title": "正在加载认证",
    "auth.loading.description": "正在建立服务端会话并检查 Bootstrap 状态。",
    "auth.loading.message": "正在加载认证…",
    "auth.unavailable.title": "认证不可用",
    "auth.unavailable.description": "控制台无法从服务端获取认证状态。",
    "auth.connectionFailed": "连接失败",
    "auth.originDenied": "浏览器 Origin 与配置的公开 URL 不一致，请从配置的 URL 打开控制台。",
    "auth.requestId": "请求 ID",
    "auth.genericFailure": "操作失败。",
    "auth.retry": "重试",
    "auth.unauthorized": "你没有权限查看此页面。",
    "auth.notFound": "页面不存在",
    "auth.notFoundDescription": "当前应用没有注册此路由。",
    "auth.notFoundMessage": "请使用主导航返回可用页面。",
    "auth.bootstrap.eyebrow": "安全初始化",
    "auth.bootstrap.title": "创建首个管理员",
    "auth.bootstrap.description":
      "为首个管理员选择密码或 Passkey。登录后可以在设置中补充另一种认证方式。",
    "auth.bootstrap.descriptionPasskeyOnly":
      "为首个管理员注册 Passkey。服务端配置已禁用密码初始化。",
    "auth.bootstrap.locked": "Bootstrap 已锁定",
    "auth.bootstrap.lockedMessage":
      "请配置包含 32 个随机字节、无填充 base64url 编码的 APP_BOOTSTRAP_TOKEN，然后重启服务。",
    "auth.bootstrap.token": "Bootstrap token",
    "auth.bootstrap.tokenHint": "初始化完成后，应从服务环境中删除这个一次性 token。",
    "auth.username": "用户名",
    "auth.displayName": "显示名称",
    "auth.initialMethod": "初始登录方式",
    "auth.passwordMethod": "使用密码创建",
    "auth.passwordMethodHint":
      "适用于当前浏览器不支持 WebAuthn 的情况。登录后可以在设置中添加 Passkey。",
    "auth.passkeyMethod": "注册 Passkey",
    "auth.passkeyMethodHint": "使用此设备或安全密钥进行抗钓鱼登录。",
    "auth.password": "密码",
    "auth.passwordHint": "至少使用 12 个字符。",
    "auth.confirmPassword": "确认密码",
    "auth.passkeyName": "Passkey 名称",
    "auth.createWithPassword": "使用密码创建管理员",
    "auth.createWithPasskey": "使用 Passkey 创建管理员",
    "auth.creating": "正在创建管理员…",
    "auth.waitingPasskey": "等待 Passkey…",
    "auth.passwordMismatch": "两次输入的密码不一致。",
    "auth.login.eyebrow": "Aster DNS",
    "auth.login.title": "登录",
    "auth.login.description": "使用 Passkey 访问 DNS 控制平面。管理员启用后也可以使用密码登录。",
    "auth.continuePasskey": "使用 Passkey 继续",
    "auth.passwordLogin": "使用密码登录",
    "auth.passwordFallback": "密码回退",
    "auth.webAuthnUnsupported": "当前浏览器不支持 WebAuthn。",
    "auth.enrollment": "使用 enrollment token 注册",
    "auth.enrollmentToken": "Enrollment token",
    "auth.registerPasskey": "注册 Passkey",
    "auth.totp.title": "双因素验证",
    "auth.totp.description": "输入身份验证器应用生成的六位验证码。",
    "auth.totp.code": "验证码",
    "auth.totp.verify": "验证验证码",
    "auth.startOver": "重新开始",
    "auth.signOut": "退出登录",
    "nav.unknownUser": "未知用户",
    "role.admin": "管理员",
    "role.operator": "操作员",
    "role.viewer": "查看者",
    "nav.dashboard": "仪表盘",
    "nav.zones": "Zone",
    "nav.accounts": "Provider 账号",
    "nav.audit": "审计日志",
    "nav.users": "用户",
    "nav.settings": "设置",
    "nav.open": "打开导航",
    "nav.close": "关闭导航",
    "nav.controlPlane": "DNS 控制平面",
    "theme.switchToLight": "切换到浅色主题",
    "theme.switchToDark": "切换到深色主题",
    "theme.followSystem": "主题跟随系统设置",
    "app.error.eyebrow": "应用错误",
    "app.error.title": "控制台无法渲染",
    "app.error.description": "客户端发生意外错误，当前页面被中断。",
    "app.error.message": "发生了意外的界面错误。",
    "app.tryAgain": "重试",
  },
  ja: {
    language: "言語",
    theme: "テーマ",
    "theme.system": "システムに合わせる",
    "theme.light": "ライト",
    "theme.dark": "ダーク",
    "language.zh-CN": "中文",
    "language.en": "English",
    "language.ja": "日本語",
    "brand.product": "Starhui product",
    "brand.eyebrow": "統合 DNS 運用",
    "brand.headline":
      "信頼できる一つのコントロールプレーンで Provider アカウント、Zone、Record を管理します。",
    "brand.description":
      "Provider の状態を正とし、認証情報はサーバー側に保持します。変更は権限で保護され、すべての DNS 変更を監査できます。",
    "auth.loading.title": "認証を読み込み中",
    "auth.loading.description": "サーバー側セッションを確立し、bootstrap 状態を確認しています。",
    "auth.loading.message": "認証を読み込み中…",
    "auth.unavailable.title": "認証を利用できません",
    "auth.unavailable.description": "コンソールがサーバーから認証状態を取得できませんでした。",
    "auth.connectionFailed": "接続に失敗しました",
    "auth.originDenied":
      "ブラウザーの Origin が設定された公開 URL と一致しません。設定済みの URL からコンソールを開いてください。",
    "auth.requestId": "リクエスト ID",
    "auth.genericFailure": "操作に失敗しました。",
    "auth.retry": "再試行",
    "auth.unauthorized": "このページを表示する権限がありません。",
    "auth.notFound": "ページが見つかりません",
    "auth.notFoundDescription": "このルートは現在のアプリケーションに登録されていません。",
    "auth.notFoundMessage": "メインナビゲーションから利用可能なページに戻ってください。",
    "auth.bootstrap.eyebrow": "安全な bootstrap",
    "auth.bootstrap.title": "最初の管理者を作成",
    "auth.bootstrap.description":
      "最初の管理者にパスワードまたは Passkey を選択します。もう一方の認証方式は後で Settings から追加できます。",
    "auth.bootstrap.descriptionPasskeyOnly":
      "最初の管理者に Passkey を登録します。サーバー設定でパスワード bootstrap は無効です。",
    "auth.bootstrap.locked": "Bootstrap はロックされています",
    "auth.bootstrap.lockedMessage":
      "32 バイトのランダム値をパディングなし base64url でエンコードした APP_BOOTSTRAP_TOKEN を設定して、サーバーを再起動してください。",
    "auth.bootstrap.token": "Bootstrap token",
    "auth.bootstrap.tokenHint":
      "この一度だけの token は bootstrap 完了後にサーバー環境から削除します。",
    "auth.username": "ユーザー名",
    "auth.displayName": "表示名",
    "auth.initialMethod": "初期ログイン方式",
    "auth.passwordMethod": "パスワードで作成",
    "auth.passwordMethodHint":
      "WebAuthn を利用できないブラウザー向けです。後で Settings から Passkey を追加できます。",
    "auth.passkeyMethod": "Passkey を登録",
    "auth.passkeyMethodHint":
      "このデバイスまたはセキュリティキーでフィッシング耐性のあるログインを行います。",
    "auth.password": "パスワード",
    "auth.passwordHint": "12 文字以上を使用してください。",
    "auth.confirmPassword": "パスワードの確認",
    "auth.passkeyName": "Passkey 名",
    "auth.createWithPassword": "パスワードで管理者を作成",
    "auth.createWithPasskey": "Passkey で管理者を作成",
    "auth.creating": "管理者を作成中…",
    "auth.waitingPasskey": "Passkey を待機中…",
    "auth.passwordMismatch": "パスワードが一致しません。",
    "auth.login.eyebrow": "Aster DNS",
    "auth.login.title": "サインイン",
    "auth.login.description":
      "Passkey で DNS コントロールプレーンにアクセスします。管理者が有効化した場合はパスワードも使用できます。",
    "auth.continuePasskey": "Passkey で続行",
    "auth.passwordLogin": "パスワードでサインイン",
    "auth.passwordFallback": "パスワードフォールバック",
    "auth.webAuthnUnsupported": "このブラウザーは WebAuthn に対応していません。",
    "auth.enrollment": "enrollment token で登録",
    "auth.enrollmentToken": "Enrollment token",
    "auth.registerPasskey": "Passkey を登録",
    "auth.totp.title": "二要素認証",
    "auth.totp.description": "認証アプリの 6 桁コードを入力してください。",
    "auth.totp.code": "認証コード",
    "auth.totp.verify": "コードを確認",
    "auth.startOver": "最初から",
    "auth.signOut": "サインアウト",
    "nav.unknownUser": "不明なユーザー",
    "role.admin": "管理者",
    "role.operator": "オペレーター",
    "role.viewer": "閲覧者",
    "nav.dashboard": "ダッシュボード",
    "nav.zones": "Zone",
    "nav.accounts": "Provider アカウント",
    "nav.audit": "監査ログ",
    "nav.users": "ユーザー",
    "nav.settings": "設定",
    "nav.open": "ナビゲーションを開く",
    "nav.close": "ナビゲーションを閉じる",
    "nav.controlPlane": "DNS コントロールプレーン",
    "theme.switchToLight": "ライトテーマに切り替え",
    "theme.switchToDark": "ダークテーマに切り替え",
    "theme.followSystem": "システム設定に合わせる",
    "app.error.eyebrow": "アプリケーションエラー",
    "app.error.title": "コンソールを表示できません",
    "app.error.description": "クライアント側で予期しないエラーが発生し、現在の画面を中断しました。",
    "app.error.message": "予期しない UI エラーが発生しました。",
    "app.tryAgain": "再試行",
  },
};

const I18nContext = createContext<I18nContextValue>();

export function I18nProvider(props: ParentProps) {
  const [language, setLanguage] = createSignal<Language>(readInitialLanguage());

  createEffect(() => {
    const current = language();
    document.documentElement.lang = current === "zh-CN" ? "zh-CN" : current;
    try {
      window.localStorage.setItem(languageStorageKey, current);
    } catch {
      // Language persistence is optional when storage is unavailable.
    }
  });

  const value: I18nContextValue = {
    language,
    setLanguage,
    t: (key, values) =>
      interpolate(translations[language()][key] ?? translations.en[key] ?? key, values),
  };

  return <I18nContext.Provider value={value}>{props.children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const context = useContext(I18nContext);
  if (context === undefined) throw new Error("I18nProvider is missing.");
  return context;
}

export function availableLanguages(): readonly Language[] {
  return supportedLanguages;
}

function readInitialLanguage(): Language {
  try {
    const saved = window.localStorage.getItem(languageStorageKey);
    if (isLanguage(saved)) return saved;
  } catch {
    // Fall back to the browser language.
  }
  const browser = (navigator.language || "en").toLowerCase();
  if (browser.startsWith("zh")) return "zh-CN";
  if (browser.startsWith("ja")) return "ja";
  return "en";
}

function isLanguage(value: string | null): value is Language {
  return value !== null && supportedLanguages.includes(value as Language);
}

function interpolate(template: string, values?: TranslationValues): string {
  if (values === undefined) return template;
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) =>
    String(values[key] ?? `{{${key}}}`),
  );
}
