package clock

import "time"

// System é o Clock real, usado em produção. Delega diretamente para a
// stdlib.
type System struct{}

// NewSystem cria um Clock apoiado no relógio de parede real.
func NewSystem() System {
	return System{}
}

// Now retorna time.Now().
func (System) Now() time.Time {
	return time.Now()
}

// AfterFunc delega para time.AfterFunc. *time.Timer já satisfaz Timer
// (seu método Stop retorna bool), então nenhum adaptador é necessário.
func (System) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}
