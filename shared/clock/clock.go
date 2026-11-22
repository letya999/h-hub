// Package clock is the single time source. Services take a Clock so
// tests can control deadlines and sweeps deterministically.
package clock

import "time"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

type system struct{}

func (system) Now() time.Time { return time.Now().UTC() }

// System returns the real clock in UTC.
func System() Clock { return system{} }

// Fake is a manually advanced clock for tests.
type Fake struct {
	t time.Time
}

// NewFake starts a fake clock at t.
func NewFake(t time.Time) *Fake { return &Fake{t: t.UTC()} }

func (f *Fake) Now() time.Time { return f.t }

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.t = f.t.Add(d) }
