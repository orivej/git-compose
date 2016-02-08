git-compose unites multiple git repositories in subdirectories of a new one, rewriting history as if they always were together.

Requirements
============

- git
- libgit2

Installation
============

After installing requirements, run `go get -u -v github.com/orivej/git-compose`.

Usage example
=============

```
git init new-repo
cd new-repo
git remote add repo1 https://.../
git remote add repo2 https://.../
git remote add repo3 https://.../
git-compose repo1 repo2 repo3
git reset --hard
# ls → repo1/ repo2/ repo3/
```

For each remote branch, git-compose will create a local one with combined history.

Caveats
=======

- git-compose does not rewrite tags yet
