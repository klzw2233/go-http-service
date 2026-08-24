# Images are https URLs; this service does not store files

An Image in a Post is an `https://` link, typically from a host the Author already uses (a 图床). The Author pastes that URL into the Body. The process never accepts image bytes.

Storing files next to the binary fights the distroless image (non-root, no shell, ephemeral disk). Object storage would be a second system. Multipart upload would be a new request shape beside the JSON API.

Broken or hotlink-blocked URLs are the host's problem, not a publishing failure.
