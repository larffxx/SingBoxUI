/**
 * Base controls.
 *
 * Small, unopinionated building blocks used by every screen. They only handle
 * presentation and accessibility; no component here knows about the backend.
 */
import { cva, type VariantProps } from 'class-variance-authority'
import { Loader2 } from 'lucide-react'
import * as React from 'react'

import { cn } from '@/shared/lib/cn'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        outline: 'border border-border bg-transparent hover:bg-accent hover:text-accent-foreground',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        sm: 'h-8 px-2.5 text-xs',
        default: 'h-9 px-3',
        lg: 'h-10 px-4',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  loading?: boolean | undefined
}

/** Button is the single button primitive; `loading` blocks re-entry. */
export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, loading = false, disabled, children, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled === true || loading}
      aria-busy={loading}
      {...props}
    >
      {loading ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : null}
      {children}
    </button>
  )
})

export const Input = React.forwardRef<
  HTMLInputElement,
  React.InputHTMLAttributes<HTMLInputElement>
>(function Input({ className, ...props }, ref) {
  return (
    <input
      ref={ref}
      className={cn(
        'h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
})

export const Textarea = React.forwardRef<
  HTMLTextAreaElement,
  React.TextareaHTMLAttributes<HTMLTextAreaElement>
>(function Textarea({ className, ...props }, ref) {
  return (
    <textarea
      ref={ref}
      className={cn(
        'min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
})

/** Badge renders a compact status token. */
export function Badge({
  className,
  tone = 'neutral',
  children,
}: {
  className?: string
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info'
  children: React.ReactNode
}) {
  const tones: Record<string, string> = {
    neutral: 'border-border bg-muted text-muted-foreground',
    success: 'border-transparent bg-success/15 text-success',
    warning: 'border-transparent bg-amber-500/15 text-amber-600 dark:text-amber-400',
    danger: 'border-transparent bg-destructive/15 text-destructive',
    info: 'border-transparent bg-primary/15 text-primary',
  }
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}

/** StatusDot is a small coloured dot for dense runtime indicators. */
export function StatusDot({
  tone = 'neutral',
  pulse = false,
  className,
  title,
}: {
  tone?: 'neutral' | 'success' | 'warning' | 'danger'
  pulse?: boolean
  className?: string
  title?: string | undefined
}) {
  const tones: Record<string, string> = {
    neutral: 'bg-muted-foreground',
    success: 'bg-success',
    warning: 'bg-amber-500',
    danger: 'bg-destructive',
  }
  return (
    <span
      title={title}
      aria-hidden={title === undefined ? true : undefined}
      className={cn(
        'inline-block h-2 w-2 shrink-0 rounded-full',
        tones[tone],
        pulse && 'animate-pulse',
        className,
      )}
    />
  )
}

export function Spinner({ className, label }: { className?: string; label?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2 text-sm text-muted-foreground', className)}>
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
      {label ?? 'Загрузка…'}
    </span>
  )
}

/** EmptyState is the standard "nothing here yet" block. */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border px-6 py-10 text-center">
      <p className="text-sm font-medium">{title}</p>
      {description ? <p className="max-w-md text-sm text-muted-foreground">{description}</p> : null}
      {action}
    </div>
  )
}

/** Alert surfaces inline information or an error with its stable code. */
export function Alert({
  tone = 'info',
  title,
  code,
  details,
  actions,
  children,
}: {
  tone?: 'info' | 'warning' | 'danger' | 'success'
  title: string
  code?: string | undefined
  details?: string[]
  actions?: React.ReactNode
  children?: React.ReactNode
}) {
  const tones: Record<string, string> = {
    info: 'border-primary/40 bg-primary/5',
    success: 'border-success/40 bg-success/5',
    warning: 'border-amber-500/40 bg-amber-500/5',
    danger: 'border-destructive/50 bg-destructive/5',
  }
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn('rounded-md border p-3 text-sm', tones[tone])}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{title}</span>
        {code ? (
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {code}
          </code>
        ) : null}
        {actions ? <span className="ml-auto flex items-center gap-2">{actions}</span> : null}
      </div>
      {children ? <div className="mt-1.5 text-muted-foreground">{children}</div> : null}
      {details && details.length > 0 ? (
        <ul className="mt-1.5 list-inside list-disc space-y-0.5 text-muted-foreground">
          {details.map((detail) => (
            <li key={detail} className="break-all font-mono text-xs">
              {detail}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
