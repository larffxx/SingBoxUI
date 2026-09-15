/**
 * Spec-driven controls for the structured editors (spec §35, §38).
 *
 * `resourceSpecs.ts` declares the fields; this module renders them. Values are
 * read from and written straight into the configuration document, so one
 * component tree serves outbounds, inbounds, endpoints and the TLS / transport
 * / multiplex sub-forms.
 *
 * Two kinds of control exist here:
 *
 * - fields whose value round-trips unchanged (text, select, boolean, nested)
 *   write on every keystroke, because the document is the single source of
 *   truth;
 * - lossy fields (multi-line lists, key/value pairs, numbers) keep a local
 *   draft so that a half-typed line is not trimmed away under the cursor, and
 *   commit the parsed value on each change.
 */
import * as React from 'react'

import { Button, Field, Input, Select, SwitchField, Textarea } from '@/shared/ui'
import type { SelectOption } from '@/shared/ui'

import {
  getBoolean,
  getNumber,
  getObject,
  getString,
  joinLines,
  listToPairs,
  pairsToList,
  setList,
  setObject,
  setValue,
  splitLines,
} from './jsonDoc'
import type { JsonObject } from './jsonDoc'
import type { FieldSpec, SubFormSpec } from './resourceSpecs'

/** Radix Select rejects an empty item value, so "not set" uses this sentinel. */
const UNSET = '__singboxui_unset__'

function toSelectOptions(spec: FieldSpec): SelectOption[] {
  return (spec.options ?? []).map((option) => ({
    value: option.value === '' ? UNSET : option.value,
    label: option.label,
  }))
}

function fromSelectValue(value: string): string {
  return value === UNSET ? '' : value
}

function hintProps(spec: FieldSpec): { hint?: string } {
  return spec.hint === undefined ? {} : { hint: spec.hint }
}

interface ControlProps {
  spec: FieldSpec
  parent: JsonObject
  onChange: (next: JsonObject) => void
  id: string
}

/** TextControl is the identity round-trip case: commit on every keystroke. */
function TextControl({ spec, parent, onChange, id }: ControlProps) {
  return (
    <Field label={spec.label} htmlFor={id} {...hintProps(spec)}>
      <Input
        id={id}
        value={getString(parent, spec.key)}
        placeholder={spec.placeholder ?? ''}
        spellCheck={false}
        autoComplete="off"
        className="font-mono text-xs"
        onChange={(event) => {
          onChange(setValue(parent, spec.key, event.target.value))
        }}
      />
    </Field>
  )
}

/** NumberControl keeps the raw text so "1." survives until it becomes "1.5". */
function NumberControl({ spec, parent, onChange, id }: ControlProps) {
  const external = getNumber(parent, spec.key)
  const asText = external === undefined ? '' : String(external)
  const [draft, setDraft] = React.useState(asText)
  const seen = React.useRef(asText)

  React.useEffect(() => {
    if (seen.current !== asText) {
      seen.current = asText
      setDraft(asText)
    }
  }, [asText])

  return (
    <Field label={spec.label} htmlFor={id} {...hintProps(spec)}>
      <Input
        id={id}
        inputMode="numeric"
        value={draft}
        placeholder={spec.placeholder ?? ''}
        className="font-mono text-xs"
        onChange={(event) => {
          const text = event.target.value
          setDraft(text)
          const parsed = text.trim() === '' ? undefined : Number(text)
          const next = parsed !== undefined && Number.isFinite(parsed) ? parsed : undefined
          seen.current = next === undefined ? '' : String(next)
          onChange(setValue(parent, spec.key, next))
        }}
      />
    </Field>
  )
}

/** SecretControl hides credentials until the user asks to see them. */
function SecretControl({ spec, parent, onChange, id }: ControlProps) {
  const [revealed, setRevealed] = React.useState(false)
  return (
    <Field label={spec.label} htmlFor={id} {...hintProps(spec)}>
      <div className="flex gap-2">
        <Input
          id={id}
          type={revealed ? 'text' : 'password'}
          value={getString(parent, spec.key)}
          placeholder={spec.placeholder ?? ''}
          spellCheck={false}
          autoComplete="off"
          className="font-mono text-xs"
          onChange={(event) => {
            onChange(setValue(parent, spec.key, event.target.value))
          }}
        />
        <Button
          type="button"
          size="sm"
          variant="outline"
          aria-pressed={revealed}
          onClick={() => {
            setRevealed((value) => !value)
          }}
        >
          {revealed ? 'Скрыть' : 'Показать'}
        </Button>
      </div>
    </Field>
  )
}

function SelectControl({ spec, parent, onChange, id }: ControlProps) {
  const value = getString(parent, spec.key)
  return (
    <Field label={spec.label} htmlFor={id} {...hintProps(spec)}>
      <Select
        id={id}
        value={value === '' ? UNSET : value}
        options={toSelectOptions(spec)}
        placeholder={spec.placeholder ?? '— не задано —'}
        onValueChange={(next) => {
          onChange(setValue(parent, spec.key, fromSelectValue(next)))
        }}
      />
    </Field>
  )
}

