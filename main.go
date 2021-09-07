package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	Path       string
	Size       int64
	pathOnDisk string
	pathInForm string
}

// describes a single website
type Site struct {
	token     string
	createdOn time.Time
	totalSize int64
	files     []*siteFile
	filePaths []string
}

var (
	flgHTTPPort   = 5550
	sites         []*Site
	muSites       sync.Mutex
	dataDirCached string
)

func getDataDir() string {
	if dataDirCached != "" {
		return dataDirCached
	}
	dataDirCached = "data"
	// remove stale files for sites
	os.RemoveAll(dataDirCached)
	must(os.MkdirAll(dataDirCached, 0755))
	return dataDirCached
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

func serveJSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	d, err := json.Marshal(v)
	if err != nil {
		logf(r.Context(), "serveJSON: json.Marshal() failed with '%s'\n", err)
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "foo.json", time.Now(), bytes.NewReader(d))
}

// GET /api/summary.json?token=${token}
func handleAPISiteFiles(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	logf(r.Context(), "handleAPISiteFiles: '%s', token: '%s'\n", r.URL, token)
	if token == "" {
		http.NotFound(w, r)
		return
	}
	site := findSiteByToken(token)
	if site == nil {
		http.NotFound(w, r)
		return
	}
	serveJSON(w, r, site.files)
}

// GET /api/summary.json
func handleAPISummary(w http.ResponseWriter, r *http.Request) {
	logf(r.Context(), "handleAPISummary: '%s'\n", r.URL)
	sitesCount := 0
	sitesSize := int64(0)
	{
		muSites.Lock()
		sitesCount = len(sites)
		for _, site := range sites {
			sitesSize += site.totalSize
		}
		muSites.Unlock()
	}
	summary := struct {
		SitesCount   int
		SitesSize    int64
		SitesSizeStr string
	}{
		SitesCount:   sitesCount,
		SitesSize:    sitesSize,
		SitesSizeStr: humanizeSize(sitesSize),
	}
	serveJSON(w, r, summary)
}

// POST /upload
// POST /api/upload
func handleUpload(w http.ResponseWriter, r *http.Request) {
	token := generateToken(tokenLength)
	dir := filepath.Join(getDataDir(), token)
	ct := r.Header.Get("content-type")
	logf(r.Context(), "handleUpload: '%s', Content-Type: '%s', token: '%s', dir: '%s'\n", r.URL, ct, token, dir)
	err := r.ParseMultipartForm(maxSize10Mb)
	if err != nil {
		logf(r.Context(), "handleUpload: r.ParseMultipartForm() failed with '%s'\n", err)
		http.NotFound(w, r)
		return
	}

	form := r.MultipartForm
	totalSize := int64(0)
	files := []*siteFile{}
	defer form.RemoveAll()

	// first collect info about file names so that we can trim common prefix
	paths := []string{}
	for path, fileHeaders := range form.File {
		// windows => unix pathname
		pathCanonical := strings.Replace(path, "\\", "/", -1)
		pathCanonical = strings.TrimPrefix(pathCanonical, "/")
		// if there are multiple files with the same name we only use first
		fh := fileHeaders[0]
		file := &siteFile{
			Path:       pathCanonical,
			Size:       fh.Size,
			pathInForm: path,
		}
		totalSize += fh.Size
		files = append(files, file)
		paths = append(paths, pathCanonical)
	}
	if len(files) == 0 {
		http.NotFound(w, r)
		return
	}
	trimCommonPrefix(paths)
	for i := 0; i < len(files); i++ {
		files[i].Path = paths[i]
		files[i].pathOnDisk = filepath.Join(dir, files[i].Path)
	}
	for _, file := range files {
		fh := form.File[file.pathInForm][0]
		fr, err := fh.Open()
		if err != nil {
			logf(r.Context(), "handleUpload: fh.Open() on '%s' failed with '%s'\n", file.pathInForm, err)
			http.NotFound(w, r)
			return
		}
		pathOnDisk := file.pathOnDisk
		if err != nil {
			logf(r.Context(), "handleUpload: os.MkdirAll('%s') failed with '%s'\n", filepath.Dir(pathOnDisk), err)
			fr.Close()
			http.NotFound(w, r)
			return
		}
		err = os.MkdirAll(filepath.Dir(pathOnDisk), 0755)
		if err != nil {
			logf(r.Context(), "handleUpload: os.MkdirAll('%s') failed with '%s'\n", filepath.Dir(pathOnDisk), err)
			fr.Close()
			http.NotFound(w, r)
			return
		}
		fw, err := os.Create(pathOnDisk)
		if err != nil {
			logf(r.Context(), "handleUpload: os.Create('%s') failed with '%s'\n", pathOnDisk, err)
			fr.Close()
			http.NotFound(w, r)
			return
		}
		_, err = io.Copy(fw, fr)
		if err != nil {
			logf(r.Context(), "handleUpload: io.Copy() on '%s' failed with '%s'\n", pathOnDisk, err)
			http.NotFound(w, r)
			return
		}
		fr.Close()
		totalSize += fh.Size
		logf(r.Context(), "handleUpload: file '%s' (canonical: '%s'), name: '%s' of size %s saved as '%s'\n", file.pathInForm, file.Path, fh.Filename, humanizeSize(fh.Size), pathOnDisk)
	}
	logf(r.Context(), "handleUpload: %d files of total size %s\n", len(files), humanizeSize(totalSize))

	site := &Site{
		token:     token,
		createdOn: time.Now(),
		files:     files,
		filePaths: paths,
		totalSize: totalSize,
	}
	muSites.Lock()
	sites = append(sites, site)
	muSites.Unlock()

	var uri string
	if len(files) > 1 {
		uri = fmt.Sprintf("https://%s/p/%s/", r.Host, token)
	} else {
		f := files[0]
		uri = fmt.Sprintf("https://%s/p/%s/%s", r.Host, token, f.Path)
	}
	rsp := bytes.NewReader([]byte(uri))
	http.ServeContent(w, r, "result.txt", time.Now(), rsp)
}

