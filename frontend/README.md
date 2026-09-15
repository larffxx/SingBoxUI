# Frontend build shell

This directory is the React frontend of SingBoxUI. It is built by Vite into
`frontend/dist`, which Wails embeds into the desktop binary (`wails.json`,
`docs/adr/001-wails-v2.md`).

## Toolchain (rewrite spec §4)

React 18 · TypeScript (strict) · Vite · TanStack Router · TanStack Query ·
React Hook Form + Zod · Monaco Editor · Tailwind CSS · Radix/shadcn-style
primitives · Vitest + React Testing Library · Playwright.

No Next.js, no Redux, no Zustand (spec §4, §58): routing owns navigation,
TanStack Query owns server state, RHF + Zod own forms, local state owns
ephemeral UI.

## Commands

| Command                | Purpose                                          |
| ---------------------- | ------------------------------------------------ |
| `npm install`          | Install dependencies from `package-lock.json`    |
| `npm run dev`          | Vite dev server on http://localhost:5173         |
| `npm run lint`         | ESLint (type-aware)                              |
| `npm run typecheck`    | `tsc -b`, strict, no emit                        |
| `npm test`             | Vitest unit/component tests (run once)           |
| `npm run test:watch`   | Vitest in watch mode                             |
| `npm run e2e`          | Playwright application-level flows               |
| `npm run build`        | Production build into `dist/` (keeps `.gitkeep`) |
| `npm run format`       | Prettier write                                   |
| `npm run format:check` | Prettier check                                   |
| `npm run gen:bindings` | Wails binding placeholder (use `make bindings`)  |

`dist/` also holds a committed `.gitkeep`: the Go side embeds `frontend/dist`, so
the directory must exist with at least one file for a fresh clone to build. Vite
empties the directory on every build, and the `postbuild` hook recreates the
placeholder so a build never stages its deletion.

## Conventions

- **TypeScript strict** is enforced; `any` and `@ts-ignore` are lint errors.
- **Theming** uses Tailwind's `class` strategy: light is the default palette,
  `<html class="dark">` selects dark tokens defined in `src/index.css`.
- **Routing**: routes are declared with the code-based `createRoute` API so no
  codegen step is required for the build to succeed (TanStack Router's
  file-based codegen can be layered on later without changing this shell).
- **Generated code** (`src/wailsjs/**`, `src/routeTree.gen.ts`) is never edited
  by hand and is excluded from lint/format.
- ESLint is deliberately not configured to suppress broad rule sets
  (spec §72): fixes belong in the source, not in comments.
