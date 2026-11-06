package syncengine

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// A decisão do envio automático do dicionário (ADR-023, "F. O PC: a opção, o
// envio e o aviso"), pura: o que fazer com a resposta do PUT ... manter. O motor
// a aplica (motor_dicionario.go), e os testes a conferem em tabela, sem banco
// nem rede.

// Os tipos das decisões do dicionário, para o relatório da rodada (Relatorio.
// Dicionario): só contagens.
const (
	// TipoDicionarioEnviado: o PUT valeu (200): a nuvem tem a cópia de agora.
	TipoDicionarioEnviado = "enviado"
	// TipoDicionarioIgual: o SHA-256 é o último que a nuvem aceitou: nada sobe.
	TipoDicionarioIgual = "igual"
	// TipoDicionarioPerdeuAFonte: o 409; a opção foi desligada.
	TipoDicionarioPerdeuAFonte = "perdeu_a_fonte"
	// TipoDicionarioRecusado: o 422 (ou outra recusa da nuvem); a opção fica, e
	// o envio só tenta de novo quando a cópia mudar.
	TipoDicionarioRecusado = "recusado"
	// TipoDicionarioEsperando: a mesma cópia que a nuvem recusou: nada sobe.
	TipoDicionarioEsperando = "esperando"
	// TipoDicionarioSemRota: o 404 (a nuvem é de antes desta versão); não
	// tenta de novo até o Agent reiniciar.
	TipoDicionarioSemRota = "sem_rota"
	// TipoDicionarioAdiado: a rede caiu, o 429, o redirecionamento ou um 5xx: a
	// fase parou, e a rodada seguinte tenta de novo.
	TipoDicionarioAdiado = "adiado"
	// TipoDicionarioForaDoLimite: a cópia de agora não cabe nos limites (aqui,
	// antes da rede): o aviso diz o motivo.
	TipoDicionarioForaDoLimite = "fora_do_limite"
	// TipoDicionarioSemArquivo: o dicionario.json não deu para ler agora.
	TipoDicionarioSemArquivo = "sem_arquivo"
)

// DecisaoDoDicionario é o que o motor faz com a resposta do PUT ... manter.
type DecisaoDoDicionario struct {
	Tipo string
	// Aceitar: guarda o SHA-256 enviado como o último que a nuvem aceitou.
	Aceitar bool
	// Desligar: grava a opção desligada (e guarda o Aviso).
	Desligar bool
	// Aviso é a frase para o painel; vazio não muda o aviso.
	Aviso string
	// LimparAviso: a cópia chegou, e o aviso que havia (um 422 de antes) sai.
	LimparAviso bool
	// SoQuandoMudar: não tenta de novo esta mesma cópia; só quando o SHA-256
	// mudar (o 422).
	SoQuandoMudar bool
	// NaoTentarMais: não tenta mais até o Agent reiniciar (o 404).
	NaoTentarMais bool
	// Parar: a fase para sem nada guardado, e a rodada seguinte tenta de novo.
	Parar bool
}

// Os avisos do dicionário (a frase vai ao painel).
const (
	AvisoDicionarioApagadoDaNuvem = "A cópia do dicionário foi apagada da nuvem."
	AvisoDicionarioSemRota        = "A nuvem ainda não recebe o dicionário."
	// AvisoDicionarioRecusado vale para a recusa que não traz um motivo que o PC
	// conheça.
	AvisoDicionarioRecusado = "A nuvem não aceitou o dicionário."
	// AvisoDicionarioDeOutro vale quando outro PC é a fonte e a nuvem não disse o
	// nome dele.
	AvisoDicionarioDeOutro = "O celular passou a usar o dicionário de outro PC."
)

