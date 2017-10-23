package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func (i *invocation) runHooks(hookDir, wd string, args []string) (found bool, _ error) {
	fis, err := ioutil.ReadDir(hookDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, fi := range fis {
		hook := exec.Command(filepath.Join(hookDir, fi.Name()), args...)
		hook.Dir = wd
		hook.Stderr = os.Stderr
		hook.Stdout = os.Stderr
		if err := hook.Run(); err != nil {
			return true, err
		}
	}
	return len(fis) > 0, nil
}
