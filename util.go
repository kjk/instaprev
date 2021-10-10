package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/kjk/common/httputil"
	"github.com/kjk/common/u"
)

var (
	must              = u.Must
	panicIf           = u.PanicIf
	isWindows         = u.IsWindows
	normalizeNewlines = u.NormalizeNewlines
	formatSize        = u.FormatSize
	pathExists        = u.PathExists
	dirExists         = u.DirExists
	httpGet           = httputil.Get
)

func ctx() context.Context {
	return context.Background()
}

func logf(ctx context.Context, format string, args ...interface{}) {
	s := format
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	}
	fmt.Print(s)
}

func stringsTrimSlashPrefix(a []string) {
	for i, s := range a {
		a[i] = strings.TrimLeft(s, `\/`)
	}
}

// when all files have the same dir prefix (e.g. "www/"), we want to remove it
// this can happen when e.g. drag & dropping a directory
func trimCommonDirPrefix(a []string) {
	if len(a) < 2 {
		return
	}
	if false {
		logf(ctx(), "trimCommonDirPrefix:\n")
		for _, s := range a {
			logf(ctx(), "%s\n", s)
		}
		logf(ctx(), "\n")
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
	// find max common prefix
	idx := 0
	for isSameCharAt(idx) {
		idx++
	}
	if idx == 0 {
		return
	}

	// backup to '/'
	s := a[0]
	didBackup := false
	for idx > 0 && s[idx-1] != '/' {
		idx--
		didBackup = true
	}
	if didBackup && s[idx] == '/' {
		idx++
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

func dumpHeaders(r *http.Request) {
	logf(ctx(), "dumpHeaders:\n")
	for key, a := range r.Header {
		if len(a) == 1 {
			logf(ctx(), "%s: '%s'\n", key, a[0])
			continue
		}
		logf(ctx(), "%s: '%#v'\n", key, a)
	}
}

// returns -1 if directory doesn't exist
func getDirectorySize(dir string) int64 {
	var totalSize int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			i, err := d.Info()
			if err == nil {
				totalSize += i.Size()
			}
		}
		return nil
	})
	return totalSize
}
