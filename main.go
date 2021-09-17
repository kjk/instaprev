package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
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
	name string // random token or premium site domain name
	// where files are stored
	// ${dataDir}/${name} for temporary sites
	// ${premiumDataDir}/${premiumName} for premium sites
	dir       string
	createdOn time.Time
	totalSize int64
	files     []*siteFile
	isSPA     bool
	isPremium bool

	// premium sites are hosted on their own subdomains
	// and need a password to upload
	uploadPassword string
}

func siteURL(r *http.Request, s *Site) string {
	host := r.Host
	if strings.HasSuffix(host, "gitpod.io") {
		// special case when testing on gitpod.io
		// no premium sites
		return fmt.Sprintf("https://%s/p/%s/", host, s.name)
	}
	// assume hosting where main site is under www.${hostBase}
	// and premium sites are at ${site}.${hostBase}
	parts := strings.Split(host, ".")
	hostBase := host
	n := len(parts)
	if n >= 2 {
		hostBase = parts[n-2] + "." + parts[n-1]
	}
	if s.isPremium {
		return fmt.Sprintf("https://%s.%s/", s.name, hostBase)
	}
	if n == 2 {
		return fmt.Sprintf("https://%s/p/%s/", hostBase, s.name)
	}
	return fmt.Sprintf("https://www.%s/p/%s/", hostBase, s.name)
}

var (
	flgHTTPPort           = 5550
	sites                 []*Site
	muSites               sync.Mutex
	dataDirCached         string
	premiumSitesDirCached string
	sitesPassword         string // preotects /sites url
)

func getSiteFilesFromDir(dir string) ([]*siteFile, int64) {
	var totalSize int64
	var res []*siteFile
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		i, err := d.Info()
		name := path[len(dir)+1:]
		site := &siteFile{
			Path:       name,
			Size:       i.Size(),
			pathOnDisk: path,
			pathInForm: name,
		}
		res = append(res, site)
		totalSize += i.Size()
		return nil
	})
	return res, totalSize
}

// for now we store premium sites in an env variable INSTA_PREV_SITES
// in the format:
// site1,password1
// site2,password2
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
			files, totalSize := getSiteFilesFromDir(dir)
			site := &Site{
				name:           name,
				uploadPassword: pwd,
				dir:            dir,
				createdOn:      time.Now(),
				isPremium:      true,
				totalSize:      totalSize,
				files:          files,
				isSPA:          true,
			}
			st, err := os.Lstat(dir)
			if err == nil {
				site.createdOn = st.ModTime()
			}
			logf(ctx(), "parsePremiumsSites: name: %s, upload password: %s, %d files, totalSize: %s\n", name, pwd, len(site.files), formatSize(site.totalSize))
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
	logf(ctx(), "parsePremiumSites: loaded %d sites\n", len(sites))
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
	if !strings.HasSuffix(s, "\n") {
		s = s + "\n"
	}
	io.WriteString(w, s)
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

