package apt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ValidateHook checks native DPkg::Pre-Install-Pkgs VERSION 3 information
// against a separately approved preview. It grants no approval and runs only
// read-only inspection/version queries. Runtime invocation binding, hook order,
// transport security and an explicit prohibition of purge belong to the executor:
// native **REMOVE** does not distinguish removal from purge.
//
// info must be a finite in-memory bytes/string reader or support SetReadDeadline
// (e.g. an OS pipe). Other readers fail closed: arbitrary io.Reader.Read cannot
// be interrupted safely. Deadline readers must honor deadlines; this method owns
// their read deadline until return. The caller must not mutate info concurrently.
func (p *Provider) ValidateHook(parent context.Context, expected Preview, info io.Reader) error {
	ctx, cancel := context.WithTimeout(parent, simulationTimeout)
	defer cancel()
	data, err := readHook(ctx, info)
	if err != nil {
		return err
	}
	rows, err := parseHook(data)
	if err != nil {
		return err
	}
	if err := p.ValidateApply(ctx, expected); err != nil {
		return err
	}
	if len(rows) != len(expected.Operations) {
		return errors.New("native action multiset differs from approved transaction")
	}
	approved := make(map[string]Operation, len(expected.Operations))
	for _, op := range expected.Operations {
		approved[op.Package+"/"+op.Kind] = op
	}
	fresh := expected
	fresh.Operations = make([]Operation, 0, len(rows))
	fresh.Metadata = []Diagnostic{}
	for _, row := range rows {
		op, ok := approved[row.pkg+"/"+row.kind]
		if !ok {
			return errors.New("unexpected native action")
		}
		delete(approved, row.pkg+"/"+row.kind)
		// SendPkgsInfo in Debian apt 2.4.5/2.6.1 dpkgpm.cc serializes
		// Pkg.CurrentVer(), not the simulation Conf line's old-version field,
		// for EVERY action. Normalize the approved configuration only; never
		// substitute expected data into missing native version/arch columns.
		old := op.OldVersion
		if op.Kind == "configuration" {
			old = op.Installed.Version
		}
		if row.old != old || row.new != op.NewVersion {
			return errors.New("native version drift")
		}
		if (old != "" && row.oldArch != op.Architecture) || (row.new != "" && row.newArch != op.Architecture) {
			return errors.New("native architecture drift")
		}
		if err := p.validateHookComparator(ctx, row); err != nil {
			return err
		}
		identity := row.pkg + ":" + op.Architecture
		if row.old == "" {
			// Also checks partial/unpacked state and a package which appeared
			// after review. Candidate flags never replace installed evidence.
			state, err := p.InspectApplyState(ctx, identity)
			if err != nil {
				return err
			}
			if state.Info.InstalledVersion != "" || state.Installed != (PackageEvidence{}) ||
				state.Info.CandidateVersion != row.new || state.Candidate != op.Candidate {
				return errors.New("native fresh-install state drift")
			}
			op.Installed, op.Candidate = state.Installed, state.Candidate
		} else {
			m := p.transactionMetadata(ctx, identity, row.old, true)
			if m.err != nil {
				return m.err
			}
			if m.evidence != op.Installed {
				return errors.New("native installed safety evidence drift")
			}
			op.Installed = m.evidence
			if op.Candidate != (PackageEvidence{}) {
				m = p.transactionMetadata(ctx, identity, row.new, false)
				if m.err != nil {
					return m.err
				}
				if m.evidence != op.Candidate {
					return errors.New("native candidate safety evidence drift")
				}
				op.Candidate = m.evidence
			}
		}
		fresh.Operations = append(fresh.Operations, op)
	}
	if len(approved) != 0 {
		return errors.New("missing native action")
	}
	return p.ValidateApply(ctx, fresh)
}

type hookRow struct {
	pkg, old, oldArch, new, newArch, comparator, kind string
}

type hookDeadlineReader interface {
	io.Reader
	SetReadDeadline(time.Time) error
}

func readHook(ctx context.Context, info io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadlineReader, timed := info.(hookDeadlineReader)
	switch info.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
	default:
		if !timed {
			return nil, errors.New("hook reader cannot enforce bounded reads")
		}
	}
	data := []byte{}
	buf := make([]byte, 4096)
	idle := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if timed {
			deadline := time.Now().Add(100 * time.Millisecond)
			if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
				deadline = end
			}
			if err := deadlineReader.SetReadDeadline(deadline); err != nil {
				return nil, errors.New("hook read deadline unavailable")
			}
		}
		n, err := info.Read(buf)
		if n < 0 || n > len(buf) {
			return nil, errors.New("invalid hook reader result")
		}
		data = append(data, buf[:n]...)
		if len(data) > maxCommandOutput {
			return nil, errors.New("native hook input limit exceeded")
		}
		if end := ctx.Err(); end != nil {
			return nil, end
		}
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil {
			var timeout net.Error
			if timed && errors.As(err, &timeout) && timeout.Timeout() {
				continue
			}
			// Reader errors may include configuration data; do not echo them.
			return nil, errors.New("native hook read failed")
		}
		if n == 0 {
			idle++
			if idle >= 100 {
				return nil, errors.New("native hook reader made no progress")
			}
		} else {
			idle = 0
		}
	}
}

