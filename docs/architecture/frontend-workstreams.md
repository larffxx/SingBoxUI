# Frontend workstream contract

Two parallel workstreams build the React UI on top of a frozen foundation. This
document is the integration contract: it lists what already exists, who owns
what, and the rules both sides must obey. The foundation (shared layer, bindings
wrapper, generated types, Tailwind tokens, Vite/TS config) is owned by the
integrator and must not be restructured by a workstream.

Spec references are to `SingBoxUI — Final Full Rewrite Specification`.

## 1. Non-negotiable rules

1. **Transport is Wails bindings only.** Never `fetch`, never `/api/*`, never
   `axios`. Import typed calls from `@/shared/api/bindings`; only that module
   imports `@wails/go/desktop/*`.
2. **No `@wails/*` imports outside `src/shared/api/**`.** Screens and features
   consume `profilesApi`, `configApi`, `runtimeApi`, `binaryApi`,
   `settingsApi`, `shareApi`, `trafficApi` and the exported model types.
3. **No global mutable state.** Server state lives in TanStack Query, UI state
   in React state/context. No module-level `let` holding app state, no Redux,
   no Zustand.
4. **Strictness is on**: `strict`, `noUnusedLocals`, `noUnusedParameters`,
   `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`,
   `verbatimModuleSyntax`, `isolatedModules`. Therefore:
   - optional props on public components are written `foo?: string | undefined`;
   - array/record access is guarded (`const first = items[0]; if (!first) …`);
   - type-only imports use `import type`.
5. **Styling** uses the Tailwind tokens defined in `frontend/tailwind.config.ts`
   (`background, foreground, card, popover, muted, primary, secondary, accent,
   destructive, success, border, input, ring`). No ad-hoc hex colours, no inline
   `style` except for measured values.
6. **UI primitives come from `@/shared/ui`** (see §3). Radix packages are only
   imported there, so focus/escape/a11y behaviour stays consistent.
7. **All user-facing strings are Russian**, matching the rest of the app.
8. **Errors**: catch `BoundCallError` from `@/shared/api/errors`; show
   `error.code` and `error.details` (never a raw stack). Use `messageFor(error)`
   and the `AppAlert` pattern from `@/shared/ui`.
9. **Tests**: Vitest + React Testing Library, files `*.test.tsx` next to the
   code. Mock the transport at the `@/shared/api/bindings` boundary (never the
   generated modules). Every workstream must keep `npm run typecheck`,
   `npm run lint`, `npx vitest run` green.

## 2. What already exists (do not duplicate)

| Path | Contents |
|---|---|
| `src/shared/lib/cn.ts` | `cn()` class merger |
| `src/shared/lib/format.ts` | bytes/rate/duration/number formatting |
| `src/shared/lib/time.ts` | `toDate()` RFC3339 normaliser (generated `time.Time` fields arrive as `any`), `relative()` |
| `src/shared/api/errors.ts` | `BoundCallError`, `unwrap()`, `messageFor()`, stable code list |
| `src/shared/api/bindings.ts` | the seven typed API objects + model type re-exports |
| `src/shared/api/keys.ts` | query key factory (`keys.*`) |
| `src/shared/ui/{primitives,layout,overlay,forms,table,tabs,json-editor}.tsx` | Button, Input, Textarea, Badge, StatusDot, Spinner, EmptyState, Alert, Card*, PageHeader, KeyValueGrid, Toolbar, Dialog*, ConfirmDialog, Tooltip*, Field, SwitchField, Select, Table*, Tabs*, JsonEditor, JsonDiff |
| `frontend/wailsjs/**` | generated bindings — regenerate with `npm run gen:bindings`, never edit |

## 3. Shared UI surface (exact names)

`@/shared/ui` re-exports: `Button` (`variant`, `size`, `loading`), `Input`,
`Textarea`, `Badge` (`tone`), `StatusDot` (`tone`, `pulse`), `Spinner`,
`EmptyState`, `Alert` (`tone`, `title`, `code`, `details`, `actions`), `Card`,
`CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter`,
`PageHeader`, `KeyValueGrid` (`items: {label, value, mono?}[]`), `Toolbar`,
`Dialog`, `DialogTrigger`, `DialogContent` (`title`, `description`, `size`),
`DialogClose`, `ConfirmDialog` (controlled), `TooltipProvider`, `Tooltip`,
`Label`, `Field`, `SwitchField`, `Select`, `Table`, `TableHeader`, `TableBody`,
`TableRow`, `TableCell`, `TableHead`, `Tabs`, `TabsList`, `TabsTrigger`,
`TabsContent`, `JsonEditor` (`value`, `onChange`, `markers`, `readOnly`,
`height`, `onFormatRequested`), `JsonDiff`.

## 4. Backend contract

Generated signatures: `frontend/wailsjs/go/desktop/*.d.ts`. Wrapped calls:
`src/shared/api/bindings.ts` (same names, camelCase). Events
(`internal/events/events.go`, payloads in `frontend/wailsjs/go/models.ts`):

| Event | Payload |
|---|---|
| `runtime:status` | `runtime.Status` |
| `runtime:log` | log record batch (never one event per line) |
| `traffic:snapshot` | `traffic.Snapshot` |
| `binary:progress` | `binary.Progress` |
| `config:apply` | `config.ApplyProgress` |
| `app:notice` | `events.Notice` (`level`, `title`, `message`, `code`, `operation`, `details`, `persistent`, `occurredAt`) |
| `profiles:changed` | profile list changed outside a bound call |

## 5. Ownership

**Workstream A — shell and app-level screens** owns
`src/app/**`, `src/features/dashboard/**`, `src/features/runtime/**`,
`src/features/settings/**`, `src/features/about/**`.
Must deliver: `App.tsx` rewrite, router (TanStack Router, file-less/route-object
style is fine), app shell (sidebar nav, runtime status bar, notice surface,
theme via `class` on `<html>`), the Wails event wiring, and named exports
`DashboardRoute`, `RuntimeRoute`, `SettingsRoute`, `AboutRoute`. Router imports
`ProfilesRoute`, `ProfileWorkspaceRoute` from `@/features/profiles` and
`BinaryRoute` from `@/features/binary` — those exist as placeholders today and
are replaced by workstream B with the same names.

**Workstream B — profile workspace** owns `src/features/profiles/**`,
`src/features/binary/**`, `src/features/share/**`.
Must deliver: profile list + create/rename/duplicate/delete/export, first-launch
legacy import flow, the profile workspace with tabs (Outbounds, Inbounds,
Endpoints, Route, DNS, Experimental, Raw), outbound/inbound editors, share-link
paste import dialog, revision history with `JsonDiff` compare and apply/rollback,
binary manager screen, and named exports `ProfilesRoute`,
`ProfileWorkspaceRoute`, `BinaryRoute` (keep these names — the router imports
them).

Do not edit files outside your ownership. If you need a shared change, write to
`docs/architecture/frontend-workstreams.md` under "Integration notes" instead of
touching the other side's files.

## 6. Verification

```sh
cd frontend
npm run typecheck     # tsc -b
npm run lint          # eslint .
npx vitest run        # unit tests
npm run build         # tsc -b && vite build
```

A workstream is done only when all four pass with real output.
