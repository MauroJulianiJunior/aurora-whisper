package main

import (
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// usuarioDoDicionarioNovo é o "usuario" do dicionário vazio que o Agent cria
// (garantirDicionario). É "local", o MESMO valor que donoPelaRegraAntiga (e o
// dictlearn, donoSemDicionario) usa para "sem dicionário".
//
// Por que não o dono do painel, como a ADR-018 sugeria na pendência. O dono
// fixo nasce DENTRO do historico.db (escolherDono), que só abre depois do
// dicionário, e a decisão de 29-set-2026 desligou os dois: "o dicionário volta
// a ser só dicionário". O que a busca exige é que o usuario do arquivo seja o
// que o runtime recebe (UsarResolver) e o que o aprendizado confere
// (dictionary.Arquivo.Usuario, ErrUsuarioDiferente), e os dois saem do próprio
// arquivo, na subida. Com "local" a regra antiga também continua coerente: se a
// migração do dono fixo falhar, o dono é o usuario do dicionário, "local", o
// mesmo que seria sem dicionário nenhum.
//
// Quem já tem dicionário (o do Mauro é "mauro") não é afetado: o arquivo dele
// existe e não é tocado.
const usuarioDoDicionarioNovo = "local"

// garantirDicionario cria na pasta de dados um dicionario.json VAZIO, mas só se
// não houver nada ali, para o aprender do painel funcionar desde o primeiro dia
// num Agent instalado (ADR-018, "O dicionário que não existe"; decisão do
// Mauro, 30-set-2026, opção (a)).
//
// # Quando: na subida, antes de abrir o dicionário
//
// Não no primeiro "Aprender". O aprendizado sobre um arquivo que aparece depois
// da subida não funcionaria: o runtime foi montado com o usuario vazio e o
// Smart nulo, e trocar o dicionário em memória (TrocarDicionario) não troca o
// usuario, de modo que a variante aprendida não agiria até reiniciar; e a rota
// que sugere ao salvar uma edição (Dicionario) também teria de criar o arquivo
// numa leitura. Criado na subida, o resto do Agent vê um dicionário comum: a
// mesma carga, o mesmo usuario no runtime e no aprendizado, nenhum caminho novo.
//
// # Nunca sobrescreve
//
// Existindo qualquer coisa no caminho, nada é feito: o dicionário de quem usa,
// inclusive o do modo portátil (o bin\ do desenvolvimento), e até um arquivo
// inválido, que abrirDicionarioEm recusa e diz. Ver dictionary.CriarSeFaltar.
//
// # O que muda e o que não muda
//
// Para quem já tem dicionário, nada. Para quem não tem, o Smart passa a rodar
// com um dicionário sem termos em vez de nenhum: o texto sai igual (um teste
// confere), e a linha "sem DictionaryRepository" deixa de aparecer a cada
// ditado.
//
// Não impede a subida: se o arquivo não pôde ser criado (pasta só de leitura,
// disco cheio), o Agent segue como seguia sem dicionário, e o aviso diz que o
// aprender do painel fica indisponível. Só o nome do arquivo vai ao log.
//
// # O que volta: criou
//
// Verdadeiro quando o arquivo não existia e foi criado agora. Para o sync do
// aprendido (ADR-019, F8), é outro arquivo: o espelho do historico.db pode
// descrever um dicionario.json que sumiu entre duas execuções, e o vazio
// criado no lugar não pode valer como "a pessoa desfez tudo"
// (prepararOAprendido).
func garantirDicionario(log *slog.Logger, caminho string) (criou bool) {
	criou, err := dictionary.CriarSeFaltar(caminho, usuarioDoDicionarioNovo)
	switch {
	case err != nil:
		log.Warn("dicionario: não consegui criar o dicionário vazio; o aprender do painel fica indisponível nesta execução",
			"arquivo", nomeDicionario, "err", err)
		return false
	case criou:
		log.Info("dicionario: criei um dicionário vazio na pasta de dados, para o aprender do painel funcionar desde o primeiro dia",
			"arquivo", nomeDicionario, "usuario", usuarioDoDicionarioNovo)
	}
	return criou
}
