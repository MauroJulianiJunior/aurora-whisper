package winruntime

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
)

// O erro do ditado com o microfone bloqueado pelo Windows mostra a
// mensagem do microfone, e não "Não consegui transcrever". Os testes
// entregam o efeito do núcleo direto a apply, com a pílula falsa.

func mostrarErro(rt *Runtime) {
	rt.apply([]dictation.Effect{dictation.ShowOverlayEffect{Attempt: 1, Phase: dictation.OverlayError}})
}

func TestMicrofoneBloqueado_ErroViraAMensagemDoMicrofone(t *testing.T) {
	for _, m := range []permissao.Motivo{
		permissao.Empresa, permissao.Aparelho, permissao.Conta, permissao.Programas, permissao.EsteApp,
	} {
		rt, buf, pilula := runtimeComLog(t, fakeFocus{})
		rt.ComConsultaDoMicrofone(func() permissao.Motivo { return m })

		mostrarErro(rt)

		if got := pilula.ultima(t); got != overlay.FaseMicrofoneBloqueado {
			t.Errorf("%s: pílula = %v, quero FaseMicrofoneBloqueado", m, got)
		}
		log := buf.String()
		for _, campo := range []string{"motivo=microfone_bloqueado", "bloqueio=" + string(m), "fase=error"} {
			if !strings.Contains(log, campo) {
				t.Errorf("%s: o log não tem %q:\n%s", m, campo, log)
			}
		}
	}
}

func TestMicrofoneBloqueado_LiberadoDesconhecidoOuSemConsultaMantemOErro(t *testing.T) {
	casos := map[string]func(*Runtime){
		"sem consulta": func(*Runtime) {},
		"liberado": func(rt *Runtime) {
			rt.ComConsultaDoMicrofone(func() permissao.Motivo { return permissao.Liberado })
		},
		"não deu para ler": func(rt *Runtime) {
			rt.ComConsultaDoMicrofone(func() permissao.Motivo { return permissao.Desconhecido })
		},
	}
	for nome, ajuste := range casos {
		rt, buf, pilula := runtimeComLog(t, fakeFocus{})
		ajuste(rt)

		mostrarErro(rt)

		if got := pilula.ultima(t); got != dictation.OverlayError {
			t.Errorf("%s: pílula = %v, quero OverlayError", nome, got)
		}
		if strings.Contains(buf.String(), "microfone_bloqueado") {
			t.Errorf("%s: o log afirma bloqueio:\n%s", nome, buf.String())
		}
	}
}

// Só o erro é trocado: um sucesso nunca vira aviso de microfone, e a
// consulta nem é feita.
func TestMicrofoneBloqueado_SoTrocaOErro(t *testing.T) {
	rt, _, pilula := runtimeComLog(t, fakeFocus{})
	consultas := 0
	rt.ComConsultaDoMicrofone(func() permissao.Motivo { consultas++; return permissao.Conta })

	rt.apply([]dictation.Effect{dictation.ShowOverlayEffect{Attempt: 1, Phase: dictation.OverlaySuccess}})

	if got := pilula.ultima(t); got != dictation.OverlaySuccess {
		t.Errorf("pílula = %v, quero OverlaySuccess", got)
	}
	if consultas != 0 {
		t.Errorf("consultou o registro %d vez(es) fora de um erro", consultas)
	}
}
