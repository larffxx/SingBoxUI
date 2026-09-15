/**
 * Traffic sparkline.
 *
 * A dependency-free SVG line chart over the bounded history kept by the runtime
 * screen. Cumulative totals decline to plot as rates, so both series are drawn
 * from the same normalised baseline.
 */
import * as React from 'react'

import { cn } from '@/shared/lib/cn'
import { formatBytes } from '@/shared/lib/format'

export interface TrafficSparklineProps {
  /** Cumulative byte totals, oldest first. */
  samples: readonly { download: number; upload: number }[]
  /** Height in px; the viewBox keeps the aspect ratio. */
  height?: number
  className?: string
  ariaLabel?: string
}

const VIEW_WIDTH = 240

function linePath(values: readonly number[], max: number, height: number): string {
  const step = values.length > 1 ? VIEW_WIDTH / (values.length - 1) : VIEW_WIDTH
  return values
    .map((value, index) => {
      const x = values.length > 1 ? index * step : VIEW_WIDTH
      const y = height - (max <= 0 ? 0 : (value / max) * height)
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
}

export function TrafficSparkline({
  samples,
  height = 56,
  className,
  ariaLabel = 'Динамика трафика',
}: TrafficSparklineProps): React.ReactElement {
  const downloads = samples.map((sample) => sample.download)
  const uploads = samples.map((sample) => sample.upload)
  const max = Math.max(1, ...downloads, ...uploads)
  const latest = samples.at(-1)

  return (
    <div className={cn('space-y-1', className)} data-testid="traffic-sparkline">
      <svg
        role="img"
        aria-label={ariaLabel}
        viewBox={`0 0 ${VIEW_WIDTH} ${height}`}
        preserveAspectRatio="none"
        className="h-14 w-full rounded-md border border-border bg-muted/30"
      >
        {samples.length > 0 ? (
          <>
            <path
              d={linePath(uploads, max, height)}
              fill="none"
              strokeWidth="1.5"
              className="stroke-muted-foreground/70"
              data-series="upload"
            />
            <path
              d={linePath(downloads, max, height)}
              fill="none"
              strokeWidth="1.5"
              className="stroke-primary"
              data-series="download"
            />
          </>
        ) : null}
      </svg>
      <p className="text-xs text-muted-foreground">
        Пик за период: {formatBytes(max === 1 && samples.length === 0 ? undefined : max)} · отдача{' '}
        <span className="text-foreground">{formatBytes(latest?.upload)}</span> · приём{' '}
        <span className="text-foreground">{formatBytes(latest?.download)}</span>
      </p>
    </div>
  )
}
