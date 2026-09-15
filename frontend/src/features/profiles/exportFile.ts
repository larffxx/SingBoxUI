/**
 * Profile export to a file on disk (spec §31).
 *
 * The backend hands back the config *content* (`profilesApi.exportProfile`), not
 * a written file: there is no filesystem binding in the transport layer (spec
 * §32 keeps the UI on Wails bindings only, and no file-write binding exists
 * yet). So the browser layer writes the blob through a download, which the
 * webview routes to the OS save dialog once native file saving is available.
 */
import type { profiles } from '@/shared/api/bindings'

export interface ExportedFile {
  fileName: string
  revisionId: string
  bytes: number
}

/** Triggers the download for an export payload and reports what was written. */
export function saveExport(exported: profiles.ExportResult): ExportedFile {
  const json = exported.configJson ?? ''
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = exported.fileName || 'singbox-profile.json'
  anchor.rel = 'noopener'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  // Revoke on the next tick: revoking synchronously can cancel the download in
  // some webviews.
  window.setTimeout(() => {
    URL.revokeObjectURL(url)
  }, 0)
  return {
    fileName: anchor.download,
    revisionId: exported.revisionId,
    bytes: blob.size,
  }
}

/** Copies text to the clipboard, reporting whether the browser allowed it. */
export async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value)
    return true
  } catch {
    return false
  }
}
