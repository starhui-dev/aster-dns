import { For, Match, Show, Switch, createMemo } from "solid-js";

import { Alert, Field } from "./ui/Layout";
import { SelectField } from "./ui/Select";
import type {
  DescriptorField,
  ExtensionContainer,
  ExtensionFieldDescriptor,
  ExtensionScope,
  ExtensionValue,
} from "../lib/dns";

export interface FieldValues {
  [key: string]: ExtensionValue | undefined;
}

export function DescriptorFields(props: {
  fields: DescriptorField[];
  values: FieldValues;
  idPrefix: string;
  disabled?: boolean | undefined;
  onChange: (key: string, value: ExtensionValue | undefined) => void;
}) {
  return (
    <div class="grid gap-4 sm:grid-cols-2">
      <For each={props.fields}>
        {(field) => (
          <DescriptorInput
            field={field}
            id={`${props.idPrefix}-${field.key}`}
            value={props.values[field.key]}
            disabled={props.disabled}
            onChange={(value) => props.onChange(field.key, value)}
          />
        )}
      </For>
    </div>
  );
}

export function ExtensionFields(props: {
  descriptors: ExtensionFieldDescriptor[];
  scope: ExtensionScope;
  recordType: string;
  values: FieldValues;
  idPrefix: string;
  includeReadOnly?: boolean | undefined;
  onChange: (key: string, value: ExtensionValue | undefined) => void;
}) {
  const fields = () =>
    props.descriptors.filter(
      (field) =>
        field.scope === props.scope &&
        (props.includeReadOnly || !field.read_only) &&
        descriptorApplies(field, props.recordType),
    );
  return (
    <Show when={fields().length > 0}>
      <div class="grid gap-4 border-t border-border pt-4 sm:grid-cols-2">
        <For each={fields()}>
          {(field) => {
            const key = extensionValueKey(field.namespace, field.key);
            return (
              <DescriptorInput
                field={{
                  key,
                  label: field.label,
                  type: field.type,
                  secret: false,
                  required: field.required,
                  options: field.options,
                  minimum: field.minimum,
                  maximum: field.maximum,
                }}
                id={`${props.idPrefix}-${field.namespace}-${field.key}`}
                value={props.values[key]}
                disabled={field.read_only}
                onChange={(value) => props.onChange(key, value)}
              />
            );
          }}
        </For>
      </div>
    </Show>
  );
}

export function ExtensionSummary(props: {
  descriptors: ExtensionFieldDescriptor[];
  scope: ExtensionScope;
  recordType: string;
  extensions?: ExtensionContainer | undefined;
}) {
  const values = () =>
    props.descriptors
      .filter((field) => field.scope === props.scope && descriptorApplies(field, props.recordType))
      .map((field) => ({ field, value: props.extensions?.[field.namespace]?.[field.key] }))
      .filter((item) => item.value !== undefined && item.value !== "" && item.value !== null);
  return (
    <Show when={values().length > 0}>
      <dl class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <For each={values()}>
          {(item) => (
            <div class="flex gap-1">
              <dt>{item.field.label}:</dt>
              <dd class="font-medium text-foreground">{displayExtensionValue(item.value)}</dd>
            </div>
          )}
        </For>
      </dl>
    </Show>
  );
}

export function extensionValuesFromContainer(
  descriptors: ExtensionFieldDescriptor[],
  scope: ExtensionScope,
  extensions?: ExtensionContainer,
): FieldValues {
  const values: FieldValues = {};
  for (const field of descriptors) {
    if (field.scope !== scope) continue;
    const value = extensions?.[field.namespace]?.[field.key];
    if (value !== undefined) values[extensionValueKey(field.namespace, field.key)] = value;
  }
  return values;
}

export function extensionContainerFromValues(
  descriptors: ExtensionFieldDescriptor[],
  scope: ExtensionScope,
  recordType: string,
  values: FieldValues,
): ExtensionContainer | undefined {
  const extensions: ExtensionContainer = {};
  for (const field of descriptors) {
    if (field.scope !== scope || field.read_only || !descriptorApplies(field, recordType)) continue;
    const value = values[extensionValueKey(field.namespace, field.key)];
    if (value === undefined || value === "") continue;
    const namespace = extensions[field.namespace] ?? {};
    namespace[field.key] = value;
    extensions[field.namespace] = namespace;
  }
  return Object.keys(extensions).length === 0 ? undefined : extensions;
}

