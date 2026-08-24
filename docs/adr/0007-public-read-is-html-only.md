# Readers get HTML; the JSON API is the Author's

A visitor opens Published Posts as HTML. `GET /api/posts` and friends are for the Author (including Drafts). An unauthenticated API read is 401, not a public JSON feed.

A second public read model would have to keep Drafts out of every list and 404, match HTML error behaviour, and freeze the JSON shape for clients we do not have. v1 has no third-party client. The Author already speaks JSON to write.
