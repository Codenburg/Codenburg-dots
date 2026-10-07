package system

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Info struct {
	ID, Name, PrettyName, IDLike  string
	Architecture, RawArchitecture string
	Supported, APTAvailable       bool
}

func ParseOSRelease(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024), 1024*1024)
	line := 0
	for s.Scan() {
		line++
		text := strings.TrimSpace(s.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		i := strings.IndexByte(text, '=')
		if i <= 0 {
			return nil, fmt.Errorf("os-release line %d: expected KEY=value", line)
		}
		key, raw := text[:i], text[i+1:]
		if !validKey(key) {
			return nil, fmt.Errorf("os-release line %d: invalid key %q", line, key)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("os-release line %d: duplicate key %q", line, key)
		}
		value, err := parseValue(raw)
		if err != nil {
			return nil, fmt.Errorf("os-release line %d: %w", line, err)
		}
		values[key] = value
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("read os-release: %w", err)
	}
	return values, nil
}

func validKey(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	quote := byte(0)
	if raw[0] == '"' || raw[0] == '\'' {
		quote, raw = raw[0], raw[1:]
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 && c == quote {
			if strings.TrimSpace(raw[i+1:]) != "" {
				return "", fmt.Errorf("trailing content after quoted value")
			}
			return b.String(), nil
		}
		if quote == 0 && (c == '"' || c == '\'') {
			return "", fmt.Errorf("unexpected quote in unquoted value")
		}
		if c == '\\' && quote != '\'' {
			i++
			if i >= len(raw) {
				return "", fmt.Errorf("trailing escape")
			}
			next := raw[i]
			// Only these characters are escaped inside double quotes. No value
			// is evaluated: dollar signs and backticks remain literal data.
			if quote == '"' && next != '$' && next != '`' && next != '"' && next != '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(next)
			continue
		}
		b.WriteByte(c)
	}
	if quote != 0 {
		return "", fmt.Errorf("unterminated quoted value")
	}
	return strings.TrimSpace(b.String()), nil
}

func IsSupportedID(id string) bool { return id == "debian" || id == "ubuntu" || id == "linuxmint" }

func NormalizeArchitecture(raw string) (normalized, original string) {
	switch raw {
	case "x86_64":
		return "amd64", raw
	case "aarch64":
		return "arm64", raw
	default:
		return raw, raw
	}
}

// machineArchitecture queries the kernel rather than the binary's build target.
// The command callback makes the timeout and output handling testable without
// requiring a particular host architecture or installed executable.
func machineArchitecture(parent context.Context, run func(context.Context, string, ...string) (string, error)) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	output, err := run(ctx, "uname", "-m")
	if ctx.Err() != nil {
		return "", "", fmt.Errorf("query machine architecture: %w", ctx.Err())
	}
	if err != nil {
		return "", "", fmt.Errorf("query machine architecture: %w", err)
	}
	raw := strings.TrimSpace(output)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", "", fmt.Errorf("uname -m returned invalid machine architecture %q", raw)
	}
	arch, original := NormalizeArchitecture(raw)
	return arch, original, nil
}

func runMachineCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w (%s)", command, err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}

func Detect() (Info, error) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return Info{}, fmt.Errorf("read /etc/os-release: %w", err)
	}
	defer f.Close()
	values, err := ParseOSRelease(f)
	if err != nil {
		return Info{}, err
	}
	arch, raw, err := machineArchitecture(context.Background(), runMachineCommand)
	if err != nil {
		return Info{}, err
	}
	id := strings.ToLower(values["ID"])
	if id == "" {
		return Info{}, fmt.Errorf("/etc/os-release is missing required ID")
	}
	name := values["NAME"]
	if name == "" {
		name = values["PRETTY_NAME"]
	}
	aptAvailable := true
	for _, command := range []string{"dpkg-query", "apt-cache", "dpkg"} {
		if _, err := exec.LookPath(command); err != nil {
			aptAvailable = false
			break
		}
	}
	return Info{ID: id, Name: name, PrettyName: values["PRETTY_NAME"], IDLike: values["ID_LIKE"], Architecture: arch, RawArchitecture: raw, Supported: IsSupportedID(id), APTAvailable: aptAvailable}, nil
}
