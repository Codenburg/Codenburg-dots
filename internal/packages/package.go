package packages

type Status string

const (
	Current         Status = "current"
	UpdateAvailable Status = "update-available"
	Available       Status = "available"
	Unavailable     Status = "unavailable"
	Unknown         Status = "unknown"
)

type Info struct {
	Name                               string
	Status                             Status
	InstalledVersion, CandidateVersion string
}

type Inspector interface {
	Inspect(name string) (Info, error)
}

// DeriveState uses a Debian version comparison performed by the provider; versions are never compared lexically.
func DeriveState(installed, candidate string, found, installedAtLeastCandidate bool) Status {
	if !found || candidate == "" || candidate == "(none)" {
		if installed != "" {
			return Unknown
		}
		return Unavailable
	}
	if installed == "" {
		return Available
	}
	if installedAtLeastCandidate {
		return Current
	}
	return UpdateAvailable
}
