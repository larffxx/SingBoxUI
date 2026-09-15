/**
 * Endpoints tab (spec §35, §38).
 *
 * Endpoints are sing-box 1.11+ resources (WireGuard peers, `wg`/`tailscale`
 * style endpoints). They live in their own top-level array and are referenced by
 * outbounds through their tag.
 */
import { getArray, setValue, type JsonObject } from '../jsonDoc'
import { ENDPOINT_TYPES } from '../resourceSpecs'
import { ResourceListEditor } from '../ResourceListEditor'
import type { DocTabProps } from './types'

export function EndpointsTab({ root, onChange }: DocTabProps) {
  const items = getArray(root, 'endpoints')

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Endpoints появились в sing-box 1.11: они описывают WireGuard-соединения отдельно от
        outbounds, а outbound с типом «wireguard» ссылается на endpoint по тегу.
      </p>

      <ResourceListEditor
        specs={ENDPOINT_TYPES}
        items={items}
        onChange={(next: JsonObject[]) => {
          onChange(setValue(root, 'endpoints', next))
        }}
        idPrefix="endpoints"
        tagPrefix="ep"
        addLabel="Добавить endpoint"
        emptyTitle="Endpoints ещё нет"
        emptyDescription="Если профиль не использует WireGuard 1.11+, этот раздел можно оставить пустым."
        defaultType="wireguard"
      />
    </div>
  )
}
