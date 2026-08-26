# Revoke-all is a password step-up; access tokens live their TTL

An authenticated endpoint will revoke every unrevoked refresh token for that User after the password is presented again. A stolen access token must not be enough to lock the Author out. Single-token `Logout` stays as it is: presenting an already-revoked refresh must not look like replay.

An access-token denylist, shortening `ACCESS_TOKEN_TTL`, or rotating `JWT_SECRET` would make revoke-all instant. They are new infrastructure, or they bounce every session including the Author's. Issued access tokens remain valid until their expiry (default 15 minutes); that window is accepted.

The endpoint and the Author-area button ship with the implementation, not with this ADR. Until then, an emergency on Homelab can still revoke rows in `refresh_tokens` over SSH.
