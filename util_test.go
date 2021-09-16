package main

import (
	"testing"
)

func TestTrimCommonDirPrefix(t *testing.T) {
	areEqual := func(aGot []string, aExp []string) {
		if aExp == nil {
			aExp = aGot
		}
		for i, got := range aGot {
			exp := aExp[i]
			if got != exp {
				t.Fatalf("exp: %v\ngot : %v\n", aExp, aGot)
			}
		}
	}
	test := func(a []string, exp []string) {
		trimCommonDirPrefix(a)
		areEqual(a, exp)
	}
	test([]string{"foo/abc.txt", "foo/ab.txt", "foo/"}, []string{"abc.txt", "ab.txt", ""})
	test([]string{"abc.txt", "ab.txt"}, []string{"abc.txt", "ab.txt"})
	test([]string{"/abc.txt", "ab.txt"}, []string{"/abc.txt", "ab.txt"})
	test([]string{"/abc.txt", "/ab.txt"}, []string{"abc.txt", "ab.txt"})
	test([]string{"foo/", "foo"}, nil)
}
