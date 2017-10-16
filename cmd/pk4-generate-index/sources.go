package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Debian/pk4/internal/index"
	"github.com/Debian/pk4/internal/write"
	"golang.org/x/sync/errgroup"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/version"
)

func getBinaryIndexFile(filename string) ([]control.BinaryIndex, error) {
	cat := exec.Command("/usr/lib/apt/apt-helper", "cat-file", filename)
	cat.Stderr = os.Stderr
	stdout, err := cat.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cat.Start(); err != nil {
		return nil, err
	}
	index, err := control.ParseBinaryIndex(bufio.NewReader(stdout))
	if err != nil {
		return nil, err
	}
	return index, cat.Wait()
}

func genIndex(bindex []control.BinaryIndex) index.Index {
	pk4index := make(index.Index)

	for _, pkg := range bindex {
		src := index.Source{
			Package: pkg.Source,
			Version: pkg.Version,
		}
		if src.Package == "" {
			src.Package = pkg.Package
		}
		if strings.HasSuffix(src.Package, ")") {
			idx := strings.Index(src.Package, " (")
			if idx == -1 {
				continue // malformed Source value
			}
			var err error
			src.Version, err = version.Parse(strings.TrimSuffix(src.Package[idx+2:], ")"))
			if err != nil {
				continue // malformed Source value
			}
			src.Package = src.Package[:idx]
		}

		for _, key := range []string{
			src.Package,
			pkg.Package,
			"src:" + src.Package,
			"bin:" + pkg.Package,
		} {
			if existing, ok := pk4index[key]; !ok || version.Compare(src.Version, existing.Version) > 0 {
				pk4index[key] = src
			}
		}
	}

	return pk4index
}

func genSources() error {
	defaultrel, err := getDefaultRelease()
	if err != nil {
		return err
	}

	targets, err := getIndexTargets()
	if err != nil {
		return err
	}

	for idx, target := range targets {
		if target.Release == defaultrel {
			targets[idx].priority = 990
		} else {
			targets[idx].priority = 500
		}
	}

	targets = append(targets, indexTarget{
		ShortDesc: "Packages",
		Filename:  "/var/lib/dpkg/status",
		Codename:  "", // n/a
		Release:   "", // n/a
		RepoURI:   "",
		// apt assigns priority 100, but pk4 weighs locally installed packages
		// the highest.
		priority: math.MaxInt64,
	})

	indices := make([]index.Index, len(targets))
	var eg errgroup.Group
	for idx, target := range targets {
		if target.ShortDesc != "Packages" || strings.HasSuffix(target.Codename, "-debug") {
			continue
		}

		idx, target := idx, target // copy
		eg.Go(func() error {
			index, err := getBinaryIndexFile(target.Filename)
			if err != nil {
				return err
			}

			indices[idx] = genIndex(index)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	keys := make(map[string]struct{})

	for idx := range targets {
		for key := range indices[idx] {
			keys[key] = struct{}{}
		}
	}

	merged := make(index.Index, len(keys))
	type srcWithPrio struct {
		priority int64
		source   index.Source
	}
	sortedkeys := make([]string, 0, len(keys))
	for key := range keys {
		byPriority := make([]srcWithPrio, 0, len(targets))
		for idx := range targets {
			if val, ok := indices[idx][key]; ok {
				byPriority = append(byPriority, srcWithPrio{
					source:   val,
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
		merged[key] = byPriority[0].source
		sortedkeys = append(sortedkeys, key)
	}

	sort.Strings(sortedkeys)

	if err := os.MkdirAll(*indexDir, 0755); err != nil {
		return err
	}

	var weg errgroup.Group

	weg.Go(func() error {
		return write.Atomically(filepath.Join(*indexDir, "sources.index"), func(w io.Writer) error {
			return merged.Encode(w)
		})
	})

	weg.Go(func() error {
		return write.Atomically(filepath.Join(*indexDir, "completion.bin.txt"), func(w io.Writer) error {
			bufw := bufio.NewWriter(w)
			for _, key := range sortedkeys {
				if strings.HasPrefix(key, "bin:") {
					fmt.Fprintln(bufw, strings.TrimPrefix(key, "bin:"))
				}
			}
			return bufw.Flush()
		})
	})

	weg.Go(func() error {
		return write.Atomically(filepath.Join(*indexDir, "completion.src.txt"), func(w io.Writer) error {
			bufw := bufio.NewWriter(w)
			for _, key := range sortedkeys {
				if strings.HasPrefix(key, "src:") {
					fmt.Fprintln(bufw, strings.TrimPrefix(key, "src:"))
				}
			}
			return bufw.Flush()
		})
	})

	weg.Go(func() error {
		return write.Atomically(filepath.Join(*indexDir, "completion.both.txt"), func(w io.Writer) error {
			bufw := bufio.NewWriter(w)
			for _, key := range sortedkeys {
				if !strings.HasPrefix(key, "src:") && !strings.HasPrefix(key, "bin:") {
					fmt.Fprintln(bufw, key)
				}
			}
			return bufw.Flush()
		})
	})

	return weg.Wait()

	// TODO(later): apt-config parser
	// TODO(later): look for pins within filepath.Join(Dir, Dir::Etc, Dir::Etc::PreferencesParts)
	// TODO(later): read pins from filepath.Join(Dir, Dir::Etc, Dir::Etc::Preferences)
}
