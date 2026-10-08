package apt

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var summaryLine = regexp.MustCompile(`^([0-9]+) upgraded, ([0-9]+) newly installed, ([0-9]+) to remove and ([0-9]+) not upgraded\.$`)
var actionLine = regexp.MustCompile(`^(Inst|Conf|Remv) ([a-z0-9][a-z0-9+.-]*(?::[a-z0-9-]+)?)(?: \[([^][ ]+)\])?(?: \(([^ ()]+) ([^()]+) \[([a-z0-9-]+)\]\))?$`)
var versionToken = regexp.MustCompile(`^[0-9][a-zA-Z0-9.+:~\-]*$`)
var architectureToken = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var newestLine = regexp.MustCompile(`^([a-z0-9][a-z0-9+.-]*(?::[a-z0-9-]+)?) is already the newest version \(([^ ]+)\)\.$`)

type simulationList struct {
	heading string
	names   []string
}

// APT's human output is deliberately accepted only in this narrow dialect.
func parseSimulation(out string) ([]Operation, []simulationList, error) {
	ops := []Operation{}
	issues := []error{}
	summary := []int{}
	partial := 0
	inList := false
	lists := []simulationList{}
	listNames := map[string]bool{}
	listKinds := map[string]string{
		"The following additional packages will be installed:":       "install",
		"The following NEW packages will be installed:":              "install",
		"The following packages will be upgraded:":                   "upgrade",
		"The following packages will be REMOVED:":                    "removal",
		"The following packages have been kept back:":                "unchanged",
		"The following held packages will be changed:":               "changed",
		"WARNING: The following essential packages will be removed.": "removal",
		"Suggested packages:":                                        "", "Recommended packages:": "",
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if m := summaryLine.FindStringSubmatch(line); m != nil {
			if len(summary) != 0 {
				issues = append(issues, errors.New("duplicate transaction summary"))
			}
			summary = make([]int, 4)
			for i := range summary {
				n, err := strconv.Atoi(m[i+1])
				summary[i] = n
				issues = append(issues, err)
			}
			inList = false
			continue
		}
		if strings.HasSuffix(line, " not fully installed or removed.") {
			n, err := strconv.Atoi(strings.TrimSuffix(line, " not fully installed or removed."))
			partial = n
			if err != nil || n < 1 {
				issues = append(issues, errors.New("invalid partial-state summary"))
			}
			continue
		}
		if m := actionLine.FindStringSubmatch(line); m != nil {
			base, arch, _ := strings.Cut(m[2], ":")
			if arch != "" && m[6] != "" && arch != m[6] {
				issues = append(issues, fmt.Errorf("contradictory architecture: %s", line))
			}
			if m[6] != "" {
				arch = m[6]
			}
			op := Operation{Package: base, Architecture: arch, OldVersion: m[3], NewVersion: m[4], Uncertainty: []string{}}
			switch m[1] {
			case "Inst":
				if op.OldVersion != "" && op.OldVersion == op.NewVersion {
					issues = append(issues, errors.New("upgrade reports unchanged version"))
				}
				op.Kind = "install"
				if op.OldVersion != "" {
					op.Kind = "upgrade"
				}
				if op.NewVersion == "" {
					issues = append(issues, errors.New("install lacks new version"))
				}
			case "Conf":
				op.Kind = "configuration"
				if op.OldVersion != "" || op.NewVersion == "" {
					issues = append(issues, errors.New("invalid configuration record"))
				}
			case "Remv":
				op.Kind = "removal"
				if op.OldVersion == "" || op.NewVersion != "" {
					issues = append(issues, errors.New("invalid removal record"))
				}
			}
			if (op.OldVersion != "" && !versionToken.MatchString(op.OldVersion)) || (op.NewVersion != "" && !versionToken.MatchString(op.NewVersion)) {
				issues = append(issues, errors.New("invalid reported version"))
			}
			ops = append(ops, op)
			inList = false
			continue
		}
		if _, heading := listKinds[line]; heading {
			lists = append(lists, simulationList{heading: line, names: []string{}})
			inList = true
			continue
		}
		if inList && line == "This should NOT be done unless you know exactly what you are doing!" {
			continue
		}
		if inList && strings.HasPrefix(line, "  ") {
			list := &lists[len(lists)-1]
			for _, name := range strings.Fields(line) {
				if name == "|" && listKinds[list.heading] == "" {
					continue
				}
				if !packageOperand.MatchString(name) {
					issues = append(issues, fmt.Errorf("invalid package-list token %q", name))
					continue
				}
				key := list.heading + "/" + name
				if listNames[key] {
					issues = append(issues, fmt.Errorf("duplicate package-list entry %q", name))
				}
				listNames[key] = true
				list.names = append(list.names, name)
			}
			continue
		}
		inList = false
		progress := strings.TrimSuffix(line, " Done")
		known := progress == "Reading package lists..." || progress == "Building dependency tree..." || progress == "Reading state information..."
		if known || newestLine.MatchString(line) || strings.HasSuffix(line, " set to manually installed.") {
			continue
		}
		issues = append(issues, fmt.Errorf("unrecognized simulation line %q", line))
	}
	counts := map[string]int{}
	seen := map[string]Operation{}
	for _, op := range ops {
		counts[op.Kind]++
		key := op.Package + ":" + op.Architecture + "/" + op.Kind
		if _, ok := seen[key]; ok {
			issues = append(issues, fmt.Errorf("duplicate operation %s", key))
		}
		seen[key] = op
	}
	for _, list := range lists {
		full := list.heading == "The following NEW packages will be installed:" ||
			list.heading == "The following packages will be upgraded:" || list.heading == "The following packages will be REMOVED:"
		if full && len(list.names) != counts[listKinds[list.heading]] {
			issues = append(issues, fmt.Errorf("incomplete/duplicate list: %s", list.heading))
		}
		if len(list.names) == 0 {
			issues = append(issues, fmt.Errorf("empty/truncated list: %s", list.heading))
		}
		for _, name := range list.names {
			base, arch, _ := strings.Cut(name, ":")
			kind := listKinds[list.heading]
			matched := kind == "" || kind == "unchanged"
			for _, op := range ops {
				if op.Package != base || (arch != "" && arch != op.Architecture) {
					continue
				}
				if kind == "unchanged" {
					matched = false
					break
				}
				if kind == "changed" || kind == op.Kind {
					matched = true
				}
			}
			if !matched {
				issues = append(issues, fmt.Errorf("package list contradicts records: %s (%s)", name, list.heading))
			}
		}
	}
	for index, op := range ops {
		prefix := op.Package + ":" + op.Architecture + "/"
		inst, ok := seen[prefix+"install"]
		if !ok {
			inst, ok = seen[prefix+"upgrade"]
		}
		if op.Kind == "configuration" && ok {
			prior := false
			for _, earlier := range ops[:index] {
				if earlier.Package == op.Package && earlier.NewVersion == op.NewVersion && earlier.Kind != "configuration" {
					prior = true
				}
			}
			if !prior || inst.NewVersion != op.NewVersion {
				issues = append(issues, errors.New("configuration contradicts unpack version/order"))
			}
		}
		if op.Kind == "removal" && ok {
			issues = append(issues, errors.New("removal contradicts unpack"))
		}
		if op.Kind == "install" || op.Kind == "upgrade" {
			if _, ok := seen[prefix+"configuration"]; !ok {
				issues = append(issues, errors.New("unpack lacks completion/configuration evidence"))
			}
		}
	}
	if len(summary) == 0 {
		issues = append(issues, errors.New("missing transaction summary"))
	} else {
		valid := counts["upgrade"] == summary[0] && counts["install"] == summary[1] && counts["removal"] == summary[2]
		if !valid || counts["configuration"] != counts["install"]+counts["upgrade"]+partial {
			issues = append(issues, errors.New("transaction counts contradict operation records"))
		}
	}
	return ops, lists, errors.Join(issues...)
}
