# Public HTML and the write API share one process

v1 serves the public blog as HTML from this Go process and accepts writes over the existing JSON API. There is no separate frontend application.

A Vue app was the obvious next step given the HomeLab notes (`app.ubuntu.test`). A git-based Markdown static site would have ignored the database and auth we already run. One process means the blog is usable the day the HTML routes exist, without another repo, build, or nginx upstream.

The editor may be a textarea. That is accepted.