func parseHook(data []byte) ([]hookRow, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' || bytes.ContainsAny(data, "\x00\r\t") {
		return nil, errors.New("empty or truncated native hook protocol")
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")
	if lines[0] != "VERSION 3" {
		return nil, errors.New("native hook protocol must be version 3")
	}
	i := 1
	for ; i < len(lines) && lines[i] != ""; i++ {
		if err := validateHookConfig(lines[i]); err != nil {
			return nil, err
		}
	}
	if i == len(lines) {
		return nil, errors.New("unterminated native hook configuration")
	}
	rows := []hookRow{}
	seen := map[string]bool{}
	for _, line := range lines[i+1:] {
		fields := strings.SplitN(line, " ", 9)
		if len(fields) != 9 || !packageOperand.MatchString(fields[0]) || strings.Contains(fields[0], ":") {
			return nil, errors.New("malformed native action row")
		}
		for _, offset := range []int{1, 5} {
			version, arch, multi := fields[offset], fields[offset+1], fields[offset+2]
			if !hookMultiArch(multi) {
				return nil, errors.New("unknown native MultiArch token")
			}
			if version == "-" {
				if arch != "-" || (multi != "none" && multi != "no") {
					return nil, errors.New("contradictory absent native version")
				}
			} else if !versionToken.MatchString(version) || !architectureToken.MatchString(arch) {
				return nil, errors.New("invalid native version or architecture")
			}
		}
		row := hookRow{pkg: fields[0], old: nonDash(fields[1]), oldArch: nonDash(fields[2]),
			comparator: fields[4], new: nonDash(fields[5]), newArch: nonDash(fields[6])}
		switch fields[8] {
		case "**REMOVE**":
			row.kind = "removal"
			if row.old == "" || row.new != "" {
				return nil, errors.New("contradictory native removal")
			}
		case "**CONFIGURE**":
			row.kind = "configuration"
			if row.new == "" {
				return nil, errors.New("missing native configuration version")
			}
		default:
			path := fields[8]
			if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, ".deb") || strings.ContainsFunc(path, func(r rune) bool {
				return r < 32 || r == 127
			}) || row.new == "" {
				return nil, errors.New("unknown or failed native action")
			}
			row.kind = "upgrade"
			if row.old == "" {
				row.kind = "install"
			}
		}
		key := row.pkg + "/" + row.kind
		if seen[key] {
			return nil, errors.New("repeated native action")
		}
		seen[key] = true
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("empty native action stream")
	}
	return rows, nil
}

func nonDash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

func hookMultiArch(s string) bool {
	switch s {
	case "none", "no", "same", "foreign", "allowed":
		return true
	default:
		return false
	}
}

func (p *Provider) validateHookComparator(ctx context.Context, row hookRow) error {
	want := "<"
	if row.new == "" {
		want = ">"
	} else if row.old != "" {
		equal, err := p.CompareVersions(ctx, row.new, "eq", row.old)
		if err != nil {
			return err
		}
		want = "="
		if !equal {
			newer, err := p.CompareVersions(ctx, row.new, "gt", row.old)
			if err != nil {
				return err
			}
			want = ">"
			if newer {
				want = "<"
			}
		}
	}
	if row.comparator != want {
		return errors.New("contradictory native version comparator")
	}
	return nil
}

// QuoteString in apt's contrib/strutl.cc percent-encodes whitespace,
// non-printable/non-ASCII bytes, percent and its supplied Bad characters. Config
// values can contain credentials. All config failures deliberately omit input.
func decodeHookConfig(s string, key bool) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' {
			if i+2 >= len(s) {
				return "", errors.New("malformed native configuration encoding")
			}
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", errors.New("malformed native configuration encoding")
			}
			b.WriteByte(byte(n))
			i += 2
		} else {
			if c <= 32 || c >= 127 || (key && (c == '=' || c == '"')) {
				return "", errors.New("malformed native configuration encoding")
			}
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

func validateHookConfig(line string) error {
	key, value, ok := strings.Cut(line, "=")
	if !ok || key == "" || value == "" {
		return errors.New("malformed native configuration directive")
	}
	key, err := decodeHookConfig(key, true)
	if err != nil {
		return err
	}
	value, err = decodeHookConfig(value, false)
	if err != nil {
		return err
	}
	key = strings.ToLower(key)
	// Repeated key:: entries are legitimate native lists. No config values
	// are retained, echoed, or treated as action rows.
	if strings.HasPrefix(key, "dpkg::options") || strings.HasPrefix(key, "dpkg::force") {
		return errors.New("native dpkg authority override forbidden")
	}
	if key == "quiet" || key == "apt::get::quiet" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n >= 2 {
			return errors.New("unsafe native quiet setting")
		}
	}
	switch key {
	case "apt::get::assume-yes", "apt::get::force-yes", "apt::get::ignore-hold", "apt::ignore-hold",
		"apt::get::allow-unauthenticated", "apt::get::allowunauthenticated", "apt::get::allow-downgrades",
		"apt::get::allow-remove-essential", "apt::get::allow-change-held-packages",
		"apt::get::automaticremove", "apt::get::autoremove", "apt::get::purge",
		"apt::get::fix-broken", "debug::nolocking":
		switch strings.ToLower(value) {
		case "false", "no", "0", "off":
		default:
			return errors.New("unsafe native authority setting")
		}
	}
	return nil
}
