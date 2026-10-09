//go:build linux

package apt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Executor is an interactive, root-required APT adapter. Construction is lazy.
// Execute must only be called by an approving/revalidating workflow. The preview
// is not itself evidence of human approval. No elevation or persistent authority
// is provided. APT/package scripts are not atomic and may have side effects.
type Executor struct {
	provider       *Provider
	stdin          io.Reader
	stdout, stderr io.Writer
	uid            func() int
	command        func(context.Context, string, ...string) *exec.Cmd
	checkHost      func() error
}

func NewExecutor(p *Provider, stdin io.Reader, stdout, stderr io.Writer) *Executor {
	return &Executor{provider: p, stdin: stdin, stdout: stdout, stderr: stderr,
		uid: os.Geteuid, command: exec.CommandContext, checkHost: checkExecutionHost}
}

func (e *Executor) Execute(ctx context.Context, preview Preview) (ret error) {
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.uid == nil || e.command == nil || e.checkHost == nil {
		return errors.New("missing execution dependencies")
	}
	if e.uid() != 0 {
		return errors.New("apt execution requires root after transaction approval; no automatic elevation")
	}
	if e.provider == nil || e.provider.runner == nil || e.stdin == nil || e.stdout == nil || e.stderr == nil {
		return errors.New("missing interactive execution dependencies")
	}
	if err := e.checkHost(); err != nil {
		return err
	}
	provider := e.provider
	// The production apply backend already owns per-query trust/environment
	// checks. Do not wrap it again or pass absolute names into its dispatcher.
	if _, guarded := provider.runner.(applyMetadataRunner); !guarded {
		provider = New(executionRunner{runner: provider.runner})
	}
	version, versionStderr, versionErr := provider.runner.Run(ctx, "apt-get", "--version")
	if versionErr != nil || versionStderr != "" || !strings.HasPrefix(version, "apt 2.6.1 (") {
		return errors.Join(errors.New("guarded execution requires source-established APT 2.6.1"), versionErr)
	}
	if err := provider.ValidateApply(ctx, preview); err != nil {
		return err
	}
	dump, stderr, err := provider.runner.Run(ctx, "apt-config", "dump")
	if err != nil || stderr != "" {
		// Runner errors may echo configuration values, including credentials.
		return errors.New("root APT configuration unavailable")
	}
	cfg, err := parseExecutionDump(dump)
	if err != nil {
		return err
	}
	hooks, err := checkExecutionConfig(cfg, false)
	if err != nil {
		return err
	}
	operands, err := executionOperands(preview)
	if err != nil {
		return err
	}
	// Copy authority before starting asynchronous transport work.
	preview.Requests = append([]string(nil), preview.Requests...)
	preview.Operations = append([]Operation(nil), preview.Operations...)
	for i := range preview.Operations {
		preview.Operations[i].Uncertainty = append([]string(nil), preview.Operations[i].Uncertainty...)
	}
	transport, err := newGuardTransport(ctx, provider, preview)
	if err != nil {
		return err
	}
	defer func() { ret = errors.Join(ret, transport.close()) }()
	hook := "/proc/" + strconv.Itoa(os.Getpid()) + "/exe --internal-apt-guard " + transport.path
	hooks = append([]string{hook}, hooks...)
	config := executionConfig(hooks)
	configPath := filepath.Join(transport.dir, "config")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return fmt.Errorf("write private command configuration: %w", err)
	}
	transport.hooks = hooks
	transport.cancel = cancel
	transport.checkHost = e.checkHost
	args := []string{"-c", configPath, "install", "--"}
	args = append(args, operands...)
	cmd := e.command(ctx, "/usr/bin/apt-get", args...)
	if cmd == nil {
		return errors.New("missing apt process")
	}
	cmd.Stdin = e.stdin
	cmd.Stdout = e.stdout
	var diagnostic diagnosticTail
	cmd.Stderr = io.MultiWriter(e.stderr, &diagnostic)
	// Deliberately do not propagate APT_CONFIG, loader overrides, unattended
	// debconf settings or arbitrary inherited executable search paths.
	// HOME is absent: libdpkg/options.c loads ~/.dpkg.cfg only when getenv
	// returns non-NULL. DPKG_FORCE/ROOT/ADMINDIR are likewise never inherited.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	if term := os.Getenv("TERM"); term != "" {
		cmd.Env = append(cmd.Env, "TERM="+term)
	}
	err = runAPTProcess(parent, cmd, transport.bind)
	// Finish the server before judging evidence: a late/replayed/incomplete
	// connection must not race a successful subprocess exit.
	closeErr := transport.close()
	guardErr := transport.result()
	if err != nil {
		err = fmt.Errorf("apt execution stopped (state may be partial): %w; stderr tail: %s", err, diagnostic.String())
	}
	return errors.Join(err, guardErr, closeErr, parent.Err())
}

