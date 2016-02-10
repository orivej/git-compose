package main

import (
	"fmt"

	"bitbucket.org/pkg/inflect"
)

func plural(qty int, singular string) string {
	s := singular
	if qty != 1 {
		s = inflect.Pluralize(s)
	}
	return fmt.Sprintf("%d %s", qty, s)
}
