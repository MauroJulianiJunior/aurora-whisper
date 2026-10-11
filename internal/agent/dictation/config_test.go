package dictation

import (
	"errors"
	"testing"
	"time"
)

func TestConfigValidate_PureModifierKeyRejected(t *testing.T) {
	for _, key := range []KeyCode{ShiftLeft, ShiftRight, ControlLeft, ControlRight, AltLeft, AltRight, MetaLeft, MetaRight} {
		cfg := Config{Key: key, HoldThreshold: 700 * time.Millisecond}
		err := cfg.Validate()
		if !errors.Is(err, ErrPureModifierKey) {
			t.Errorf("key=%d: Validate() = %v, want errors.Is(_, ErrPureModifierKey)", key, err)
		}
	}
}

func TestConfigValidate_ThresholdOutOfRange(t *testing.T) {
	cases := []time.Duration{0, 299 * time.Millisecond, 2001 * time.Millisecond, -time.Second}
	for _, d := range cases {
		cfg := Config{Key: KeyF, HoldThreshold: d}
		err := cfg.Validate()
		if !errors.Is(err, ErrThresholdOutOfRange) {
			t.Errorf("threshold=%s: Validate() = %v, want errors.Is(_, ErrThresholdOutOfRange)", d, err)
		}
	}
}

func TestConfigValidate_ThresholdBoundaryInclusive(t *testing.T) {
	for _, d := range []time.Duration{MinHoldThreshold, MaxHoldThreshold} {
		cfg := Config{Key: KeyF, HoldThreshold: d}
		if err := cfg.Validate(); err != nil {
			t.Errorf("threshold=%s: Validate() = %v, want nil", d, err)
		}
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	cfg := Config{Key: KeyF, HoldThreshold: 700 * time.Millisecond, BeepEnabled: true}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
