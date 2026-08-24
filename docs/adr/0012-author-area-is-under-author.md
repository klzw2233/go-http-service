# The Author area lives under `/author`

Login, the Author's Post list, and the editor are `/author/login`, `/author/posts`, and `/author/posts/{slug}`. The public blog is `/` and `/posts/{slug}`.

Putting edit next to the public Slug (`/posts/{slug}/edit`) would have advertised a back office on every Post. A single-page app with no Author-area URLs would have left the textarea with nowhere to live.

These HTML pages are still served to anyone who requests them: a GET has no Bearer token. What they must not leak is Draft Bodies; those stay behind the JSON API.
