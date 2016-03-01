package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/golang/example/stringutil"
	"github.com/gosuri/uiprogress"
	"github.com/orivej/e"
	git "github.com/orivej/git2go"
)

type CommitInfo struct {
	Side     int
	Branches []string // Short names of branches containing this commit.
}

type BranchInfo struct {
	Side   int
	Commit *git.Commit
}

type Mode int

const (
	NoMode Mode = iota
	CompleteMode
	FinalMode
)

var modes = map[string]Mode{"complete": CompleteMode, "final": FinalMode}

var (
	flVerbose = flag.Bool("v", false, "enable verbose logging")
	flRepoTag = flag.Bool("repo/tag", false, "relate a tag to the repository by the beginning of tag name")
	flTagRepo = flag.Bool("tag/repo", false, "relate a tag to the repository by the end of tag name")
	flMode    = flag.String("mode", "final", `composition mode:
        * final :: merge branches with a final merge commit
        * complete :: interspose commits on all branches (does not work correctly)`)
	flInterpose = flag.String("interpose", "", `in final mode, interpose rather than merge the specified branch`)
)

const usage = `usage : git-compose [<options>] <repository>...

Compose all branches from repositories (URLs, paths, or remote names) into a
single repository in the current directory.  If either -repo/tag or -tag/repo is
given, also compose and rewrite relevant tags.

`

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *flRepoTag && *flTagRepo {
		fmt.Fprintln(os.Stderr, "-repo/tag is not compatible with -tag/repo")
		os.Exit(1)
	}
	mode := modes[*flMode]
	if mode == NoMode {
		fmt.Fprintln(os.Stderr, "Unknown mode: ", *flMode)
		os.Exit(1)
	}
	remotePaths := flag.Args()
	if len(remotePaths) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	nSides := len(remotePaths)
	commitInfo := make(map[git.Oid]CommitInfo)
	finalBranchInfo := make(map[string][]BranchInfo)
	roots := []*git.Commit{}
	sideNames := make([]string, nSides)
	for i, path := range remotePaths {
		sideNames[i] = filepath.Base(path)
	}
	var sideNamesTree *Tree
	switch {
	case *flRepoTag:
		sideNamesTree = StringTree(sideNames)
	case *flTagRepo:
		sideNamesTree = StringTree(reverseStrings(sideNames))
	}

	repo, err := git.InitRepository(".", false)
	e.Exit(err)
	tagList, err := repo.Tags.List()
	e.Exit(err)
	if *flTagRepo {
		tagList = reverseStrings(tagList)
	}
	var relevantTagList []string

	for i, remotePath := range remotePaths {
		// Fetch.
		remotePath, err := filepath.Abs(remotePath)
		e.Exit(err)

		name := sideNames[i]
		remoteGlob := fmt.Sprint("refs/remotes/", name, "/*")
		switch {
		case *flRepoTag:
			sideNamesTree.Insert(name, true)
		case *flTagRepo:
			sideNamesTree.Insert(stringutil.Reverse(name), true)
		}

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

		// For each commit to be rewritten, store it side number in commitInfo.
		log.Printf("walking %q", name)
		walker, err := repo.Walk()
		e.Exit(err)
		err = walker.PushGlob(remoteGlob)
		e.Exit(err)
		if *flTagRepo || *flRepoTag {
			key := name
			if *flTagRepo {
				key = stringutil.Reverse(name)
			}
			relevantTags := sideNamesTree.FilterStrings(key, tagList)
			for _, tagName := range relevantTags {
				if *flTagRepo {
					tagName = stringutil.Reverse(tagName)
				}
				ref, err := repo.References.Lookup("refs/tags/" + tagName)
				e.Exit(err)
				obj, err := ref.Peel(git.ObjectCommit)
				if err != nil {
					// Ignore tags that do not point to commits.
					continue
				}
				relevantTagList = append(relevantTagList, tagName)
				walker.Push(obj.Id())
				// Add the tag to the list of roots for rewrite.
				commit, err := repo.LookupCommit(obj.Id())
				e.Exit(err)
				roots = append(roots, commit)
			}
		}
		sharedCommitsCount := 0
		err = walker.Iterate(func(commit *git.Commit) bool {
			oid := *commit.Id()
			if _, ok := commitInfo[oid]; !ok {
				commitInfo[oid] = CommitInfo{Side: i}
			} else {
				sharedCommitsCount++
			}
			return true
		})
		e.Exit(err)
		if sharedCommitsCount > 0 {
			log.Printf("did not change side of %s", plural(sharedCommitsCount, "shared commit"))
		}

		// Update commitInfo with lists of commit branches.
		iter, err := repo.NewReferenceIteratorGlob(remoteGlob)
		e.Exit(err)
		for {
			ref, err := iter.Next()
			if git.IsErrorCode(err, git.ErrIterOver) {
				break
			}
			e.Exit(err)
			// Seed search with head commit.
			refCommit, err := repo.LookupCommit(ref.Target())
			e.Exit(err)
			roots = append(roots, refCommit)
			// Store commit branches.
			branchName, err := ref.Branch().Name()
			e.Exit(err)
			branchName = strings.TrimPrefix(branchName, name+"/")
			if mode == FinalMode && branchName != *flInterpose {
				// Add branches other than "master" for final merge.
				finalBranchInfo[branchName] = append(finalBranchInfo[branchName], BranchInfo{
					Side:   i,
					Commit: refCommit,
				})
				// Skip interposing.
				continue
			}
			walker.Reset()
			err = walker.Push(ref.Target())
			e.Exit(err)
			walker.SimplifyFirstParent()
			err = walker.Iterate(func(commit *git.Commit) bool {
				oid := *commit.Id()
				ci := commitInfo[oid]
				ci.Branches = append(ci.Branches, branchName)
				if mode == CompleteMode && branchName == "master" {
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

	filterMapping := make(map[git.Oid]git.Oid) // Map old commits to new commits.
	newHeads := make(map[string]*git.Commit)   // Map new heads to new commits.

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
			parentOid := filterMapping[*oldParentOid]
			newParent, err := repo.LookupCommit(&parentOid)
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
		newTreeID, err := tb.Write()
		e.Exit(err)
		newTree, err := repo.LookupTree(newTreeID)
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

	// Create finally-merged branches.
	sig, err = repo.DefaultSignature()
	e.Exit(err)
	for branchName, branchInfos := range finalBranchInfo {
		nParents := len(branchInfos)
		var commitID *git.Oid
		if nParents == 1 {
			oldOid := filterMapping[*branchInfos[0].Commit.Id()]
			commit, err := repo.LookupCommit(&oldOid)
			e.Exit(err)
			commitID = commit.Id()
		} else {
			parents := make([]*git.Commit, nParents)
			parentNames := make([]string, nParents)
			tb, err := repo.TreeBuilder()
			e.Exit(err)
			for k, branchInfo := range branchInfos {
				parentOid := filterMapping[*branchInfo.Commit.Id()]
				parentCommit, err := repo.LookupCommit(&parentOid)
				e.Exit(err)
				parents[k] = parentCommit
				name := sideNames[branchInfo.Side]
				parentNames[k] = name
				oldTree, err := branchInfo.Commit.Tree()
				e.Exit(err)
				err = tb.Insert(name, oldTree.Id(), int(git.FilemodeTree))
				e.Exit(err)
			}
			newTreeID, err := tb.Write()
			e.Exit(err)
			newTree, err := repo.LookupTree(newTreeID)
			e.Exit(err)
			message := fmt.Sprintf("Compose branch '%s' from %s", branchName, strings.Join(parentNames, ", "))
			commitID, err = repo.CreateCommit("", sig, sig, message, newTree, parents...)
			e.Exit(err)
		}
		_, err = repo.References.Create("refs/heads/"+branchName, commitID, true, "")
		e.Exit(err)
	}
	// Create heads for interposed branches.
	for headName, commit := range newHeads {
		_, err = repo.References.Create("refs/heads/"+headName, commit.Id(), true, "")
		e.Exit(err)
	}
	// Create tags.
	rewrittenTagsCount := 0
	for {
		// This loop supports rewriting tags of tags...
		rewrittenTags := false
		for _, tagName := range relevantTagList {
			ref, err := repo.References.Lookup("refs/tags/" + tagName)
			e.Exit(err)
			oldOid := *ref.Target()
			if newOid, ok := filterMapping[oldOid]; ok {
				// Lightweight tag pointing to a rewritten commit.
				commit, err := repo.LookupCommit(&newOid)
				e.Exit(err)
				_, err = repo.Tags.CreateLightweight(tagName, commit, true)
				e.Exit(err)
				rewrittenTags = true
				rewrittenTagsCount++
				if *flVerbose {
					fmt.Println("rewritten lightweight tag", tagName, &oldOid, "→", &newOid)
				}
			}
			tag, err := repo.LookupTag(&oldOid)
			if err == nil {
				// Annotated tag.
				oldOid = *tag.Target().Id()
				if newOid, ok := filterMapping[oldOid]; ok {
					commit, err := repo.LookupCommit(&newOid)
					e.Exit(err)
					// Currently git2go does not support overwriting annotated tags.
					_, err = repo.Tags.Create(tagName, commit, tag.Tagger(), tag.Message(), true)
					e.Exit(err)
					rewrittenTags = true
					rewrittenTagsCount++
					if *flVerbose {
						fmt.Println("rewritten annotated tag", tagName, &oldOid, "→", &newOid)
					}
				}
			}
		}
		if !rewrittenTags {
			break
		}
	}
	log.Printf("merged %s, interposed %s, rewritten %s",
		plural(len(finalBranchInfo), "branch"),
		plural(len(newHeads), "branch"),
		plural(rewrittenTagsCount, "tag"))
}
