package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gosuri/uiprogress"
	"github.com/orivej/e"
	git "github.com/orivej/git2go"
)

type CommitInfo struct {
	Side     int
	Branches []string // Short names of branches containing this commit.
}

var (
	flVerbose = flag.Bool("v", false, "enable verbose logging")
)

const usage = `usage : git-compose [<options>] <repository>...

Compose all branches from repositories (URLs, paths, or remote names)
into a single repository in the current directory.

`

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	remotePaths := flag.Args()
	if len(remotePaths) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	nSides := len(remotePaths)
	sideNames := make([]string, nSides)
	commitInfo := make(map[git.Oid]CommitInfo)
	roots := []*git.Commit{}

	repo, err := git.InitRepository(".", false)
	e.Exit(err)

	for i, remotePath := range remotePaths {
		// Fetch.
		remotePath, err := filepath.Abs(remotePath)
		e.Exit(err)

		name := filepath.Base(remotePath)
		sideNames[i] = name
		remoteGlob := fmt.Sprint("refs/remotes/", name, "/*")

		remote, err := repo.Remotes.Lookup(name)
		if err != nil {
			log.Printf("fetching %q (%s)", name, err)
			remote, err = repo.Remotes.Create(name, remotePath)
			e.Exit(err)
			// Use "git fetch" because libgit2 fetch is horribly slow for big local repos.
			// err = remote.Fetch(nil, nil, "")
			_ = remote
			cmd := exec.Command("git", "fetch", name)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			e.Exit(err)
		}
		// Store commit info.
		remoteRoots := []*git.Commit{}

		log.Printf("walking %q", name)
		walker, err := repo.Walk()
		e.Exit(err)
		err = walker.PushGlob(remoteGlob)
		e.Exit(err)
		err = walker.Iterate(func(commit *git.Commit) bool {
			oid := *commit.Id()
			commitInfo[oid] = CommitInfo{Side: i}
			return true
		})
		e.Exit(err)

		iter, err := repo.NewReferenceIteratorGlob(remoteGlob)
		e.Exit(err)
		for {
			ref, err := iter.Next()
			if err, ok := err.(*git.GitError); ok && err.Code == git.ErrIterOver {
				break
			}
			e.Exit(err)
			// Seed search with head commit.
			refCommit, err := repo.LookupCommit(ref.Target())
			e.Exit(err)
			roots = append(roots, refCommit)
			remoteRoots = append(remoteRoots, refCommit)
			// Store commit branches.
			branchName, err := ref.Branch().Name()
			e.Exit(err)
			branchName = strings.TrimPrefix(branchName, name+"/")
			walker.Reset()
			err = walker.Push(ref.Target())
			e.Exit(err)
			walker.SimplifyFirstParent()
			err = walker.Iterate(func(commit *git.Commit) bool {
				oid := *commit.Id()
				ci := commitInfo[oid]
				ci.Branches = append(ci.Branches, branchName)
				if branchName == "master" {
					// Reduce risk of missing sibling trees
					// with our strategy that takes them
					// from the first parent by making
					// "master" the first parent.
					n := len(ci.Branches) - 1
					ci.Branches[0], ci.Branches[n] = ci.Branches[n], ci.Branches[0]
				}

				commitInfo[oid] = ci
				return true
			})
			e.Exit(err)
		}
	}

	log.Printf("composing %s (%s)", plural(nSides, "side"), plural(len(commitInfo), "commit"))
	tb, err := repo.TreeBuilder()
	e.Exit(err)
	emptyTreeOid, err := tb.Write()
	e.Exit(err)
	emptyTree, err := repo.LookupTree(emptyTreeOid)
	e.Exit(err)
	sig := &git.Signature{Name: "root", Email: "root"} // Deterministic signature.
	composedOid, err := repo.CreateCommit("", sig, sig, "", emptyTree, roots...)
	e.Exit(err)
	if *flVerbose {
		log.Println("virtual common head:", composedOid)
	}

	filterMapping := make(map[git.Oid]git.Oid)
	newHeads := make(map[string]*git.Commit)

	var progress *uiprogress.Progress
	var bar *uiprogress.Bar
	if !*flVerbose {
		progress = uiprogress.New()
		progress.Out = os.Stderr
		bar = progress.AddBar(len(commitInfo))
		progress.Start()
	}

	// libgit2 chronological topological walker in fact is not chronological.
	// totalWalker, err := repo.Walk()
	totalWalker, err := NewReverseTopologicalDateOrderCommitWalker(repo)
	e.Exit(err)
	// totalWalker.Sorting(git.SortTopological | git.SortTime | git.SortReverse)
	totalWalker.Push(composedOid)
	err = totalWalker.Iterate(func(commit *git.Commit) bool {
		oid := *commit.Id()
		if oid == *composedOid {
			return true // Skip our composed oid (and immediately finish).
		}
		ci := commitInfo[oid]

		parents := []*git.Commit{}
		useParentsFrom := uint(0)
		if len(ci.Branches) > 0 {
			// Replace first parent (if any) with heads of relevant branches.
			useParentsFrom = 1
			usedHeads := map[git.Oid]bool{}
			for _, branch := range ci.Branches {
				head := newHeads[branch]
				if head != nil && !usedHeads[*head.Id()] {
					parents = append(parents, head)
					usedHeads[*head.Id()] = true
				}
			}
		}
		// Translate old parents.
		for i := useParentsFrom; i < commit.ParentCount(); i++ {
			oldParentOid := commit.Parent(uint(i)).Id()
			newParentOid := filterMapping[*oldParentOid]
			newParent, err := repo.LookupCommit(&newParentOid)
			if err != nil {
				e.Exit(fmt.Errorf("Error: %v parent %v in %q was not mapped yet", &oid, oldParentOid, sideNames[ci.Side]))
			}
			parents = append(parents, newParent)
		}
		// Update trees.
		var tb *git.TreeBuilder
		if len(parents) > 0 {
			parentTree, err := parents[0].Tree()
			e.Exit(err)
			tb, err = repo.TreeBuilderFromTree(parentTree)
			e.Exit(err)
		} else {
			tb, err = repo.TreeBuilder()
			e.Exit(err)
		}
		tree, err := commit.Tree()
		e.Exit(err)
		err = tb.Insert(sideNames[ci.Side], tree.Id(), int(git.FilemodeTree))
		e.Exit(err)
		newTreeId, err := tb.Write()
		e.Exit(err)
		newTree, err := repo.LookupTree(newTreeId)
		e.Exit(err)
		// Commit.
		newOid, err := repo.CreateCommit("", commit.Author(), commit.Committer(), commit.Message(), newTree, parents...)
		e.Exit(err)
		filterMapping[oid] = *newOid
		// Update cached heads.
		newCommit, err := repo.LookupCommit(newOid)
		e.Exit(err)
		for _, branch := range ci.Branches {
			newHeads[branch] = newCommit
		}

		if bar != nil {
			bar.Incr()
		}
		if *flVerbose {
			fmt.Println(commit.Committer().When, &oid, ci.Side, ci.Branches, "→", newOid, len(parents))
		}

		return true
	})
	e.Exit(err)
	if progress != nil {
		progress.Stop()
	}

	for headName, commit := range newHeads {
		_, err = repo.References.Create("refs/heads/"+headName, commit.Id(), true, "")
		e.Exit(err)
	}
	log.Printf("composed %s", plural(len(newHeads), "branch"))
}