func executionOperands(v Preview) ([]string, error) {
	operands := []string{}
	for _, name := range v.Requests {
		base, _, _ := strings.Cut(name, ":")
		found := false
		for _, op := range v.Operations {
			if op.Package != base || op.Kind != "install" {
				continue
			}
			invalidPackage := !packageOperand.MatchString(base) || strings.HasSuffix(base, "-") || strings.HasSuffix(base, "+")
			invalidVersion := !architectureToken.MatchString(op.Architecture) || !versionToken.MatchString(op.NewVersion)
			if found || invalidPackage || invalidVersion {
				return nil, errors.New("ambiguous install operand")
			}
			operand := base
			// Architecture: all is not a supported apt-get architecture qualifier.
			if op.Architecture != "all" {
				operand += ":" + op.Architecture
			}
			operands = append(operands, operand+"="+op.NewVersion)
			found = true
		}
		if !found {
			return nil, errors.New("missing requested install pin")
		}
	}
	return operands, nil
}

const preconfigureHook = "/usr/sbin/dpkg-preconfigure --apt || true"

var executionFalseKeys = []string{
	"APT::Get::Assume-Yes", "APT::Get::Force-Yes", "APT::Get::Ignore-Hold", "APT::Ignore-Hold",
	"APT::Get::AllowUnauthenticated", "APT::Get::Allow-Unauthenticated", "APT::Get::Allow-Downgrades",
	"APT::Get::Allow-Remove-Essential", "APT::Get::Allow-Change-Held-Packages",
	"APT::Get::Allow-Insecure-Repositories", "Acquire::AllowInsecureRepositories", "Acquire::AllowDowngradeToInsecureRepositories",
	"APT::Get::AutomaticRemove", "APT::Get::AutoRemove", "APT::Get::Purge", "APT::Get::Fix-Broken", "Debug::NoLocking",
}

type executionSettings map[string][]string

// apt-config dump is a snapshot, not an effective apt-get configuration. Reject
// binary-specific authority settings instead of incorrectly applying its scope.
// Parsing deliberately rejects escaped/ambiguous syntax without echoing values:
// an unrelated proxy credential must never enter errors or our generated file.
func parseExecutionDump(dump string) (executionSettings, error) {
	if len(dump) > maxCommandOutput || !strings.HasSuffix(dump, "\n") {
		return nil, errors.New("incomplete APT configuration dump")
	}
	cfg := executionSettings{}
	for _, line := range strings.Split(strings.TrimSuffix(dump, "\n"), "\n") {
		key, value, ok := strings.Cut(line, " \"")
		if !ok || key == "" || !strings.HasSuffix(value, "\";") || strings.ContainsAny(key, " \t\r\n\\\"") {
			return nil, errors.New("unsupported APT configuration syntax")
		}
		value = strings.TrimSuffix(value, "\";")
		if strings.ContainsAny(value, "\"\\\r\n\x00") {
			return nil, errors.New("unsupported APT configuration encoding")
		}
		key = strings.ToLower(key)
		cfg[key] = append(cfg[key], value)
	}
	return cfg, nil
}

