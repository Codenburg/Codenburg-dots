//go:build linux

package apt

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

type dpkgFixtureInfo struct {
	os.FileInfo
	uid uint32
}

func (i dpkgFixtureInfo) Sys() any { return &syscall.Stat_t{Uid: i.uid} }

func dpkgFixtureFS(files fstest.MapFS) dpkgConfigFS {
	name := func(path string) string {
		if path == "/" {
			return "."
		}
		return strings.TrimPrefix(path, "/")
	}
	return dpkgConfigFS{
		lstat: func(path string) (os.FileInfo, error) {
			info, err := files.Stat(name(path))
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil, syscall.ENOENT
				}
				return nil, err
			}
			return dpkgFixtureInfo{FileInfo: info}, nil
		},
		open:    func(path string) (io.ReadCloser, error) { return files.Open(name(path)) },
		readDir: func(path string) ([]os.DirEntry, error) { return fs.ReadDir(files, name(path)) },
	}
}

func dpkgFixtureFiles(text string) fstest.MapFS {
	return fstest.MapFS{
		"etc":               {Mode: fs.ModeDir | 0755},
		"etc/dpkg":          {Mode: fs.ModeDir | 0755},
		"etc/dpkg/dpkg.cfg": {Data: []byte(text), Mode: 0644},
	}
}

