<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

## Manafold frontend

Next.js 16 App Router. Middleware is renamed **Proxy** (`src/proxy.ts`, exports
`proxy`). `params`/`searchParams` are async in server components — the builder
pages are `"use client"` and use the `useParams()` hook instead. Every backend
call goes through `src/lib/api.ts`, which reaches the Go backend only through
the same-origin `/api/*` rewrite in `next.config.ts` (ACCT-016). See the repo
root `AGENTS.md` for the LID workflow and `docs/intent/platform-shell/` +
`docs/intent/deck-building/` for this UI's design.
