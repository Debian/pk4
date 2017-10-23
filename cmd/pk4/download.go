package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Debian/pk4/internal/humanbytes"
	"github.com/Debian/pk4/internal/write"
	"golang.org/x/sync/errgroup"
	"pault.ag/go/debian/control"
)

// downloadFile downloads uri to dest, falling back to snapshotBase when
// encountering any status but HTTP 200.
func (i *invocation) downloadFile(dest, snapshotBase, uri string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil // file already exists
	}
	return write.Atomically(dest, func(w io.Writer) error {
		// Try to download the file from the mirror first:
		i.V().Printf("downloading %s", uri)
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "pk4")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		if got, want := resp.StatusCode, http.StatusOK; got != want {
			// Discard the Body (for Keep-Alive).
			ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if snapshotBase == "" {
				return fmt.Errorf("unexpected HTTP status code: got %d, want %d", got, want)
			}

			u, _ := url.Parse(snapshotBase)
			u.Path = path.Join(u.Path, strings.TrimPrefix(uri, i.mirrorUrl+"/"))
			i.V().Printf("HTTP %d, falling back to %s", got, u.String())
			req, err := http.NewRequest("GET", u.String(), nil)
			if err != nil {
				return err
			}
			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			if got, want := resp.StatusCode, http.StatusOK; got != want {
				return fmt.Errorf("unexpected HTTP status code: got %d, want %d", got, want)
			}
		}
		if _, err := io.Copy(w, resp.Body); err != nil {
			return err
		}

		return nil
	})
}

