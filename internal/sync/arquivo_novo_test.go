package syncengine

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O dicionario.json que passou a ser outro arquivo (o recriado vazio na
// subida, ou o de outro usuário): a tabela de DecidirArquivoNovo.
func TestDecidirArquivoNovo(t *testing.T) {
	sai := &storage.NovaProjecao{}
	pendente := &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoArquivoIndisponivel)}
	for _, c := range []struct {
		nome  string
		e     storage.EstadoDoAprendido
		quero storage.AcaoDoAprendido
	}{
		{"sem linha: nada", semLinha(), storage.AcaoDoAprendido{}},
		{"fora da projeção: nada", com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil }),
			storage.AcaoDoAprendido{}},
		{"mudado aqui (o Desfazer que a varredura não viu, ou o segurado): a varredura decide",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VersaoLocal = 3 }), storage.AcaoDoAprendido{}},
		{"com marcador (o envio que espera): a varredura decide",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Marcador = &storage.Marcador{EntityType: storage.EntidadeAprendido, EntityID: idSF}
			}), storage.AcaoDoAprendido{}},
		{"em voo: a varredura decide", emVoo(confirmado(), 2), storage.AcaoDoAprendido{}},
		{"o par que a nuvem confirmou: sai da projeção e volta pelo pendente, com o texto da base", confirmado(),
			storage.AcaoDoAprendido{Projecao: sai, Nuvem: guardadoDe(3, parDe("sf", "ECF")), Situacao: pendente}},
		{"o restrito volta com a restrição e o domínio",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Projecao = &dictionary.ParDoPainel{ID: idSF, Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "fiscal",
					EmAliasRestrito: true}
			}),
			storage.AcaoDoAprendido{Projecao: sai, Situacao: pendente,
				Nuvem: guardadoDe(3, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "fiscal"})}},
		{"a etiqueta guarda o par da nuvem da base: é ele que volta",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Projecao = projDe("sf", "ECD")
				e.Nuvem, e.Situacao = guardadoDe(3, parDe("sf", "ECD")), storage.EspelhoTrocadoEmOutro
				e.Perdido = &storage.ParPerdido{Variante: "sf", Termo: "ECF"}
			}),
			storage.AcaoDoAprendido{Projecao: sai, Situacao: pendente}},
		{"a nuvem desfez o par: só sai da projeção",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.BaseDesfeita = true }),
			storage.AcaoDoAprendido{Projecao: sai}},
		{"a nuvem nunca o teve: só sai da projeção",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.ServerVersion, e.VersaoBase, e.VersaoLocal = nil, 0, 0 }),
			storage.AcaoDoAprendido{Projecao: sai}},
		{"um par da nuvem mais novo já esperava: continua pendente",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(5, parDe("sf", "ECD")) }),
			storage.AcaoDoAprendido{Projecao: sai}},
		{"o não aplicado (a troca daqui no arquivo, a da nuvem pendente): continua pendente",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Nuvem = guardadoDe(3, parDe("sf", "ECD"))
				e.Situacao, e.Motivo = storage.EspelhoNaoAplicado, string(dictionary.MotivoVarianteDeOutroTermo)
			}),
			storage.AcaoDoAprendido{Projecao: sai}},
		{"a intenção sobre o arquivo antigo sai; o par dela fica pendente",
			com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, projDe("sf", "ECD"), guardadoDe(4, parDe("sf", "ECD"))
			}),
			storage.AcaoDoAprendido{Projecao: sai, Situacao: semSituacaoGravada}},
		{"a intenção de um par novo (sem projeção) sai",
			com(semLinha(), func(e *storage.EstadoDoAprendido) {
				e.TemLinha = true
				e.Situacao, e.Esperada, e.Nuvem = storage.EspelhoAplicando, projDe("sf", "ECD"), guardadoDe(1, parDe("sf", "ECD"))
			}),
			storage.AcaoDoAprendido{Projecao: sai, Situacao: semSituacaoGravada}},
	} {
		if got := DecidirArquivoNovo(c.e); !reflect.DeepEqual(got, c.quero) {
			t.Errorf("%s:\n tenho %s\n quero %s", c.nome, acaoEmTexto(got), acaoEmTexto(c.quero))
		}
	}
}

