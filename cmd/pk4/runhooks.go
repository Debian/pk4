package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func (i *invocation) runHooks(hookDir, wd string) error {
	fis, err := ioutil.ReadDir(hookDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, fi := range fis {
		hook := exec.Command(filepath.Join(hookDir, fi.Name()))
		hook.Dir = wd
		hook.Stderr = os.Stderr
		hook.Stdout = os.Stderr
		if err := hook.Run(); err != nil {
			return err
		}
	}
	return nil
}
