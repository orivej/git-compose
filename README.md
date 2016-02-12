git-compose unites multiple git repositories in subdirectories of a new one, rewriting history as if they always were together.

Installation
============

Run requirements: `git`

Build requirements: `golang-go>=1.4 cmake pkg-config`

Installation:
```
go get -u -v -d github.com/orivej/git2go
go generate github.com/orivej/git2go
go get -u -v github.com/orivej/git-compose
```

Usage example
=============

For each remote branch, git-compose will create a local one with combined history.

```
git init new-repo
cd new-repo
git remote add repo1 https://.../
git remote add repo2 https://.../
git remote add repo3 https://.../
git fetch --all -np
git-compose repo1 repo2 repo3
git reset --hard
# ls → repo1/ repo2/ repo3/
```

Caveats
=======

- git-compose does not rewrite tags yet