// O percurso, com o dicionario.json e o historico.db de verdade: o PC
// conectado tem pares sincronizados, e o arquivo é trocado por um vazio (o
// que o Agent recria na subida). Sem SoltarOArquivoAntigo, a varredura
// manda o delete de cada um (o controle: é o defeito). Com ele, nenhum
// delete nasce, os pares da nuvem voltam ao arquivo na mesma varredura, e só
// o Desfazer do painel que a varredura ainda não tinha visto sobe (é um
// desfazer de verdade). De novo antes da varredura, não muda nada.
func TestArquivoNovo_NaoViraDeleteEOsParesVoltam(t *testing.T) {
	for _, soltar := range []bool{false, true} {
		p := novoPCQueVarre(t)
		p.aceitar()
		ecf := p.sincronizado("ECF", "esse efe")
		ecd := p.sincronizado("ECD", "ecedê")
		desfeito := p.sincronizado("ECF", "efe cê")
		p.desfazer("efe cê")
		if err := (dictionary.Semente{Usuario: "mauro"}).Gravar(p.arq.Caminho); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if soltar {
			n, err := SoltarOArquivoAntigo(ctx, p.fila, p.agora)
			if err != nil || n != 2 {
				t.Fatalf("SoltarOArquivoAntigo = %d, %v; quero os 2 pares sincronizados", n, err)
			}
			if e := p.estado(ecf); e.Projecao != nil || e.MudouAqui() || e.Situacao != storage.EspelhoNaoAplicado ||
				e.Nuvem == nil || e.Nuvem.Par != parDe("esse efe", "ECF") {
				t.Fatalf("o par solto: %+v", e)
			}
			// De novo (a subida que caiu no meio e refaz), antes da varredura:
			// nada a mudar.
			if n, err := SoltarOArquivoAntigo(ctx, p.fila, p.agora); err != nil || n != 0 {
				t.Errorf("de novo: %d, %v", n, err)
			}
		}

		r := p.varrer()
		marcados := p.marcados()
		sort.Strings(marcados)
		if !soltar {
			quero := []string{desfeito, ecd, ecf}
			sort.Strings(quero)
			if !reflect.DeepEqual(marcados, quero) {
				t.Fatalf("o controle: os marcados são %v; sem o passo, quero o delete dos três", marcados)
			}
			continue
		}
		if !reflect.DeepEqual(marcados, []string{desfeito}) || r.Trocas != 0 || r.Freio != 0 {
			t.Fatalf("depois do arquivo novo: marcados %v, M %d, freio %d; quero só o Desfazer", marcados, r.Trocas, r.Freio)
		}
		s := p.arquivo()
		for id, termo := range map[string]string{ecf: "ECF", ecd: "ECD"} {
			if proj := projecaoNo(s, id); proj == nil || proj.Termo != termo {
				t.Errorf("o par %s não voltou ao arquivo: %+v", termo, proj)
			}
			if e := p.estado(id); e.Situacao != storage.EspelhoAplicado || e.MudouAqui() || e.Projecao == nil || e.Nuvem != nil {
				t.Errorf("o espelho do par %s que voltou: %+v", termo, e)
			}
		}
		if projecaoNo(s, desfeito) != nil {
			t.Error("o par desfeito no painel voltou ao arquivo")
		}
		enviadas := len(p.nuvem.enviadas)
		p.enviar()
		if novas := p.nuvem.enviadas[enviadas:]; len(novas) != 1 || novas[0].EntityID != desfeito || novas[0].Operation != OpDelete {
			t.Errorf("subiram %+v; quero só o delete do Desfazer", novas)
		}
	}
}
