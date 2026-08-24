# Public HTML is also Cache-Control: no-store

Every response already gets `no-store` from the global security headers, including `/` and `/posts/{slug}`. v1 keeps that. Unpublish must not leave a readable copy in a browser or shared cache.

Per-path caching (`public, max-age=…` on the blog, `no-store` on JSON) would have been the usual blog default. This site has no traffic that needs it, and a cached Published page after Unpublish is a worse bug than an extra hit to origin.

Someone will try to "fix" the public pages. That change needs a new decision, not a drive-by header edit.
