/**
 * Experimental tab (spec §35).
 *
 * `experimental` is where sing-box hides its control surfaces: the on-disk cache,
 * the Clash API dashboard and the V2Ray stats API. Every block stays off unless
 * the user turns it on, because all three open a local port or write to disk.
 */
import { Alert, Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/shared/ui'

import { getBoolean, getObject, getString, setObject, type JsonObject } from '../jsonDoc'
import { SpecFields } from '../fields'
import { EXPERIMENTAL_FIELDS } from './experimentalFields'
import type { DocTabProps } from './types'

export function ExperimentalTab({ root, onChange }: DocTabProps) {
  const experimental = getObject(root, 'experimental')
  const clashApi = getObject(experimental, 'clash_api')

  const setExperimental = (next: JsonObject) => {
    onChange(setObject(root, 'experimental', next))
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Экспериментальные возможности</CardTitle>
          <CardDescription>
            Все блоки необязательны: выключенный блок полностью удаляется из конфигурации.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          <SpecFields
            specs={EXPERIMENTAL_FIELDS}
            parent={experimental}
            idPrefix="experimental"
            onChange={setExperimental}
          />
        </CardContent>
      </Card>

      {getBoolean(clashApi, 'enabled') && getString(clashApi, 'secret') === '' ? (
        <Alert tone="warning" title="Clash API включён без секрета">
          Любое локальное приложение сможет управлять маршрутизацией через{' '}
          <code>{getString(clashApi, 'external_controller') || '127.0.0.1:9090'}</code>. Задайте
          секрет, если на машине есть недоверенные процессы.
        </Alert>
      ) : null}

      <Alert tone="info" title="Изменения применяются при следующем запуске">
        Рантайм перечитывает эти параметры только при применении конфигурации.
      </Alert>
    </div>
  )
}
