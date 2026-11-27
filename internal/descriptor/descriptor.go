// Package descriptor is the single source of truth for the service
// inventory: deploy/descriptor.yaml is validated here and rendered into
// deploy/compose.yaml (rules 3, 7-11; enforcement.md section 3).
package descriptor

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	"gopkg.in/yaml.v3"
)

type File struct {
	Version  int       `yaml:"version"`
	Postgres Postgres  `yaml:"postgres"`
	Volumes  []Volume  `yaml:"volumes"`
	Services []Service `yaml:"services"`
	Clients  []Client  `yaml:"clients"`
}

type Postgres struct {
	Image  string  `yaml:"image"`
	Mem    string  `yaml:"mem"`
	CPU    float64 `yaml:"cpu"`
	Pids   int     `yaml:"pids"`
	Volume string  `yaml:"volume"`
}

type Volume struct {
	Name string `yaml:"name"`
}

type Service struct {
	Name     string     `yaml:"name"`
	Zone     string     `yaml:"zone"`
	Image    string     `yaml:"image"`
	Deploy   bool       `yaml:"deploy"`
	Database string     `yaml:"database"`
	Mem      string     `yaml:"mem"`
	CPU      float64    `yaml:"cpu"`
	Pids     int        `yaml:"pids"`
	Env      []string   `yaml:"env"`
	Volumes  []Mount    `yaml:"volumes"`
	Edges    []Edge     `yaml:"edges"`
	Workers  wv1.Policy `yaml:"workers,omitempty"`
}

// Client is a non-container principal (hubctl, tests) allowed to hold
// service edges. It runs no image and owns no database.
type Client struct {
	Name  string `yaml:"name"`
	Edges []Edge `yaml:"edges"`
}

type Mount struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	RW   bool   `yaml:"rw"`
}

type Edge struct {
	To  string   `yaml:"to"`
	Ops []string `yaml:"ops"`
}

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	envRe    = regexp.MustCompile(`^HHUB_[A-Z0-9_]+$`)
	memRe    = regexp.MustCompile(`^[0-9]+[bkmg]$`)
	imageRe  = regexp.MustCompile(`^[a-zA-Z0-9./_:-]+@sha256:[0-9a-f]{64}$`)
	opRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	spaceEnv = regexp.MustCompile(`DSN|DB|POSTGRES|PG`)
	zones    = map[string]bool{
		"control": true, "agent": true, "edge": true,
		"egress": true, "secret": true, "space": true,
	}
	dbs = map[string]bool{"postgres": true, "sqlcipher": true, "none": true}
)

const maxEnv = 10

// Load reads and validates a descriptor file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("descriptor: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate enforces the descriptor schema: resource ceilings are
// mandatory and env lists are capped (broken-example: a service without
// ceilings or with 11 env vars is rejected).
func (f *File) Validate() error {
	if f.Version != 1 {
		return fmt.Errorf("descriptor: version must be 1, got %d", f.Version)
	}
	if !imageRe.MatchString(f.Postgres.Image) {
		return fmt.Errorf("descriptor: postgres image must be digest-pinned")
	}
	if err := ceiling("postgres", f.Postgres.Mem, f.Postgres.CPU, f.Postgres.Pids); err != nil {
		return err
	}
	vol, err := f.checkVolumes()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range f.Services {
		s := &f.Services[i]
		if err := s.validate(seen, vol); err != nil {
			return err
		}
		if err := s.checkWorkers(); err != nil {
			return err
		}
	}
	return f.checkClients()
}

func (s Service) checkWorkers() error {
	if s.Name == "spawn-controller" {
		return s.Workers.Validate() //nolint:wrapcheck // descriptor validation passes through the precise field error
	}
	if !s.Workers.Empty() {
		return fmt.Errorf("descriptor: %s cannot own worker limits", s.Name)
	}
	return nil
}

func (f *File) checkVolumes() (map[string]bool, error) {
	vol := map[string]bool{}
	for _, v := range f.Volumes {
		if !nameRe.MatchString(v.Name) || vol[v.Name] {
			return nil, fmt.Errorf("descriptor: bad or duplicate volume %q", v.Name)
		}
		vol[v.Name] = true
	}
	if !vol[f.Postgres.Volume] {
		return nil, fmt.Errorf("descriptor: postgres volume %q is not declared", f.Postgres.Volume)
	}
	return vol, nil
}

func (s *Service) validate(seen map[string]bool, vol map[string]bool) error {
	if !nameRe.MatchString(s.Name) || seen[s.Name] {
		return fmt.Errorf("descriptor: bad or duplicate service %q", s.Name)
	}
	seen[s.Name] = true
	if !zones[s.Zone] {
		return fmt.Errorf("descriptor: %s: unknown zone %q", s.Name, s.Zone)
	}
	if !dbs[s.Database] {
		return fmt.Errorf("descriptor: %s: database must be postgres|sqlcipher|none", s.Name)
	}
	if s.Zone == "space" && s.Database != "none" {
		return fmt.Errorf("descriptor: %s: space zone never holds a database (sys.space-no-db)", s.Name)
	}
	if s.Image != "" && !imageRe.MatchString(s.Image) {
		return fmt.Errorf("descriptor: %s: image must be digest-pinned", s.Name)
	}
	if err := ceiling(s.Name, s.Mem, s.CPU, s.Pids); err != nil {
		return err
	}
	if err := s.checkEnv(); err != nil {
		return err
	}
	if err := s.checkMounts(vol); err != nil {
		return err
	}
	return s.checkEdges()
}