function DescriptorInput(props: {
  field: DescriptorField;
  id: string;
  value: ExtensionValue | undefined;
  disabled?: boolean | undefined;
  onChange: (value: ExtensionValue | undefined) => void;
}) {
  const hint = createMemo(() => props.field.description);
  const describedBy = createMemo(() => (hint() ? `${props.id}-hint` : undefined));
  return (
    <Show
      when={props.field.secret && props.field.type !== "string"}
      fallback={
        <Field label={props.field.label} for={props.id} hint={hint()}>
          <Switch>
            <Match when={props.field.type === "boolean"}>
              <label class="flex min-h-10 items-center gap-3 rounded-md border border-input bg-surface px-3 text-sm">
                <input
                  id={props.id}
                  type="checkbox"
                  aria-describedby={describedBy()}
                  checked={props.value === true}
                  disabled={props.disabled}
                  onChange={(event) => props.onChange(event.currentTarget.checked)}
                />
                <span>{props.value === true ? "Enabled" : "Disabled"}</span>
              </label>
            </Match>
            <Match when={props.field.type === "enum"}>
              <SelectField
                id={props.id}
                value={typeof props.value === "string" ? props.value : ""}
                options={[
                  { value: "", label: "Select…" },
                  ...(props.field.options ?? []).map((option) => ({
                    value: option.value,
                    label: option.label,
                  })),
                ]}
                describedBy={describedBy()}
                required={props.field.required}
                disabled={props.disabled}
                onChange={(value) => props.onChange(value || undefined)}
              />
            </Match>
            <Match when={props.field.type === "string_list"}>
              <textarea
                id={props.id}
                class="text-input min-h-24"
                aria-describedby={describedBy()}
                required={props.field.required}
                disabled={props.disabled}
                value={Array.isArray(props.value) ? props.value.join("\n") : ""}
                placeholder="One value per line"
                onInput={(event) =>
                  props.onChange(
                    event.currentTarget.value
                      .split("\n")
                      .map((value) => value.trim())
                      .filter(Boolean),
                  )
                }
              />
            </Match>
            <Match when={props.field.type === "integer"}>
              <input
                id={props.id}
                class="text-input"
                type="number"
                aria-describedby={describedBy()}
                required={props.field.required}
                disabled={props.disabled}
                min={props.field.minimum}
                max={props.field.maximum}
                value={typeof props.value === "number" ? props.value : ""}
                onInput={(event) =>
                  props.onChange(
                    event.currentTarget.value === ""
                      ? undefined
                      : Number(event.currentTarget.value),
                  )
                }
              />
            </Match>
            <Match when={true}>
              <input
                id={props.id}
                class="text-input"
                type={props.field.secret ? "password" : "text"}
                aria-describedby={describedBy()}
                required={props.field.required}
                disabled={props.disabled}
                autocomplete={props.field.secret ? "new-password" : undefined}
                value={typeof props.value === "string" ? props.value : ""}
                placeholder={props.field.placeholder}
                onInput={(event) => props.onChange(event.currentTarget.value || undefined)}
              />
            </Match>
          </Switch>
        </Field>
      }
    >
      <Alert variant="danger" role="alert">
        Unsupported secret field schema for {props.field.label}. Contact an administrator.
      </Alert>
    </Show>
  );
}

function descriptorApplies(field: ExtensionFieldDescriptor, recordType: string): boolean {
  return (field.applicable_when ?? []).every(
    (condition) => condition.field !== "type" || condition.values.includes(recordType),
  );
}

function extensionValueKey(namespace: string, key: string): string {
  return `${namespace}.${key}`;
}

function displayExtensionValue(value: ExtensionValue | undefined): string {
  if (Array.isArray(value)) return value.join(", ");
  if (typeof value === "boolean") return value ? "Yes" : "No";
  return String(value ?? "");
}
