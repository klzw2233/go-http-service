# A Post is opened by its Slug, which does not change

Public HTML lives at `/posts/{slug}`. The numeric id stays the primary key and does not appear in public URLs. The Slug is required at creation and is immutable.

Date-prefixed URLs (`/2026/08/...`) couple the path to a publish date that Unpublish/Publish would then have to freeze or rewrite. Serving both id and Slug is two public names for one Post.

Changing a Slug later is a broken bookmark. If a title is wrong, rename the title, not the Slug.
