package aprendiz

import (
	"context"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// SmartDaSemente monta o Smart sobre um repositório em memória novo,
// populado com a semente. É o mesmo caminho da subida (abrirDicionarioEm, no
// Agent) e o da troca na hora: o dicionário que vale depois de aprender é
// montado exatamente como o que vale depois de reiniciar.
func SmartDaSemente(s dictionary.Semente) (*resolver.SmartResolver, error) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		return nil, err
	}
	return resolver.NewSmartResolver(repo), nil
}

// Validar é o Arquivo.Validar do Agent: o que não carrega no repositório em
// memória não chega ao disco.
func Validar(s dictionary.Semente) error {
	_, err := SmartDaSemente(s)
	return err
}

// Agiria é o Arquivo.ValidarAprendida do Agent: prova que a variante que o
// Aprender do painel acabou de acrescentar AGE, isto é, que o Smart da semente
// nova, diante da variante sozinha, escreve o termo. Sem isso, o painel
// responderia "aprendido" para variantes que nunca fazem nada: outro termo de
// prioridade maior leva a melhor na busca (a prioridade vem antes do
// casamento), ou uma palavra da variante já é termo e veta a frase ("pedro
// das", com o termo DAS). Devolve dictionary.ErrNaoAgiria nesses casos.
//
// É o mesmo Smart, montado pelo mesmo caminho, que vale depois de gravar; a
// variante sozinha é o caso mais favorável a ela (sem vizinhas que disputem as
// palavras), então o que não age aqui também não age num ditado.
//
// O Aprender põe a variante em alias: a restrição dela é a da entrada, e os
// domínios da conferência são os de DominiosDaConferencia sem restrição a
// mais.
//
// Não loga nada: a variante é texto ditado.
func Agiria(s dictionary.Semente, feito dictionary.Aprendida) error {
	return agiriaEm(s, feito, false)
}

// ValidarPar é o Arquivo.ValidarPar do Agent: o par que chegou da nuvem, como
// o lote do PC que recebe o deixaria no arquivo (dictionary.ParDoPainel, já
// com a adaptação deste PC), tem de AGIR aqui, em cada domínio onde ele age
// neste PC. A restrição é a efetiva do par (a entrada restrita, ou a variante
// em alias_restrito): o alias restrito confere só no domínio da entrada, onde
// o Search o mostra; conferido também no domínio desconhecido, daria sempre
// "não agiria". ErrNaoAgiria deixa o par "não aplicado".
func ValidarPar(s dictionary.Semente, p dictionary.ParDoPainel) error {
	return agiriaEm(s, dictionary.Aprendida{Termo: p.Termo, Variante: p.Variante}, p.Restrito)
}

func agiriaEm(s dictionary.Semente, feito dictionary.Aprendida, restrito bool) error {
	smart, err := SmartDaSemente(s)
	if err != nil {
		return err
	}
	age, err := AgeEm(smart, s, feito, restrito)
	if err != nil {
		return err
	}
	if !age {
		return dictionary.ErrNaoAgiria
	}
	return nil
}

// AgeEm diz se a variante de feito age no Smart do dicionário s, em todo
// domínio onde ela vive (DominiosDaConferencia, com a restrição dada). smart é
// o de s, montado por quem chama, para quem confere muitas variantes montá-lo
// uma vez só.
func AgeEm(smart *resolver.SmartResolver, s dictionary.Semente, feito dictionary.Aprendida, restrito bool) (bool, error) {
	for _, dominio := range DominiosDaConferencia(s, feito.Termo, restrito) {
		age, err := AgeNoDominio(smart, s.Usuario, feito, dominio)
		if err != nil || !age {
			return false, err
		}
	}
	return true, nil
}

// AgeNoDominio diz se o Smart, diante da variante sozinha, escreve o termo,
// com o app em foco no domínio dado (nil: o desconhecido).
func AgeNoDominio(smart *resolver.SmartResolver, usuario string, feito dictionary.Aprendida, dominio *string) (bool, error) {
	out, err := smart.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:      usuario,
		ASRResult:   asr.ASRResult{Text: feito.Variante},
		CloudPolicy: resolver.CloudDisabled,
		Domain:      dominio,
	})
	if err != nil {
		return false, err
	}
	return IgualNoDicionario(out.FinalText, feito.Termo), nil
}

// DominiosDaConferencia são os domínios em que o "agiria" procura uma
// variante de termo, onde nil é o domínio desconhecido. restrito é a
// restrição da variante além da entrada (a variante em alias_restrito; o par
// recebido traz a efetiva, que já inclui a da entrada):
//
//   - termo com domínio, restrito (a entrada, ou a variante): só o dele;
//   - termo com domínio, sem restrição: o desconhecido e o dele;
//   - termo sem domínio, termo novo (que ainda nem tem entrada) e restrito
//     sem domínio: só o desconhecido.
//
// A variante age onde o Search a mostra: a entrada com domínio que a pessoa
// não restringiu aparece nos dois, e cada um tem os seus concorrentes (no
// desconhecido, o termo não restrito de qualquer domínio; no domínio do
// termo, também o restrito a ele, que só aparece ali). A entrada restrita, e
// a variante em alias_restrito, só aparecem no domínio da entrada.
//
// O termo sem domínio também aparece em todo domínio, e não é conferido em
// cada um de propósito: ali o termo restrito a um domínio leva a busca por
// desenho, e recusar por isso recusaria quase toda variante.
//
// Restrito sem domínio não tem onde agir: o Search só mostra a entrada (ou o
// alias) restrita quando o domínio pedido bate com o dela. A conferência no
// desconhecido não a acha, e recusa a variante (ErrNaoAgiria), que é o que
// aconteceria num ditado.
func DominiosDaConferencia(s dictionary.Semente, termo string, restrito bool) []*string {
	for _, e := range s.Entradas {
		if !IgualNoDicionario(e.Canonica, termo) {
			continue
		}
		if e.Dominio == "" {
			break
		}
		dominio := e.Dominio
		if e.Restrito || restrito {
			return []*string{&dominio}
		}
		return []*string{nil, &dominio}
	}
	return []*string{nil}
}

// IgualNoDicionario compara como o Search compara: sem caixa, acento nem
// pontuação, e sem espaços.
func IgualNoDicionario(a, b string) bool {
	return dictionary.CollapseSpaces(dictionary.Normalize(a)) == dictionary.CollapseSpaces(dictionary.Normalize(b))
}

// PalavraComum é o Arquivo.PalavraComum do Agent: para o termo, a guarda do
// motor de sugestão (learning.Arriscada); para a grafia errada (e a variante
// que chega da nuvem), a mais estrita (learning.GrafiaArriscada), que recusa a
// polaridade em qualquer palavra e a grafia só de palavras funcionais. O
// motivo é o código do learning.
func PalavraComum(texto string, grafia bool) string {
	if grafia {
		return string(learning.GrafiaArriscada(texto))
	}
	return string(learning.Arriscada(texto))
}