func TestNativeDpkgKnownDefaultProfile(t *testing.T) {
	for _, tc := range []struct {
		name, text string
	}{
		{name: "empty"},
		{name: "standard", text: "# default native individual signatures\nno-debsig\nlog /var/log/dpkg.log\n"},
		{name: "equals and trailing spaces", text: "no-debsig \t\nlog=/var/log/dpkg.log\t\n"},
		{name: "no final newline", text: "log /var/log/dpkg.log"},
		{name: "native blank lines", text: " \t\v\f\n# comments can contain 'quotes'\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkNativeDpkgConfig(dpkgFixtureFS(dpkgFixtureFiles(tc.text))); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		files fstest.MapFS
	}{
		{name: "no dpkg directory", files: fstest.MapFS{"etc": {Mode: fs.ModeDir | 0755}}},
		{name: "no optional configuration", files: fstest.MapFS{"etc/dpkg": {Mode: fs.ModeDir | 0755}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkNativeDpkgConfig(dpkgFixtureFS(tc.files)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeDpkgUnsupportedOptionsAndSyntax(t *testing.T) {
	for _, text := range []string{
		"force-all", "force-confnew", "force-confold", "force-depends", "no-force-all", "refuse-all",
		"ignore-depends=demo", "remove", "purge", "install", "configure", "triggers-only", "no-triggers",
		"pre-invoke=private-hook", "post-invoke=private-hook", "status-logger=private-hook", "status-fd=1",
		"root=/alternate", "admindir=/alternate", "instdir=/alternate", "log=/alternate",
		"include=/alternate", "path-exclude=/usr/*", "unknown", "--no-debsig", " no-debsig", "\tlog /var/log/dpkg.log",
		"no-debsig=", "no-debsig #inline", "log /var/log/dpkg.log #inline", "log==/var/log/dpkg.log",
		"log='/var/log/dpkg.log'", "log=\"/var/log/dpkg.log\"", "log \"unbalanced", "log\t/var/log/dpkg.log",
		"no-debsig\r", "# comment\x00force-all", "no-debsig\nno-debsig", "log=/var/log/dpkg.log\nlog /var/log/dpkg.log",
	} {
		t.Run(strings.ReplaceAll(text, "\n", "/"), func(t *testing.T) {
			err := checkNativeDpkgConfig(dpkgFixtureFS(dpkgFixtureFiles(text + "\n")))
			if err == nil {
				t.Fatal("unsupported native profile accepted")
			}
			if strings.Contains(err.Error(), "private-hook") || strings.Contains(err.Error(), "/alternate") {
				t.Fatal("configuration value leaked in error")
			}
		})
	}
}

func TestNativeDpkgPhysicalLineBoundsBeforeComments(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		accepted   bool
	}{
		{name: "bounded comment", text: "#" + strings.Repeat("x", 1020) + "\n", accepted: true},
		{name: "conservative boundary comment", text: "#" + strings.Repeat("x", 1021) + "\n"},
		{name: "fgets hidden force chunk", text: "#" + strings.Repeat("x", 1022) + "force-all\n"},
		{name: "long comment without newline", text: "#" + strings.Repeat("x", 1023)},
		{name: "long blank line", text: strings.Repeat(" ", 1024) + "\n"},
		{name: "comment carriage return", text: "# comment\r\n"},
		{name: "comment nul", text: "# comment\x00\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNativeDpkgConfig(dpkgFixtureFS(dpkgFixtureFiles(tc.text)))
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
		})
	}
}

func TestNativeDpkgActiveFragmentFilter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active bool
	}{
		{name: "01_safe", active: true}, {name: "ABC-123", active: true}, {name: "_", active: true},
		{name: ".hidden"}, {name: "old.bak"}, {name: "old~"}, {name: "old.dpkg-old"},
		{name: "old.dpkg-dist"}, {name: "contains space"}, {name: "nonascii-é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := dpkgFixtureFiles("no-debsig\nlog /var/log/dpkg.log\n")
			files["etc/dpkg/dpkg.cfg.d/"+tc.name] = &fstest.MapFile{Data: []byte("force-all\n"), Mode: 0666}
			err := checkNativeDpkgConfig(dpkgFixtureFS(files))
			if (err != nil) != tc.active {
				t.Fatalf("active=%v err=%v", tc.active, err)
			}
		})
	}
	files := dpkgFixtureFiles("log /var/log/dpkg.log\n")
	files["etc/dpkg/dpkg.cfg.d/01-default"] = &fstest.MapFile{Data: []byte("no-debsig\n"), Mode: 0644}
	if err := checkNativeDpkgConfig(dpkgFixtureFS(files)); err != nil {
		t.Fatal("supported split profile rejected", err)
	}
	files["etc/dpkg/dpkg.cfg.d/02-duplicate"] = &fstest.MapFile{Data: []byte("no-debsig\n"), Mode: 0644}
	if err := checkNativeDpkgConfig(dpkgFixtureFS(files)); err == nil {
		t.Fatal("cross-file duplicate accepted")
	}
}

func TestNativeDpkgFilesystemTrustAndReadFailures(t *testing.T) {
	for _, path := range []string{
		"/", "/etc", "/etc/dpkg", "/etc/dpkg/dpkg.cfg.d", "/etc/dpkg/dpkg.cfg",
		"/etc/dpkg/dpkg-query.cfg.d", "/etc/dpkg/dpkg-query.cfg",
	} {
		for _, kind := range []string{"owner", "writable", "symlink", "wrong type", "unreadable", "enotdir"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				files := dpkgFixtureFiles("")
				files["etc/dpkg/dpkg.cfg.d"] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
				files["etc/dpkg/dpkg-query.cfg.d"] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
				files["etc/dpkg/dpkg-query.cfg"] = &fstest.MapFile{Mode: 0644}
				fsys := dpkgFixtureFS(files)
				original := fsys.lstat
				fsys.lstat = func(p string) (os.FileInfo, error) {
					info, err := original(p)
					if p != path || err != nil {
						return info, err
					}
					if kind == "unreadable" {
						return nil, syscall.EACCES
					}
					if kind == "enotdir" {
						return nil, syscall.ENOTDIR
					}
					if kind == "owner" {
						return dpkgFixtureInfo{FileInfo: info, uid: 1000}, nil
					}
					mode := info.Mode()
					switch kind {
					case "writable":
						mode |= 0020
					case "symlink":
						mode = fs.ModeSymlink | 0777
					case "wrong type":
						if info.IsDir() {
							mode = 0644
						} else {
							mode = fs.ModeDir | 0755
						}
					}
					return dpkgFixtureInfo{FileInfo: &fixtureModeInfo{FileInfo: info, mode: mode}}, nil
				}
				if err := checkNativeDpkgConfig(fsys); err == nil {
					t.Fatal("untrusted configuration accepted")
				}
			})
		}
	}
	for _, kind := range []string{"open", "read", "close", "directory"} {
		t.Run(kind, func(t *testing.T) {
			files := dpkgFixtureFiles("")
			files["etc/dpkg/dpkg.cfg.d"] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
			fsys := dpkgFixtureFS(files)
			if kind == "directory" {
				fsys.readDir = func(string) ([]os.DirEntry, error) { return nil, syscall.EACCES }
			} else {
				fsys.open = func(string) (io.ReadCloser, error) {
					if kind == "open" {
						return nil, syscall.EACCES
					}
					return failingDpkgReader{kind: kind}, nil
				}
			}
			if err := checkNativeDpkgConfig(fsys); err == nil {
				t.Fatal("configuration read failure accepted")
			}
		})
	}
}

