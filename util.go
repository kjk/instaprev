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
