package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Debian/pk4/internal/humanbytes"

	"pault.ag/go/debian/control"
)

type verboseLogger bool

func (v verboseLogger) Printf(format string, args ...interface{}) {
	if !bool(v) {
		return
	}
	log.Output(2, fmt.Sprintf(format, args...))
}

type invocation struct {
	dest           string
	bin            bool
	src            bool
	version        string
	file           bool
	arg            string
	indexDir       string
	configDir      string
	verbose        bool
	diskUsageLimit int64

	// TODO(security): ideally, allowUnauthenticated would not be implemented at
	// all. However, snapshot.debian.org does not currently provide an
	// up-to-date signature for once-verified packages, see
	// https://bugs.debian.org/763419. Without such a trust path, we must rely
	// on verifying signatures against the the current debian-keyring. That
	// should work in most cases, but there are notable exceptions, like paultag
	// revoking his key, rendering the fluxbox signatures unverifiable.
	allowUnauthenticated bool

	snapshotBase string                            // for testing
	mirrorUrl    string                            // for testing
	lookPath     func(file string) (string, error) // for testing
}

func (i *invocation) V() verboseLogger {
	return verboseLogger(i.verbose)
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
		DiskUsageLimit string `control:"Disk-Usage-Limit"`
	}
	if err := control.Unmarshal(&config, bytes.NewReader(b)); err != nil {
		return err
	}
	i.V().Printf("read config from %s: %+v", configPath, config)
	if config.DiskUsageLimit != "" {
		if v, err := humanbytes.Parse(config.DiskUsageLimit); err != nil {
			log.Printf("invalid Disk-Usage-Limit value %q in config file %s: %v", config.DiskUsageLimit, configPath, err)
		} else {
			i.diskUsageLimit = v
		}
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
		lookPath:       exec.LookPath,
		indexDir:       "/var/cache/pk4",
		diskUsageLimit: 1 * 1024 * 1024 * 1024, // 1 GB
		// TODO(https://bugs.debian.org/740096): switch to https once available
		snapshotBase: "http://snapshot.debian.org/",
		mirrorUrl:    "https://deb.debian.org/debian",
	}

	flag.StringVar(&i.dest, "dest",
		filepath.Join("~", ".cache", "pk4"),
		"Directory in which to store source packages")

	flag.BoolVar(&i.src, "src",
		false,
		"Restrict search to source packages only")

	flag.BoolVar(&i.bin, "bin",
		false,
		"Restrict search to binary packages only")

	flag.StringVar(&i.version, "version",
		"",
		"Use the specified source package version (default: installed package version, or latest known if not installed)")

	flag.BoolVar(&i.file, "file",
		false,
		"Interpret the argument as a file name and operate on the package providing the file")

	flag.BoolVar(&i.allowUnauthenticated, "allow_unauthenticated",
		false,
		"Whether to allow unauthenticated source packages, i.e. disable signature checking")

	flag.BoolVar(&i.verbose, "verbose",
		false,
		"Whether to print messages to stderr")

	complete := flag.Bool("complete",
		false,
		"Whether to return shell completions. Should usually be set by shell completion functions only.")

	resolve := flag.Bool("resolve_only",
		false,
		`Resolve the provided arguments to source package and source package version, then print them to stdout in %s\t%s\n format and exit`)

	shell := flag.String("shell",
		os.Getenv("SHELL"),
		"Which shell to start in the output directory after downloading the source")

	flag.Parse()

	if i.bin && i.src {
		log.Fatalf("At most one of -bin or -src must be specified, not both")
	}

	i.dest = resolveTilde(i.dest)
	i.configDir = resolveTilde("~/.config/pk4")
	configPath := filepath.Join(i.configDir, "pk4.deb822")
	if err := i.readConfig(configPath); err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(i.dest, 0755); err != nil {
		log.Fatal(err)
	}

	for n := 0; n < flag.NArg(); n++ {
		i.arg = flag.Arg(n)

		if *complete {
			choices, err := i.complete()
			if err != nil {
				log.Fatal(err)
			}
			for _, choice := range choices {
				fmt.Println(choice)
			}
			return
		}

		srcpkg, srcversion, err := i.resolve()
		if err != nil {
			log.Fatal(err)
		}

		if *resolve {
			fmt.Printf("%s\t%s\n", srcpkg, srcversion)
			return
		}

		outputDir, err := i.download(srcpkg, srcversion)
		if err != nil {
			log.Fatal(err)
		}
		if flag.NArg() == 1 {
			subshell := exec.Command(*shell)
			subshell.Dir = outputDir
			subshell.Stdout = os.Stdout
			subshell.Stdin = os.Stdin
			subshell.Stderr = os.Stderr
			subshell.Run()
		}
	}

	if flag.NArg() == 1 {
		return // already started a shell in the for loop, done
	}
	subshell := exec.Command(*shell)
	subshell.Dir = i.dest
	subshell.Stdout = os.Stdout
	subshell.Stdin = os.Stdin
	subshell.Stderr = os.Stderr
	subshell.Run()
}