// DecidirEnvioDoDicionario decide o que fazer com a resposta do envio
// automático (PUT ... manter), a tabela da ADR-023, F:
//   - 200: a nuvem tem a cópia; guarda o SHA-256 (e o aviso de antes sai);
//   - 409: este PC não é mais a fonte; a opção é desligada, com o aviso "O
//     celular passou a usar o dicionário de NOME." (fonte "outro") ou "A cópia do
//     dicionário foi apagada da nuvem." (fonte "nenhuma");
//   - 422 (e o 400 e o 403, que o contrato não prevê e repetiriam o mesmo
//     pedido): o aviso com o motivo, a opção fica, e só tenta de novo quando a
//     cópia mudar;
//   - 404: a nuvem é de antes desta versão; o aviso, e nada mais até reiniciar;
//   - qualquer outro status (um 5xx): a fase para, sem aviso; a rodada seguinte
//     tenta de novo. O 401, o 429, o redirecionamento e a rede nem chegam aqui:
//     o cliente os devolve como erro.
func DecidirEnvioDoDicionario(r RespostaDoDicionario) DecisaoDoDicionario {
	switch r.Status {
	case http.StatusOK:
		return DecisaoDoDicionario{Tipo: TipoDicionarioEnviado, Aceitar: true, LimparAviso: true}
	case http.StatusConflict:
		return DecisaoDoDicionario{Tipo: TipoDicionarioPerdeuAFonte, Desligar: true, Aviso: AvisoDeQuemEAFonte(r.Estado)}
	case http.StatusUnprocessableEntity, http.StatusBadRequest, http.StatusForbidden:
		aviso := AvisoDicionarioRecusado
		if r.Status == http.StatusUnprocessableEntity {
			aviso = FraseDoMotivoDaCopia(r.Motivo)
		}
		return DecisaoDoDicionario{Tipo: TipoDicionarioRecusado, Aviso: aviso, SoQuandoMudar: true}
	case http.StatusNotFound:
		return DecisaoDoDicionario{Tipo: TipoDicionarioSemRota, Aviso: AvisoDicionarioSemRota, NaoTentarMais: true}
	}
	return DecisaoDoDicionario{Tipo: TipoDicionarioAdiado, Parar: true}
}

// AvisoDeQuemEAFonte é o aviso de quem perdeu a fonte (o 409, ou o GET que viu a
// fonte em outro PC), pelo estado que a nuvem mandou.
func AvisoDeQuemEAFonte(e *EstadoDoDicionario) string {
	if e == nil {
		return AvisoDicionarioDeOutro
	}
	switch e.Fonte {
	case FonteNenhuma:
		return AvisoDicionarioApagadoDaNuvem
	case FonteOutro:
		if nome := NomeDoAparelhoParaOAviso(e.Aparelho); nome != "" {
			return "O celular passou a usar o dicionário de " + nome + "."
		}
	}
	return AvisoDicionarioDeOutro
}

// NomeDoAparelhoParaOAviso limpa o nome do PC que a nuvem mandou antes de ele ir
// a uma frase do painel: sem caractere de controle, sem espaço nas pontas, até 80
// caracteres. Vazio se não sobra nada.
func NomeDoAparelhoParaOAviso(bruto string) string {
	var b strings.Builder
	for _, r := range bruto {
		if !unicode.IsControl(r) && r != utf8.RuneError {
			b.WriteRune(r)
		}
	}
	nome := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(nome) > 80 {
		nome = strings.TrimSpace(string([]rune(nome)[:80]))
	}
	return nome
}

// FraseDoMotivoDaCopia é a frase do painel para um motivo fechado da cópia
// (dictionary.MotivoDaCopia), o da recusa da nuvem (o 422) e o da conferência
// feita aqui antes de mandar. Nunca leva um termo. Um motivo que o PC não
// conhece vira a frase genérica.
func FraseDoMotivoDaCopia(motivo string) string {
	switch dictionary.MotivoDaCopia(motivo) {
	case dictionary.MotivoCopiaGrandeDemais:
		return fmt.Sprintf("O dicionário é grande demais para levar ao celular (o limite é %d KB).", dictionary.MaxBytesDaCopia>>10)
	case dictionary.MotivoCopiaTermosDemais:
		return fmt.Sprintf("O dicionário tem termos demais para levar ao celular (o limite é %d).", dictionary.MaxTermosDaCopia)
	case dictionary.MotivoCopiaVariantesDemais:
		return fmt.Sprintf("O dicionário tem variantes demais para levar ao celular (o limite é %d por termo e %d no total).",
			dictionary.MaxVariantesPorTermoDaCopia, dictionary.MaxVariantesDaCopia)
	case dictionary.MotivoCopiaTermoInvalido:
		return "Um termo ou uma variante do dicionário está vazio ou passa de 80 caracteres, e não dá para levar ao celular."
	}
	return "O dicionário não pôde ser levado ao celular."
}
