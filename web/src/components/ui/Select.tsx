import { Select as KobalteSelect, type SelectRootItemComponentProps } from "@kobalte/core/select";
import { Check, ChevronDown } from "lucide-solid";
import { Show, createMemo, type Component, type JSX } from "solid-js";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
  icon?: JSX.Element | undefined;
}

export function SelectField(props: {
  id: string;
  label?: string | undefined;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  hint?: string | undefined;
  describedBy?: string | undefined;
  required?: boolean | undefined;
  disabled?: boolean | undefined;
  name?: string | undefined;
  class?: string | undefined;
  triggerClass?: string | undefined;
  labelClass?: string | undefined;
  icon?: JSX.Element | undefined;
}) {
  const selectedOption = createMemo(() =>
    props.options.find((option) => option.value === props.value),
  );

  const itemComponent: Component<SelectRootItemComponentProps<SelectOption>> = (itemProps) => (
    <KobalteSelect.Item
      item={itemProps.item}
      class="flex cursor-default items-center justify-between gap-3 rounded-md px-3 py-2 text-sm text-foreground outline-none data-[highlighted]:bg-muted data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
    >
      <div class="flex min-w-0 items-center gap-2">
        <Show when={itemProps.item.rawValue.icon}>{itemProps.item.rawValue.icon}</Show>
        <KobalteSelect.ItemLabel>{itemProps.item.rawValue.label}</KobalteSelect.ItemLabel>
      </div>
      <KobalteSelect.ItemIndicator>
        <Check size={16} strokeWidth={2} aria-hidden="true" />
      </KobalteSelect.ItemIndicator>
    </KobalteSelect.Item>
  );

  return (
    <KobalteSelect<SelectOption>
      id={`${props.id}-root`}
      class={props.class ?? ""}
      options={props.options}
      optionValue="value"
      optionTextValue="label"
      optionDisabled="disabled"
      multiple={false}
      value={selectedOption() ?? null}
      onChange={(value) => props.onChange(value?.value ?? "")}
      required={props.required ?? false}
      disabled={props.disabled ?? false}
      name={props.name ?? ""}
      itemComponent={itemComponent}
    >
      <Show when={props.label !== undefined}>
        <KobalteSelect.Label
          class={props.labelClass ?? "mb-1.5 block text-sm font-medium text-foreground"}
        >
          {props.label}
        </KobalteSelect.Label>
      </Show>
      <KobalteSelect.HiddenSelect
        id={`${props.id}-native`}
        onChange={(event) => props.onChange(event.currentTarget.value)}
      />
      <KobalteSelect.Trigger
        id={props.id}
        aria-describedby={props.describedBy}
        class={[
          "flex min-h-10 w-full items-center justify-between gap-2 rounded-md border border-input bg-surface px-3 py-2 text-left text-sm text-foreground shadow-sm outline-none transition-colors hover:border-primary focus:border-primary focus:ring-2 focus:ring-focus/20 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground data-[expanded]:border-primary",
          props.triggerClass,
        ]
          .filter(Boolean)
          .join(" ")}
      >
        <Show when={props.icon}>{props.icon}</Show>
        <Show when={selectedOption()?.icon}>{selectedOption()?.icon}</Show>
        <KobalteSelect.Value<SelectOption>>
          {(state) => state.selectedOption()?.label ?? ""}
        </KobalteSelect.Value>
        <KobalteSelect.Icon>
          <ChevronDown size={16} strokeWidth={1.8} aria-hidden="true" />
        </KobalteSelect.Icon>
      </KobalteSelect.Trigger>
      {props.hint !== undefined && (
        <KobalteSelect.Description
          id={`${props.id}-hint`}
          class="mt-1.5 text-xs leading-5 text-muted-foreground"
        >
          {props.hint}
        </KobalteSelect.Description>
      )}
      <KobalteSelect.Portal>
        <KobalteSelect.Content class="z-[60] min-w-[var(--kb-popper-anchor-width)] overflow-hidden rounded-md border border-border bg-surface p-1 text-foreground shadow-xl">
          <KobalteSelect.Listbox class="max-h-72 overflow-y-auto outline-none" />
        </KobalteSelect.Content>
      </KobalteSelect.Portal>
    </KobalteSelect>
  );
}