type fixtureModeInfo struct {
	os.FileInfo
	mode fs.FileMode
}

func (i *fixtureModeInfo) Mode() fs.FileMode { return i.mode }
func (i *fixtureModeInfo) IsDir() bool       { return i.mode.IsDir() }

type failingDpkgReader struct{ kind string }

func (r failingDpkgReader) Read([]byte) (int, error) {
	if r.kind == "read" {
		return 0, errors.New("private-read-detail")
	}
	return 0, io.EOF
}
func (r failingDpkgReader) Close() error {
	if r.kind == "close" {
		return errors.New("private-close-detail")
	}
	return nil
}

func TestNativeDpkgQueryConfigurationProfile(t *testing.T) {
	for _, tc := range []struct {
		name, target, text string
		accepted           bool
	}{
		{name: "comment only query", target: "dpkg-query.cfg", text: "# native query defaults\n \t\n", accepted: true},
		{name: "query admindir", target: "dpkg-query.cfg", text: "admindir=/alternate\n"},
		{name: "query root", target: "dpkg-query.cfg", text: "root=/alternate\n"},
		{name: "query active fragment", target: "dpkg-query.cfg.d/01_redirect", text: "admindir=/alternate\n"},
		{name: "query backup ignored", target: "dpkg-query.cfg.d/01_redirect.bak", text: "admindir=/alternate\n", accepted: true},
		{name: "query dotfile ignored", target: "dpkg-query.cfg.d/.redirect", text: "admindir=/alternate\n", accepted: true},
		{name: "dpkg default not query default", target: "dpkg-query.cfg", text: "no-debsig\n"},
		{name: "query oversized comment", target: "dpkg-query.cfg", text: "#" + strings.Repeat("x", 1022) + "admindir=/alternate\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := dpkgFixtureFiles("no-debsig\nlog /var/log/dpkg.log\n")
			files["etc/dpkg/"+tc.target] = &fstest.MapFile{Data: []byte(tc.text), Mode: 0644}
			err := checkNativeDpkgConfig(dpkgFixtureFS(files))
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
		})
	}
}

func TestNativeDpkgAggregateBounds(t *testing.T) {
	for _, kind := range []string{"file", "actual read", "files", "entries", "total", "ambiguous"} {
		t.Run(kind, func(t *testing.T) {
			files := dpkgFixtureFiles("")
			fsys := dpkgFixtureFS(files)
			switch kind {
			case "file":
				files["etc/dpkg/dpkg.cfg"].Data = []byte(strings.Repeat("#\n", dpkgMaxConfigFile))
			case "actual read":
				fsys.open = func(string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(strings.Repeat("#\n", dpkgMaxConfigFile))), nil
				}
			case "files", "entries", "total", "ambiguous":
				n := dpkgMaxConfigFiles + 1
				if kind == "entries" {
					n = dpkgMaxConfigEntries + 1
				}
				if kind == "total" {
					n = 20
				}
				for i := 0; i < n; i++ {
					text := "# empty\n"
					if kind == "total" {
						text = strings.Repeat("#\n", dpkgMaxConfigFile/2)
					}
					files["etc/dpkg/dpkg.cfg.d/"+strings.Repeat("a", i+1)] = &fstest.MapFile{Mode: 0644, Data: []byte(text)}
				}
				if kind == "ambiguous" {
					original := fsys.readDir
					fsys.readDir = func(path string) ([]os.DirEntry, error) {
						entries, err := original(path)
						return append(entries[:1], entries[0]), err
					}
				}
			}
			if err := checkNativeDpkgConfig(fsys); err == nil {
				t.Fatal("native configuration bound exceeded")
			}
		})
	}
}
