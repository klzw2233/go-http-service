#!/usr/bin/env bash
# One-way copy of Posts from the Author JSON API into this directory.
# The server is the source of truth. These Markdown files are not written back.
#
# Usage (from anywhere):
#   AUTHOR_USERNAME=jimmy AUTHOR_PASSWORD=... ./notes/blog/pull-blog-posts.sh
#   AUTHOR_USERNAME=jimmy AUTHOR_PASSWORD=... ./notes/blog/pull-blog-posts.sh hello-homelab
#
# Optional: BLOG_BASE_URL (default http://127.0.0.1:8080)
#
# Extra arguments are Slugs to pull. With no arguments, every Post is pulled.
# Creates this directory if it is missing (the script may live only in git
# until the first run). Overwrites existing {slug}.md files.
set -euo pipefail

out_dir="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out_dir"

if [[ -z "${AUTHOR_USERNAME:-}" || -z "${AUTHOR_PASSWORD:-}" ]]; then
  echo "AUTHOR_USERNAME and AUTHOR_PASSWORD must be set in the environment" >&2
  echo "AUTHOR_PASSWORD is the Author login password, not DEV_AUTHOR_PASSWORD" >&2
  exit 1
fi

base="${BLOG_BASE_URL:-http://127.0.0.1:8080}"
base="${base%/}"

login_body="$(python3 -c 'import json,os; print(json.dumps({"username": os.environ["AUTHOR_USERNAME"], "password": os.environ["AUTHOR_PASSWORD"]}))')"

http_code="$(mktemp)"
trap 'rm -f "$http_code"' EXIT

resp="$(curl -sS -o - -w '%{http_code}' -X POST "$base/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "$login_body")"
# curl -w appends the status code to the body; split them without printing tokens.
status="${resp: -3}"
body="${resp:0:${#resp}-3}"

if [[ "$status" != "200" ]]; then
  echo "login failed (HTTP $status)" >&2
  exit 1
fi

export PULL_LOGIN_JSON="$body"
export PULL_BASE="$base"
export PULL_OUT_DIR="$out_dir"
python3 - "$@" <<'PY'
import json, os, sys, urllib.error, urllib.request

base = os.environ["PULL_BASE"]
out_dir = os.environ["PULL_OUT_DIR"]
login = json.loads(os.environ["PULL_LOGIN_JSON"])
token = login.get("access_token")
if not token:
    print("login failed; response has no access_token", file=sys.stderr)
    sys.exit(1)

req = urllib.request.Request(
    base + "/api/posts",
    headers={"Authorization": "Bearer " + token},
)
try:
    with urllib.request.urlopen(req) as r:
        posts = json.load(r)
except urllib.error.HTTPError as e:
    print(f"GET /api/posts failed (HTTP {e.code})", file=sys.stderr)
    sys.exit(1)

if not isinstance(posts, list):
    print("GET /api/posts did not return a list", file=sys.stderr)
    sys.exit(1)

wanted = sys.argv[1:]
by_slug = {p.get("slug"): p for p in posts if isinstance(p, dict) and p.get("slug")}

if wanted:
    selected = []
    for slug in wanted:
        p = by_slug.get(slug)
        if p is None:
            print(f"missing slug: {slug}", file=sys.stderr)
            continue
        selected.append(p)
else:
    selected = [p for p in posts if isinstance(p, dict) and p.get("slug")]

os.makedirs(out_dir, exist_ok=True)

for p in selected:
    slug = p["slug"]
    path = os.path.join(out_dir, slug + ".md")
    title = p.get("title") or ""
    draft = bool(p.get("draft"))
    body = p.get("body") or ""
    text = (
        "---\n"
        f"title: {json.dumps(title, ensure_ascii=False)}\n"
        f"slug: {slug}\n"
        f"draft: {'true' if draft else 'false'}\n"
        "---\n\n"
        + body
    )
    if not text.endswith("\n"):
        text += "\n"
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)
    state = "Draft" if draft else "Published"
    print(f"{path}  ({state})")
PY
