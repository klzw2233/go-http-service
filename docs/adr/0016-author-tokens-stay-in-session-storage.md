# Author tokens stay in sessionStorage

The Author area keeps the token pair in `sessionStorage` (`authorTokens`). Reloading the same tab keeps the session; a new tab does not. That is accepted. Closing the browser drops the session.

`localStorage` would have shared tabs and survived closing the browser, which widens XSS and "someone else at this computer". An HttpOnly cookie session would have overturned ADR 0008: CSRF, cookie flags, and a second credential next to refresh-token rotation, plus a broken Bearer contract.

When opening a second `/author` tab becomes a real habit, copy the pair through `BroadcastChannel` into the new tab's `sessionStorage`. Do not leave tokens in `localStorage`. The `storage` event does not carry `sessionStorage` across tabs; a `localStorage` mailbox that is written and immediately deleted is a fallback, not a place to keep the pair. Do not implement either path until the pain is real.
