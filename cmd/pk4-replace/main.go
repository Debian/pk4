// Program pk4-replace builds the sources in the current directory using sbuild,
// then replaces the subset of currently installed binary packages with the
// newly built packages.
package main

import (
	"bytes"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pault.ag/go/debian/control"
)

// errDryRun is a sentinel used when regular control flow is aborted because a
// dry-run was requested.
var errDryRun = errors.New("dry run requested")

type invocation struct {
	configDir    string
	buildCommand []string

	dryRun bool
}

func (i *invocation) build() (changesFile string, _ error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	sbuild := exec.Command(i.buildCommand[0], i.buildCommand[1:]...)
	log.Printf("Building package using %q", sbuild.Args)
	if i.dryRun {
		return "", errDryRun
	}
	sbuild.ExtraFiles = []*os.File{pw} // populates fd 3
	if err := sbuild.Run(); err != nil {
		return "", err
	}
	if err := pw.Close(); err != nil {
		return "", err
	}
	// NOTE: we can only do the read this late because we assume the pipe never
	// fills its buffer. Given that we are just printing a file path to it,
	// filling the buffer seems very unlikely.
	b, err := ioutil.ReadAll(pr)
	return strings.TrimSpace(string(b)), err
}

// packagesToReplace finds out which binary packages of the just-built binary
// packages are currently installed on the system and returns the paths to their
// intended replacement .deb files.
func (i *invocation) packagesToReplace(changesFile string) ([]string, error) {
	changes, err := control.ParseChangesFile(changesFile)
	if err != nil {
		return nil, err
	}
	args := []string{"-f", "${db:Status-Status} ${Package}_\n", "--show"}
	query := exec.Command("dpkg-query", append(args, changes.Binaries...)...)
	out, err := query.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, err
		}
		// exec.ExitError is okay, dpkg-query still returns partial output.
	}
	var prefixes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "installed ") {
			prefixes = append(prefixes, strings.TrimPrefix(line, "installed "))
		}
	}

	filenames := make([]string, 0, len(changes.Files))
	// This approach is O(n²), but n is small.
	for _, f := range changes.Files {
		if !strings.HasSuffix(f.Filename, ".deb") {
			continue
		}
		for _, prefix := range prefixes {
			if !strings.HasPrefix(f.Filename, prefix) {
				continue
			}
			filenames = append(filenames, f.Filename)
			break
		}
	}
	return filenames, nil
}

func (i *invocation) logic() error {
	changesFile, err := i.build()
	if err != nil {
		if err == errDryRun {
			return nil
		}
		return err
	}

	pkgs, err := i.packagesToReplace(changesFile)
	if err != nil {
		return err
	}

	dir := filepath.Dir(changesFile)
	args := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		args[i] = filepath.Join(dir, pkg)
	}
	install := exec.Command("sudo", append([]string{"dpkg", "-i"}, args...)...)
	log.Printf("Installing replacement packages using %q", install.Args)
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	return install.Run()
}

func (i *invocation) readConfig(configPath string) error {
	b, err := ioutil.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var config struct {
		BuildCommand []string `control:"Build-Command" delim:"\n" strip:"\n\r\t "`
	}
	if err := control.Unmarshal(&config, bytes.NewReader(b)); err != nil {
		return err
	}
	log.Printf("read config from %s: %+v", configPath, config)
	if len(config.BuildCommand) > 0 {
		i.buildCommand = config.BuildCommand
	}
	return nil
}

func resolveTilde(s string) string {
	if !strings.HasPrefix(s, "~") {
		return s
	}
	// We need logic to resolve paths with a tilde prefix: bash passes such
	// paths unexpanded.
	homedir := os.Getenv("HOME")
	if homedir == "" {
		log.Fatalf("Cannot resolve path %q: environment variable $HOME empty", s)
	}
	return filepath.Join(homedir, strings.TrimPrefix(s, "~"))
}

func main() {
	i := invocation{
		buildCommand: []string{
			"sbuild",
			"--post-build-commands",
			"echo %SBUILD_CHANGES > /proc/self/fd/3",
			"-A",
			"--no-clean-source",
			"--dpkg-source-opt=--auto-commit",
		},
	}

	flag.BoolVar(&i.dryRun, "dry_run",
		false,
		"Print the build command and exit")

	i.configDir = resolveTilde("~/.config/pk4")
	configPath := filepath.Join(i.configDir, "pk4.deb822")
	if err := i.readConfig(configPath); err != nil {
		log.Fatal(err)
	}

	flag.Parse()

	if err := i.logic(); err != nil {
		log.Fatal(err)
	}
}
