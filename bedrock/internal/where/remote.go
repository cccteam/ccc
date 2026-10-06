package where

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

const (
	// gitConfig is the repository's configuration file, under its git directory.
	gitConfig = "config"
	// gitdirPrefix starts the one line a worktree's .git file holds.
	gitdirPrefix = "gitdir:"
	// originSection is the section of the remote bedrock reads.
	originSection = `[remote "origin"]`
)

// remoteRE matches the GitHub forms of a remote URL: git@github.com:owner/name.git,
// ssh://git@github.com/owner/name.git, https://github.com/owner/name(.git).
var remoteRE = regexp.MustCompile(`^(?:git@github\.com:|ssh://git@github\.com/|https://github\.com/)([^/]+)/([^/]+?)(?:\.git)?/?$`)

// Remote is the GitHub repository the repository at root pushes to: the owner and
// name of its origin remote. A worktree's .git file is followed to the git directory.
func Remote(repoRoot string) (owner, name string, err error) {
	dir, err := gitDirectory(repoRoot)
	if err != nil {
		return "", "", err
	}
	url, err := originURL(filepath.Join(dir, gitConfig))
	if err != nil {
		return "", "", err
	}
	m := remoteRE.FindStringSubmatch(url)
	if m == nil {
		return "", "", errors.Newf("origin remote %q of %s is not a GitHub repository URL (git@github.com:owner/name.git or https://github.com/owner/name)", url, repoRoot)
	}

	return m[1], m[2], nil
}

// gitDirectory is the repository's git directory: .git when it is a directory, else
// the directory the .git file names (a worktree).
func gitDirectory(repoRoot string) (string, error) {
	path := filepath.Join(repoRoot, gitDir)
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.Wrapf(err, "no %s at %s", gitDir, repoRoot)
	}
	if info.IsDir() {
		return path, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, gitdirPrefix) {
		return "", errors.Newf("%s is neither a directory nor a worktree's pointer (%s ...)", path, gitdirPrefix)
	}
	dir := strings.TrimSpace(strings.TrimPrefix(line, gitdirPrefix))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	// A worktree's git directory holds no config of its own; the main one, two levels
	// up (<main>/.git/worktrees/<name>), does.
	if filepath.Base(filepath.Dir(dir)) == "worktrees" {
		dir = filepath.Dir(filepath.Dir(dir))
	}

	return dir, nil
}

// originURL reads the url of [remote "origin"] from the configuration file.
func originURL(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", errors.Wrapf(err, "no git configuration at %s", file)
	}
	defer f.Close()
	inOrigin := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "["):
			inOrigin = line == originSection
		case inOrigin && strings.HasPrefix(line, "url"):
			_, value, found := strings.Cut(line, "=")
			if found {
				return strings.TrimSpace(value), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", errors.Wrap(err, "bufio.Scanner.Scan()")
	}

	return "", errors.Newf("%s names no origin remote: bedrock addresses the repository through its origin on GitHub", file)
}
