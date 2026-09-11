/**
 * Traffic history for the runtime sparkline.
 *
 * `traffic:snapshot` events (spec §46) carry a cumulative `traffic.Snapshot`;
 * the chart needs a bounded series of samples. The reducer lives here so it can
 * be unit-tested without a component, and the cap keeps memory flat.
 */
import type { traffic } from '@/shared/api/bindings'

/** Most recent samples kept for the sparkline. */
export const TRAFFIC_HISTORY_CAP = 60

export interface TrafficSample {
  download: number
  upload: number
}

/** toSample flattens the backend snapshot into the two plotted series. */
export function toSample(snapshot: traffic.Snapshot): TrafficSample {
  return { download: snapshot.downloadTotal ?? 0, upload: snapshot.uploadTotal ?? 0 }
}

/**
 * pushSample appends a sample to the history, dropping the oldest one when the
 * cap is reached. When the totals reset (a restart resets sing-box counters) the
 * history restarts too, so the chart never shows a straight drop to zero.
 */
export function pushSample(
  history: readonly TrafficSample[],
  sample: TrafficSample,
  cap = TRAFFIC_HISTORY_CAP,
): TrafficSample[] {
  const last = history.at(-1)
  if (last && sample.download < last.download && sample.upload < last.upload) {
    return [sample]
  }
  return [...history, sample].slice(-cap)
}

/** Rate samples derived from the cumulative totals, for the rate sparkline. */
export function toRates(samples: readonly TrafficSample[]): TrafficSample[] {
  return samples.slice(1).map((sample, index) => {
    const previous = samples[index]
    return {
      download: Math.max(0, sample.download - (previous?.download ?? 0)),
      upload: Math.max(0, sample.upload - (previous?.upload ?? 0)),
    }
  })
}
