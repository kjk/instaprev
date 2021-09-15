package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
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
	token string
	// where files are stored
	// ${dataDir}/${token} for temporary sites
	// ${premiumDataDir}/${premiumName} for premium sites
	dir       string
	createdOn time.Time
	totalSize int64
	files     []*siteFile
	isSPA     bool

	// premium sites are hosted on their own subdomains
	// and need a token to upload
	premiumName    string
	uploadPassword string
}

var (
	flgHTTPPort           = 5550
	sites                 []*Site
	muSites               sync.Mutex
	dataDirCached         string
	premiumSitesDirCached string
)

// for now we store premium sites in an env variable INSTA_PREV_SITES
// in the format:
// site1,token1
// site2,token2
func parsePremiumSites() {
	logf(ctx(), "parsePremiumsSites:\n")
	parseSites := func(s string) {
		s = normalizeNewlines(s)
		lines := strings.Split(s, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			parts := strings.Split(l, ",")
			if len(parts) != 2 {
				logf(ctx(), "parsePremiumsSites: invalid line '%s'\n", l)
				continue
			}
			// TODO: sanitize name to be url and dir name compatible
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			pwd := strings.TrimSpace(parts[1])
			if len(name) == 0 || len(pwd) == 0 {
				logf(ctx(), "parsePremiumsSites: invalid line '%s'\n", l)
				continue
			}
			dir := filepath.Join(getPremiumSitesDir(), name)
			site := &Site{
				token:          name,
				premiumName:    name,
				uploadPassword: pwd,
				dir:            dir,
				createdOn:      time.Now(), // TODO: not really
			}
			logf(ctx(), "parsePremiumsSites: name: %s, upload password: %s\n", name, pwd)
			sites = append(sites, site)
		}
	}
	parseSites(os.Getenv("INSTA_PREV_SITES"))
	// this is on render.com
	d, err := os.ReadFile("/etc/secrets/premium_sites.txt")
	if err == nil {
		logf(ctx(), "parsePremiumSites: parsing from /etc/secrets/premium_sites.txt\n")
		parseSites(string(d))
	}
	logf(ctx(), "parsePremiumSitesFromEnv: loaded %d sites\n", len(sites))
}

func getPremiumSitesDir() string {
	if premiumSitesDirCached != "" {
		return premiumSitesDirCached
	}
	// on render.com
	if dirExists("/var/data") {
		premiumSitesDirCached = "/var/data"
	} else {
		premiumSitesDirCached = getDataDir()
	}
	return premiumSitesDirCached
}

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

type siteFilesResult struct {
	Files []*siteFile
	IsSPA bool
}

// GET /api/site-info.json?token=${token}
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
	v := &siteFilesResult{
		Files: site.files,
		IsSPA: site.isSPA,
	}
	serveJSON(w, r, v)
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
		SitesSizeStr: formatSize(sitesSize),
	}
	serveJSON(w, r, summary)
}

type siteInfo struct {
	Token     string
	FileCount int
	TotalSize int64
	IsSPA     bool
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
			IsSPA:     site.isSPA,
		}
		v = append(v, si)
	}
	muSites.Unlock()
	serveJSON(w, r, v)
}

