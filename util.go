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

func stringsTrimSlashPrefix(a []string) {
	for i, s := range a {
		a[i] = strings.TrimLeft(s, `\/`)
	}
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

func httpDownload(url string) ([]byte, error) {
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
