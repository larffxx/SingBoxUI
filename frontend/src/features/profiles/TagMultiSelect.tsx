/**
 * Multi-select over resource tags (spec §35).
 *
 * `selector`, `urltest`, `route.rules[].outbound` and `route.rules[].rule_set`
 * all reference other entries *by tag*. A plain text list would let the user
 * invent a tag that does not exist, so those references get a picker built from
 * the tags that are actually declared in the document.
 */
import { Button } from '@/shared/ui'

export function TagMultiSelect({
  id,
  label,
  hint,
  values,
  options,
  onChange,
  emptyLabel = 'Нет доступных тегов',
}: {
  id: string
  label: string
  hint?: string
  values: string[]
  options: string[]
  onChange: (next: string[]) => void
  emptyLabel?: string
}) {
  const missing = values.filter((value) => !options.includes(value))

  const toggle = (tag: string) => {
    onChange(values.includes(tag) ? values.filter((value) => value !== tag) : [...values, tag])
  }

  return (
    <div className="space-y-1.5">
      <p className="text-sm font-medium leading-none" id={id}>
        {label}
      </p>
      {options.length === 0 ? (
        <p className="text-xs text-muted-foreground">{emptyLabel}</p>
      ) : (
        <div className="flex flex-wrap gap-1.5" role="group" aria-labelledby={id}>
          {options.map((tag) => {
            const active = values.includes(tag)
            return (
              <Button
                key={tag}
                type="button"
                size="sm"
                variant={active ? 'default' : 'outline'}
                aria-pressed={active}
                className="font-mono text-xs"
                onClick={() => {
                  toggle(tag)
                }}
              >
                {tag}
              </Button>
            )
          })}
        </div>
      )}
      {missing.length > 0 ? (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          В документе нет тега {missing.map((tag) => `«${tag}»`).join(', ')} — ссылка останется как
          введена.
        </p>
      ) : null}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}
