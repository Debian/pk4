package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (i *invocation) complete() ([]string, error) {
	path := "both"
	if i.src {
		path = "src"
	} else if i.bin {
		path = "bin"
	}
	f, err := os.Open(filepath.Join(i.indexDir, fmt.Sprintf("completion.%s.txt", path)))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var choices []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if i.arg == "" || strings.HasPrefix(scanner.Text(), i.arg) {
			choices = append(choices, scanner.Text())
		}
	}
	return choices, scanner.Err()
}
