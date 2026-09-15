/**
 * Class name helper.
 *
 * Tailwind utility merging without a global stylesheet: `cn` joins conditional
 * class names and lets later entries win for the same utility group.
 */
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
