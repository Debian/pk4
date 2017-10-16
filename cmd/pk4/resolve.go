package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/version"
)

func (i *invocation) resolveFile(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// TODO(correctness): escape resolved, as dpkg expects a pattern
	name, err := i.lookPath("dpkg")
	if err != nil {
		return "", err
	}
	dpkg := exec.Command(name, "-S", resolved)
	dpkg.Stderr = os.Stderr
	out, err := dpkg.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("path %q is not provided by any package", resolved)
	}
	if len(lines) > 1 {
		return "", fmt.Errorf("unexpected dpkg -S output: more than one line: %s", string(out))
	}
	result := lines[0]
	idx := strings.LastIndex(result, ":")
	if idx == -1 {
		return "", fmt.Errorf("unexpected dpkg -S output: no colon found in %q", result)
	}
	packages := strings.Split(result[:idx], ",")
	if len(packages) > 1 {
		log.Printf("Use one of:")
		seen := make(map[string]bool, len(packages))
		sorted := make([]string, 0, len(packages))
		for _, pkg := range packages {
			pkg = strings.TrimSpace(pkg)
			if idx := strings.Index(pkg, ":"); idx > -1 {
				pkg = pkg[:idx] // strip e.g. :amd64 suffix
			}
			if seen[pkg] {
				continue
			}
			sorted = append(sorted, pkg)
			seen[pkg] = true
		}
		sort.Strings(sorted)
		for _, pkg := range sorted {
			log.Printf("  pk4 %s", pkg)
		}
		return "", fmt.Errorf("path %q is provided by more than one package", resolved)
	}
	i.V().Printf("path %s belongs to binary package %s", path, packages[0])
	return packages[0], nil
}

func (i *invocation) binaryPkgsOf(srcpkg string) ([]string, error) {
	name, err := i.lookPath("apt-cache")
	if err != nil {
		return nil, err
	}

	showsrc := exec.Command(name, "--only-source", "showsrc", srcpkg)
	showsrc.Stderr = os.Stderr
	out, err := showsrc.Output()
	if err != nil {
		return nil, err
	}
	var pkgs []struct {
		Package string
		Version version.Version
		Binary  []string `control:"Binary" delim:"," strip:" "`
	}
	if err := control.Unmarshal(&pkgs, bytes.NewReader(out)); err != nil {
		return nil, err
	}
	present := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, bin := range pkg.Binary {
			present[bin] = true
		}
	}
	binaries := make([]string, 0, len(present))
	for name := range present {
		binaries = append(binaries, name)
	}
	return binaries, nil
}

func (i *invocation) firstInstalledOf(binaries []string) (string, error) {
	name, err := i.lookPath("dpkg-query")
	if err != nil {
		return "", err
	}
	query := exec.Command(name, append([]string{"--show"}, binaries...)...)
	// Intentionally discard stderr: dpkg-query will print one line to stderr
	// for each package which is not installed.
	out, err := query.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return "", err
		}
		// exec.ExitError is okay, dpkg-query still returns partial output.
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 2 || parts[1] == "" {
			continue // an uninstalled package will be listed without version
		}
		return parts[0], nil
	}
	return "", fmt.Errorf("none of these packages is installed: %v", binaries)
}

func (i *invocation) resolveSource(arg0 string) (srcpkg string, srcversion string, _ error) {
	srcpkg = arg0
	if i.version != "" {
		return srcpkg, i.version, nil // user-specified
	}

	// If any binary built from the specified source is installed, use the first
	// installed version we can find.
	binaries, err := i.binaryPkgsOf(srcpkg)
	if err != nil {
		return "", "", err
	}
	if len(binaries) == 0 {
		return "", "", fmt.Errorf("source package %q not found", srcpkg)
	}
	binariesprint := fmt.Sprintf("%v", binaries)
	if len(binariesprint) > 100 {
		binariesprint = binariesprint[:100] + "…"
	}
	i.V().Printf("source package %s builds binary packages %s", srcpkg, binariesprint)
	binpkg, err := i.firstInstalledOf(binaries)
	if err == nil {
		return i.resolveBinary(binpkg)
	}
	srcpkg, srcversion, err = i.lookup("src:" + srcpkg)
	if err != nil {
		return "", "", fmt.Errorf("lookup(%q): %v", "src:"+srcpkg, err)
	}
	i.V().Printf("no binary package of source package %s installed, falling back to latest available source package %s %s", srcpkg, srcpkg, srcversion)
	return srcpkg, srcversion, err
}

func (i *invocation) resolveBinary(binpkg string) (srcpkg string, srcversion string, _ error) {
	if i.file || strings.HasPrefix(binpkg, "/") {
		var err error
		binpkg, err = i.resolveFile(binpkg)
		if err != nil {
			return "", "", err
		}
	}

	if idx := strings.Index(binpkg, ":"); idx > -1 {
		binpkg = binpkg[:idx] // strip e.g. :amd64 suffix
	}

	var err error
	key := binpkg
	if i.bin {
		key = "bin:" + key
	}
	srcpkg, srcversion, err = i.lookup(key)
	if err != nil {
		return "", "", fmt.Errorf("lookup(%q): %v", key, err)
	}
	i.V().Printf("binary package %s resolved to source package %s %s", binpkg, srcpkg, srcversion)

	if i.version != "" {
		return srcpkg, i.version, nil // user-specified
	}
	return srcpkg, srcversion, nil
}

func (i *invocation) resolve() (srcpkg string, srcversion string, _ error) {
	if i.src {
		return i.resolveSource(i.arg)
	}
	return i.resolveBinary(i.arg)
}
