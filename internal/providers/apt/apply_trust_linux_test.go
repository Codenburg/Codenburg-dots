//go:build linux

package apt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"testing/fstest"
)

func TestTrustedExecutionPathReportsRejectedNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "untrusted-tool")
	if err := os.WriteFile(path, []byte("inert fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := before.Sys().(*syscall.Stat_t).Uid
	want := fmt.Sprintf("untrusted execution path or ancestor: requested=%q rejected=%q uid=%d mode=0666 sticky=false", path, path, owner)
	if err := trustedExecutionPath(path); err == nil || err.Error() != want {
		t.Fatalf("want exact rejected-node diagnostic %q, got %v", want, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatalf("trust inspection modified fixture: %v", err)
	}
}

type executionTrustNode struct {
	mode    os.FileMode
	uid     uint32
	unknown bool
}

type executionTrustInfo struct {
	os.FileInfo
	node executionTrustNode
}

func (i executionTrustInfo) Sys() any {
	if i.node.unknown {
		return nil
	}
	return &syscall.Stat_t{Uid: i.node.uid}
}

func TestTrustedExecutionPathPolicyAndAncestors(t *testing.T) {
	const binary = "/usr/local/bin/cdots-fixed"
	const guard = "/tmp/cdots-123/guard"
	for _, tc := range []struct {
		name, requested, resolved, rejected string
		change                              func(map[string]executionTrustNode)
	}{
		{name: "root binary"},
		{name: "proc executable alias", requested: "/proc/self/exe", resolved: binary},
		{name: "non-root binary", rejected: binary, change: func(n map[string]executionTrustNode) {
			n[binary] = executionTrustNode{mode: 0755, uid: 1000}
		}},
		{name: "group-writable binary", rejected: binary, change: func(n map[string]executionTrustNode) {
			n[binary] = executionTrustNode{mode: 0775}
		}},
		{name: "world-writable binary", rejected: binary, change: func(n map[string]executionTrustNode) {
			n[binary] = executionTrustNode{mode: 0777}
		}},
		{name: "non-root ancestor", rejected: "/usr/local", change: func(n map[string]executionTrustNode) {
			n["/usr/local"] = executionTrustNode{mode: os.ModeDir | 0755, uid: 1000}
		}},
		{name: "group-writable ancestor", rejected: "/usr/local/bin", change: func(n map[string]executionTrustNode) {
			n["/usr/local/bin"] = executionTrustNode{mode: os.ModeDir | 0775}
		}},
		{name: "sticky outside tmp is not exempt", rejected: "/usr/local/bin", change: func(n map[string]executionTrustNode) {
			n["/usr/local/bin"] = executionTrustNode{mode: os.ModeDir | os.ModeSticky | 0777}
		}},
		{name: "root-owned sticky tmp", requested: "/tmp"},
		{name: "non-sticky writable tmp", requested: "/tmp", rejected: "/tmp", change: func(n map[string]executionTrustNode) {
			n["/tmp"] = executionTrustNode{mode: os.ModeDir | 0777}
		}},
		{name: "non-root sticky tmp", requested: "/tmp", rejected: "/tmp", change: func(n map[string]executionTrustNode) {
			n["/tmp"] = executionTrustNode{mode: os.ModeDir | os.ModeSticky | 0777, uid: 1000}
		}},
		{name: "private guard under sticky tmp", requested: guard},
		{name: "non-root private guard directory", requested: guard, rejected: "/tmp/cdots-123", change: func(n map[string]executionTrustNode) {
			n["/tmp/cdots-123"] = executionTrustNode{mode: os.ModeDir | 0700, uid: 1000}
		}},
		{name: "sticky private directory is not exempt", requested: guard, rejected: "/tmp/cdots-123", change: func(n map[string]executionTrustNode) {
			n["/tmp/cdots-123"] = executionTrustNode{mode: os.ModeDir | os.ModeSticky | 0777}
		}},
		{name: "missing ownership", rejected: binary, change: func(n map[string]executionTrustNode) {
			n[binary] = executionTrustNode{mode: 0755, unknown: true}
		}},
		{name: "root binary inside user-owned home", requested: "/proc/self/exe", resolved: "/home/tomas/cdots-fixed", rejected: "/home/tomas"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes := map[string]executionTrustNode{
				"/":                       {mode: os.ModeDir | 0755},
				"/usr":                    {mode: os.ModeDir | 0755},
				"/usr/local":              {mode: os.ModeDir | 0755},
				"/usr/local/bin":          {mode: os.ModeDir | 0755},
				binary:                    {mode: 0755},
				"/tmp":                    {mode: os.ModeDir | os.ModeSticky | 0777},
				"/tmp/cdots-123":          {mode: os.ModeDir | 0700},
				guard:                     {mode: os.ModeSocket | 0600},
				"/home":                   {mode: os.ModeDir | 0755},
				"/home/tomas":             {mode: os.ModeDir | 0755, uid: 1000},
				"/home/tomas/cdots-fixed": {mode: 0755},
			}
			if tc.change != nil {
				tc.change(nodes)
			}
			before := make(map[string]executionTrustNode, len(nodes))
			for path, node := range nodes {
				before[path] = node
			}
			requested, resolved := tc.requested, tc.resolved
			if requested == "" {
				requested = binary
			}
			if resolved == "" {
				resolved = requested
			}
			resolutions := 0
			visits := []string{}
			err := trustedExecutionPathWithFS(requested, func(path string) (string, error) {
				resolutions++
				if path != requested {
					t.Fatalf("unexpected resolution: %q", path)
				}
				return resolved, nil
			}, func(path string) (os.FileInfo, error) {
				visits = append(visits, path)
				node, ok := nodes[path]
				if !ok {
					t.Fatalf("unexpected stat: %q", path)
				}
				info, err := (fstest.MapFS{"node": &fstest.MapFile{Mode: node.mode}}).Stat("node")
				return executionTrustInfo{FileInfo: info, node: node}, err
			})
			if tc.rejected == "" {
				if err != nil {
					t.Fatalf("trusted fixture rejected: %v", err)
				}
			} else {
				node := nodes[tc.rejected]
				owner := fmt.Sprintf("uid=%d", node.uid)
				if node.unknown {
					owner = "ownership=unavailable"
				}
				want := fmt.Sprintf("untrusted execution path or ancestor: requested=%q rejected=%q %s mode=%04o sticky=%t", requested, tc.rejected, owner, node.mode.Perm(), node.mode&os.ModeSticky != 0)
				if err == nil || err.Error() != want {
					t.Fatalf("want %q, got %v", want, err)
				}
			}
			wantVisits := []string{}
			for path := resolved; ; path = filepath.Dir(path) {
				wantVisits = append(wantVisits, path)
				if path == tc.rejected || path == "/" {
					break
				}
			}
			if resolutions != 1 || !reflect.DeepEqual(visits, wantVisits) || !reflect.DeepEqual(nodes, before) {
				t.Fatalf("unexpected traversal or fixture mutation: resolutions=%d visits=%v", resolutions, visits)
			}
		})
	}
}

func TestTrustedExecutionPathLookupFailuresRemainClosed(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		resolveErr error
		statCalls  int
	}{
		{name: "resolution", want: "trusted execution path unavailable", resolveErr: errors.New("private resolver detail")},
		{name: "stat", want: "trusted execution ownership unavailable", statCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := trustedExecutionPathWithFS("/trusted/tool", func(string) (string, error) {
				return "/trusted/tool", tc.resolveErr
			}, func(string) (os.FileInfo, error) {
				calls++
				return nil, errors.New("private stat detail")
			})
			if err == nil || err.Error() != tc.want || calls != tc.statCalls {
				t.Fatalf("err=%v stat calls=%d", err, calls)
			}
		})
	}
}
