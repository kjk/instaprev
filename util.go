package main

import (
	"context"
	"fmt"
)

func logf(ctx context.Context, format string, args ...interface{}) {
	s := format
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	}
	fmt.Print(s)
}

func ctx() context.Context {
	return context.Background()
}
