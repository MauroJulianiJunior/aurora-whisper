package winruntime

import (
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Dicionario é tudo o que o runtime usa por ditado e que vem do
// dicionario.json: o resolver (o Smart), o domínio do app em foco e a
// metade de rede do Polish, cujas guardas consultam o MESMO dicionário do
// Smart.
//
// Troca-se INTEIRO, e nunca um pedaço: um ditado que consulta o dicionário
// no Smart e de novo nas guardas do Polish tem de ver o mesmo dicionário
// nas duas.
//
// O vocabulário do ASR (o prompt) vem junto desde o "Adicionar termo"
// (ADR-020): a dica que a pessoa liga tem de valer no próximo ditado, sem
// reiniciar. Ele vai por pedido ao ASR (asr.ASROptions.Vocabulary), então nada
// no ASR precisa reiniciar para trocar.
type Dicionario struct {
	// Resolver é o Smart com o dicionário novo. Obrigatório: sem ele o pedido
	// é descartado, e o que valia continua valendo.
	Resolver resolver.Resolver
	// DominioDoApp traduz o executável em foco no domínio do dicionário novo
	// (o mapa "dominios" do arquivo). Nil, como em UsarDominioDoApp, é
	// "domínio desconhecido".
	DominioDoApp func(processName string) *string
	// AbrirPolidor monta o Polish do ditado com as guardas sobre o dicionário
	// novo. Nil desliga o Polish, como a falta de UsarPolish.
	AbrirPolidor AbrirPolidor
	// Vocabulario é o prompt do ASR do dicionário novo (o que UsarVocabulario
	// define na subida). Nil mantém o que valia; um ponteiro para "" esvazia o
	// prompt, como um dicionário sem termo nenhum.
	Vocabulario *string
}

// TrocarDicionario pede que d passe a valer a partir do PRÓXIMO ditado,
// sem reiniciar o Agent. Seguro de qualquer goroutine: só guarda o pedido, e
// um pedido novo substitui o que ainda não foi aplicado.
//
// # Por que no próximo keydown, e só com a máquina parada
//
// É o mesmo desenho de Ajustar, pelo mesmo motivo: o resolver e o que
// depende dele são da goroutine de Run, e um ditado que já começou termina
// com o dicionário com que começou, sem meio-termo. Trocá-lo no meio faria o
// Smart ver um dicionário e as guardas do Polish, daí a pouco, outro.
//
// Publicar o pedido é um ponteiro atômico (nada é lido pela metade), e quem
// o consome é a goroutine de Run, no keydown, antes de despachá-lo.
func (rt *Runtime) TrocarDicionario(d Dicionario) { rt.dicionario.Store(&d) }

// aplicarDicionario aplica o pedido de TrocarDicionario, se houver e se a
// máquina estiver parada. Só na goroutine de Run.
func (rt *Runtime) aplicarDicionario() {
	if rt.machine.CurrentAttempt() != dictation.NoAttempt {
		return // no meio de um ditado: o pedido espera o próximo
	}
	d := rt.dicionario.Swap(nil)
	if d == nil {
		return
	}
	if d.Resolver == nil {
		// Defeito de quem chamou: sem resolver o ditado perderia a correção
		// de termos. Segue o que valia.
		rt.log.Warn("winruntime: troca de dicionário sem resolver; segue o dicionário anterior")
		return
	}
	rt.resolucao, rt.dominioDoApp, rt.abrirPolidor = d.Resolver, d.DominioDoApp, d.AbrirPolidor
	if d.Vocabulario != nil {
		// O ditado que já está no ASR segue com o prompt com que foi: transcrever
		// copia o prompt antes de soltar a goroutine.
		rt.vocabulario = *d.Vocabulario
	}
	// Sem texto: só que trocou, e o tamanho do prompt.
	rt.log.Info("winruntime: dicionário trocado; vale a partir deste ditado",
		"vocabulario_trocado", d.Vocabulario != nil,
		"vocabulario_runas", utf8.RuneCountInString(rt.vocabulario))
}
