package v1

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ReadPolicy reads the spawn-controller worker policy packaged from the descriptor.
func ReadPolicy() (Policy, error) {
	var file struct {
		Services []struct {
			Name    string `yaml:"name"`
			Workers Policy `yaml:"workers"`
		} `yaml:"services"`
	}
	data, err := os.ReadFile("/usr/share/hhub/descriptor.yaml")
	if err != nil {
		return Policy{}, fmt.Errorf("worker descriptor: %w", err)
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Policy{}, fmt.Errorf("worker descriptor: %w", err)
	}
	for _, svc := range file.Services {
		if svc.Name == "spawn-controller" {
			if err := svc.Workers.Validate(); err != nil {
				return Policy{}, fmt.Errorf("worker descriptor: %w", err)
			}
			return svc.Workers, nil
		}
	}
	return Policy{}, fmt.Errorf("worker descriptor has no spawn-controller configuration")
}
