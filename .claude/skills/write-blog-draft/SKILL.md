---
name: write-blog-draft
description: Create or replace a Draft Post on this blog via the Author JSON API. Use when the user gives a Slug and wants writing sent to the service as a Draft. Never Publish. Never put passwords in the prompt or in output.
---

# Write a blog Draft

Send writing to this service as a **Post** Draft. This is not a notebook and not a file upload. The server is the source of truth; local Markdown under `notes/blog/` is a one-way copy from `pull-blog-posts`.

Read `CONTEXT.md` and `docs/adr/0016-agent-drafts-are-a-client-convention.md` before acting. Use those terms (Post, Draft, Slug, Title, Body, Author, Agent). Do not say note, notebook, article, or admin.

## Credentials

Read from the environment only. Never ask the user to paste a password into the chat. Never echo the password, access token, or refresh token.

| Variable | Required | Default |
|---|---|---|
| `BLOG_BASE_URL` | no | `http://127.0.0.1:8080` |
| `AUTHOR_USERNAME` | yes | — |
| `AUTHOR_PASSWORD` | yes | — |

`AUTHOR_PASSWORD` is the Author login password. It is not `DEV_AUTHOR_PASSWORD` (that only seeds an empty database).

If `AUTHOR_USERNAME` or `AUTHOR_PASSWORD` is unset, stop and tell the user which variable is missing. Do not invent a password.

Login once per session (`POST /api/auth/login`). Reuse the access token. Login is rate-limited to 5 attempts per minute; do not login per Post.

```bash
# Login. Do not print the JSON; capture tokens in the shell only.
base="${BLOG_BASE_URL:-http://127.0.0.1:8080}"
resp="$(curl -sS -X POST "$base/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${AUTHOR_USERNAME}\",\"password\":\"${AUTHOR_PASSWORD}\"}")"
access="$(printf '%s' "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"
```

On 401, stop. Do not retry in a loop.

## What the user must give

- **Slug** (required): ASCII `[a-z0-9]+(?:-[a-z0-9]+)*`. The Author chooses it. Never invent a Slug.
- **Title** (optional on create): if omitted, propose one from the conversation, then prefix `#N ` as below.
- **Body**: the Markdown source to store. Produce a complete Body each time, not an append, unless the user says to append.

If the Slug is missing, stop and ask for it.

## Procedure

1. `GET $base/api/posts` with `Authorization: Bearer $access`.
2. Find the Post whose `slug` equals the given Slug (exact match).
3. Branch:

### No Post with that Slug → create

1. From every `title` in the list, parse a leading `#` + digits + space (regex `^#([0-9]+) `). Let N be max + 1, or 1 if none match.
2. Title to send: `#N ` + the human title (the title without a `#N ` prefix). Do not double-prefix.
3. `POST $base/api/posts` with JSON `{"title":"...","slug":"...","body":"..."}`.
4. Expect 201. Report the Slug and that it is a Draft. Do not Publish.

### Post exists and `draft` is true → replace Body

1. Keep the existing Title unless the user explicitly asked to change it (including its `#N ` prefix).
2. `GET $base/api/posts/{slug}` for the current Body.
3. Write a complete new Body from the conversation (default). If the user said to append, put the new paragraphs after the current Body with a blank line between.
4. `PATCH $base/api/posts/{slug}` with JSON that includes `title` (unchanged unless step 1) and `body` (the full new source).
5. Expect 200. Do not Publish.

### Post exists and `draft` is false → stop

Tell the user this Slug is Published. Do not PATCH. Do not Unpublish. Ask them to pick another Slug or Unpublish in the Author area (`/author/posts/{slug}`).

## Never

- `POST /api/posts/{slug}/publish` or `/unpublish`
- PATCH a Published Post
- Put `---` YAML, a duplicate `# {Title}` heading, passwords, DSN, JWT, or LAN IPs in the Body
- Write "as an AI" / "as a language model"
- Invent image paths on this host; images are `https://` URLs only (ADR-0013)
- Use tables, footnotes, GFM extras, or raw HTML (ADR-0011: headings, lists, links `http`/`https`/`mailto`, emphasis, block quotes, code blocks, `https://` images)

The public page already renders Title as `<h1>`. Start the Body with a paragraph or `##`, not the same heading as Title.

## After a successful write

Tell the user the Slug, the Title as stored, and that Publish is still a human action in the Author area. Do not print tokens.
