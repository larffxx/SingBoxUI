/**
 * Outbounds tab (spec §35, §38, §39).
 *
 * Outbounds are the only place where the profile talks to the outside world, so
 * this tab carries the two share-link flows as well: paste-links import (the
 * backend parses, the editor appends what the user confirms) and export of a
 * single outbound back into a link.
 */
import { Alert, Button } from '@/shared/ui'

import * as React from 'react'

import { ShareExportDialog, SharePasteDialog } from '@/features/share'

import { getArray, getString, getStringList, setList, setValue, type JsonObject } from '../jsonDoc'
import { OUTBOUND_TYPES } from '../resourceSpecs'
import { ResourceListEditor } from '../ResourceListEditor'
import { TagMultiSelect } from '../TagMultiSelect'
import type { DocTabProps } from './types'

/** Types whose `outbounds` field references other outbounds by tag. */
const TAG_REFERENCE_TYPES = new Set(['selector', 'urltest'])

export function OutboundsTab({ root, onChange }: DocTabProps) {
  const items = getArray(root, 'outbounds')
  const [pasteOpen, setPasteOpen] = React.useState(false)
  const [exported, setExported] = React.useState<JsonObject | null>(null)
  const [notes, setNotes] = React.useState<string[]>([])

  const tags = React.useMemo(
    () => items.map((item) => getString(item, 'tag')).filter((tag) => tag !== ''),
    [items],
  )

  const setItems = (next: JsonObject[]) => {
    onChange(setValue(root, 'outbounds', next))
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Порядок важен: sing-box использует первый подходящий outbound. Правила маршрутизации
        ссылаются на outbounds по тегу.
      </p>

      {notes.length > 0 ? (
        <Alert
          tone="warning"
          title="Импорт выполнен с замечаниями"
          actions={
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                setNotes([])
              }}
            >
              Скрыть
            </Button>
          }
          details={notes}
        >
          Добавленные outbounds ниже — проверьте теги и параметры.
        </Alert>
      ) : null}

      <ResourceListEditor
        specs={OUTBOUND_TYPES}
        items={items}
        onChange={setItems}
        idPrefix="outbounds"
        tagPrefix="out"
        addLabel="Добавить outbound"
        emptyTitle="Outbounds ещё нет"
        emptyDescription="Добавьте outbound вручную, вставьте share-ссылку или примените шаблон профиля."
        defaultType="vless"
        toolbar={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setPasteOpen(true)
            }}
          >
            Вставить share-ссылку
          </Button>
        }
        itemActions={(item) => (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            aria-label={`Экспортировать ${getString(item, 'tag')}`}
            onClick={() => {
              setExported(item)
            }}
          >
            Ссылка
          </Button>
        )}
        extraItemContent={(item, index) => {
          const type = getString(item, 'type')
          if (!TAG_REFERENCE_TYPES.has(type)) return null
          return (
            <TagMultiSelect
              id={`outbounds-${index}-refs`}
              label="Вложенные outbounds"
              hint="Порядок перебора задаётся порядком перечисленных тегов."
              values={getStringList(item, 'outbounds')}
              options={tags.filter((tag) => tag !== getString(item, 'tag'))}
              onChange={(next) => {
                setItems(
                  items.map((candidate, position) =>
                    position === index ? setList(candidate, 'outbounds', next) : candidate,
                  ),
                )
              }}
            />
          )
        }}
      />

      <SharePasteDialog
        open={pasteOpen}
        onOpenChange={setPasteOpen}
        existingTags={tags}
        onImported={(added, importNotes) => {
          setItems([...items, ...added])
          setNotes(importNotes)
        }}
      />

      <ShareExportDialog
        open={exported !== null}
        onOpenChange={(open) => {
          if (!open) setExported(null)
        }}
        outbound={exported ?? {}}
      />
    </div>
  )
}
