/**
 * Generic list editor for sing-box collections (spec §35, §38).
 *
 * Outbounds, inbounds and endpoints all follow the same shape — an array of
 * tagged objects whose body depends on `type` — so this component renders the
 * list, the add/duplicate/delete/reorder actions and, for the selected entry,
 * the per-type dynamic fields plus its TLS / transport / multiplex sub-forms.
 *
 * The component is controlled: it never keeps a copy of the configuration, it
 * only reports the next array through `onChange`.
 */
import * as React from 'react'

import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  ConfirmDialog,
  EmptyState,
  Select,
} from '@/shared/ui'

import { SpecFields, SubFormSection } from './fields'
import { cloneDoc, getString } from './jsonDoc'
import type { JsonObject } from './jsonDoc'
import { findTypeSpec, subFormsFor } from './resourceSpecs'
import type { ResourceTypeSpec } from './resourceSpecs'
import { uniqueTag } from './tags'

export function ResourceListEditor({
  specs,
  items,
  onChange,
  idPrefix,
  tagPrefix,
  addLabel,
  emptyTitle,
  emptyDescription,
  defaultType,
  toolbar,
  itemActions,
  extraItemContent,
}: {
  specs: ResourceTypeSpec[]
  items: JsonObject[]
  onChange: (next: JsonObject[]) => void
  idPrefix: string
  /** Tag prefix for new entries, for example `out` or `in`. */
  tagPrefix: string
  addLabel: string
  emptyTitle: string
  emptyDescription: string
  defaultType?: string
  /** Extra toolbar content, for example the share-link import button. */
  toolbar?: React.ReactNode
  itemActions?: (item: JsonObject, index: number) => React.ReactNode
  /** Extra content rendered inside the expanded card, below the sub-forms. */
  extraItemContent?: (item: JsonObject, index: number) => React.ReactNode
}) {
  const [newType, setNewType] = React.useState(defaultType ?? specs[0]?.type ?? '')
  const [openIndex, setOpenIndex] = React.useState<number | null>(null)
  const [pendingRemove, setPendingRemove] = React.useState<number | null>(null)

  const replace = React.useCallback(
    (index: number, next: JsonObject) => {
      onChange(items.map((item, position) => (position === index ? next : item)))
    },
    [items, onChange],
  )

  const tags = React.useMemo(() => items.map((item) => getString(item, 'tag')), [items])

  const add = () => {
    const spec = findTypeSpec(specs, newType) ?? specs[0]
    if (!spec) return
    const tag = uniqueTag(tags, `${tagPrefix}-${spec.type}`)
    onChange([...items, { ...cloneDoc(spec.defaults), type: spec.type, tag }])
    setOpenIndex(items.length)
  }

  const duplicate = (index: number) => {
    const item = items[index]
    if (!item) return
    const copy = cloneDoc(item)
    const base = getString(item, 'tag')
    const tag = uniqueTag(tags, `${base === '' ? tagPrefix : base}-copy`)
    onChange([...items.slice(0, index + 1), { ...copy, tag }, ...items.slice(index + 1)])
    setOpenIndex(index + 1)
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (index < 0 || index >= items.length || target < 0 || target >= items.length) return
    const next = [...items]
    const current = next[index]
    const swapped = next[target]
    if (!current || !swapped) return
    next[index] = swapped
    next[target] = current
    onChange(next)
    setOpenIndex(target)
  }

  const remove = () => {
    if (pendingRemove === null) return
    onChange(items.filter((_item, position) => position !== pendingRemove))
    setPendingRemove(null)
  }

  const removeTag = pendingRemove === null ? '' : getString(items[pendingRemove] ?? {}, 'tag')

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div className="flex items-end gap-2">
          <Select
            id={`${idPrefix}-new-type`}
            value={newType}
            options={specs.map((spec) => ({ value: spec.type, label: spec.label }))}
            placeholder="Тип"
            onValueChange={setNewType}
          />
          <Button type="button" size="sm" onClick={add}>
            {addLabel}
          </Button>
        </div>
        {toolbar ? <div className="flex flex-wrap items-center gap-2">{toolbar}</div> : null}
      </div>

      {items.length === 0 ? (
        <EmptyState title={emptyTitle} description={emptyDescription} />
      ) : (
        <ul className="space-y-3">
          {items.map((item, index) => {
            const type = getString(item, 'type')
            const tag = getString(item, 'tag')
            const spec = findTypeSpec(specs, type)
            const open = openIndex === index
            return (
              // Keyed by position and type only: the tag is edited *inside*
              // this subtree, so including it would remount the card (and the
              // focused input) on every keystroke, dropping all but the first
              // character the user types.
              <li key={`${index}-${type}`}>
                <Card>
                  <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
                    <div className="flex items-center gap-2">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-expanded={open}
                        aria-label={`Развернуть ${tag}`}
                        onClick={() => {
                          setOpenIndex(open ? null : index)
                        }}
                      >
                        <span aria-hidden>{open ? '▾' : '▸'}</span>
                      </Button>
                      <span className="text-xs text-muted-foreground">#{index + 1}</span>
                      <span className="font-mono text-xs">{tag === '' ? '(без тега)' : tag}</span>
                      <Badge tone="info">{type === '' ? 'без типа' : type}</Badge>
                    </div>
                    <div className="flex flex-wrap items-center gap-1">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Поднять ${tag}`}
                        disabled={index === 0}
                        onClick={() => {
                          move(index, -1)
                        }}
                      >
                        ↑
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Опустить ${tag}`}
                        disabled={index === items.length - 1}
                        onClick={() => {
                          move(index, 1)
                        }}
                      >
                        ↓
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          duplicate(index)
                        }}
                      >
                        Дублировать
                      </Button>
                      {itemActions?.(item, index)}
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setPendingRemove(index)
                        }}
                      >
                        Удалить
                      </Button>
                    </div>
                  </CardHeader>
                  {open ? (
                    <CardContent className="space-y-3">
                      {spec ? (
                        <>
                          <Select
                            id={`${idPrefix}-${index}-type`}
                            value={type}
                            options={specs.map((option) => ({
                              value: option.type,
                              label: option.label,
                            }))}
                            placeholder="Тип"
                            onValueChange={(nextType) => {
                              const nextSpec = findTypeSpec(specs, nextType)
                              if (!nextSpec) return
                              const kept: JsonObject = {
                                ...cloneDoc(nextSpec.defaults),
                                type: nextType,
                              }
                              if (tag !== '') kept['tag'] = tag
                              replace(index, kept)
                            }}
                          />
                          <SpecFields
                            specs={spec.fields}
                            parent={item}
                            idPrefix={`${idPrefix}-${index}`}
                            onChange={(next) => {
                              replace(index, next)
                            }}
                          />
                          {subFormsFor(spec).map((form) => (
                            <SubFormSection
                              key={form.key}
                              form={form}
                              parent={item}
                              idPrefix={`${idPrefix}-${index}`}
                              onChange={(next) => {
                                replace(index, next)
                              }}
                            />
                          ))}
                          {extraItemContent?.(item, index)}
                        </>
                      ) : (
                        <p className="text-xs text-muted-foreground">
                          Тип «{type}» неизвестен этому редактору — откройте вкладку «Raw JSON».
                        </p>
                      )}
                    </CardContent>
                  ) : null}
                </Card>
              </li>
            )
          })}
        </ul>
      )}

      <ConfirmDialog
        open={pendingRemove !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRemove(null)
        }}
        title="Удалить элемент?"
        description={`Элемент «${removeTag}» будет удалён из конфигурации. Изменение попадёт в ревизию только после сохранения.`}
        confirmLabel="Удалить"
        destructive
        onConfirm={remove}
      />
    </div>
  )
}
