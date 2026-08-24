# The Author is named in config; public registration is closed

Who may write is `AUTHOR_USERNAME` (an existing User). Write endpoints reject any other signed-in User. `POST /api/users` rejects unauthenticated callers.

The first-row-in-the-table rule would have made tests and leftover seed data choose the Author. A `role` column would have been a permission model this product does not have. Leaving registration open on a reachable HomeLab would have let anyone become a writer.

Creating the Author User itself is an operator step (existing register path before this ships, or a one-off insert), not a product feature.
