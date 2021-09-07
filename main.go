package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	flgHTTPPort = 5550
)

func handleUpload(w http.ResponseWriter, r *http.Request) {
	// TODO: implement me
	logf(r.Context(), "handleUpload: '%s'\n", r.URL)
	http.NotFound(w, r)
}

func handlePreview(w http.ResponseWriter, r *http.Request) {
	// TODO: implement me
	logf(r.Context(), "handlePreview: '%s'\n", r.URL)
	http.NotFound(w, r)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Path
	logf(r.Context(), "handleIndex: '%s'\n", r.URL)
	if strings.HasPrefix(uri, "/p/") {
		handlePreview(w, r)
		return
	}
	if strings.HasPrefix(uri, "/api/upload") {
		handleUpload(w, r)
		return
	}
	http.NotFound(w, r)
}

func doRunServer() {
	httpAddr := fmt.Sprintf(":%d", flgHTTPPort)
	if isWindows() {
		// prevents windows firewall warning
		httpAddr = fmt.Sprintf("localhost:%d", flgHTTPPort)
	}
	mux := &http.ServeMux{}
	mux.HandleFunc("/", handleIndex)
	var handler http.Handler = mux
	httpSrv := &http.Server{
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second, // introduced in Go 1.8
		Handler:      handler,
	}
	httpSrv.Addr = httpAddr
	ctx := ctx()
	logf(ctx, "Starting server on %s\n", httpAddr)
	chServerClosed := make(chan bool, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		// mute error caused by Shutdown()
		if err == http.ErrServerClosed {
			err = nil
		}
		must(err)
		logf(ctx, "HTTP server shutdown gracefully\n")
		chServerClosed <- true
	}()

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt /* SIGINT */, syscall.SIGTERM)

	sig := <-c
	logf(ctx, "Got signal %s\n", sig)

	if httpSrv != nil {
		// Shutdown() needs a non-nil context
		_ = httpSrv.Shutdown(context.Background())
		select {
		case <-chServerClosed:
			// do nothing
		case <-time.After(time.Second * 5):
			// timeout
		}
	}
}

func main() {
	var (
		flgRun bool
	)
	{
		flag.BoolVar(&flgRun, "run", false, "run the server")
		flag.Parse()
	}
	if flgRun {
		doRunServer()
		return
	}
	flag.Usage()
}
