package main

import (
	radix "github.com/armon/go-radix"
)

type Tree struct {
	*radix.Tree
}

func StringTree(ss []string) *Tree {
	t := radix.New()
	for _, s := range ss {
		t.Insert(s, nil)
	}
	return &Tree{t}
}

func (t *Tree) FilterStrings(key string, ss []string) []string {
	var rs []string
	for _, s := range ss {
		if match, _, _ := t.LongestPrefix(s); match == key {
			rs = append(rs, s)
		}
	}
	return rs
}
