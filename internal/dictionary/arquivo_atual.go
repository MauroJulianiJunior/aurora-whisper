package dictionary

// Atual devolve o dicionário como está no arquivo agora, sem tocá-lo. É o
// que a API do painel passa ao motor de sugestão (learning.Sugerir), para
// ele saber o que já está no dicionário: o mesmo arquivo que Aprender relê
// antes de gravar, e não uma cópia da subida que o dictlearn pode ter
// superado.
//
// Como Aprendidas, espera uma gravação em curso (no Windows, renomear por
// cima de um arquivo que alguém está lendo falha). Os erros são os de ler:
// ErrSemArquivo, ErrArquivoInvalido e ErrUsuarioDiferente. O arquivo nunca
// é tocado.
//
// # Por que está num arquivo à parte, e usa os internos de Arquivo
//
// Atual é da F3 e Arquivo (arquivo.go) é da F2: este arquivo entrou sem editar
// o da F2, para as duas fatias se juntarem sem conflito. Ele usa a trava (a.mu)
// e a leitura (a.ler) de Arquivo, que são privados, o que é aceitável: é o
// mesmo pacote, e é exatamente o que Aprendidas faz (a mesma trava, a mesma
// leitura). O que não pode acontecer é um dos dois mudar sozinho: quem mexer em
// a.mu ou em a.ler confere Aprendidas e Atual juntas (arquivo_atual_test.go
// prova que Atual vê o arquivo de agora, sem tocá-lo, e que as duas esperam
// uma gravação em curso). Se um dia a F2 expuser um Atual/Ler público, este
// arquivo deixa de existir.
func (a *Arquivo) Atual() (Semente, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ler()
}
