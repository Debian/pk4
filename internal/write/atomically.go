package write

import (
	"bufio"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

func tempDir(dest string) string {
	tempdir := os.Getenv("TMPDIR")
	if tempdir == "" {
		// Convenient for development: decreases the chance that we
		// cannot move files due to /tmp being on a different file
		// system.
		tempdir = filepath.Dir(dest)
	}
	return tempdir
}

func Atomically(dest string, write func(io.Writer) error) (err error) {
	f, err := ioutil.TempFile(tempDir(dest), "pk4-")
	if err != nil {
		return err
	}
	defer func() {
		// Remove the tempfile if an error occurred
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	defer f.Close()

	bufw := bufio.NewWriter(f)

	if err := write(bufw); err != nil {
		return err
	}

	if err := bufw.Flush(); err != nil {
		return err
	}

	if err := f.Chmod(0644); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(f.Name(), dest)
}
