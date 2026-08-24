# Concurrent edits: last save wins

Two editor tabs writing the same Post do not conflict: the later save replaces the Body and Title. There is no version token and no edit lock.

Optimistic concurrency (409 when `updated_at` does not match) is the correct multi-tab behaviour and needs UI the v1 editor will not have. A lock would have the Author locking themselves out of a leftover tab.

Losing the earlier tab's unsaved words is accepted.
