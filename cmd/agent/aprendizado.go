package main

import (
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/aprendiz"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// Este arquivo é o aprendizado pelo painel do lado do binário do Agent
// (etapa 3, fatia F2; ADR-019, F8g): o dicionario.json, a trava, as
// conferências e o aplicador do aprendido que chega da nuvem moram em
// internal/agent/aprendiz (o Aplicador); aqui fica só o que depende do
// runtime do Windows: gravado o arquivo, o dicionário em memória e o prompt
// do ASR são trocados na hora (winruntime.Runtime.TrocarDicionario), com o
// Polish do Smart novo, para o próximo ditado já sair certo, sem reiniciar.
//
// A API local do painel (F3) chama só as interfaces dictionary.Aprendizado,
// localapi.Aprendizado e dictionary.Termos; o sync (F8g), a
// DicionarioDoAprendido (montarSync).

// trocadorDeDicionario é o pedaço do runtime que o aprendizado usa. Uma
// interface, e não o *winruntime.Runtime, para o teste não montar um
// runtime inteiro.
type trocadorDeDicionario interface {
	TrocarDicionario(winruntime.Dicionario)
}

// aprendizadoDoAgent é o aprendiz.Aplicador com a troca no runtime do Agent.
//
// # O log
//
// Só contagens. A variante e o termo são texto que a pessoa ditou: nunca
// vão a log (CLAUDE.md, "Safety rules").
type aprendizadoDoAgent struct {
	*aprendiz.Aplicador
	rt     trocadorDeDicionario
	polish *polishDoAgent
	// montar é smartconta.SmartDaSemente. Um campo, e não a chamada direta, para
	// o teste poder falhar a montagem de DEPOIS de gravar, que na prática não
	// acontece (Validar roda a mesma montagem antes de gravar).
	montar func(dictionary.Semente) (*resolver.SmartResolver, error)
	// semConferir: na subida, não deu para conferir se o dicionario.json é o
	// que o espelho do sync conhece (prepararOAprendido falhou). Nesta
	// execução, o laço o vê como o arquivo indisponível (dicionarioDoSync); o
	// painel continua igual.
	semConferir bool
}

var _ dictionary.Aprendizado = (*aprendizadoDoAgent)(nil)

// novoAprendizado monta o aprendizado sobre o dicionário em caminho, do
// usuário que o Agent carregou na subida. Usuário vazio é "não havia
// dicionário na subida": nada é gravado nem trocado (ErrSemArquivo), porque
// o resolver busca por esse usuário e um dicionário que apareceu depois não
// seria encontrado.
//
// Desde a ADR-018 o Agent cria um dicionário vazio na subida quando nada existe
// (garantirDicionario), e então o usuário não é vazio: o caso de cima só sobra
// quando a criação falhou (pasta sem permissão, disco cheio). É por isso que o
// aprendizado não cria o arquivo sozinho, na hora do primeiro clique.
func novoAprendizado(caminho, usuario string, rt trocadorDeDicionario, polish *polishDoAgent, log *slog.Logger) *aprendizadoDoAgent {
	if log == nil {
		log = slog.Default()
	}
	a := &aprendizadoDoAgent{rt: rt, polish: polish, montar: smartconta.SmartDaSemente}
	a.Aplicador = aprendiz.Novo(aprendiz.Config{
		Caminho: caminho,
		Usuario: usuario,
		Trocar:  a.trocar,
		Montar:  func(s dictionary.Semente) (*resolver.SmartResolver, error) { return a.montar(s) },
		// O "Adicionar termo": cada grafia nova age, e nada que agia deixa de
		// agir (termos.go).
		ConferirMudanca: conferirTermos,
		Log:             log,
	})
	return a
}

// trocar entrega ao runtime o dicionário novo: o Smart, o domínio do app, o
// Polish sobre o MESMO Smart e o prompt do ASR. Roda sob a trava do
// Aplicador.
func (a *aprendizadoDoAgent) trocar(t aprendiz.Troca) {
	vocab := t.Vocabulario
	a.rt.TrocarDicionario(winruntime.Dicionario{
		Resolver:     t.Smart,
		DominioDoApp: t.Semente.DominioDoApp,
		AbrirPolidor: a.polish.com(t.Smart).abrir,
		Vocabulario:  &vocab,
	})
}
