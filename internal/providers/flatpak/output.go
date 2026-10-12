package flatpak

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func table(output string, columns int) ([][]string, []error) {
	rows := [][]string{}
	errs := []error{}
	// flatpak_table_printer_print emits a newline even with no rows.
	if output == "" || output == "\n" {
		return rows, errs
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		// Flatpak's print_row uses g_strchomp, dropping trailing empty cells.
		// Only the final column is optional in our closed query shapes:
		// remote options or application version. Never pad required identity.
		if len(fields) == columns-1 {
			fields = append(fields, "")
		}
		invalidText := !utf8.ValidString(line) || strings.IndexFunc(line, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) >= 0
		if len(fields) != columns || invalidText {
			errs = append(errs, fmt.Errorf("malformed flatpak row %d: expected %d tab-separated fields", i+1, columns))
			continue
		}
		rows = append(rows, fields)
	}
	return rows, errs
}
func parseRef(scope Scope, remote, raw, version string) (Ref, error) {
	parts := strings.Split(raw, "/")
	if len(parts) == 4 && parts[0] == "app" {
		parts = parts[1:]
	}
	if len(parts) != 3 {
		return Ref{}, fmt.Errorf("malformed application ref %q", raw)
	}
	s := Selection{ID: parts[0], Scope: scope, Remote: remote, Arch: parts[1], Branch: parts[2]}
	if !validOperand(remote) || !validOperand(s.Arch) || !validOperand(s.Branch) {
		return Ref{}, fmt.Errorf("incomplete application ref %q", raw)
	}
	if err := s.Validate(); err != nil {
		return Ref{}, fmt.Errorf("malformed application ref: %w", err)
	}
	return Ref{Scope: scope, Remote: remote, ID: s.ID, Arch: s.Arch, Branch: s.Branch, Version: version}, nil
}
func parseRemote(scope Scope, row []string) (Remote, error) {
	if !validOperand(row[0]) {
		return Remote{}, fmt.Errorf("invalid configured remote %q", row[0])
	}
	result := Remote{Scope: scope, Name: row[0]}
	if row[1] == "" {
		return result, nil
	}
	for _, option := range strings.Split(row[1], ",") {
		// Unknown flags are diagnostic rather than silently treated as enabled.
		switch option {
		case "disabled":
			result.Disabled = true
		case "system", "user", "no-gpg-verify", "no-enumerate", "no-deps", "oci":
		default:
			return Remote{}, fmt.Errorf("unrecognized remote option %q", option)
		}
	}
	return result, nil
}
