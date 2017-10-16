package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Debian/pk4/internal/index"
)

const srcfilesResponse = `{"_comment": "foo", "version": "2.10-1", "result": [{"hash": "f7bebf6f9c62a2295e889f66e05ce9bfaed9ace3"}, {"hash": "58d7c33cffb9ccda8f31781b7c9be0c4f6d1cb58"}, {"hash": "baef5bf30c74a138561a4395794447b1a09d243f"}], "fileinfo": {"baef5bf30c74a138561a4395794447b1a09d243f": [{"name": "hello_2.10-1.debian.tar.xz", "archive_name": "debian", "path": "/pool/main/h/hello", "first_seen": "20150322T153011Z", "size": 6072}, {"name": "hello_2.10-1.debian.tar.xz", "archive_name": "debian-debug", "path": "/pool/main/h/hello", "first_seen": "20170315T030828Z", "size": 6072}], "f7bebf6f9c62a2295e889f66e05ce9bfaed9ace3": [{"name": "hello_2.10.orig.tar.gz", "archive_name": "debian", "path": "/pool/main/h/hello", "first_seen": "20150322T153011Z", "size": 725946}, {"name": "hello-traditional_2.10.orig.tar.gz", "archive_name": "debian", "path": "/pool/main/h/hello-traditional", "first_seen": "20150322T153011Z", "size": 725946}, {"name": "hello_2.10.orig.tar.gz", "archive_name": "debian-debug", "path": "/pool/main/h/hello", "first_seen": "20170315T030828Z", "size": 725946}, {"name": "hello_2.10.orig.tar.gz", "archive_name": "debian-security", "path": "/pool/updates/main/h/hello", "first_seen": "20170419T102349Z", "size": 725946}], "58d7c33cffb9ccda8f31781b7c9be0c4f6d1cb58": [{"name": "hello_2.10-1.dsc", "archive_name": "debian", "path": "/pool/main/h/hello", "first_seen": "20150322T153011Z", "size": 1323}, {"name": "hello_2.10-1.dsc", "archive_name": "debian-debug", "path": "/pool/main/h/hello", "first_seen": "20170315T030828Z", "size": 1323}]}, "package": "hello"}`

func TestDownload(t *testing.T) {
	t.Parallel()

	for _, entry := range []struct {
		name     string
		fallback bool
		idx      index.URIs
	}{
		{
			name:     "MirrorFromIndex",
			fallback: false,
			idx: index.URIs{
				index.Source{
					Package: "hello",
					Version: mustParseVersion("2.10-1"),
				}: index.DSC{
					URL:  "/debian/pool/main/h/hello/hello_2.10-1.dsc",
					Size: 733341,
				},
			},
		},

		{
			name:     "MirrorFromSnapshot",
			fallback: false,
		},

		{
			name:     "Snapshot",
			fallback: true,
		},
	} {
		entry := entry // copy
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()

			dest, err := ioutil.TempDir("", "pk4test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dest)

			mux := http.NewServeMux()
			mux.HandleFunc("/mr/package/hello/2.10-1/srcfiles", func(w http.ResponseWriter, r *http.Request) {
				if r.FormValue("fileinfo") != "1" {
					http.Error(w, "expected ?fileinfo=1", http.StatusBadRequest)
					return
				}
				w.Write([]byte(srcfilesResponse))
			})
			if entry.fallback {
				mux.Handle("/archive/debian/20150322T153011Z/pool/main/h/hello/",
					http.StripPrefix("/archive/debian/20150322T153011Z/pool/main/h/hello/",
						http.FileServer(http.Dir("testdata/Download"))))
			} else {
				mux.Handle("/debian/pool/main/h/hello/",
					http.StripPrefix("/debian/pool/main/h/hello/",
						http.FileServer(http.Dir("testdata/Download"))))
			}
			ts := httptest.NewServer(mux)
			defer ts.Close()

			i := invocation{
				snapshotBase:   ts.URL + "/",
				mirrorUrl:      ts.URL + "/debian",
				verbose:        *verbose,
				dest:           dest,
				diskUsageLimit: 50 * 1024 * 1024, // 50 MB
				lookPath: func(file string) (string, error) {
					fake := filepath.Join("testdata", "Download", file)
					return filepath.Abs(fake)
				},
			}

			if len(entry.idx) > 0 {
				for key, dsc := range entry.idx {
					dsc.URL = ts.URL + dsc.URL
					entry.idx[key] = dsc
				}
				idx, err := os.Create(filepath.Join(dest, "uris.index"))
				if err != nil {
					t.Fatal(err)
				}
				defer idx.Close()
				if err := entry.idx.Encode(idx); err != nil {
					t.Fatal(err)
				}
				if err := idx.Close(); err != nil {
					t.Fatal(err)
				}
				i.indexDir = dest
			}

			if _, err := i.download("hello", "2.10-1"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
