//go:build !windows

package chaves

// gerenciador fora do Windows: não há Gerenciador de Credenciais, e toda
// chamada diz isso — em vez de fingir que guardou.
type gerenciador struct{}

// NovoGerenciador devolve um Cofre que recusa tudo com ErrNaoSuportado.
func NovoGerenciador() Cofre { return gerenciador{} }

func (gerenciador) Gravar(string, string) error { return ErrNaoSuportado }
func (gerenciador) Ler(string) (string, error)  { return "", ErrNaoSuportado }
func (gerenciador) Apagar(string) error         { return ErrNaoSuportado }