// Only known default dpkg-preconfigure is retained, behind the guard. Custom
// hooks, dpkg options, alternate databases/chroots and binary overrides have no
// established coverage and fail before launching apt-get.
func checkExecutionConfig(cfg executionSettings, guarded bool) ([]string, error) {
	hooks := []string{}
	defaults := map[string][]string{
		"dir": {"/"}, "dir::state": {"var/lib/apt", "var/lib/apt/", "/var/lib/apt/"},
		"dir::state::status":          {"/var/lib/dpkg/status"},
		"dir::state::lists":           {"lists/"},
		"dir::state::extended_states": {"extended_states"},
		"dir::state::cdroms":          {"cdroms.list"},
		"dir::bin::methods":           {"/usr/lib/apt/methods"},
		"dir::bin::solvers":           {"solvers"},
		"dir::bin::planners":          {"planners"},
		"dir::bin::dpkg":              {"/usr/bin/dpkg"},
		"dir::etc":                    {"etc/apt", "etc/apt/", "/etc/apt/"},
		"dir::etc::main":              {"apt.conf"}, "dir::etc::parts": {"apt.conf.d"},
	}
	for key, values := range cfg {
		for _, value := range values {
			if value == "" {
				if _, scalar := defaults[key]; scalar {
					return nil, errors.New("empty APT execution path")
				}
				continue
			}
			aptGetScope := strings.HasPrefix(key, "binary::apt-get::") || key == "binary::apt-get"
			configScope := strings.HasPrefix(key, "binary::apt-config::") || key == "binary::apt-config"
			if aptGetScope || configScope {
				return nil, errors.New("unsupported binary-specific execution configuration")
			}
			if key == "binary" && ((!guarded && value != "apt-config") || (guarded && value != "apt-get")) {
				return nil, errors.New("unexpected native binary configuration scope")
			}
			if allowed, ok := defaults[key]; ok {
				good := false
				for _, v := range allowed {
					if value == v {
						good = true
					}
				}
				if !good {
					return nil, errors.New("unsupported APT root/database/configuration redirect")
				}
			}
			_, knownPath := defaults[key]
			statePath := strings.HasPrefix(key, "dir::state::")
			binaryPath := strings.HasPrefix(key, "dir::bin::")
			if (statePath || binaryPath) && !knownPath {
				return nil, errors.New("unsupported APT state/executable redirect")
			}
			if (key == "apt::solver" || key == "apt::planner") && value != "internal" {
				return nil, errors.New("external APT solver/planner unsupported")
			}
			rootOverride := key == "rootdir" || strings.Contains(key, "chroot")
			debugOverride := strings.HasPrefix(key, "debug::") && key != "debug::nolocking"
			if rootOverride || debugOverride {
				return nil, errors.New("unsupported root/debug execution override")
			}
			if strings.Contains(key, "invoke") || strings.Contains(key, "hook") {
				return nil, errors.New("unsupported native execution hook")
			}
			if strings.HasPrefix(key, "dpkg::") {
				switch {
				case key == "dpkg::pre-install-pkgs::":
					if !guarded && value != preconfigureHook {
						return nil, errors.New("unsupported pre-install hook")
					}
					hooks = append(hooks, value)
				case key == "dpkg::use-pty":
					// A supported BOOL (configure-index). The snapshot may use
					// native PTY virtualization; our late command config disables
					// it while preserving the inherited operator terminal.
					switch strings.ToLower(value) {
					case "true", "yes", "1", "on", "false", "no", "0", "off":
					default:
						return nil, errors.New("unsupported native PTY setting")
					}
				case key == "dpkg::path" && value == "/usr/sbin:/usr/bin:/sbin:/bin":
				case strings.HasPrefix(key, "dpkg::tools::options::"):
					// The sole existing known hook uses protocol 1/stdin. Other executable
					// options could alter our hook's first-token protocol lookup.
					tail := strings.TrimPrefix(key, "dpkg::tools::options::")
					if guarded && strings.HasPrefix(tail, "/proc/") {
						continue
					}
					if tail != "/usr/sbin/dpkg-preconfigure::version" || value != "1" {
						return nil, errors.New("unsupported native hook protocol options")
					}
				case key == "dpkg::progress-fancy" && (value == "0" || value == "false"):
				default:
					return nil, errors.New("unsupported dpkg execution override")
				}
			}
			knownFalse := false
			for _, falseKey := range executionFalseKeys {
				if key == strings.ToLower(falseKey) {
					knownFalse = true
				}
			}
			if knownFalse {
				switch strings.ToLower(value) {
				case "false", "no", "0", "off":
				default:
					return nil, errors.New("unsafe native execution configuration")
				}
			}
			if strings.HasPrefix(key, "apt::get::allow") && !knownFalse {
				return nil, errors.New("unsupported native allowance configuration")
			}
			tlsSafety := strings.HasSuffix(key, "::verify-peer") || strings.HasSuffix(key, "::verify-host")
			dateSafety := strings.HasSuffix(key, "::check-date") || strings.HasSuffix(key, "::check-valid-until")
			if strings.HasPrefix(key, "acquire::") && (tlsSafety || dateSafety) {
				switch strings.ToLower(value) {
				case "true", "yes", "1", "on":
				default:
					return nil, errors.New("native acquisition safeguard disabled")
				}
			}
			// Unsafe pre-existing settings are rejected, not silently replaced.
			if err := validateHookConfig(encodeConfig(key) + "=" + encodeConfig(value)); err != nil {
				return nil, err
			}
		}
	}
	if !guarded && len(hooks) > 1 {
		return nil, errors.New("repeated preconfigure hook")
	}
	return hooks, nil
}

