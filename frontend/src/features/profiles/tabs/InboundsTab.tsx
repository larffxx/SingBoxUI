/**
 * Inbounds tab (spec §35, §38).
 *
 * Inbounds describe what sing-box listens on. TUN needs elevated privileges —
 * that is called out here because the failure mode is a confusing
 * `PRIVILEGE_DENIED` later in the runtime.
 */
import { Alert } from '@/shared/ui'

import { getArray, getString, setValue, type JsonObject } from '../jsonDoc'
import { INBOUND_TYPES } from '../resourceSpecs'
import { ResourceListEditor } from '../ResourceListEditor'
import type { DocTabProps } from './types'

export function InboundsTab({ root, onChange }: DocTabProps) {
  const items = getArray(root, 'inbounds')
  const tunPresent = items.some((item) => getString(item, 'type') === 'tun')

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Входящие подключения. Без inbound-ов профиль работает только как клиент с TUN-ом, заданным
        отдельно.
      </p>

      {tunPresent ? (
        <Alert tone="warning" title="TUN требует прав администратора">
          При запуске приложение запросит повышение прав. Если запрос отклонить, рантайм вернёт
          ошибку PRIVILEGE_DENIED — сам профиль останется корректным.
        </Alert>
      ) : null}

      <ResourceListEditor
        specs={INBOUND_TYPES}
        items={items}
        onChange={(next: JsonObject[]) => {
          onChange(setValue(root, 'inbounds', next))
        }}
        idPrefix="inbounds"
        tagPrefix="in"
        addLabel="Добавить inbound"
        emptyTitle="Inbounds ещё нет"
        emptyDescription="Добавьте TUN для прозрачного проксирования или локальный SOCKS/HTTP-порт для отладки."
        defaultType="tun"
      />
    </div>
  )
}