func (s *Service) checkEnv() error {
	if len(s.Env) > maxEnv {
		return fmt.Errorf("descriptor: %s: env has %d entries, max %d", s.Name, len(s.Env), maxEnv)
	}
	for _, e := range s.Env {
		if !envRe.MatchString(e) {
			return fmt.Errorf("descriptor: %s: env %q must match HHUB_*", s.Name, e)
		}
		if s.Zone == "space" && spaceEnv.MatchString(e) {
			return fmt.Errorf("descriptor: %s: space zone may not carry database env %q", s.Name, e)
		}
	}
	return nil
}

func (s *Service) checkMounts(vol map[string]bool) error {
	for _, m := range s.Volumes {
		if !vol[m.Name] || !strings.HasPrefix(m.Path, "/") {
			return fmt.Errorf("descriptor: %s: bad volume mount %q", s.Name, m.Name)
		}
	}
	return nil
}

func (s *Service) checkEdges() error {
	for _, e := range s.Edges {
		if e.To == s.Name || len(e.Ops) == 0 {
			return fmt.Errorf("descriptor: %s: bad edge to %q", s.Name, e.To)
		}
		for _, op := range e.Ops {
			if !opRe.MatchString(op) {
				return fmt.Errorf("descriptor: %s: bad op %q", s.Name, op)
			}
		}
	}
	return nil
}

// checkClients validates non-container principals: unique names that
// do not collide with services, with well-formed edges.
func (f *File) checkClients() error {
	svcNames := map[string]bool{}
	for _, s := range f.Services {
		svcNames[s.Name] = true
	}
	seen := map[string]bool{}
	for _, c := range f.Clients {
		if !nameRe.MatchString(c.Name) || seen[c.Name] || svcNames[c.Name] {
			return fmt.Errorf("descriptor: bad or duplicate client %q", c.Name)
		}
		seen[c.Name] = true
		if err := c.checkEdges(); err != nil {
			return err
		}
	}
	return nil
}

func (c Client) checkEdges() error {
	for _, e := range c.Edges {
		if len(e.Ops) == 0 {
			return fmt.Errorf("descriptor: %s: bad edge to %q", c.Name, e.To)
		}
		for _, op := range e.Ops {
			if !opRe.MatchString(op) {
				return fmt.Errorf("descriptor: %s: bad op %q", c.Name, op)
			}
		}
	}
	return nil
}

// EdgeTargets validates that every edge points at a declared service.
func (f *File) EdgeTargets() error {
	names := make(map[string]bool, len(f.Services))
	for _, s := range f.Services {
		names[s.Name] = true
	}
	for _, s := range f.Services {
		for _, e := range s.Edges {
			if !names[e.To] {
				return fmt.Errorf("descriptor: %s: edge to unknown service %q", s.Name, e.To)
			}
		}
	}
	for _, c := range f.Clients {
		for _, e := range c.Edges {
			if !names[e.To] {
				return fmt.Errorf("descriptor: %s: edge to unknown service %q", c.Name, e.To)
			}
		}
	}
	return nil
}

// EdgePair is one declared (caller, callee, op) triple — the raw data
// the edges table is built from.
type EdgePair struct {
	To   string
	From string
	Op   string
}

// EdgePairs flattens every service and client edge into caller,
// callee, op triples.
func (f *File) EdgePairs() []EdgePair {
	var out []EdgePair
	add := func(from string, edges []Edge) {
		for _, e := range edges {
			for _, op := range e.Ops {
				out = append(out, EdgePair{To: e.To, From: from, Op: op})
			}
		}
	}
	for _, s := range f.Services {
		add(s.Name, s.Edges)
	}
	for _, c := range f.Clients {
		add(c.Name, c.Edges)
	}
	return out
}

func ceiling(name, mem string, cpu float64, pids int) error {
	if !memRe.MatchString(mem) {
		return fmt.Errorf("descriptor: %s: mem ceiling required (e.g. 256m)", name)
	}
	if cpu <= 0 || cpu > 8 {
		return fmt.Errorf("descriptor: %s: cpu ceiling out of range", name)
	}
	if pids < 16 || pids > 4096 {
		return fmt.Errorf("descriptor: %s: pids ceiling out of range", name)
	}
	return nil
}

// DBServices returns services that own a Postgres database and role.
func (f *File) DBServices() []Service {
	var out []Service
	for _, s := range f.Services {
		if s.Database == "postgres" {
			out = append(out, s)
		}
	}
	return out
}

// AllEnv returns the declared HHUB_* variables per service name.
func (f *File) AllEnv() map[string][]string {
	m := make(map[string][]string, len(f.Services))
	for _, s := range f.Services {
		m[s.Name] = slices.Clone(s.Env)
	}
	return m
}
