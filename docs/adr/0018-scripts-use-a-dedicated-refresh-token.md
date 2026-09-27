# Scripts use a dedicated refresh token; there is no PAT

A non-browser client authenticates the same way the Author area does: `POST /api/auth/login` once, then `POST /api/auth/refresh`, persisting the new refresh token after every rotation. It must not share that refresh token with the browser or with another script. Two holders of one refresh look like replay; `TryRotate` revokes every session for that User.

A PAT (named, non-rotating, separately scoped, separately revoked) is a second credential type this product does not have a client for. Passkeys are a browser replacement for the password and are not scheduled. The existing refresh token already is the long-lived API credential.

The cost: a leaked refresh file is Author-equivalent until revoke-all or expiry (`REFRESH_TOKEN_TTL`, default 30 days), and it bypasses TOTP (ADR 0017). Document the curl flow in the notes; do not invent a PAT.
