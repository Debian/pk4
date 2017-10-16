package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComplete(t *testing.T) {
	t.Parallel()

	indexDir, err := ioutil.TempDir("", "pk4test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(indexDir)

	var (
		bin = []string{
			"fluxbox",
		}

		src = []string{
			"xorg-server",
		}

		both = []string{
			"fluxbox",
			"xorg-server",
		}
	)

	if err := ioutil.WriteFile(filepath.Join(indexDir, "completion.bin.txt"), []byte(strings.Join(bin, "\n")), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(indexDir, "completion.src.txt"), []byte(strings.Join(src, "\n")), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(indexDir, "completion.both.txt"), []byte(strings.Join(both, "\n")), 0600); err != nil {
		t.Fatal(err)
	}

	// This subtest is required for the tear-down code (defer) to run only after
	// all parallel subtests have completed.
	t.Run("Complete", func(t *testing.T) {
		for _, entry := range []struct {
			name string
			want []string

			invocation
		}{
			{
				name: "Bin",
				want: bin,
				invocation: invocation{
					bin: true,
				},
			},

			{
				name: "Src",
				want: src,
				invocation: invocation{
					src: true,
				},
			},

			{
				name: "Both",
				want: both,
			},

			{
				name: "BothPrefix",
				want: []string{"xorg-server"},
				invocation: invocation{
					arg: "x",
				},
			},
		} {

			entry := entry // copy
			t.Run(entry.name, func(t *testing.T) {
				t.Parallel()

				i := entry.invocation
				i.indexDir = indexDir

				got, err := i.complete()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, entry.want) {
					t.Fatalf("unexpected completion result: got %v, want %v", got, entry.want)
				}
			})
		}
	})
}
