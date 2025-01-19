// There doesn't seem to be a good way programmaticlly to interface with git.
// The "official" way is just to issue commands to the OS.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

type Git struct {
	RepoPath string
}

func NewGit(path string) Git {
	if path == "" {
		path = "."
	}
	return Git{RepoPath: path}
}

func (g Git) add() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd() error: %v", err)
	}
	if cwd != g.RepoPath {
		err := os.Chdir(g.RepoPath)
		if err != nil {
			return fmt.Errorf("unable to change to repo path %q: %v", g.RepoPath, err)
		}
	}
	addCmd := exec.Command("git", "add", g.RepoPath)
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("error adding files: %v", err)
	}
	return nil
}

func (g Git) commit(msg string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd() error: %v", err)
	}
	if cwd != g.RepoPath {
		err := os.Chdir(g.RepoPath)
		if err != nil {
			return fmt.Errorf("unable to change to repo path %q: %v", g.RepoPath, err)
		}
	}
	commitCmd := exec.Command("git", "commit", "-m", msg)
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("error committing changes: %v", err)
	}
	return nil
}

func (g Git) Push() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd() error: %v", err)
	}
	if cwd != g.RepoPath {
		err := os.Chdir(g.RepoPath)
		if err != nil {
			return fmt.Errorf("unable to change to repo path %q: %v", g.RepoPath, err)
		}
	}
	pushCmd := exec.Command("git", "push")
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("error pushing changes: %v", err)
	}
	return nil
}
