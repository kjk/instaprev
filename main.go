package main

import (
	"archive/zip"
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
	tokenLength  = 6                // like transfer.sh
	maxSize10Mb  = 1024 * 1024 * 20 // this is 10 MB in html front-end
	timeTwoHours = time.Hour * 2
)

var blacklistedExt = []string{
	"exe",
	"mp4",
	"avi",
	"flv",
	"mpg",
	"mpeg",
	"mov",
	"mkv",
	"wmv",
	"dll",
	"so",
}

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

// "foo.BaR" => "bar"
func getExt(s string) string {
	s = filepath.Ext(s)
	s = strings.ToLower(s)
	return strings.TrimPrefix(s, ".")
}

func isZipFile(path string) bool {
	return getExt(path) == "zip"
}

func isBlacklistedFileType(path string) bool {
	ext := getExt(path)
	for _, s := range blacklistedExt {
		if ext == s {
			return true
		}
	}
	return false
}

func serveJSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	d, err := json.Marshal(v)
	if err != nil {
		serveInternalError(w, r, "serveJSON: json.Marshal() failed with '%s'\n", err)
		return
	}
	//logf(r.Context(), "serveJSON:\n%s\n", string(d))
	var zeroTime time.Time
	http.ServeContent(w, r, "foo.json", zeroTime, bytes.NewReader(d))
}

func serveErrorStatus(w http.ResponseWriter, r *http.Request, status int, s string, args ...interface{}) {
	if len(args) > 0 {
		s = fmt.Sprintf(s, args...)
	}
	logf(r.Context(), s)
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(s)))
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(s))
}

func serveBadRequestError(w http.ResponseWriter, r *http.Request, s string, args ...interface{}) {
	serveErrorStatus(w, r, http.StatusBadRequest, s, args...)
}

func serveInternalError(w http.ResponseWriter, r *http.Request, s string, args ...interface{}) {
	serveErrorStatus(w, r, http.StatusInternalServerError, s, args...)
}

func servePlainText(w http.ResponseWriter, r *http.Request, s string) {
	var zeroTime time.Time
	http.ServeContent(w, r, "foo.txt", zeroTime, bytes.NewReader([]byte(s)))
}

