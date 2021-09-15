package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err.Error())
	}
}

func panicIf(cond bool, args ...interface{}) {
	if !cond {
		return
	}
	s := "condition failed"
	if len(args) > 0 {
		s = fmt.Sprintf("%s", args[0])
		if len(args) > 1 {
			s = fmt.Sprintf(s, args[1:]...)
		}
	}
	panic(s)
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

func normalizeNewlines(s string) string {
	// replace CR LF (windows) with LF (unix)
	s = strings.Replace(s, string([]byte{13, 10}), "\n", -1)
	// replace CF (mac) with LF (unix)
	s = strings.Replace(s, string([]byte{13}), "\n", -1)
	return s
}

func formatSize(n int64) string {
	sizes := []int64{1024 * 1024 * 1024, 1024 * 1024, 1024}
	suffixes := []string{"GB", "MB", "kB"}

	for i, size := range sizes {
		if n >= size {
			s := fmt.Sprintf("%.2f", float64(n)/float64(size))
			return strings.TrimSuffix(s, ".00") + " " + suffixes[i]
		}
	}
	return fmt.Sprintf("%d bytes", n)
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
	// backup to '/'
	s := a[0]
	didBackup := false
	for idx > 0 && s[idx] != '/' {
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

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// can be used for http.Get() requests with better timeouts. New one must be created
// for each Get() request
func newTimeoutClient(connectTimeout time.Duration, readWriteTimeout time.Duration) *http.Client {
	timeoutDialer := func(cTimeout time.Duration, rwTimeout time.Duration) func(net, addr string) (c net.Conn, err error) {
		return func(netw, addr string) (net.Conn, error) {
			conn, err := net.DialTimeout(netw, addr, cTimeout)
			if err != nil {
				return nil, err
			}
			conn.SetDeadline(time.Now().Add(rwTimeout))
			return conn, nil
		}
	}

	return &http.Client{
		Transport: &http.Transport{
			Dial:  timeoutDialer(connectTimeout, readWriteTimeout),
			Proxy: http.ProxyFromEnvironment,
		},
	}
}

func httpGet(url string) ([]byte, error) {
	// default timeout for http.Get() is really long, so dial it down
	// for both connection and read/write timeouts
	timeoutClient := newTimeoutClient(time.Second*120, time.Second*120)
	resp, err := timeoutClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("'%s': status code not 200 (%d)", url, resp.StatusCode))
	}
	return ioutil.ReadAll(resp.Body)
}
