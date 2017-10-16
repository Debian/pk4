package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/Debian/pk4/internal/index"
)

var verbose = flag.Bool("verbose", false, "Whether to print messages to stdout")

func TestResolve(t *testing.T) {
	t.Parallel()

	vimAbs, err := filepath.Abs("testdata/File/vim")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range []struct {
		name string
		idx  index.Index

		wantSrcpkg     string
		wantSrcversion string

		invocation
	}{
		{
			name:           "BinaryPackageInstalled", // TODO: add a corresponding test for index creation
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "2:1.19.3-2",
			idx: index.Index{
				"xserver-xephyr": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.3-2"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				arg:     "xserver-xephyr",
			},
		},

		{
			name:           "BinaryPackageInstalledArch",
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "2:1.19.3-2",
			idx: index.Index{
				"xserver-xephyr": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.3-2"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				arg:     "xserver-xephyr:amd64",
			},
		},

		{
			name:           "BinaryPackageInstalledVersion",
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "3:1.22",
			idx: index.Index{
				"xserver-xephyr": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.3-2"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				arg:     "xserver-xephyr",
				version: "3:1.22",
			},
		},

		{
			name: "BinaryPackageNotInstalled",
			idx: index.Index{
				"fluxbox": index.Source{
					Package: "fluxbox",
					Version: mustParseVersion("1.3.5-2"),
				},
			},
			wantSrcpkg:     "fluxbox",
			wantSrcversion: "1.3.5-2",

			invocation: invocation{
				verbose: *verbose,
				arg:     "fluxbox",
			},
		},

		{
			name: "BinaryPackageNotInstalledVersion",
			idx: index.Index{
				"fluxbox": index.Source{
					Package: "fluxbox",
					Version: mustParseVersion("1.3.5-2"),
				},
			},
			wantSrcpkg:     "fluxbox",
			wantSrcversion: "4:1.55",

			invocation: invocation{
				verbose: *verbose,
				arg:     "fluxbox",
				version: "4:1.55",
			},
		},

		{
			name:           "SourcePackageInstalled",
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "2:1.19.3-2",
			idx: index.Index{
				"xserver-xephyr": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.3-2"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				src:     true,
				arg:     "xorg-server",
			},
		},

		{
			name: "SourcePackageInstalledImplicit",
			idx: index.Index{
				"xorg-server": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.3-2"),
				},
			},
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "2:1.19.3-2",

			invocation: invocation{
				verbose: *verbose,
				src:     false,
				arg:     "xorg-server",
			},
		},

		{
			name: "SourcePackageNotInstalled",
			idx: index.Index{
				"src:xorg-server": index.Source{
					Package: "xorg-server",
					Version: mustParseVersion("2:1.19.1-4"),
				},
			},
			wantSrcpkg:     "xorg-server",
			wantSrcversion: "2:1.19.1-4",

			invocation: invocation{
				verbose: *verbose,
				src:     true,
				arg:     "xorg-server",
			},
		},

		{
			name:           "SourcePackageVersion",
			wantSrcpkg:     "hello",
			wantSrcversion: "2.10-1",

			invocation: invocation{
				verbose: *verbose,
				src:     true,
				version: "2.10-1",
				arg:     "hello",
			},
		},

		{
			name:           "File",
			wantSrcpkg:     "vim",
			wantSrcversion: "2:8.0.0197-5",
			idx: index.Index{
				"vim-gtk": index.Source{
					Package: "vim",
					Version: mustParseVersion("2:8.0.0197-5"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				file:    true,
				arg:     vimAbs,
			},
		},

		{
			name:           "FileImplicit",
			wantSrcpkg:     "vim",
			wantSrcversion: "2:8.0.0197-5",
			idx: index.Index{
				"vim-gtk": index.Source{
					Package: "vim",
					Version: mustParseVersion("2:8.0.0197-5"),
				},
			},

			invocation: invocation{
				verbose: *verbose,
				file:    false,
				arg:     vimAbs,
			},
		},
	} {

		dest, err := ioutil.TempDir("", "pk4test")
		if err != nil {
			t.Fatal(err)
		}
		entry, dest := entry, dest // copy
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			defer os.RemoveAll(dest)

			i := entry.invocation
			i.dest = filepath.Join(dest, "dest")

			if len(entry.idx) > 0 {
				idx, err := os.Create(filepath.Join(dest, "sources.index"))
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

			i.lookPath = func(file string) (string, error) {
				fake := filepath.Join("testdata", entry.name, file)
				return fake, nil
			}

			srcpkg, srcversion, err := i.resolve()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := srcpkg, entry.wantSrcpkg; got != want {
				t.Fatalf("unexpected srcpkg: got %q, want %q", got, want)
			}
			if got, want := srcversion, entry.wantSrcversion; got != want {
				t.Fatalf("unexpected srcversion: got %q, want %q", got, want)
			}
		})
	}
}
