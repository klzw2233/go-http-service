# Single-author public blog

The first product on this service is a personal blog: one person writes, anyone may read published posts without signing in.

A multi-author CMS (roles, who can edit whose posts) and a private notebook (everything behind login) were both possible on the same tables. Either would have delayed a usable public URL and pulled in a permission model the `users` table does not have.

Open registration (`POST /api/users`) still exists in the skeleton and contradicts this product. Closing that gap is a separate decision.
