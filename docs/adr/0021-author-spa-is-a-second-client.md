# An Author SPA is a second client; public HTML stays in this process

A future Author-area SPA may run on its own origin and call the existing JSON write API (`/api/auth/*`, `/api/posts*`, preview). Public Home and Post pages stay HTML from this Go process. Unauthenticated `GET /api/posts` remains 401. The Go `/author/*` shells stay until that SPA actually replaces them.

Replacing the public blog with a SPA would have required a public JSON read that never leaks Drafts, a second reader UI, and a reversal of ADR 0007. Changing the CORS default to `*` (or auto-allowing localhost) would have undone fail-closed CORS. Serving the SPA from this process would have mixed two build systems into the binary the distroless image runs.

The only backend change the SPA needs is configuration: set `CORS_ALLOWED_ORIGINS` to that origin's exact scheme-host-port (for example `http://localhost:5173`). Tokens stay in that origin's `sessionStorage` (ADR 0008, 0016); they are not shared with `/author`. Preview stays `POST /api/posts/preview` so Markdown sanitising does not fork. Site name stays the Go constant, duplicated in the SPA if it needs the string. This does not overturn ADR 0002: the public site is still one process.
