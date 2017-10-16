package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Debian/pk4/internal/index"
)

var notFound = errors.New("key not found in index")

func lookup(path, key string) (string, error) {
	// TODO(performance): consider mmaping the file to avoid the seek
	// syscalls. Use golang.org/x/exp/mmap after updating the
	// golang-golang-x-exp-dev Debian package.

	const offsetLen = 4 // sizeof(uint32)

	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// If pk4 was invoked shortly after the initial installation, index
		// generation could still be in progress. Confirm, then wait for the
		// pk4-generate-index.service unit to leave ActiveState=activating:
		const unit = "pk4-generate-index.service"
		b, eerr := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", unit).Output()
		if eerr != nil {
			return "", err
		}
		if strings.TrimSpace(string(b)) == "activating" {
			log.Printf("Waiting for pk4-generate-index.service to activate")
		}
		for strings.TrimSpace(string(b)) == "activating" {
			time.Sleep(1 * time.Second)
			b, eerr = exec.Command("systemctl", "show", "-p", "ActiveState", "--value", unit).Output()
			if eerr != nil {
				return "", err
			}
		}
		// retry without waiting
		f, err = os.Open(path)
		if err != nil {
			return "", err
		}
	}
	defer f.Close()

	if _, err := f.Seek(-1*offsetLen, io.SeekEnd); err != nil {
		return "", err
	}

	var indexBlockOffset uint32
	if err := binary.Read(f, binary.LittleEndian, &indexBlockOffset); err != nil {
		return "", err
	}

	if _, err := f.Seek(int64(indexBlockOffset)+((2*offsetLen)*(int64(len(key))-1)), io.SeekStart); err != nil {
		return "", err
	}

	var blockIndex index.BlockLocation
	if err := binary.Read(f, binary.LittleEndian, &blockIndex); err != nil {
		return "", err
	}

	// TODO(performance): switch to binary search after mmap'ing
	entries := int(blockIndex.BlockLength) / (len(key) + offsetLen)
	if _, err := f.Seek(int64(blockIndex.BlockOffset), io.SeekStart); err != nil {
		return "", err
	}

	var (
		keyBytes  = []byte(key)
		name      = make([]byte, len(key))
		srcOffset uint32
		found     = false
	)
	for i := 0; i < entries; i++ {
		if _, err := f.Read(name); err != nil {
			return "", err
		}
		if err := binary.Read(f, binary.LittleEndian, &srcOffset); err != nil {
			return "", err
		}
		if bytes.Equal(name, keyBytes) {
			found = true
			break
		}
	}
	if !found {
		return "", notFound
	}

	if _, err := f.Seek(int64(srcOffset), io.SeekStart); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", scanner.Err()
	}
	return scanner.Text(), nil
}

// lookupDSC returns the URI of the DSC file for srcpkg in srcversion.
func (inv *invocation) lookupDSC(srcpkg, srcversion string) (index.DSC, error) {
	key := fmt.Sprintf("%s\t%s", srcpkg, srcversion)
	val, err := lookup(filepath.Join(inv.indexDir, "uris.index"), key)
	if err != nil {
		return index.DSC{}, err
	}

	parts := strings.Split(strings.TrimSpace(val), "\t")
	if got, want := len(parts), 2; got != want {
		return index.DSC{}, fmt.Errorf(`corrupt index: len(Split(%q, "\t")) = %d, want %d`, val, got, want)
	}

	size, err := strconv.ParseInt(parts[1], 0, 64)
	if err != nil {
		return index.DSC{}, err
	}

	return index.DSC{URL: parts[0], Size: size}, nil
}

func (inv *invocation) lookup(key string) (srcpkg string, srcversion string, _ error) {
	val, err := lookup(filepath.Join(inv.indexDir, "sources.index"), key)
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(strings.TrimSpace(val), "\t")
	if got, want := len(parts), 2; got != want {
		return "", "", fmt.Errorf(`corrupt index: len(Split(%q, "\t")) = %d, want %d`, val, got, want)
	}

	return parts[0], parts[1], nil
}
