package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxSize20Mb = 1024 * 1024 * 20 // this is 10 MB in html front-end
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
func canonicalPath(path string) string {
	// windows => unix pathname
	path = strings.Replace(path, "\\", "/", -1)
	return strings.TrimPrefix(path, "/")
}

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
		stringsTrimSlashPrefix(fileNames)
		trimCommonDirPrefix(fileNames)

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

func isSPA(r *http.Request) bool {
	q := r.URL.RawQuery
	q = strings.ToLower(q)
	if strings.Contains(q, "spa") {
		logf(r.Context(), "isSPA: '%s' is SPA\n", r.URL)
		return true
	}
	return false
}

// this is an upload of a raw file. try to auto-detect what it is
func handleUploadMaybeRaw(w http.ResponseWriter, r *http.Request) {
	token := generateToken(tokenLength)
	tmpPath := filepath.Join(getDataDir(), token+".dat")
	defer os.Remove(tmpPath)
	logf(r.Context(), "handleUploadMaybeRaw: '%s', token: '%s', tmpPath: '%s'\n", r.URL, token, tmpPath)

	f, err := os.Create(tmpPath)
	if err != nil {
		logf(r.Context(), "handleUploadMaybeRaw: os.Create('%s') failed with '%s'\n", tmpPath, err)
		http.NotFound(w, r)
		return
	}
	_, err = io.Copy(f, r.Body)
	if err != nil {
		logf(r.Context(), "handleUploadMaybeRaw: io.Copy() for '%s' failed with '%s'\n", tmpPath, err)
		http.NotFound(w, r)
		return
	}
	err = f.Close()
	if err != nil {
		logf(r.Context(), "handleUploadMaybeRaw: f.Close() failed with '%s'\n", err)
		http.NotFound(w, r)
		return
	}
	site := &Site{
		token:     token,
		createdOn: time.Now(),
		totalSize: 0,
		isSPA:     isSPA(r),
	}

	path := r.URL.Path
	isZip := isZipFile(path)
	if isZip || path == "/upload" || path == "/upload/api" {
		// assume that uploads to /upload are .zip files
		// because that's what tutorial says
		// TODO: should try to auto-detect name of the file
		zipFiles := []string{tmpPath}
		// TODO: decide if I should delete the zip file after unpacking
		_ = unpackZipFiles(zipFiles, site)
	} else {
		// otherwise save upload to /foo.txt as foo.txt
		if !isBlacklistedFileType(path) {
			path = canonicalPath(path)
			pathOnDisk := filepath.Join(getDataDir(), token, path)
			err = os.MkdirAll(filepath.Dir(pathOnDisk), 0755)
			if err != nil {
				serveInternalError(w, r, "Error: handleUploadMaybeRaw: os.MkdirAll('%s') failed with '%s'", filepath.Dir(pathOnDisk), err)
				return
			}
			err = os.Rename(tmpPath, pathOnDisk)
			if err != nil {
				serveInternalError(w, r, "Error: handleUploadMaybeRaw: os.Rename('%s', '%s') failed with '%s'", tmpPath, pathOnDisk, err)
				return
			}
			size := int64(0)
			st, err := os.Lstat(pathOnDisk)
			must(err)
			size = st.Size()
			sf := &siteFile{
				Path:       path,
				Size:       size,
				pathOnDisk: pathOnDisk,
				pathInForm: path,
			}
			site.files = append(site.files, sf)
		}
	}

	if len(site.files) == 0 {
		http.NotFound(w, r)
		return
	}

	muSites.Lock()
	sites = append(sites, site)
	muSites.Unlock()

	var uri string
	if len(site.files) > 1 {
		uri = fmt.Sprintf("https://%s/p/%s/", r.Host, token)
	} else {
		f := site.files[0]
		uri = fmt.Sprintf("https://%s/p/%s/%s", r.Host, token, f.Path)
	}
	rsp := bytes.NewReader([]byte(uri))
	http.ServeContent(w, r, "result.txt", time.Now(), rsp)
}

// POST /upload
// POST /api/upload
func handleUpload(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("content-type")
	if ct == "" {
		handleUploadMaybeRaw(w, r)
		return
	}
	token := generateToken(tokenLength)
	dir := filepath.Join(getDataDir(), token)
	host := r.URL.Hostname()
	logf(r.Context(), "handleUpload: '%s', Content-Type: '%s', host: '%s', token: '%s', dir: '%s'\n", r.URL, ct, host, token, dir)
	err := r.ParseMultipartForm(maxSize20Mb)
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
	stringsTrimSlashPrefix(paths)
	trimCommonDirPrefix(paths)

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
		isSPA:     isSPA(r),
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
		f := site.files[0]
		uri = fmt.Sprintf("https://%s/p/%s/%s", r.Host, token, f.Path)
	}
	rsp := bytes.NewReader([]byte(uri))
	http.ServeContent(w, r, "result.txt", time.Now(), rsp)
}
