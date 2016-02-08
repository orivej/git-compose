package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	git "github.com/libgit2/git2go"
	"github.com/orivej/e"
)

type CommitInfo struct {
	Side        int
	MasterOid   git.Oid
	MasterDepth int // distance from the head
}

func main() {
	flag.Parse()
	repoPaths := flag.Args()

	repo, err := git.InitRepository(".", false)
	e.Exit(err)

	sideNames := make([]string, len(repoPaths))
	roots := make([]*git.Commit, len(repoPaths))
	commitInfo := make(map[git.Oid]CommitInfo)

	for i, srcPath := range repoPaths {
		srcPath, err := filepath.Abs(srcPath)
		e.Exit(err)

		name := filepath.Base(srcPath)
		sideNames[i] = name

		log.Println("fetch", name)
		remote, err := repo.Remotes.Lookup(name)
		if err != nil {
			remote, err = repo.Remotes.Create(name, srcPath)
		}
		e.Exit(err)
		// err = remote.Fetch(nil, nil, "")
		// e.Exit(err)
		_ = remote
		cmd := exec.Command("git", "fetch", name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		e.Exit(err)

		log.Println("walk master of", name)
		refName := fmt.Sprint("refs/remotes/", name, "/master")
		ref, err := repo.References.Lookup(refName)
		e.Exit(err)

		roots[i], err = repo.LookupCommit(ref.Target())
		e.Exit(err)

		walker, err := repo.Walk()
		e.Exit(err)

		err = walker.Push(ref.Target())
		e.Exit(err)

		walker.Sorting(git.SortTopological)
		walker.SimplifyFirstParent()
		j := 0
		err = walker.Iterate(func(commit *git.Commit) bool {
			oid := *commit.Id()
			commitInfo[oid] = CommitInfo{
				Side:        i,
				MasterOid:   oid,
				MasterDepth: j,
			}
			j++
			return true
		})
		e.Exit(err)

		log.Println("walk all of", name)
		walker.Reset()
		walker.Sorting(git.SortTopological | git.SortReverse)
		err = walker.Iterate(func(commit *git.Commit) bool {
			oid := *commit.Id()
			if _, ok := commitInfo[oid]; !ok {
				masterInfo := commitInfo[*commit.ParentId(0)]
				for j := uint(1); j < commit.ParentCount(); j++ {
					parentInfo := commitInfo[*commit.ParentId(j)]
					if masterInfo.MasterDepth > parentInfo.MasterDepth {
						masterInfo = parentInfo
					}
				}
				commitInfo[oid] = masterInfo
			}
			return true
		})
		e.Exit(err)

	}

	log.Println("compose master")
	tb, err := repo.TreeBuilder()
	e.Exit(err)
	emptyTreeOid, err := tb.Write()
	e.Exit(err)
	emptyTree, err := repo.LookupTree(emptyTreeOid)
	e.Exit(err)
	sig, err := repo.DefaultSignature()
	e.Exit(err)
	composedOid, err := repo.CreateCommit("", sig, sig, "", emptyTree, roots...)
	e.Exit(err)
	totalWalker, err := repo.Walk()
	e.Exit(err)
	totalWalker.Push(composedOid)
	totalWalker.Sorting(git.SortTopological | git.SortTime | git.SortReverse)

	var lastCommitId git.Oid
	var prevMaster *git.Commit
	filterMapping := make(map[git.Oid]git.Oid)
	masterTrees := make([]*git.Tree, len(repoPaths))
	err = totalWalker.Iterate(func(commit *git.Commit) bool {
		oid := *commit.Id()
		ci := commitInfo[oid]
		isMaster := oid == ci.MasterOid

		tree, err := commit.Tree()
		e.Exit(err)
		if isMaster {
			masterTrees[ci.Side] = tree
		}

		tb, err := repo.TreeBuilder()
		e.Exit(err)
		for i, name := range sideNames {
			if i == ci.Side {
				err := tb.Insert(name, tree.Id(), int(git.FilemodeTree))
				e.Exit(err)
			} else if mtree := masterTrees[i]; mtree != nil {
				err := tb.Insert(name, mtree.Id(), int(git.FilemodeTree))
				e.Exit(err)
			}
		}

		newTreeId, err := tb.Write()
		e.Exit(err)
		newTree, err := repo.LookupTree(newTreeId)
		e.Exit(err)

		newParents := make([]*git.Commit, commit.ParentCount())
		for i := range newParents {
			oldParentOid := commit.Parent(uint(i)).Id()
			newParentOid := filterMapping[*oldParentOid]
			newParent, err := repo.LookupCommit(&newParentOid)
			if err != nil {
				log.Fatalln("Error:", &oid, "parent", i, oldParentOid, "was not mapped yet")
			}
			e.Exit(err)
			newParents[i] = newParent
		}
		if isMaster && prevMaster != nil {
			if len(newParents) > 0 {
				newParents[0] = prevMaster
			} else {
				newParents = append(newParents, prevMaster)
			}
		}
		newOid, err := repo.CreateCommit("", commit.Author(), commit.Committer(), commit.Message(), newTree, newParents...)
		e.Exit(err)
		lastCommitId = *newOid
		filterMapping[oid] = *newOid
		if isMaster {
			prevMaster, err = repo.LookupCommit(newOid)
			e.Exit(err)
		}

		fmt.Println(commit.Committer().When, &oid, ci.Side, isMaster, "→", newOid, newParents)

		return true
	})
	e.Exit(err)

	_, err = repo.References.Create("refs/heads/master", prevMaster.Id(), true, "create master")
	e.Exit(err)
	err = repo.ResetToCommit(prevMaster, git.ResetHard, nil)
}
