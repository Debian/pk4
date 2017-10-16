package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"

	"pault.ag/go/debian/control"
)

var (
	indexDir = flag.String("index_dir",
		"/var/cache/pk4",
		"Directory to store the pk4 index files in")
)

// TODO(later): optimize this program

// index needs to support these ops:
// src:<srcpkg> → <srcversion>
// bin:<binpkg> → <srcpkg>\t<srcversion>
// <bin-or-srcpkg> → <srcpkg>\t<srcversion>

// first idea:
// top-level index:
// uint32(<key-length>), uint32(<same-len-block-offset>), uint32(<same-len-block-len>)
// in each same-len-block, keys are sorted (for binary search) and yield an uint32(<src-offset>), where one can read one line (<src>\t<version>)
// for completion mode, store a separate file containing just the keys

type indexTarget struct {
	ShortDesc string
	Filename  string
	Codename  string
	Release   string
	RepoURI   string `control:"Repo-URI"`
	priority  int64
}

func getDefaultRelease() (string, error) {
	defaultrel := exec.Command("apt-config", "--format", "%v%n", "dump", "APT::Default-Release")
	defaultrel.Stderr = os.Stderr
	out, err := defaultrel.Output()
	return strings.TrimSpace(string(out)), err
}

func getIndexTargets() ([]indexTarget, error) {
	indextargets := exec.Command("apt-get", "indextargets")
	indextargets.Stderr = os.Stderr
	out, err := indextargets.Output()
	if err != nil {
		return nil, err
	}
	var targets []indexTarget
	return targets, control.Unmarshal(&targets, bytes.NewReader(out))
}

func main() {
	flag.Parse()
	if err := genSources(); err != nil {
		log.Fatal(err)
	}
	if err := genURIs(); err != nil {
		log.Fatal(err)
	}
}