// downloadDSC downloads the .dsc file and all files referenced by it.
func (i *invocation) downloadDSC(dest, snapshotBase, uri string) error {
	dscPath := filepath.Join(filepath.Dir(dest), filepath.Base(uri))
	if err := i.downloadFile(dscPath, snapshotBase, uri); err != nil {
		return err
	}
	dsc, err := control.ParseDscFile(dscPath)
	if err != nil {
		return err
	}
	var eg errgroup.Group
	for _, f := range dsc.Files {
		dest := filepath.Join(filepath.Dir(dest), f.Filename)
		u, err := url.Parse(uri)
		if err != nil {
			return err
		}
		u.Path = filepath.Join(filepath.Dir(u.Path), f.Filename)
		eg.Go(func() error { return i.downloadFile(dest, snapshotBase, u.String()) })
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	if !i.allowUnauthenticated {
		name, err := i.lookPath("dscverify")
		if err != nil {
			return err
		}
		dscverify := exec.Command(name, dscPath)
		dscverify.Dir = filepath.Dir(dest)
		dscverify.Stderr = os.Stderr
		if err := dscverify.Run(); err != nil {
			return fmt.Errorf("dscverify %s failed: %v", dscPath, err)
		}
	}
	return nil
}

func (i *invocation) capDiskUsage(except string) error {
	entries, err := ioutil.ReadDir(i.dest)
	if err != nil {
		return err
	}
	// Sort ascendingly by creation time, i.e. the pk4 download time.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime().Before(entries[j].ModTime())
	})
	var eg errgroup.Group
	sums := make([]int64, len(entries))
	for idx, entry := range entries {
		idx, entry := idx, entry // copy
		eg.Go(func() error {
			var sum int64
			subdir := filepath.Join(i.dest, entry.Name())
			err := filepath.Walk(subdir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				sum += info.Size()
				return nil
			})
			sums[idx] = sum
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	var sum int64
	for _, subsum := range sums {
		sum += subsum
	}
	i.V().Printf("pk4 destdir %s currently uses %s of disk space", i.dest, humanbytes.Format(sum))
	deleted := false
	for j := 0; sum > i.diskUsageLimit && j < len(entries); j++ {
		if strings.HasPrefix(entries[j].Name(), except) {
			continue // avoid deleting the package we are about to download/unpack
		}
		i.V().Printf("  deleting %s (%d bytes)", entries[j].Name(), sums[j])
		if err := os.RemoveAll(filepath.Join(i.dest, entries[j].Name())); err != nil {
			return err
		}
		sum -= sums[j]
		deleted = true
	}
	if deleted {
		i.V().Printf("now using %s of disk space (limit: %s)", humanbytes.Format(sum), humanbytes.Format(i.diskUsageLimit))
	} else {
		i.V().Printf("already below the limit of %s", humanbytes.Format(i.diskUsageLimit))
	}
	return nil
}

func available(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs(%s): %v", path, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// downloadDSCAndUnpack downloads the .dsc file fpath and unpacks it to dest.
func (i *invocation) downloadDSCAndUnpack(dest, srcpkg, srcversion, snapshotBase, fpath string, totalSize int64) error {
	i.V().Printf("downloading source package %s %s (%s)", srcpkg, srcversion, humanbytes.Format(totalSize))

	available, err := available(i.dest)
	if err != nil {
		return err
	}

	var eg errgroup.Group

	if uint64(totalSize) >= available {
		// download won’t succeed without prior cleanup
		if err := i.capDiskUsage(srcpkg); err != nil {
			return err
		}
	} else {
		eg.Go(func() error { return i.capDiskUsage(srcpkg) })
	}

	eg.Go(func() error { return i.downloadDSC(dest, snapshotBase, fpath) })

	if err := eg.Wait(); err != nil {
		return err
	}

	hooksFound, err := i.runHooks(filepath.Join(i.configDir, "hooks-enabled", "unpack"), filepath.Dir(dest), []string{filepath.Base(fpath), dest})
	if hooksFound {
		return err
	}

	// no unpack hooks? fall back to dpkg-source -x

	name, err := i.lookPath("dpkg-source")
	if err != nil {
		return err
	}
	args := []string{"-x", filepath.Base(fpath), dest}
	if i.allowUnauthenticated {
		args = append([]string{"--no-check"}, args...)
	}

	dpkgSource := exec.Command(name, args...)
	dpkgSource.Dir = filepath.Dir(dest)
	dpkgSource.Stderr = os.Stderr
	return dpkgSource.Run()
}

func (i *invocation) downloadSource(dest, srcpkg, srcversion string) error {
	dsc, err := i.lookupDSC(srcpkg, srcversion)
	if err == nil {
		return i.downloadDSCAndUnpack(dest, srcpkg, srcversion, "", dsc.URL, dsc.Size)
	}
	if err != notFound && !os.IsNotExist(err) {
		return err
	}
	// fallback to snapshot.debian.org lookup

	// TODO(https://bugs.debian.org/740096): switch to https once available
	u, _ := url.Parse(i.snapshotBase)
	u.Path = "/mr/package/" + srcpkg + "/" + srcversion + "/srcfiles"
	v := url.Values{}
	v.Set("fileinfo", "1")
	u.RawQuery = v.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pk4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		return fmt.Errorf("%q: unexpected HTTP status code: got %d, want %d", u.String(), got, want)
	}
	var srcfiles struct {
		Fileinfo map[string][]struct {
			Name        string `json:"name"`
			ArchiveName string `json:"archive_name"`
			Path        string `json:"path"`
			FirstSeen   string `json:"first_seen"`
			Size        int64  `json:"size"`
		} `json:"fileinfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&srcfiles); err != nil {
		return err
	}

	// sum up total size first
	var totalSize int64
	for _, infos := range srcfiles.Fileinfo {
		for _, info := range infos {
			if info.ArchiveName != "debian" {
				continue // skip e.g. “debian-debug”
			}
			totalSize += info.Size
		}
	}

	// download DSC
	for _, infos := range srcfiles.Fileinfo {
		for _, info := range infos {
			if info.ArchiveName != "debian" {
				continue // skip e.g. “debian-debug”
			}
			if !strings.HasSuffix(info.Name, ".dsc") {
				continue
			}
			snapshotBase := i.snapshotBase + path.Join("archive", info.ArchiveName, info.FirstSeen)
			fpath := i.mirrorUrl + path.Join(info.Path, info.Name)
			return i.downloadDSCAndUnpack(dest, srcpkg, srcversion, snapshotBase, fpath, totalSize)
		}
	}
	return fmt.Errorf("could not find .dsc file on snapshot.debian.org") // TODO
}

func (i *invocation) download(srcpkg, srcversion string) (outputDir string, _ error) {
	outputDir = filepath.Join(i.dest, srcpkg+"-"+srcversion) // per dpkg-source(1)

	// TODO(https://bugs.debian.org/877969): consider switching to dgit clone

	// We cannot use apt-get source because it fails when the package is no
	// longer referenced by sources.list, i.e. when apt-cache policy <package>
	// only lists /var/lib/dpkg/status in the version table for the currently
	// installed version.

	_, err := os.Stat(outputDir)
	if err == nil {
		return outputDir, nil // nothing to do
	}

	if !os.IsNotExist(err) {
		return "", err
	}

	if err := i.downloadSource(outputDir, srcpkg, srcversion); err != nil {
		return "", err
	}

	if _, err := i.runHooks(filepath.Join(i.configDir, "hooks-enabled", "after-download"), outputDir, nil); err != nil {
		log.Printf("hook failure: %v", err)
		// hooks are best-effort, don’t fail
	}

	return outputDir, nil
}