func expireSitesLoop() {
	dataDir := getDataDir()
	for {
		time.Sleep(time.Hour)
		var newSites []*Site
		muSites.Lock()
		nExpired := 0
		for _, site := range sites {
			if site.premiumName != "" {
				// premium sites do not expire
				continue
			}
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
	logf(r.Context(), "servePathInSite: toFind: '%s'\n", toFind)

	// in SPA mode or with custom 404.html this is a special url that shows files
	if toFind == "_dir" {
		path := filepath.Join("www", "listSiteFiles.html")
		logf(r.Context(), "servePathInSite: serving '%s'\n", path)
		http.ServeFile(w, r, path)
		return
	}

	if toFind == "_spa" {
		// toggle SPA mode
		site.isSPA = !site.isSPA
		redirectURL := fmt.Sprintf("/p/%s/_dir", site.token)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	var fileIndex *siteFile
	var file404 *siteFile

	// TODO: could cache it
	for _, f := range site.files {
		if f.Path == "index.html" {
			fileIndex = f
			continue
		}
		if f.Path == "404.html" {
			file404 = f
		}
	}

	if toFind == "" {
		if len(site.files) == 1 {
			toFind = site.files[0].Path
		} else {
			toFind = "index.html"
		}
	}

	logf(r.Context(), "servePathInSite: path: '%s', rest: '%s', toFind: '%s', hasIndex: %v, has404: %v\n", path, rest, toFind, fileIndex != nil, file404 != nil)
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
		if site.isSPA && fileIndex != nil {
			logf(r.Context(), "serving index.html because '%s' not found and isSPA\n", toFind)
			file = fileIndex
		} else if file404 != nil {
			logf(r.Context(), "serving 404.html because '%s' not found\n", toFind)
			file = fileIndex
		}
	}
	if file != nil {
		logf(r.Context(), "servePathInSite: serving '%s'\n", file.pathOnDisk)
		http.ServeFile(w, r, file.pathOnDisk)
		return
	}

	path404 := filepath.Join("www", "listSiteFiles.html")
	logf(r.Context(), "servePathInSite: serving listSiteFiles.html because '%s' doesn't exist\n", toFind)
	http.ServeFile(w, r, path404)
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
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		handleUpload(w, r)
		return
	}

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
	if path == "/api/site-info.json" {
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
	if path == "/sites" {
		filePath := filepath.Join("www", "listSites.html")
		http.ServeFile(w, r, filePath)
		return
	}

	redirectURL := siteMaybeRedirectForPath(r)
	if redirectURL != "" {
		logf(r.Context(), "httpIndex: redirectng '%s' => '%s'\n", path, redirectURL)
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	dir := "www"
	uriPath := path
	logf(r.Context(), "serveFile: dir: '%s', uriPath: '%s'\n", dir, uriPath)
	fileName := strings.TrimPrefix(uriPath, "/")
	if fileName == "" {
		fileName = "index.html"
	}
	filePath := filepath.Join(dir, uriPath)
	if pathExists(filePath) {
		http.ServeFile(w, r, filePath)
		return
	}
	filePath += ".html"
	if pathExists(filePath) {
		http.ServeFile(w, r, filePath)
		return
	}

	logf(r.Context(), "handleIndex: '%s' not found\n", r.URL)
	http.NotFound(w, r)
}

// if uploaded files use absolute urls, they'll have incorrect paths on
// our server
// we try to deduce from referer which site this request was meant to
// this builds the url on our site or "" if nothing is matching
func siteMaybeRedirectForPath(r *http.Request) string {
	// referer is a full URL https://${host}${path}
	// extract ${path}
	referer := r.Header.Get("referer")
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
	parsePremiumSites()

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
	logf(ctx, "Starting server on http://%s, data dir: '%s', premium data dir: '%s'\n", httpAddr, getDataDir(), getPremiumSitesDir())
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

func deployToRender() {
	deployURL := os.Getenv("INSTANT_PREVIEW_DEPLOY_HOOK")
	panicIf(deployURL == "", "needs env variable INSTANT_PREVIEW_DEPLOY_HOOK")
	d, err := httpGet(deployURL)
	must(err)
	logf(ctx(), "%s\n", string(d))
}

func main() {
	var (
		flgRun    bool
		flgDeploy bool
	)
	{
		flag.BoolVar(&flgRun, "run", false, "run the server")
		flag.BoolVar(&flgDeploy, "deploy", false, "deploy to render.com")
		flag.Parse()
	}

	if flgDeploy {
		deployToRender()
		return
	}

	if flgRun {
		doRunServer()
		return
	}

	flag.Usage()
}
