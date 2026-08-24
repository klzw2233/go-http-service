# The Body is a safe CommonMark subset; raw HTML is text

Allowed: headings, lists, links (`http` / `https` / `mailto` only), emphasis, block quotes, code blocks. Images are allowed only with `https://` URLs. Raw HTML in the source is escaped, never inserted. No tables, footnotes, or GitHub-flavoured extras.

The Author area holds Bearer tokens in the browser (ADR-0008). Executable HTML in a Post is a token theft. A sanitizer whitelist is ongoing work, not a v1 feature. GFM would have widened the dialect that Go (public page) and JS (Preview) must both implement the same way.
