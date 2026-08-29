import alibabaCloudIcon from "@lobehub/icons-static-svg/icons/alibabacloud-color.svg";
import cloudflareIcon from "@lobehub/icons-static-svg/icons/cloudflare-color.svg";
import huaweiCloudIcon from "@lobehub/icons-static-svg/icons/huaweicloud-color.svg";
import tencentCloudIcon from "@lobehub/icons-static-svg/icons/tencentcloud-color.svg";
import { Show } from "solid-js";

import { useI18n } from "../app/i18n";
import type { ProviderTypeDefinition } from "../lib/dns";

const providerIcons: Record<string, string> = {
  aliyun: alibabaCloudIcon,
  cloudflare: cloudflareIcon,
  huawei: huaweiCloudIcon,
  tencent: tencentCloudIcon,
};

const fallbackProviderNames: Record<string, Record<string, string>> = {
  aliyun: { "zh-CN": "阿里云 DNS", en: "Alibaba Cloud DNS", ja: "Alibaba Cloud DNS" },
  cloudflare: { "zh-CN": "Cloudflare DNS", en: "Cloudflare DNS", ja: "Cloudflare DNS" },
  huawei: { "zh-CN": "华为云 DNS", en: "Huawei Cloud DNS", ja: "Huawei Cloud DNS" },
  tencent: { "zh-CN": "腾讯云 DNSPod", en: "Tencent Cloud DNSPod", ja: "Tencent Cloud DNSPod" },
};

export function providerDisplayName(
  provider: ProviderTypeDefinition | undefined,
  providerType: string,
  language: string,
): string {
  return (
    provider?.display_names?.[language] ??
    fallbackProviderNames[providerType]?.[language] ??
    provider?.display_names?.en ??
    fallbackProviderNames[providerType]?.en ??
    provider?.display_name ??
    providerType
  );
}

export function ProviderIcon(props: { providerType: string; class?: string | undefined }) {
  const icon = () => providerIcons[props.providerType];

  return (
    <Show when={icon()}>
      {(source) => (
        <img
          class={props.class ?? "h-5 w-5 shrink-0 object-contain"}
          src={source()}
          alt=""
          aria-hidden="true"
          data-provider-icon={props.providerType}
        />
      )}
    </Show>
  );
}

export function ProviderIdentity(props: {
  provider?: ProviderTypeDefinition | undefined;
  providerType: string;
  class?: string | undefined;
  iconClass?: string | undefined;
}) {
  const { language } = useI18n();
  const label = () => providerDisplayName(props.provider, props.providerType, language());

  return (
    <span class={props.class ?? "inline-flex min-w-0 items-center gap-1.5"}>
      <ProviderIcon providerType={props.providerType} class={props.iconClass} />
      <span class="truncate">{label()}</span>
    </span>
  );
}