// toggle SPA mode
// GET /__instantpreviewinternal/api/toggle-spa?name=${name}
func handleAPIToggleSpa(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	logf(r.Context(), "handleAPIToggleSpa: '%s', name: '%s'\n", r.URL, name)
	if name == "" {
		serveBadRequestError(w, r, "Error: missing 'name' argument to /__instantpreviewinternal/api/site-info.json")
		return
	}
	site := findSiteByName(name)
	if site == nil {
		logf(r.Context(), "handleAPIToggleSpa: didn't find site for name '%s'\n", name)
		http.NotFound(w, r)
		return
	}
	site.isSPA = !site.isSPA

	redirectURL := r.Header.Get("referer")
	if redirectURL == "" {
		if site.isPremium {
			redirectURL = "_dir"
		} else {
			redirectURL = fmt.Sprintf("/p/%s/_dir", site.name)
		}
	}
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// GET /__instantpreviewinternal/api/site-info.json?name=${name}
func handleAPISiteFiles(w http.ResponseWriter, r *http.Request) {
	//site, _ := findPremiumSiteFromHost(r.Host)
	var site *Site
	if site == nil {
		name := r.FormValue("name")
		logf(r.Context(), "handleAPISiteFiles: '%s', name: '%s'\n", r.URL, name)
		if name == "" {
			serveBadRequestError(w, r, "Error: missing 'name' argument to /__instantpreviewinternal/api/site-info.json")
			return
		}
		site = findSiteByName(name)
		if site == nil {
			logf(r.Context(), "handleAPISiteFiles: didn't find site for name '%s'\n", name)
			http.NotFound(w, r)
			return
		}
	} else {
		logf(ctx(), "handleAPISiteFiles: found premium site %s\n", site.name)
	}

	logf(ctx(), "handleAPISiteFiles: '%s', site: %s, %d files, premium?: %v\n", r.URL.Path, site.name, len(site.files), site.isPremium)
	v := &siteFilesResult{
		Files: site.files,
		IsSPA: site.isSPA,
	}
	serveJSON(w, r, v)
}

// GET /__instantpreviewinternal/api/summary.json
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

// GET /__instantpreviewinternal/api/sites.json
func handleAPISites(w http.ResponseWriter, r *http.Request) {
	pwd := r.URL.Query().Get("pwd")
	logf(r.Context(), "handleAPISites: '%s', pwd: '%s'\n", r.URL, pwd)
	if pwd != sitesPassword {
		logf(r.Context(), "handleAPISites: password in url doesn't match sitesPassword\n")
		http.NotFound(w, r)
		return
	}
	var v []interface{}
	muSites.Lock()
	for _, site := range sites {
		si := struct {
			Name      string
			FileCount int
			TotalSize int64
			IsSPA     bool
			IsPremium bool
			URL       string
		}{
			Name:      site.name,
			FileCount: len(site.files),
			TotalSize: site.totalSize,
			IsSPA:     site.isSPA,
			IsPremium: site.isPremium,
			URL:       siteURL(r, site),
		}
		v = append(v, si)
	}
	muSites.Unlock()
	serveJSON(w, r, v)
}

// GET /sites
func handleSites(w http.ResponseWriter, r *http.Request) {
	pwd := r.URL.Query().Get("pwd")
	logf(r.Context(), "handleAPISites: '%s', pwd: '%s'\n", r.URL, pwd)
	if pwd != sitesPassword {
		logf(r.Context(), "handleAPISites: password in url doesn't match sitesPassword\n")
		http.NotFound(w, r)
		return
	}
	filePath := filepath.Join("www", "listSites.html")
	http.ServeFile(w, r, filePath)
}

func expireSitesLoop() {
	for {
		time.Sleep(time.Hour)
		var newSites []*Site
		muSites.Lock()
		nExpired := 0
		for _, site := range sites {
			elapsed := time.Since(site.createdOn)
			// premium sites do not expire
			if site.isPremium || elapsed < timeTwoHours {
				newSites = append(newSites, site)
			} else {
				os.RemoveAll(site.dir)
				logf(ctx(), "expired site '%s' and deleted directory '%s'\n", site.name, site.dir)
				nExpired++
			}
		}
		sites = newSites
		muSites.Unlock()
		logf(ctx(), "expireSitesLoop: expired %d sites\n", nExpired)
	}
}

func findSiteByName(name string) *Site {
	muSites.Lock()
	defer muSites.Unlock()
	for _, site := range sites {
		if site.name == name {
			return site
		}
	}
	return nil

}
func findSiteByPath(path string) *Site {
	path = strings.TrimPrefix(path, "/p/")
	// extract name
	if len(path) < 6 {
		return nil
	}
	return findSiteByName(path[:6])
}

func servePathInSite(w http.ResponseWriter, r *http.Request, site *Site, path string) {
	var realPath string
	if site.isPremium {
		realPath = path
	} else {
		realPath = path[9:] // strip /p/${name}
	}
	if realPath == "" {
		// TODO: maybe also add query params etc.
		newURL := path + "/"
		logf(r.Context(), "servePathInSite: redirecting '%s' to '%s'\n", path, newURL)
		http.Redirect(w, r, newURL, http.StatusTemporaryRedirect) // 307
		return
	}
	toFind := strings.TrimPrefix(realPath, "/")
	logf(r.Context(), "servePathInSite: toFind: '%s'\n", toFind)

	// in SPA mode or with custom 404.html this is a special url that shows files
	if toFind == "_dir" {
		path := filepath.Join("www", "listSiteFiles.html")
		logf(r.Context(), "servePathInSite: serving '%s'\n", path)
		http.ServeFile(w, r, path)
		return
	}

	var fileIndex *siteFile
	var file404 *siteFile

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

	logf(r.Context(), "servePathInSite: path: '%s', rest: '%s', toFind: '%s', hasIndex: %v, has404: %v\n", path, realPath, toFind, fileIndex != nil, file404 != nil)
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

// GET /p/${name}/${path}
func handlePreview(w http.ResponseWriter, r *http.Request) {
	logf(r.Context(), "handlePreview: '%s'\n", r.URL)
	path := r.URL.Path
	site := findSiteByPath(path)
	if site == nil {
		logf(r.Context(), "handlePreview: didn't find site\n")
		http.NotFound(w, r)
		return
	}
	servePathInSite(w, r, site, path)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			logf(ctx(), "handleIndex: caught panic serving URL '%s'\n", r.URL)
			stack := debug.Stack()
			serveErrorStatus(w, r, http.StatusInternalServerError, "Error: panic serving '%s' with:\n%s\n%s\n", r.URL, p, string(stack))
			os.Stderr.Write(stack)
		}
	}()

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		handleUpload(w, r)
		return
	}

	path := r.URL.Path

	// this happens before resolving premium site info
	// to minimize possibility of conflict with the site
	if path == "/__instantpreviewinternal/api/site-info.json" {
		handleAPISiteFiles(w, r)
		return
	}
	if path == "/__instantpreviewinternal/api/summary.json" {
		handleAPISummary(w, r)
		return
	}
	if path == "/__instantpreviewinternal/api/sites.json" {
		handleAPISites(w, r)
		return
	}
	if path == "/__instantpreviewinternal/api/toggle-spa" {
		handleAPIToggleSpa(w, r)
		return
	}
	if path == "/__instantpreviewinternal/main.js" {
		filePath := filepath.Join("www", "main.js")
		http.ServeFile(w, r, filePath)
		return
	}
	if path == "/__instantpreviewinternal/main.css" {
		filePath := filepath.Join("www", "main.css")
		http.ServeFile(w, r, filePath)
		return
	}

	site, name := findPremiumSiteFromHost(r.Host)
	if site != nil {
		servePathInSite(w, r, site, path)
		return
	}

	if name != "" {
		// request for premium site but no such site available
		filePath := filepath.Join("www", "noPremiumSite.html")
		http.ServeFile(w, r, filePath)
		return
	}

	// handle explicitly for less logging
	if path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}

	if strings.HasPrefix(path, "/p/") {
		handlePreview(w, r)
		return
	}

	if strings.HasPrefix(path, "/api/upload") || strings.HasPrefix(path, "/upload") {
		handleUpload(w, r)
		return
	}

	if path == "/ping" {
		servePlainText(w, r, "pong")
		return
	}
	if path == "/sites" {
		handleSites(w, r)
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
	newURL := "/p/" + site.name + r.URL.Path
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

	sitesPassword = os.Getenv("SITES_PASSWORD")
	logf(ctx, "Starting server on http://%s, data dir: '%s', premium data dir: '%s', /sites password: '%s'\n", httpAddr, getDataDir(), getPremiumSitesDir(), sitesPassword)
	parsePremiumSites()

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
