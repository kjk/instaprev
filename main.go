package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	tokenLength  = 6 // like transfer.sh
	maxSize10Mb  = 1024 * 1024 * 10
	timeTwoHours = time.Hour * 2
)

type siteFile struct {
	path    string
	size    int64
	content []byte
}

// describes a single website
type Site struct {
	token     string
	createdOn time.Time
	files     []siteFile
	filePaths []string
}

var (
	flgHTTPPort = 5550
	sites       []*Site
	muSites     sync.Mutex
)

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

func handleUpload(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("content-type")
	logf(r.Context(), "handleUpload: '%s', Content-Type: '%s'\n", r.URL, ct)
	err := r.ParseMultipartForm(maxSize10Mb)
	if err != nil {
		logf(r.Context(), "handleUpload: r.ParseMultipartForm() failed with '%s'\n", err)
		http.NotFound(w, r)
		return
	}
	form := r.MultipartForm
	totalSize := int64(0)
	files := []siteFile{}
	for path, fileHeaders := range form.File {
		// if there are multiple files with the same name we only use first
		fh := fileHeaders[0]
		pathCanonical := strings.TrimPrefix(path, "/")
		// windows => unix pathname
		pathCanonical = strings.Replace(pathCanonical, "\\", "/", -1)
		file := siteFile{
			path: pathCanonical,
			size: fh.Size,
		}
		fr, err := fh.Open()
		if err != nil {
			logf(r.Context(), "handleUpload: fh.Open() on '%s' failed with '%s'\n", path, err)
			http.NotFound(w, r)
			return
		}
		d, err := ioutil.ReadAll(fr)
		if err != nil {
			logf(r.Context(), "handleUpload: ioutil.ReadAll() on '%s' failed with '%s'\n", path, err)
			http.NotFound(w, r)
			return
		}
		fr.Close()
		file.content = d
		files = append(files, file)
		totalSize += fh.Size
		logf(r.Context(), "handleUpload: file '%s' (canonical: '%s'), name: '%s' of size %s\n", path, pathCanonical, fh.Filename, humanizeSize(fh.Size))
	}
	logf(r.Context(), "handleUpload: %d files of total size %s\n", len(files), humanizeSize(totalSize))
	if len(files) == 0 {
		http.NotFound(w, r)
		return
	}

	paths := []string{}
	{
		for _, f := range files {
			paths = append(paths, f.path)
		}
		trimCommonPrefix(paths)
		for i := 0; i < len(files); i++ {
			files[i].path = paths[i]
		}
	}

	token := generateToken(tokenLength)
	site := &Site{
		token:     token,
		createdOn: time.Now(),
		files:     files,
		filePaths: paths,
	}
	muSites.Lock()
	sites = append(sites, site)
	muSites.Unlock()

	var uri string
	if len(files) > 1 {
		uri = fmt.Sprintf("https://%s/p/%s/", r.Host, token)
	} else {
		f := files[0]
		uri = fmt.Sprintf("https://%s/p/%s/%s", r.Host, token, f.path)
	}
	rsp := bytes.NewReader([]byte(uri))
	http.ServeContent(w, r, "result.txt", time.Now(), rsp)
}

func expireSitesLoop() {
	for {
		time.Sleep(time.Hour)
		var newSites []*Site
		muSites.Lock()
		nExpired := 0
		for _, site := range sites {
			elapsed := time.Since(site.createdOn)
			if elapsed < timeTwoHours {
				newSites = append(newSites, site)
			} else {
				nExpired++
			}
		}
		sites = newSites
		muSites.Unlock()
		logf(ctx(), "expireSitesLoop: expired %d sites\n", nExpired)
	}
}

func findSiteByPath(path string) *Site {
	path = strings.TrimPrefix(path, "/p/")
	// extract token
	if len(path) < 6 {
		return nil
	}
	token := path[:6]
	muSites.Lock()
	defer muSites.Unlock()
	for _, site := range sites {
		if site.token == token {
			return site
		}
	}
	return nil
}

// /p/${token}
func handlePreview(w http.ResponseWriter, r *http.Request) {
	logf(r.Context(), "handlePreview: '%s'\n", r.URL)
	path := r.URL.Path
	site := findSiteByPath(path)
	if site == nil {
		logf(r.Context(), "handlePreview: didn't find site\n")
		http.NotFound(w, r)
		return
	}
	rest := path[6:]
	if rest == "" {
		// TODO: maybe also add query params etc.
		newURL := path + "/"
		http.Redirect(w, r, newURL, http.StatusTemporaryRedirect) // 307
		return
	}
	path = strings.TrimPrefix(rest, "/")
	logf(r.Context(), "handlePreview: path: '%s', files: %v\n", path, site.filePaths)
	findFileByPath := func() *siteFile {
		for _, f := range site.files {
			if f.path == path {
				return &f
			}
		}
		return nil
	}
	file := findFileByPath()
	if file == nil {
		http.NotFound(w, r)
		return
	}
	fr := bytes.NewReader(file.content)
	http.ServeContent(w, r, file.path, site.createdOn, fr)
}

func serveFile(w http.ResponseWriter, r *http.Request, dir string, uriPath string) {
	logf(r.Context(), "serveFile: dir: '%s', uriPath: '%s'\n", dir, uriPath)
	fileName := strings.TrimPrefix(uriPath, "/")
	if fileName == "" {
		fileName = "index.html"
	}
	// TODO: server 404.html if not found
	path := filepath.Join(dir, uriPath)
	http.ServeFile(w, r, path)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/p/") {
		handlePreview(w, r)
		return
	}
	if strings.HasPrefix(path, "/api/upload") {
		handleUpload(w, r)
		return
	}
	referer := r.Header.Get("referer")
	if referer != "" {
		logf(r.Context(), "handleIndex: referer: '%s'\n", referer)
		//site := findSiteByPath(referer)
	}

	logf(r.Context(), "handleIndex: '%s'\n", r.URL)
	serveFile(w, r, "www", path)
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

	go expireSitesLoop()

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt /* SIGINT */, syscall.SIGTERM)

	sig := <-c
	logf(ctx, "Got signal %s\n", sig)

	if httpSrv != nil {
		// Shutdown() needs a non-nil context
		_ = httpSrv.Shutdown(ctx)
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