func executionConfig(hooks []string) string {
	var b strings.Builder
	for _, key := range []string{"DPkg::Pre-Invoke", "DPkg::Post-Invoke", "DPkg::Pre-Install-Pkgs", "DPkg::Tools::Options"} {
		fmt.Fprintf(&b, "#clear %s;\n", key)
	}
	for _, hook := range hooks {
		fmt.Fprintf(&b, "DPkg::Pre-Install-Pkgs:: \"%s\";\n", hook)
	}
	token, _, _ := strings.Cut(hooks[0], " ")
	fmt.Fprintf(&b, "DPkg::Tools::Options::%s::Version \"3\";\nDPkg::Tools::Options::%s::InfoFD \"0\";\n", token, token)
	if len(hooks) > 1 {
		b.WriteString("DPkg::Tools::Options::/usr/sbin/dpkg-preconfigure::Version \"1\";\n")
	}
	for _, key := range executionFalseKeys {
		fmt.Fprintf(&b, "%s \"false\";\n", key)
	}
	// Debian APT 2.6.1 dpkgpm.cc:1210/1309/2008 and fileutl.cc:898:
	// false => master=-1/slave=NULL => SetupSlave returns before setsid.
	// ExecFork itself preserves the managed apt group and inherited TTY.
	// This does not prevent intentionally daemonized maintainer-script services.
	b.WriteString("Dpkg::Use-Pty \"false\";\nquiet \"0\";\nAPT::Get::Quiet \"0\";\n")
	return b.String()
}

