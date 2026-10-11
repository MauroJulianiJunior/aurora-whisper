package main

import "testing"

func TestRepsFor_DefaultsPerMode(t *testing.T) {
	cases := []struct {
		mode string
		want int
	}{
		{"cold", defaultColdReps},
		{"warm", defaultWarmReps},
		{"control", defaultControlReps},
	}
	for _, c := range cases {
		if got := repsFor(c.mode, 0); got != c.want {
			t.Errorf("repsFor(%q, 0) = %d, want %d", c.mode, got, c.want)
		}
	}
}

func TestRepsFor_OverrideWins(t *testing.T) {
	if got := repsFor("cold", 7); got != 7 {
		t.Errorf("repsFor(\"cold\", 7) = %d, want 7", got)
	}
	if got := repsFor("warm", 1000); got != 1000 {
		t.Errorf("repsFor(\"warm\", 1000) = %d, want 1000", got)
	}
}

func TestRepsFor_NonPositiveOverrideIgnored(t *testing.T) {
	if got := repsFor("warm", 0); got != defaultWarmReps {
		t.Errorf("repsFor(\"warm\", 0) = %d, want default %d", got, defaultWarmReps)
	}
	if got := repsFor("warm", -1); got != defaultWarmReps {
		t.Errorf("repsFor(\"warm\", -1) = %d, want default %d", got, defaultWarmReps)
	}
}

func TestRepsFor_UnknownModeFallsBackToWarmDefault(t *testing.T) {
	if got := repsFor("algo-desconhecido", 0); got != defaultWarmReps {
		t.Errorf("repsFor(unknown, 0) = %d, want default warm %d", got, defaultWarmReps)
	}
}
