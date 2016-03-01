git-compose unites multiple git repositories in subdirectories of a new one, facilitating future merges.

Installation
============

Run requirements: `git`

Build requirements: `golang-go>=1.4 cmake pkg-config`

Installation:

```bash
go get -u -v -d github.com/orivej/git2go
go generate github.com/orivej/git2go
go get -u -v github.com/orivej/git-compose
```

Usage example
=============

For each remote branch, git-compose will create a local one with combined history, moving data into a subdirectory with the name of the remote.  In the default "final" mode, it will do so by merging rewritten branches together in the end.  In the "total" mode which does not work correctly, it will attempt to rewrite history as if repositories were always developed together.

```bash
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

git-compose will also rewrite local tags that start (when running with `-repo/tag`) or end (when running with `-tag/repo`) with one of the specified repositories.

Example of converting remote `tag`s to local `tag/repo`s:

```bash
git init new-repo
cd new-repo
git remote add repo1 https://.../
git remote add repo2 https://.../
git remote add repo3 https://.../
for repo in repo1 repo2 repo3; do git config --add "remote.${repo}.fetch" "+refs/tags/*:refs/tags/*/$repo"; done
git fetch --all -np
git-compose -tag/repo repo1 repo2 repo3
git reset --hard
# ls → repo1/ repo2/ repo3/
# git tag → v1.0.0/repo1 v1.0.0/repo2 ...
```

Caveats
=======

git-compose does not know if a tag to compose was already rewritten by its previous invocation.  Either do not rewrite tags more than once, or, when rewriting tags multiple times, make sure to run `git fetch --all -np` between runs that rewrite tags.