func encodeConfig(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c <= 32 || c >= 127 || strings.ContainsRune("%=\"", rune(c)) {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func checkExecutionHost() error {
	// Native cancellation coverage is restricted to the source-established
	// APT 2.6.1 no-PTY profile, generated before launch and verified by the
	// first native hook. Root trust/configuration checks are still mandatory.
	unsafeEnvironment := []string{
		"APT_CONFIG", "DPKG_FORCE", "DPKG_ROOT", "DPKG_ADMINDIR", "LD_PRELOAD", "LD_LIBRARY_PATH", "GCONV_PATH", "GLIBC_TUNABLES",
	}
	for _, key := range unsafeEnvironment {
		if os.Getenv(key) != "" {
			return errors.New("inherited APT/dpkg configuration redirect unsupported")
		}
	}
	// Resolve trusted binaries and configuration ancestors without following
	// user-controlled writable directories. Root adversaries are out of scope.
	trustedPaths := []string{
		"/", "/tmp", "/usr", "/usr/bin", "/usr/bin/apt-get", "/usr/bin/apt-config", "/usr/bin/apt-cache",
		"/usr/bin/dpkg-query", "/usr/bin/dpkg", "/usr/sbin", "/usr/sbin/dpkg-preconfigure",
		"/etc", "/etc/apt", "/etc/apt/apt.conf.d", "/proc/self/exe",
	}
	for _, path := range trustedPaths {
		if err := trustedExecutionPath(path); err != nil {
			return err
		}
	}
	// Config parts may be symlinks; require each resolved file to be root-owned
	// and not writable by others. Never read or serialize their contents here.
	entries, err := os.ReadDir("/etc/apt/apt.conf.d")
	if err != nil {
		return errors.New("APT configuration directory unavailable")
	}
	paths := []string{"/etc/apt/apt.conf"}
	for _, entry := range entries {
		paths = append(paths, filepath.Join("/etc/apt/apt.conf.d", entry.Name()))
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) && path == "/etc/apt/apt.conf" {
			continue
		}
		if err != nil {
			return errors.New("APT configuration ownership unavailable")
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsupported APT configuration file type")
		}
		if err := trustedExecutionPath(path); err != nil {
			return err
		}
		if err := checkConfigIncludes(path); err != nil {
			return err
		}
	}
	return checkNativeDpkgConfig(nativeDpkgConfigFS())
}

func trustedExecutionPath(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("trusted execution path unavailable")
	}
	for {
		info, err := os.Stat(resolved)
		if err != nil {
			return errors.New("trusted execution ownership unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		stickyTmp := resolved == "/tmp" && info.Mode()&os.ModeSticky != 0
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 && !stickyTmp {
			return errors.New("untrusted execution path or ancestor")
		}
		if resolved == "/" {
			return nil
		}
		resolved = filepath.Dir(resolved)
	}
}

// Includes/directives can reach configuration outside the verified root-owned
// files. The minimal adapter rejects them instead of inventing an include graph.
// Values (which may contain proxy credentials) are never echoed or persisted.
func checkConfigIncludes(path string) (ret error) {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("APT configuration unavailable")
	}
	defer func() { ret = errors.Join(ret, file.Close()) }()
	scanner := bufio.NewScanner(io.LimitReader(file, maxCommandOutput+1))
	total := 0
	for scanner.Scan() {
		total += len(scanner.Bytes()) + 1
		if total > maxCommandOutput || strings.Contains(scanner.Text(), "#") {
			return errors.New("APT configuration directives/includes unsupported")
		}
	}
	if scanner.Err() != nil {
		return errors.New("APT configuration scan incomplete")
	}
	return nil
}

// Read-only guard queries use fixed trusted binary paths AND a clean environment.
// ExecRunner normally inherits HOME, which could load ~/.dpkg-query.cfg and
// redirect protection/state evidence. Keep its bounded command behavior here,
// without changing the read-only provider API or process-wide environment.
// Injected runners remain the private deterministic evidence seam.
type executionRunner struct {
	runner  Runner
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r executionRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	switch name {
	case "dpkg-query", "apt-cache", "dpkg", "apt-config", "apt-get":
	default:
		return "", "", errors.New("unexpected execution metadata command")
	}
	path := "/usr/bin/" + name
	switch r.runner.(type) {
	case ExecRunner, *ExecRunner:
		return r.runNativeMetadata(ctx, path, args...)
	default:
		return r.runner.Run(ctx, path, args...)
	}
}

func (r executionRunner) runNativeMetadata(parent context.Context, path string, args ...string) (string, string, error) {
	timeout := commandTimeout
	if path == "/usr/bin/apt-get" && len(args) > 0 && args[0] == "--simulate" {
		timeout = simulationTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(ctx, path, args...)
	if cmd == nil {
		return "", "", errors.New("missing execution metadata process")
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	var stdout, stderr boundedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		err = errors.Join(err, errors.New("execution metadata output limit exceeded"))
	}
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

type diagnosticTail struct{ data []byte }

func (d *diagnosticTail) Write(p []byte) (int, error) {
	const limit = 4096
	n := len(p)
	if len(p) >= limit {
		d.data = append(d.data[:0], p[len(p)-limit:]...)
	} else {
		d.data = append(d.data, p...)
		if len(d.data) > limit {
			d.data = append([]byte(nil), d.data[len(d.data)-limit:]...)
		}
	}
	return n, nil
}
func (d *diagnosticTail) String() string { return strings.TrimSpace(string(d.data)) }
