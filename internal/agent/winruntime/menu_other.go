//go:build !windows

package winruntime

// menuEmPrimeiroPlano: fora do Windows não há menu do Win32.
func menuEmPrimeiroPlano() bool { return false }
