package accesstypes

import (
	"testing"
	"time"
)

func TestEnvironment_Now(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 28, 15, 4, 5, 0, time.UTC)
	eastern := time.FixedZone("UTC-5", -5*60*60)

	tests := []struct {
		name    string
		env     Environment
		wantNow time.Time
		wantOK  bool
	}{
		{
			name: "zero environment carries nothing",
			env:  Environment{},
		},
		{
			name: "new environment carries nothing",
			env:  NewEnvironment(),
		},
		{
			name:    "WithNow records the instant",
			env:     NewEnvironment().WithNow(fixed),
			wantNow: fixed,
			wantOK:  true,
		},
		{
			name:    "WithNow normalizes to UTC",
			env:     NewEnvironment().WithNow(fixed.In(eastern)),
			wantNow: fixed,
			wantOK:  true,
		},
		{
			name:    "EnvironmentAt pins the instant",
			env:     EnvironmentAt(fixed),
			wantNow: fixed,
			wantOK:  true,
		},
		{
			name:    "a set zero instant is present",
			env:     NewEnvironment().WithNow(time.Time{}),
			wantNow: time.Time{},
			wantOK:  true,
		},
		{
			name:    "the last WithNow in a chain wins",
			env:     NewEnvironment().WithNow(fixed.Add(-time.Hour)).WithNow(fixed),
			wantNow: fixed,
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now, ok := tt.env.Now()
			if ok != tt.wantOK {
				t.Fatalf("Now() ok = %v, want %v", ok, tt.wantOK)
			}
			if !now.Equal(tt.wantNow) {
				t.Errorf("Now() = %v, want %v", now, tt.wantNow)
			}
			if ok && now.Location() != time.UTC {
				t.Errorf("Now() location = %v, want UTC", now.Location())
			}
		})
	}
}

// TestEnvironment_chainingCopies pins the value semantics: With* returns a
// copy, so a derived Environment never mutates the one it chained from.
func TestEnvironment_chainingCopies(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 8, 28, 15, 4, 5, 0, time.UTC)

	base := NewEnvironment()
	derived := base.WithNow(fixed)

	if _, ok := base.Now(); ok {
		t.Error("base.Now() ok = true after deriving, want the base unchanged")
	}
	if now, ok := derived.Now(); !ok || !now.Equal(fixed) {
		t.Errorf("derived.Now() = (%v, %v), want (%v, true)", now, ok, fixed)
	}
}
