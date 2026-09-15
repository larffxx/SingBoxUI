/**
 * vite empties `dist` on every build, which deletes the `.gitkeep` that keeps the
 * directory in git. The Go side embeds `frontend/dist` with `//go:embed
 * all:frontend/dist`, so the directory has to hold at least one file for a fresh
 * clone to build at all. Recreating the placeholder here stops `npm run build`
 * from leaving a staged deletion of a file the repository needs.
 */
import { mkdirSync, writeFileSync } from 'node:fs'

mkdirSync('dist', { recursive: true })
writeFileSync('dist/.gitkeep', '')
