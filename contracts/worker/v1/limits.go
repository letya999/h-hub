package v1

import (
	"fmt"
	"path"
	"regexp"
)

// Limits is the spawn-controller concurrency policy declared in the descriptor.
type Limits struct {
	MaxPerUser int `yaml:"max_per_user"`
	MaxTotal   int `yaml:"max_total"`
}

func (l Limits) Validate() error {
	if l.MaxPerUser <= 0 || l.MaxTotal < l.MaxPerUser {
		return fmt.Errorf("worker concurrency limits must satisfy 0 < max_per_user <= max_total")
	}
	return nil
}

type Policy struct {
	Limits   `yaml:",inline"`
	Profiles map[string]Profile `yaml:"profiles"`
}

type Profile struct {
	Image      string `yaml:"image"`
	Entrypoint string `yaml:"entrypoint"`
	Memory     int64  `yaml:"memory_bytes"`
	CPU        int64  `yaml:"nano_cpus"`
	PIDs       int64  `yaml:"pids"`
}

var profileName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var imageRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_:@-]*$`)

func (p Policy) Empty() bool { return p.Limits == (Limits{}) && len(p.Profiles) == 0 }

func (p Policy) Validate() error {
	if err := p.Limits.Validate(); err != nil {
		return err
	}
	if _, ok := p.Profiles["terminal"]; !ok {
		return fmt.Errorf("the terminal worker profile is required")
	}
	for name, profile := range p.Profiles {
		if !profileName.MatchString(name) || !profile.valid() {
			return fmt.Errorf("invalid worker profile %q", name)
		}
	}
	return nil
}

func (p Profile) valid() bool {
	return imageRef.MatchString(p.Image) && path.IsAbs(p.Entrypoint) && path.Clean(p.Entrypoint) == p.Entrypoint &&
		p.Memory > 0 && p.CPU > 0 && p.PIDs > 0
}