function BooleanControl({ spec, parent, onChange, id }: ControlProps) {
  return (
    <SwitchField
      id={id}
      label={spec.label}
      checked={getBoolean(parent, spec.key)}
      {...(spec.hint === undefined ? {} : { description: spec.hint })}
      onCheckedChange={(checked) => {
        onChange(setValue(parent, spec.key, checked ? true : undefined))
      }}
    />
  )
}

/** LinesControl serves both string lists and `Key: value` maps. */
function LinesControl({ spec, parent, onChange, id }: ControlProps) {
  const isPairs = spec.kind === 'pairs'
  const raw = parent[spec.key]
  const external = (isPairs ? pairsToList(raw) : splitLines(raw)).join('\n')
  const [draft, setDraft] = React.useState(external)
  const seen = React.useRef(external)

  React.useEffect(() => {
    if (seen.current !== external) {
      seen.current = external
      setDraft(external)
    }
  }, [external])

  return (
    <Field label={spec.label} htmlFor={id} {...hintProps(spec)}>
      <Textarea
        id={id}
        rows={isPairs ? 3 : 4}
        value={draft}
        placeholder={spec.placeholder ?? ''}
        spellCheck={false}
        className="font-mono text-xs"
        onChange={(event) => {
          const text = event.target.value
          setDraft(text)
          if (isPairs) {
            const pairs = listToPairs(splitLines(text))
            seen.current = pairsToList(pairs).join('\n')
            onChange(setObject(parent, spec.key, pairs))
          } else {
            const lines = joinLines(text)
            seen.current = lines.join('\n')
            onChange(setList(parent, spec.key, lines))
          }
        }}
      />
    </Field>
  )
}

/** NestedControl renders a fixed sub-object (for example `peer` or `utls`). */
function NestedControl({ spec, parent, onChange, id }: ControlProps) {
  const value = getObject(parent, spec.key)
  return (
    <div className="space-y-3 rounded-md border border-border/70 bg-muted/20 p-3">
      <p className="text-xs font-medium text-muted-foreground">{spec.label}</p>
      {spec.hint === undefined ? null : (
        <p className="text-xs text-muted-foreground">{spec.hint}</p>
      )}
      <SpecFields
        specs={spec.fields ?? []}
        parent={value}
        idPrefix={id}
        onChange={(next) => {
          onChange(setObject(parent, spec.key, next))
        }}
      />
    </div>
  )
}

/** FieldControl dispatches one spec entry to its control. */
export function FieldControl(props: ControlProps) {
  const { spec } = props
  switch (spec.kind) {
    case 'text':
      return <TextControl {...props} />
    case 'number':
      return <NumberControl {...props} />
    case 'secret':
      return <SecretControl {...props} />
    case 'select':
      return <SelectControl {...props} />
    case 'boolean':
      return <BooleanControl {...props} />
    case 'list':
    case 'pairs':
      return <LinesControl {...props} />
    case 'nested':
      return <NestedControl {...props} />
  }
}

/** SpecFields renders a whole field table against one JSON object. */
export function SpecFields({
  specs,
  parent,
  onChange,
  idPrefix,
}: {
  specs: FieldSpec[]
  parent: JsonObject
  onChange: (next: JsonObject) => void
  idPrefix: string
}) {
  return (
    <div className="space-y-3">
      {specs.map((spec) => (
        <FieldControl
          key={spec.key}
          spec={spec}
          parent={parent}
          onChange={onChange}
          id={`${idPrefix}-${spec.key}`}
        />
      ))}
    </div>
  )
}

function newSubFormValue(form: SubFormSpec): JsonObject {
  switch (form.key) {
    case 'transport':
      return { type: 'ws', path: '/ws' }
    case 'multiplex':
      return { enabled: true, protocol: 'smux' }
    default:
      return { enabled: true }
  }
}

/** SubFormSection renders TLS / transport / multiplex for one resource. */
export function SubFormSection({
  form,
  parent,
  onChange,
  idPrefix,
}: {
  form: SubFormSpec
  parent: JsonObject
  onChange: (next: JsonObject) => void
  idPrefix: string
}) {
  const value = getObject(parent, form.key)
  const present = Object.keys(value).length > 0

  return (
    <section className="space-y-3 rounded-md border border-border/70 p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="space-y-0.5">
          <h4 className="text-sm font-medium">{form.label}</h4>
          <p className="text-xs text-muted-foreground">{form.description}</p>
        </div>
        {present ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              onChange(setObject(parent, form.key, {}))
            }}
          >
            Очистить
          </Button>
        ) : (
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => {
              onChange(setObject(parent, form.key, newSubFormValue(form)))
            }}
          >
            Включить
          </Button>
        )}
      </div>
      {present ? (
        <SpecFields
          specs={form.fields}
          parent={value}
          idPrefix={`${idPrefix}-${form.key}`}
          onChange={(next) => {
            onChange(setObject(parent, form.key, next))
          }}
        />
      ) : (
        <p className="text-xs text-muted-foreground">Раздел не задан.</p>
      )}
    </section>
  )
}
