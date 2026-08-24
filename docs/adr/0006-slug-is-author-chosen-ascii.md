# A Slug is author-chosen ASCII, not generated from the Title

The Author types the Slug. Allowed shape: lowercase ASCII letters, digits, hyphen-separated (`hello-homelab`). The Title may be any language and may change; the Slug does not follow it.

Generating from a Chinese Title would have required a transliteration library or Unicode in the path. Unicode Slugs work in browsers and fight with terminals, nginx, and this repo's Win10/Ubuntu path. Redirecting old Slugs after a rename is a second public name, which ADR-0004 already refused.
