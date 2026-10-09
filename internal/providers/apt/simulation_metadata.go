package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type metadata struct {
	arch                           string
	critical, conffiles, essential bool
	evidence                       PackageEvidence
	err                            error
	diagnostic                     Diagnostic
}

func (p *Provider) transactionMetadata(ctx context.Context, identity, version string, old bool) metadata {
	m := metadata{}
	if old {
		// dpkg-query(1): --status displays the installed database entry. Unlike
		// -W formatted flags, this distinguishes omission from an empty field.
		m.diagnostic = p.diagnostic(ctx, "dpkg-query", "--status", "--", identity)
	} else {
		m.diagnostic = p.diagnostic(ctx, "apt-cache", "show", "--", identity+"="+version)
	}
	issues := []error{m.diagnostic.Err}
	base, arch, _ := strings.Cut(identity, ":")
	if !packageOperand.MatchString(identity) || !versionToken.MatchString(version) {
		issues = append(issues, errors.New("invalid metadata identity/version"))
	}
	records := []map[string]string{}
	record := map[string]string{}
	positive := false
	flush := func() {
		if len(record) == 0 {
			return
		}
		bound := record["package"] == base && record["version"] == version
		archBound := architectureToken.MatchString(record["architecture"]) &&
			(arch == "" || arch == record["architecture"])
		// Preserve bound positive risk even in a contradictory record set,
		// without promoting that set to known/comparable safety evidence.
		if bound && archBound && positive {
			m.critical = true
		}
		records = append(records, record)
		record = map[string]string{}
		positive = false
	}
	previous := ""
	identityConflict := false
	for _, line := range strings.Split(m.diagnostic.Stdout, "\n") {
		if line == "" {
			flush()
			previous = ""
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			switch previous {
			case "":
				issues = append(issues, errors.New("orphan metadata continuation"))
			case "package", "architecture", "version", "status", "essential", "protected":
				issues = append(issues, errors.New("continued scalar metadata field"))
			default:
				record[previous] += "\n" + line
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || strings.ContainsAny(key, " \t\r") {
			issues = append(issues, errors.New("malformed metadata field"))
			previous = ""
			continue
		}
		key = strings.ToLower(key) // deb-control(5): field tags are case insensitive.
		value = strings.TrimSpace(value)
		if _, duplicate := record[key]; duplicate {
			if key == "package" || key == "version" || key == "architecture" {
				identityConflict = true
			}
			issues = append(issues, fmt.Errorf("duplicate/ambiguous metadata field %s", key))
		}
		record[key] = value
		previous = key
		if (key == "essential" || key == "protected") && value == "yes" {
			positive = true
		}
	}
	flush()
	if len(records) != 1 {
		issues = append(issues, errors.New("metadata identity/version/architecture is ambiguous or contradictory"))
		m.err = fmt.Errorf("assess %s=%s: %w", identity, version, errors.Join(issues...))
		return m
	}
	fields := records[0]
	m.arch = fields["architecture"]
	bound := fields["package"] == base && fields["version"] == version
	if !bound || !architectureToken.MatchString(m.arch) || (arch != "" && arch != m.arch) || identityConflict {
		issues = append(issues, errors.New("metadata identity/version/architecture is ambiguous or contradictory"))
		m.arch = ""
	}
	m.essential = bound && !identityConflict && fields["essential"] == "yes"
	// deb-control(5), Essential and Protected: yes|no, "usually only needed
	// when the answer is yes" (Protected supported since dpkg 1.20.1).
	// https://manpages.debian.org/bookworm/dpkg/deb-control.5.en.html
	// Omission means no ONLY within a successfully queried, bound full record.
	// Explicit empty/malformed values are never omission and remain unresolved.
	for _, flag := range []string{"essential", "protected"} {
		if value, present := fields[flag]; present && value != "yes" && value != "no" {
			issues = append(issues, fmt.Errorf("unknown %s status for %s=%s", flag, identity, version))
		}
	}
	status := ""
	if old {
		status = fields["status"]
		if status != "install ok installed" && status != "hold ok installed" {
			issues = append(issues, errors.New("unknown or partial installed metadata status"))
		}
		m.conffiles = fields["conffiles"] != ""
	}
	m.err = errors.Join(issues...)
	if m.err != nil {
		m.err = fmt.Errorf("assess %s=%s: %w", identity, version, m.err)
		return m
	}
	m.evidence = PackageEvidence{
		Package: base, Architecture: m.arch, Version: version, Known: true,
		Essential: fields["essential"] == "yes", Protected: fields["protected"] == "yes",
		Status: status, Held: status == "hold ok installed",
	}
	return m
}
