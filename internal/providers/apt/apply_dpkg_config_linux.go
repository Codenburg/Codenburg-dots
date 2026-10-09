//go:build linux

package apt

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// libdpkg/options.h (1.21.22): fgets uses char[MAX_CONFIG_LINE].
	// Reject even the boundary-length physical line before skipping comments;
	// otherwise a comment's next fgets chunk can become an active option.
	dpkgMaxConfigLine    = 1024
	dpkgMaxConfigFile    = 64 * 1024
	dpkgMaxConfigTotal   = 1024 * 1024
	dpkgMaxConfigFiles   = 64
	dpkgMaxConfigEntries = 256
)

// Private filesystem seam; no alternate root/configuration is exposed by the CLI.
type dpkgConfigFS struct {
	lstat   func(string) (os.FileInfo, error)
	open    func(string) (io.ReadCloser, error)
	readDir func(string) ([]os.DirEntry, error)
}

func nativeDpkgConfigFS() dpkgConfigFS {
	return dpkgConfigFS{
		lstat:   os.Lstat,
		open:    func(path string) (io.ReadCloser, error) { return os.Open(path) },
		readDir: readDpkgConfigDir,
	}
}

func readDpkgConfigDir(path string) ([]os.DirEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("native dpkg configuration directory unavailable")
	}
	entries, readErr := file.ReadDir(dpkgMaxConfigEntries + 1)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("native dpkg configuration directory read incomplete")
	}
	return entries, nil
}

// Tagged libdpkg/options.c loads cfg.d ASCII alphanumeric/_/- names, then
// dpkg.cfg, then $HOME/.dpkg.cfg ONLY if HOME exists. Executor's explicit child
// environment omits HOME and every DPKG_* override; no user config is loaded.
// This is a known-default profile, not a general dpkg parser or policy override.
func checkNativeDpkgConfig(fsys dpkgConfigFS) error {
	// dpkg-query uses the same loader with its own program name. Its root/DB
	// options can redirect the safety evidence; dpkg.cfg does not cover them.
	for _, tool := range []string{"dpkg", "dpkg-query"} {
		if err := checkNativeDpkgToolConfig(fsys, tool); err != nil {
			return err
		}
	}
	return nil
}

func checkNativeDpkgToolConfig(fsys dpkgConfigFS, tool string) error {
	for _, path := range []string{"/", "/etc"} {
		if _, err := trustedDpkgConfigNode(fsys, path, true, false); err != nil {
			return err
		}
	}
	exists, err := trustedDpkgConfigNode(fsys, "/etc/dpkg", true, true)
	if err != nil || !exists {
		return err
	}
	paths := []string{}
	main := "/etc/dpkg/" + tool + ".cfg"
	parts := main + ".d"
	exists, err = trustedDpkgConfigNode(fsys, parts, true, true)
	if err != nil {
		return err
	}
	if exists {
		entries, err := fsys.readDir(parts)
		if err != nil {
			return errors.New("native dpkg configuration directory unavailable")
		}
		if len(entries) > dpkgMaxConfigEntries {
			return errors.New("native dpkg configuration entry limit exceeded")
		}
		names := map[string]bool{}
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") || names[name] {
				return errors.New("ambiguous native dpkg configuration entry")
			}
			names[name] = true
			if activeDpkgConfigName(name) {
				paths = append(paths, filepath.Join(parts, name))
			}
		}
	}
	paths = append(paths, main)
	if len(paths) > dpkgMaxConfigFiles {
		return errors.New("native dpkg configuration file limit exceeded")
	}
	seen := map[string]bool{}
	total := 0
	for _, path := range paths {
		// Only the main file is optional. An active directory entry that vanishes
		// is ambiguous freshness evidence, even though native fopen ignores ENOENT.
		exists, err := trustedDpkgConfigNode(fsys, path, false, path == main)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		data, err := readDpkgConfigFile(fsys, path)
		if err != nil {
			return err
		}
		total += len(data)
		if total > dpkgMaxConfigTotal {
			return errors.New("native dpkg configuration total limit exceeded")
		}
		if err := checkDpkgConfigData(data, seen, tool == "dpkg"); err != nil {
			return err
		}
	}
	return nil
}

func trustedDpkgConfigNode(fsys dpkgConfigFS, path string, directory, optional bool) (bool, error) {
	info, err := fsys.lstat(path)
	if optional && errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil || info == nil {
		return false, errors.New("native dpkg configuration ownership unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
		return false, errors.New("untrusted native dpkg configuration or ancestor")
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return false, errors.New("unsupported native dpkg configuration file type")
	}
	if !directory && info.Size() > dpkgMaxConfigFile {
		return false, errors.New("native dpkg configuration file limit exceeded")
	}
	return true, nil
}

func readDpkgConfigFile(fsys dpkgConfigFS, path string) ([]byte, error) {
	file, err := fsys.open(path)
	if err != nil {
		return nil, errors.New("native dpkg configuration unavailable")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, dpkgMaxConfigFile+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("native dpkg configuration read incomplete")
	}
	if len(data) > dpkgMaxConfigFile {
		return nil, errors.New("native dpkg configuration file limit exceeded")
	}
	return data, nil
}

func activeDpkgConfigName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range []byte(name) {
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		digit := c >= '0' && c <= '9'
		if !letter && !digit && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func checkDpkgConfigData(data []byte, seen map[string]bool, nativeDefaults bool) error {
	if bytes.ContainsAny(data, "\r\x00") {
		return errors.New("unsupported native dpkg configuration encoding")
	}
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		if len(line) >= dpkgMaxConfigLine-1 {
			return errors.New("native dpkg configuration physical line limit exceeded")
		}
		// Native str_rtrim_spaces trims the tail, NOT leading whitespace.
		text := strings.TrimRight(string(line), " \t\n\v\f")
		if text == "" || text[0] == '#' {
			continue
		}
		if !nativeDefaults {
			return errors.New("unsupported native dpkg evidence-tool configuration")
		}
		key := ""
		switch text {
		case "no-debsig":
			// Native individual .deb signature default only. This does not
			// weaken the independent APT repository/authenticity safeguards.
			key = "no-debsig"
		case "log /var/log/dpkg.log", "log=/var/log/dpkg.log":
			key = "log"
		default:
			// No quoting, includes, double equals, inline comments, actions,
			// force/refuse, hooks, redirects or unknown keys are interpreted.
			return errors.New("unsupported native dpkg configuration option or syntax")
		}
		if seen[key] {
			return errors.New("duplicate native dpkg configuration option")
		}
		seen[key] = true
	}
	return nil
}
