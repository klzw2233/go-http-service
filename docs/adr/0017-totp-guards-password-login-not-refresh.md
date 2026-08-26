# TOTP guards password login, not refresh

Before this blog is reachable on the public internet, `POST /api/auth/login` will require TOTP. Recovery is ten single-use backup codes, shown once, stored as SHA-256 hashes (high-entropy, so not bcrypt). Generating a new set revokes the old set. There is no email or SMS recovery. Homelab stays password-only until that work is scheduled.

A stolen password can Publish and edit Posts; this blog does not Destroy. TOTP is the extra factor on that guessable secret. Refresh tokens, access tokens, and backup codes are high-entropy and are not second-factored.

Refresh does not ask for TOTP. Asking would break scripts and the existing session model. A refresh token is therefore full Author power once issued. File permissions, a dedicated login for each script (ADR 0018), and revoke-all (ADR 0019) are the remaining controls. Enrollment UI is not designed here.
