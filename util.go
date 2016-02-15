package main

import (
	"fmt"

	"github.com/golang/example/stringutil"

	"bitbucket.org/pkg/inflect"
)

func plural(qty int, singular string) string {
	s := singular
	if qty != 1 {
		s = inflect.Pluralize(s)
	}
	return fmt.Sprintf("%d %s", qty, s)
}

func reverseStrings(ss []string) []string {
	rs := make([]string, len(ss))
	for i := range ss {
		rs[i] = stringutil.Reverse(ss[i])
	}
	return rs
}
