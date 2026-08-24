# A Draft has no public URL; Preview happens in the editor

`GET /posts/{slug}` is the public page. If the Post is a Draft, that request is 404, including when the Author types it into the address bar. Preview is rendered in the editor from the current textarea, not from a second route.

A `/author/posts/{slug}/preview` page would have been a second render pipeline (JS) next to the public one (Go), and the two would drift. Cookie auth would have let the public URL itself show Drafts to the Author; that was refused in ADR-0008.

Someone will try to "fix" the 404 for a signed-in Author. The server cannot see a Bearer token on a normal GET, so that fix is either cookies or a lie.
