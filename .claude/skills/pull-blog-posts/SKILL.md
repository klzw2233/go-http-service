---
name: pull-blog-posts
description: Download Posts from this blog's Author JSON API into notes/blog/{slug}.md as a one-way local copy. Use when the user wants local Markdown of Drafts and/or Published Posts. Overwrites existing files. Does not write back to the server.
---

# Pull blog Posts

Copy Posts from the service onto disk. The server stays the source of truth. Editing these files does not update the blog; sending writing to the service is `write-blog-draft`.

Read `CONTEXT.md` before acting. Use Post, Draft, Slug, Title, Body. Do not say note or notebook for these files.

## Credentials

Same as `write-blog-draft`. Environment only; never echo secrets.

| Variable | Required | Default |
|---|---|---|
| `BLOG_BASE_URL` | no | `http://127.0.0.1:8080` |
| `AUTHOR_USERNAME` | yes | — |
| `AUTHOR_PASSWORD` | yes | — |

Login once per session. Do not login per Post.

Prefer the tracked script (creates `notes/blog/` if needed, overwrites `{slug}.md`):

```bash
./notes/blog/pull-blog-posts.sh
# or one Post:
./notes/blog/pull-blog-posts.sh hello-homelab
```

If you must do it inline:

```bash
base="${BLOG_BASE_URL:-http://127.0.0.1:8080}"
resp="$(curl -sS -X POST "$base/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${AUTHOR_USERNAME}\",\"password\":\"${AUTHOR_PASSWORD}\"}")"
access="$(printf '%s' "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"
```

On 401, stop.

## What to pull

- Default: every Post from `GET $base/api/posts` (Drafts and Published).
- If the user names one or more Slugs: only those. Missing Slug → report it, continue the rest.

Do not scrape public HTML. Body lives on the Author JSON API.

## Files

Path: `notes/blog/{slug}.md` (repo root). Create `notes/blog/` if needed.

File name is the Slug plus `.md`. No `#`, no id, no Title in the name.

If the file already exists, overwrite it. Do not write `.md.new`. Do not ask. Local edits to these copies are discarded; that is the point of a one-way export.

These files are gitignored (`notes/blog/`). Do not `git add` them.

## File format

YAML front matter, then the Body exactly as stored. The YAML is local metadata. Never send it back as Body.

```markdown
---
title: "#3 Homelab notes"
slug: hello-homelab
draft: true
---

First paragraph of the Body.

## A heading in the Body
```

- `title`: the Title field, including any `#N ` prefix.
- `slug`: the Slug.
- `draft`: JSON `draft` boolean (`true` / `false`).
- Body: `body` from the JSON, unchanged. Do not add a `# {Title}` line.

If `body` contains a line that is only `---`, that is still Body, not a second front matter. Write the file so the first `---` / closing `---` pair is the YAML you added.

## After a successful pull

List the paths written and whether each is Draft or Published. Do not print tokens. Remind the user that changing these files does not change the server.
