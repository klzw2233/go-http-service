# Unpublish does not erase the first Publish time; Posts are not Destroyed

A Post is either a Draft or Published. Published means a reader can open it now. The first successful Publish is remembered even after Unpublish. There is no Destroy, no trash, no scheduled Publish.

Clearing the first Publish time on Unpublish would have made "never published" and "taken down" the same fact. Soft-delete would have been a third state. Hard-delete is irreversible in a product whose recovery path is Unpublish.