func expireSitesLoop() {
	dataDir := getDataDir()
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
				dir := filepath.Join(dataDir, site.token)
				os.RemoveAll(dir)
				logf(ctx(), "expired site '%s' and deleted directory '%s'\n", site.token, dir)
				nExpired++
			}
		}
		sites = newSites
		muSites.Unlock()
		logf(ctx(), "expireSitesLoop: expired %d sites\n", nExpired)
	}
}

func findSiteByToken(token string) *Site {
	muSites.Lock()
	defer muSites.Unlock()
	for _, site := range sites {
		if site.token == token {
			return site
		}
	}
	return nil

}
func findSiteByPath(path string) *Site {
	path = strings.TrimPrefix(path, "/p/")
	// extract token
	if len(path) < 6 {
		return nil
	}
	token := path[:6]
	return findSiteByToken(token)
}

func servePathInSite(w http.ResponseWriter, r *http.Request, path string, site *Site) {
	rest := path[9:] // strip /p/${token}
	if rest == "" {
		// TODO: maybe also add query params etc.
		newURL := path + "/"
		logf(r.Context(), "servePathInSite: redirecting '%s' to '%s'\n", path, newURL)
		http.Redirect(w, r, newURL, http.StatusTemporaryRedirect) // 307
		return
	}
	toFind := strings.TrimPrefix(rest, "/")
	if toFind == "" {
		if len(site.filePaths) == 1 {
			toFind = site.files[0].Path
		} else {
			toFind = "index.html"
		}
	}
	logf(r.Context(), "servePathInSite: path: '%s', rest: '%s', toFind: '%s'\n", path, rest, toFind)
	toFind2 := toFind + ".html" // also serve clean urls with ".html" stripped off
	findFileByPath := func() *siteFile {
		for _, f := range site.files {
			if f.Path == toFind {
				return f
			}
			if f.Path == toFind2 {
				return f
			}
		}
		return nil
	}
	file := findFileByPath()
	// TODO: serve a listing of files
	if file == nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, file.pathOnDisk)
}

// GET /p/${token}/${path}
func handlePreview(w http.ResponseWriter, r *http.Request) {
	logf(r.Context(), "handlePreview: '%s'\n", r.URL)
	path := r.URL.Path
	site := findSiteByPath(path)
	if site == nil {
		logf(r.Context(), "handlePreview: didn't find site\n")
		http.NotFound(w, r)
		return
	}
	servePathInSite(w, r, path, site)
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
	if strings.HasPrefix(path, "/api/upload") || strings.HasPrefix(path, "/upload") {
		handleUpload(w, r)
		return
	}
	if strings.HasPrefix(path, "/api/summary.json") {
		handleAPISummary(w, r)
		return
	}
	if strings.HasPrefix(path, "/api/site-files.json") {
		handleAPISiteFiles(w, r)
	}
	referer := r.Header.Get("referer")
	redirectURL := siteRedirectForPath(referer, r)
	if redirectURL != "" {
		logf(r.Context(), "httpIndex: redirectng '%s' => '%s'\n", path, redirectURL)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	// assuming those are our own html files
	logf(r.Context(), "handleIndex: '%s'\n", r.URL)
	serveFile(w, r, "www", path)
}

// if uploaded files use absolute urls, they'll have incorrect paths on
// our server
// we try to deduce from referer which site this request was meant to
// this builds the url on our site or "" if nothing is matching
func siteRedirectForPath(referer string, r *http.Request) string {
	// referer is a full URL https://${host}${path}
	// extract ${path}
	if referer == "" {
		return ""
	}
	logf(r.Context(), "siteRedirectForPath: '%s', host: '%s'\n", r.URL, r.Host)
	idx := strings.Index(referer, r.Host)
	if idx == -1 {
		return ""
	}
	path := referer[idx+len(r.Host):]
	logf(r.Context(), "siteRedirectForPath: path from referer: '%s', host: '%s'\n", path, r.Host)
	site := findSiteByPath(path)
	if site == nil {
		return ""
	}
	// TODO: add query params and hash
	newURL := "/p/" + site.token + r.URL.Path
	logf(r.Context(), "siteRedirectForPath: path: '%s', newURL: '%s', r.URL.RawQuery: '%s'\n", path, newURL, r.URL.RawQuery)
	return newURL
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
	logf(ctx, "Starting server on %s, data dir: '%s'\n", httpAddr, getDataDir())
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
