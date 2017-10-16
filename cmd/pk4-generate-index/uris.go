package main

import (
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Debian/pk4/internal/index"
	"github.com/Debian/pk4/internal/write"
	"golang.org/x/sync/errgroup"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/version"
)

// sourceIndex contains precisely the fields we are interested in, resulting in
// a more memory- and CPU-efficient parsing than using control.SourceIndex.
//
// TODO(later): control.SourceIndex parses Files into []string, which is not
// ideal. Send a PR for that after verifying it doesn’t break anyone.
type sourceIndex struct {
	Package   string
	Version   version.Version
	Directory string
	Files     []control.MD5FileHash `control:"Files" delim:"\n" strip:"\n\r\t "`
}

func getSourceIndexFile(filename string) ([]sourceIndex, error) {
	cat := exec.Command("/usr/lib/apt/apt-helper", "cat-file", filename)
	cat.Stderr = os.Stderr
	stdout, err := cat.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cat.Start(); err != nil {
		return nil, err
	}
	var index []sourceIndex
	if err := control.Unmarshal(&index, stdout); err != nil {
		return nil, err
	}
	return index, cat.Wait()
}

func genURIIndex(sindex []sourceIndex, repoURI string) index.URIs {
	idx := make(index.URIs)

	for _, pkg := range sindex {
		src := index.Source{
			Package: pkg.Package,
			Version: pkg.Version,
		}

		var size int64
		for _, f := range pkg.Files {
			size += f.Size
		}
		var uri string
		for _, f := range pkg.Files {
			if !strings.HasSuffix(f.Filename, ".dsc") {
				continue
			}
			uri = repoURI + path.Join(pkg.Directory, f.Filename)
			break
		}

		if _, ok := idx[src]; !ok {
			idx[src] = index.DSC{
				URL:  uri,
				Size: size,
			}
		}
	}

	return idx
}

func genURIs() error {
	defaultrel, err := getDefaultRelease()
	if err != nil {
		return err
	}

	targets, err := getIndexTargets()
	if err != nil {
		return err
	}
	indices := make([]index.URIs, len(targets))

	for idx, target := range targets {
		if target.Release == defaultrel {
			targets[idx].priority = 990
		} else {
			targets[idx].priority = 500
		}
	}

	var eg errgroup.Group
	for idx, target := range targets {
		if target.ShortDesc != "Sources" || strings.HasSuffix(target.Codename, "-debug") {
			continue
		}

		idx, target := idx, target // copy
		eg.Go(func() error {
			index, err := getSourceIndexFile(target.Filename)
			if err != nil {
				return err
			}

			indices[idx] = genURIIndex(index, target.RepoURI)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	keys := make(map[index.Source]struct{})

	for idx := range targets {
		for key := range indices[idx] {
			keys[key] = struct{}{}
		}
	}

	merged := make(index.URIs, len(keys))
	type srcWithPrio struct {
		priority int64
		dsc      index.DSC
	}
	for key := range keys {
		byPriority := make([]srcWithPrio, 0, len(targets))
		for idx := range targets {
			if val, ok := indices[idx][key]; ok {
				byPriority = append(byPriority, srcWithPrio{
					dsc:      val,
					priority: targets[idx].priority,
				})
			}
		}
		if len(byPriority) == 0 {
			continue
		}
		sort.Slice(byPriority, func(i, j int) bool {
			return byPriority[i].priority >= byPriority[j].priority
		})
		merged[key] = byPriority[0].dsc
	}

	if err := os.MkdirAll(*indexDir, 0755); err != nil {
		return err
	}

	return write.Atomically(filepath.Join(*indexDir, "uris.index"), func(w io.Writer) error {
		return merged.Encode(w)
	})

	// TODO(later): apt-config parser
	// TODO(later): look for pins within filepath.Join(Dir, Dir::Etc, Dir::Etc::PreferencesParts)
	// TODO(later): read pins from filepath.Join(Dir, Dir::Etc, Dir::Etc::Preferences)
}
