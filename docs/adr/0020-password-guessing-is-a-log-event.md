# Password guessing is a log event; there is no bot challenge

`POST /api/auth/login` and the future revoke-all endpoint, when the password is wrong, will emit a dedicated structured log event (`event=login_failed` or equivalent) at Warn, with method, path, client IP, request id, and username. The password is never logged. Other 401s stay on their existing paths: expired access on a write, failed refresh, and replay (already its own Warn).

A CAPTCHA or Turnstile on the login page would duplicate the existing per-IP login bucket and add a third-party dependency. A "recent failures" page in the Author area would need a way to read it while locked out. Mail or Telegram alerts are a new outbound dependency.

Until that line exists, operators grep `status=401 path=/api/auth/login`. The dedicated event ships with the implementation, not with this ADR. The public login page and the public Posts share a hostname; the extra factor on that page is TOTP (ADR 0017), not a robot challenge.