// GET /api/site-files.json?token=${token}
func handleAPISiteFiles(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	logf(r.Context(), "handleAPISiteFiles: '%s', token: '%s'\n", r.URL, token)
	if token == "" {
		serveBadRequestError(w, r, "Error: missing 'token' arg")
		return
	}
	site := findSiteByToken(token)
	if site == nil {
		logf(r.Context(), "handleAPISiteFiles: didn't find site for token '%s'\n", token)
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

type siteInfo struct {
	Token     string
	FileCount int
	TotalSize int64
}

// GET /api/sites.json
// TODO: protect with password
func handleAPISites(w http.ResponseWriter, r *http.Request) {
	logf(r.Context(), "handleAPISites: '%s'\n", r.URL)
	v := []siteInfo{}

	muSites.Lock()
	for _, site := range sites {
		si := siteInfo{
			Token:     site.token,
			FileCount: len(site.files),
			TotalSize: site.totalSize,
		}
		v = append(v, si)
	}
	muSites.Unlock()
	serveJSON(w, r, v)
}

func canonicalPath(path string) string {
	// windows => unix pathname
	path = strings.Replace(path, "\\", "/", -1)
	return strings.TrimPrefix(path, "/")
}

/*
func getSiteFileNames(site *Site) []string {
	var res []string
	for _, f := range site.files {
		res = append(res, f.Path)
	}
	return res
}
*/

// updates info in site
func unpackZipFiles(zipFiles []string, site *Site) error {
	var lastErr error
	dir := filepath.Join(getDataDir(), site.token)
	for _, zipFile := range zipFiles {
		logf(ctx(), "unpackZipFiles: unpacking '%s'\n", zipFile)
		st, err := os.Lstat(zipFile)
		if err != nil {
			lastErr = err
			logf(ctx(), "unpackZipFile: os.Lstat('%s') failed with '%s'\n", zipFile, err)
			continue
		}
		size := st.Size()
		f, err := os.Open(zipFile)
		if err != nil {
			lastErr = err
			logf(ctx(), "unpackZipFile: os.Open('%s') failed with '%s'\n", zipFile, err)
			continue
		}
		zr, err := zip.NewReader(f, size)
		if err != nil {
			lastErr = err
			logf(ctx(), "unpackZipFile: zip.NewReader() for '%s' failed with '%s'\n", zipFile, err)
			f.Close()
			continue
		}

		// trim common prefix. if files inside zip files are all under foo/,
		// we want to remove foo/ from the paths and host the files under root
		// TODO: possible that files extract from zip will over-write other files
		fileNames := []string{}
		for _, f := range zr.File {
			path := canonicalPath(f.Name)
			fileNames = append(fileNames, path)
		}
		trimCommonPrefix(fileNames)

		// now extract using fixed-up file names
		for i, f := range zr.File {
			if f.FileInfo().IsDir() {
				logf(ctx(), "unpackZipFile: skipping directory '%s' in '%s'\n", f.Name, zipFile)
				continue
			}
			if isBlacklistedFileType(f.Name) {
				logf(ctx(), "unpackZipFile: skipping blacklisted file '%s' in '%s'\n", f.Name, zipFile)
				continue
			}

			fr, err := f.Open()
			if err != nil {
				lastErr = err
				logf(ctx(), "unpackZipFile: f.Open() of '%s' in '%s' failed with '%s'\n", f.Name, zipFile, err)
				continue
			}
			path := filepath.Join(dir, fileNames[i])
			logf(ctx(), "  unpacking '%s' => '%s'\n", f.Name, path)

			err = os.MkdirAll(filepath.Dir(path), 755)
			if err != nil {
				fr.Close()
				lastErr = err
				logf(ctx(), "unpackZipFile: os.MkdirAll('%s') for '%s' failed with '%s'\n", filepath.Dir(path), zipFile, err)
				continue
			}

			w, err := os.Create(path)
			if err != nil {
				fr.Close()
				lastErr = err
				logf(ctx(), "unpackZipFile: os.Create('%s') for '%s' failed with '%s'\n", path, zipFile, err)
				continue
			}
			_, err = io.Copy(w, fr)
			fr.Close()
			err2 := w.Close()

			if err != nil || err2 != nil {
				lastErr = err
				if err == nil {
					lastErr = err2
				}
				logf(ctx(), "unpackZipFile: io.Copy() to '%s' for '%s' failed with '%s'\n", path, zipFile, err)
			}
			sf := &siteFile{
				Path:       fileNames[i],
				Size:       int64(f.UncompressedSize64),
				pathOnDisk: path,
				pathInForm: fileNames[i],
			}
			site.files = append(site.files, sf)
			site.totalSize += int64(f.UncompressedSize64)
		}
		f.Close()
	}
	return lastErr
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
		serveBadRequestError(w, r, "Error: handleUpload: r.ParseMultipartForm() failed with '%s'\n", err)
		return
	}

	form := r.MultipartForm
	totalSize := int64(0)
	files := []*siteFile{}
	defer form.RemoveAll()

	// first collect info about file names so that we can trim common prefix
	paths := []string{}
	for path, fileHeaders := range form.File {
		if isBlacklistedFileType(path) {
			continue
		}
		pathCanonical := canonicalPath(path)
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
		serveBadRequestError(w, r, "Error: no files")
		return
	}
	trimCommonPrefix(paths)

	var zipFiles []string
	for i := 0; i < len(files); i++ {
		pathOnDisk := filepath.Join(dir, files[i].Path)
		files[i].Path = paths[i]
		files[i].pathOnDisk = pathOnDisk
		if isZipFile(pathOnDisk) {
			zipFiles = append(zipFiles, pathOnDisk)
		}
	}

	for _, file := range files {
		fh := form.File[file.pathInForm][0]
		fr, err := fh.Open()
		if err != nil {
			serveInternalError(w, r, "Error: fh.Open() on '%s' failed with '%s'\n", file.pathInForm, err)
			return
		}
		pathOnDisk := file.pathOnDisk
		if err != nil {
			serveInternalError(w, r, "handleUpload: os.MkdirAll('%s') failed with '%s'\n", filepath.Dir(pathOnDisk), err)
			fr.Close()
			return
		}
		err = os.MkdirAll(filepath.Dir(pathOnDisk), 0755)
		if err != nil {
			serveInternalError(w, r, "handleUpload: os.MkdirAll('%s') failed with '%s'\n", filepath.Dir(pathOnDisk), err)
			fr.Close()
			return
		}
		fw, err := os.Create(pathOnDisk)
		if err != nil {
			serveInternalError(w, r, "handleUpload: os.Create('%s') failed with '%s'\n", pathOnDisk, err)
			fr.Close()
			return
		}
		_, err = io.Copy(fw, fr)
		if err != nil {
			serveInternalError(w, r, "handleUpload: io.Copy() on '%s' failed with '%s'\n", pathOnDisk, err)
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
		totalSize: totalSize,
	}

	// TODO: decide if I should delete the zip file after unpacking
	_ = unpackZipFiles(zipFiles, site)

	muSites.Lock()
	sites = append(sites, site)
	muSites.Unlock()

	var uri string
	if len(site.files) > 1 {
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
		if len(site.files) == 1 {
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
	if file == nil {
		path404 := filepath.Join("www", "404site.html")
		logf(r.Context(), "servePathInSite: file doesn't exist, serving '%s'\n", path404)
		http.ServeFile(w, r, path404)
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
	if path == "/api/summary.json" {
		handleAPISummary(w, r)
		return
	}
	if path == "/api/site-files.json" {
		handleAPISiteFiles(w, r)
		return
	}
	if path == "/api/sites.json" {
		handleAPISites(w, r)
		return
	}
	if path == "/ping" {
		servePlainText(w, r, "pong")
		return
	}

	// give priority to our own files
	dir := "www"
	uriPath := path
	logf(r.Context(), "serveFile: dir: '%s', uriPath: '%s'\n", dir, uriPath)
	fileName := strings.TrimPrefix(uriPath, "/")
	if fileName == "" {
		fileName = "index.html"
	}
	filePath := filepath.Join(dir, uriPath)
	if fileExists(filePath) {
		http.ServeFile(w, r, filePath)
		return
	}
	filePath += ".html"
	if fileExists(filePath) {
		http.ServeFile(w, r, filePath)
		return
	}

	referer := r.Header.Get("referer")
	redirectURL := siteRedirectForPath(referer, r)
	if redirectURL != "" {
		logf(r.Context(), "httpIndex: redirectng '%s' => '%s'\n", path, redirectURL)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	logf(r.Context(), "handleIndex: '%s' not found\n", r.URL)
	http.NotFound(w, r)
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
		go func() {
			_ = httpSrv.Shutdown(ctx)
		}()
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
