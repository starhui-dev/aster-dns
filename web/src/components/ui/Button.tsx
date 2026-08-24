import { splitProps, type ComponentProps } from "solid-js";

export type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";
export type ButtonSize = "sm" | "md";

type ButtonProps = ComponentProps<"button"> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
};

const variantClasses: Record<ButtonVariant, string> = {
  primary:
    "border-primary bg-primary text-primary-foreground hover:border-primary-hover hover:bg-primary-hover active:border-primary-active active:bg-primary-active",
  secondary:
    "border-border bg-surface text-foreground hover:border-primary hover:text-primary active:bg-primary-subtle",
  danger:
    "border-danger/35 bg-surface text-danger hover:bg-danger-surface active:bg-danger-surface",
  ghost:
    "border-transparent bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground",
};

const sizeClasses: Record<ButtonSize, string> = {
  sm: "min-h-8 px-3 py-1.5 text-xs",
  md: "min-h-10 px-4 py-2 text-sm",
};

export function Button(props: ButtonProps) {
  const [local, rest] = splitProps(props, ["children", "class", "variant", "size", "type"]);

  return (
    <button
      {...rest}
      type={local.type ?? "button"}
      class={[
        "inline-flex items-center justify-center gap-2 rounded-md border font-semibold shadow-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-50",
        variantClasses[local.variant ?? "secondary"],
        sizeClasses[local.size ?? "md"],
        local.class,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      {local.children}
    </button>
  );
}
