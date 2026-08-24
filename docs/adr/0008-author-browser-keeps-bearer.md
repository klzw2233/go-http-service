# The Author's browser keeps Bearer tokens; there is no cookie session

Login remains `POST /api/auth/login` returning a token pair. The Author's HTML pages use JavaScript to store those tokens and to call the JSON write API. Ordinary form POST and a bare browser GET cannot authenticate the Author.

A cookie session would have let a textarea submit without JS, and is how a classic blog does it. It would also have added CSRF, cookie flags, and a second credential next to refresh-token rotation. Putting the access token in a cookie would have broken the existing Bearer contract and its tests.

The cost: the editor is not a form. XSS on an Author page can steal tokens, so Post bodies must not become executable HTML in that page.
