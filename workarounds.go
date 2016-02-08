package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/orivej/e"
	git "github.com/orivej/git2go"
)

type CommitWalker interface {
	Push(id *git.Oid) error // Add walking root.
	Iterate(fun git.RevWalkIterator) error
}

type commitWalker struct {
	repo *git.Repository
	oids []string
}

func NewReverseTopologicalDateOrderCommitWalker(repo *git.Repository) (CommitWalker, error) {
	return &commitWalker{repo: repo}, nil
}

func (cw *commitWalker) Push(id *git.Oid) error {
	cw.oids = append(cw.oids, id.String())
	return nil
}

func (cw *commitWalker) Iterate(fun git.RevWalkIterator) error {
	args := append([]string{"rev-list", "--date-order", "--reverse"}, cw.oids...)
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		e.Print(err)
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		oid, err := git.NewOid(line)
		if err != nil {
			e.Print(err)
			return err
		}
		commit, err := cw.repo.LookupCommit(oid)
		if err != nil {
			e.Print(err)
			return err
		}
		if !fun(commit) {
			break
		}
	}
	return nil
}
