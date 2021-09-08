package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err.Error())
	}
}

func logf(ctx context.Context, format string, args ...interface{}) {
	s := format
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	}
	fmt.Print(s)
}

func isWindows() bool {
	return strings.Contains(runtime.GOOS, "windows")
}

func ctx() context.Context {
	return context.Background()
}

func humanizeSize(i int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	fs := func(n int64, d float64, size string) string {
		s := fmt.Sprintf("%.2f", float64(n)/d)
		return strings.TrimSuffix(s, ".00") + " " + size
	}

	if i > tb {
		return fs(i, tb, "TB")
	}
	if i > gb {
		return fs(i, gb, "GB")
	}
	if i > mb {
		return fs(i, mb, "MB")
	}
	if i > kb {
		return fs(i, kb, "kB")
	}
	return fmt.Sprintf("%d B", i)
}

// when dropping a directory, all files have common prefix, which we want to remove
func trimCommonPrefix(a []string) {
	if len(a) < 2 {
		return
	}
	isSameCharAt := func(idx int) bool {
		var c byte
		for n, s := range a {
			if idx >= len(s) {
				return false
			}
			c2 := s[idx]
			if n == 0 {
				c = c2
				continue
			}
			if c != c2 {
				return false
			}
		}
		return true
	}
	idx := 0
	for {
		if isSameCharAt(idx) {
			idx++
			continue
		}
		if idx == 0 {
			return
		}
		// logf(ctx(), "removing common prefix '%s'\n", a[0][:i])
		for i, s := range a {
			a[i] = s[idx:]
		}
		return
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
