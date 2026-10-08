package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const installedMetadataFormat = "Package: ${Package}\\nArchitecture: ${Architecture}\\nVersion: ${Version}\\n" +
	"Status: ${Status}\\nEssential: ${Essential}\\nProtected: ${Protected}\\nConffiles: ${Conffiles}\\n\\n"

type metadata struct {
	arch                           string
	critical, conffiles, essential bool
	err                            error
	diagnostic                     Diagnostic
}

func (p *Provider) transactionMetadata(ctx context.Context, identity, version string, old bool) metadata {
	m := metadata{}
	if old {
		m.diagnostic = p.diagnostic(ctx, "dpkg-query", "-W", "-f="+installedMetadataFormat, "--", identity)
	} else {
		m.diagnostic = p.diagnostic(ctx, "apt-cache", "show", "--", identity+"="+version)
	}
	issues := []error{m.diagnostic.Err}
	fields := map[string]string{}
	base, arch, _ := strings.Cut(identity, ":")
	record := map[string]string{}
	positive := false
	flush := func() {
		bound := record["Package"] == base && record["Version"] == version
		archBound := architectureToken.MatchString(record["Architecture"]) && (arch == "" || arch == record["Architecture"])
		if bound && archBound && positive {
			m.critical = true
		}
		record = map[string]string{}
		positive = false
	}
	previous := ""
	records := 0
	identityConflict := false
	for _, line := range strings.Split(m.diagnostic.Stdout, "\n") {
		if line == "" {
			flush()
			previous = ""
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if previous == "" {
				issues = append(issues, errors.New("orphan metadata continuation"))
			} else {
				fields[previous] += "\n" + line
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if !ok {
			issues = append(issues, errors.New("malformed metadata field"))
			continue
		}
		if key == "Package" {
			flush()
			records++
		}
		if _, ok := fields[key]; ok {
			if key == "Package" || key == "Version" || key == "Architecture" {
				identityConflict = true
			}
			issues = append(issues, fmt.Errorf("duplicate/ambiguous metadata field %s", key))
		}
		fields[key] = value
		record[key] = value
		previous = key
		// Preserve positive evidence even when the record set is contradictory.
		if (key == "Essential" || key == "Protected") && value == "yes" {
			positive = true
		}
	}
	flush()
	m.arch = fields["Architecture"]
	bound := records == 1 && fields["Package"] == base && fields["Version"] == version
	m.essential = bound && !identityConflict && fields["Essential"] == "yes"
	if !bound || !architectureToken.MatchString(m.arch) || (arch != "" && arch != m.arch) {
		issues = append(issues, errors.New("metadata identity/version/architecture is ambiguous or contradictory"))
		m.arch = ""
	}
	if identityConflict {
		m.arch = ""
	}
	for _, flag := range []string{"Essential", "Protected"} {
		if fields[flag] != "yes" && fields[flag] != "no" {
			issues = append(issues, fmt.Errorf("unknown %s status for %s=%s", flag, identity, version))
		}
	}
	if old {
		if fields["Status"] != "install ok installed" && fields["Status"] != "hold ok installed" {
			issues = append(issues, errors.New("unknown or partial installed metadata status"))
		}
		m.conffiles = fields["Conffiles"] != ""
	}
	m.err = errors.Join(issues...)
	if m.err != nil {
		m.err = fmt.Errorf("assess %s=%s: %w", identity, version, m.err)
	}
	return m
}
