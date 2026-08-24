# Go HTTP Service

A single-author public blog: visitors read published posts without signing in; the author signs in to write.

## Language

**User**:
A registered account that can sign in.
_Avoid_: account, member, customer

**Author**:
The User who writes Posts. This blog has one author; the public does not write.
_Avoid_: admin, editor, writer

**Post**:
A piece of writing with a Title, a Slug, and a Body. The unit a reader opens and the author edits.
_Avoid_: article, page, blog, content, entry

**Title**:
The human-readable heading of a Post. It is not unique, it may change, and it is not the public name.
_Avoid_: name, headline, slug

**Body**:
The Markdown source of a Post. It is not HTML. Raw HTML in the source is shown as text.
_Avoid_: content, markdown, html, copy

**Image**:
A picture shown in a Post by an `https://` URL the Author hosts elsewhere. This blog does not store image files.
_Avoid_: upload, attachment, media, file

**Draft**:
A Post that is not publicly readable. Every Post begins as a Draft; Publish is a later act, not part of creation.
_Avoid_: unpublished, private, hidden

**Published**:
The state of a Post that anyone can read without signing in.
_Avoid_: live, public, released

**Publish**:
The act of making a Draft publicly readable.
_Avoid_: release, go live, post (as a verb)

**Unpublish**:
The act of returning a Published Post to a Draft. The writing is kept; the public can no longer open it. The first time it was Published is still remembered.
_Avoid_: withdraw, take down, delete

**Slug**:
The stable public name of a Post in its URL. The Author chooses it at creation from ASCII letters, digits, and hyphens; it is never derived from the Title and never changed. No two Posts share a Slug, including Drafts.
_Avoid_: permalink, path, alias, id (in a public URL)

**Destroy**:
Permanently removing a Post so it cannot be opened or edited. This blog does not Destroy Posts.
_Avoid_: delete, remove, trash

**Preview**:
The Author seeing a Draft rendered inside the editor. It is not a URL. Opening the Slug in the address bar is the public page, which a Draft does not have.
_Avoid_: draft URL, unpublished page, live preview

**Author area**:
The HTML pages the Author uses to sign in, list every Post (Drafts included, marked as Drafts), and edit. They are not the public blog.
_Avoid_: admin, dashboard, backend, CMS, console

**Home**:
The public list of Published Posts, at `/`. Title and first Publish time only, newest first. It is not an about page.
_Avoid_: index, feed, archive, about, landing

**Site name**:
`Personal Blog - klzw2233`. The string on the Home heading, the browser tab, and the suffix of a Post page title (`{Title} · {Site name}`).
_Avoid_: go-http-service, brand, blog title
