package main

import (
	"context"
	"fmt"
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
	)
	fs := func(n int64, d float64, size string) string {
		s := fmt.Sprintf("%.2f", float64(n)/d)
		return strings.TrimSuffix(s, ".00") + " " + size
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
	return fmt.Sprintf("%d bytes", i)
}
