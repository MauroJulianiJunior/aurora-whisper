package syncengine

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os percursos do aprendido (F8f2) pelo storage de verdade (o espelho num
// historico.db em t.TempDir()) e pelo lote de verdade do dicionário (a
// Semente em memória: nenhum teste toca um dicionario.json): cada ação que as
// decisões devolvem passa no executor (que recusaria a página inteira), e as
// tabelas da ADR fecham de ponta a ponta no PC. A nuvem é de roteiro, com as
// respostas da tabela da nuvem (cloudstore.DecideLearned); a mesma conversa
// contra a nuvem de verdade está em internal/server/integration.
//
// O Receber e a varredura que estes testes montam à mão são os da F8f4 e da
// F8f3, reduzidos ao que as decisões precisam: um par por vez, sem trava nem
// arquivo.

var agoraDoPercurso = time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)

// pcQueAprende é um PC com o espelho de verdade e o dicionário em memória.
type pcQueAprende struct {
	pcDeTeste
	t       *testing.T
	semente dictionary.Semente
	seq     int64
	// enviadas são as mutações do aprendido, na ordem.
	enviadas []Mutation
}

func novoPCQueAprende(t *testing.T) *pcQueAprende {
	t.Helper()
	p := novoPC(t)
	if _, _, err := p.fila.Preparar(context.Background(), "a1", "p1", agoraDoPercurso); err != nil {
		t.Fatal(err)
	}
	// O "Enviar o histórico" já clicado (D3): o upsert sai.
	if _, err := p.db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x')`); err != nil {
		t.Fatal(err)
	}
	return &pcQueAprende{pcDeTeste: p, t: t, semente: dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECD", Prioridade: 90},
			{Canonica: "LALUR", Prioridade: 80, Dominio: "contabil"},
		},
	}}
}

// estado é o que o espelho sabe do id (o zero, sem linha).
func (p *pcQueAprende) estado(id string) storage.EstadoDoAprendido {
	p.t.Helper()
	es, err := p.fila.Espelho(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	for _, e := range es {
		if e.ID == id {
			return e
		}
	}
	return storage.EstadoDoAprendido{ID: id}
}

// noArquivo é a projeção do id no dicionário de agora.
func (p *pcQueAprende) noArquivo(id string) *dictionary.ParDoPainel {
	for _, par := range p.semente.ParesDoPainel() {
		if par.ID == id {
			return &par
		}
	}
	return nil
}

// receber é o que o Receber faz com uma mudança learned do pull.
func (p *pcQueAprende) receber(c Change, indisponivel bool) DecisaoDoAprendido {
	p.t.Helper()
	id := strings.ToLower(c.EntityID)
	return p.terminar(PlanejarPar(p.estado(id), c, ArquivoDoPar{Indisponivel: indisponivel, Projecao: p.noArquivo(id)}))
}

// tentarDeNovo é a varredura tentando o par da nuvem guardado.
func (p *pcQueAprende) tentarDeNovo(id string) (DecisaoDoAprendido, bool) {
	p.t.Helper()
	plano, ok := PlanejarPendente(p.estado(id), ArquivoDoPar{Projecao: p.noArquivo(id)})
	if !ok {
		return DecisaoDoAprendido{}, false
	}
	return p.terminar(plano), true
}

// terminar são os passos 2 e 3: o lote, a intenção antes da gravação, e o
// espelho na transação da página, com o cursor.
func (p *pcQueAprende) terminar(plano PlanoDoPar) DecisaoDoAprendido {
	p.t.Helper()
	ctx := context.Background()
	var r *dictionary.ResultadoDoPar
	if plano.Lote {
		novo, lote, err := p.semente.AplicarPares([]dictionary.ParDaNuvem{plano.Par}, dictionary.ConferenciasDoLote{})
		if err != nil {
			p.t.Fatal(err)
		}
		res := lote.Resultados[0]
		if a, ok := IntencaoDoPar(plano, res); ok {
			if !lote.Mudou {
				p.t.Fatal("a intenção sem gravação")
			}
			if err := p.fila.MudarAprendido(ctx, agoraDoPercurso, []string{plano.ID},
				func(storage.EstadoDoAprendido) storage.AcaoDoAprendido { return a }); err != nil {
				p.t.Fatalf("a intenção: %v", err)
			}
			if e := p.estado(plano.ID); e.Situacao != storage.EspelhoAplicando {
				p.t.Fatalf("a intenção não ficou: %+v", e)
			}
		}
		p.semente, r = novo, &res
	}
	var d DecisaoDoAprendido
	p.seq++
	cursor := p.seq
	if err := p.fila.AplicarComAprendido(ctx, "a1", "e1", nil, &cursor, nil, storage.RespostaDoAprendido{
		IDs: []string{plano.ID}, Agora: agoraDoPercurso,
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido { d = DecidirPar(e, plano, r); return d.Acao },
	}); err != nil {
		p.t.Fatalf("a página: %v", err)
	}
	return d
}

// varredura é a F8f3 vendo uma mudança daqui no id: a projeção nova e o
// marcador; com o envio em voo, só a versão local.
func (p *pcQueAprende) varredura(id string) {
	p.t.Helper()
	proj := p.noArquivo(id)
	if err := p.fila.MudarAprendido(context.Background(), agoraDoPercurso, []string{id},
		func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			if e.Marcador != nil && e.Marcador.EmVoo() {
				return storage.AcaoDoAprendido{MudouAqui: true}
			}
			return storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: proj}, MudouAqui: true,
				Marcador: storage.MarcadorCriar}
		}); err != nil {
		p.t.Fatal(err)
	}
}

// painel é o Aprender ou o Desfazer do painel: o arquivo muda, e a versão
// local sobe no ato (o gancho AoMudar).
func (p *pcQueAprende) painel(f func(dictionary.Semente) (dictionary.Semente, dictionary.Aprendida, error), id string) {
	p.t.Helper()
	novo, _, err := f(p.semente)
	if err != nil {
		p.t.Fatal(err)
	}
	p.semente = novo
	if err := p.fila.SubirVersaoLocal(context.Background(), []string{id}); err != nil {
		p.t.Fatal(err)
	}
}

func aprenderNoPainel(termo, variante string) func(dictionary.Semente) (dictionary.Semente, dictionary.Aprendida, error) {
	return func(s dictionary.Semente) (dictionary.Semente, dictionary.Aprendida, error) {
		return s.AprenderVariante(termo, variante)
	}
}

func desfazerNoPainel(variante string) func(dictionary.Semente) (dictionary.Semente, dictionary.Aprendida, error) {
	return func(s dictionary.Semente) (dictionary.Semente, dictionary.Aprendida, error) {
		return s.DesfazerVariante(variante)
	}
}

// enviar é o envio do aprendido: a mutação (o id do envio em voo, ou um
// novo), o envio marcado antes da "rede", e a resposta da nuvem aplicada.
// nuvem devolve false quando a resposta se perde.
func (p *pcQueAprende) enviar(nuvem func(Mutation) (MutationResult, bool)) []DecisaoDoAprendido {
	p.t.Helper()
	ctx := context.Background()
	itens, err := p.fila.PendentesDoAprendido(ctx, 10)
	if err != nil {
		p.t.Fatal(err)
	}
	var ds []DecisaoDoAprendido
	for _, it := range itens {
		id := it.Marcador.MutationID
		if id == "" {
			id = storage.NewID()
		}
		m, err := MutacaoDoAprendido(it, id)
		if err != nil {
			p.t.Fatal(err)
		}
		if !it.Marcador.EmVoo() {
			if err := p.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: it.ID,
				MutationID: id, Versao: it.VersaoLocal}}); err != nil {
				p.t.Fatal(err)
			}
		}
		p.enviadas = append(p.enviadas, m)
		r, ok := nuvem(m)
		if !ok {
			continue
		}
		var d DecisaoDoAprendido
		if err := p.fila.AplicarComAprendido(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{
			IDs: []string{it.ID}, Agora: agoraDoPercurso,
			Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
				d = DecidirResultadoDoAprendido(e, r)
				return d.Acao
			},
		}); err != nil {
			p.t.Fatalf("a resposta: %v", err)
		}
		ds = append(ds, d)
	}
	return ds
}

// responde é a nuvem de roteiro que responde r a qualquer mutação, com o id
// dela.
func responde(r MutationResult) func(Mutation) (MutationResult, bool) {
	return func(m Mutation) (MutationResult, bool) {
		r.MutationID = m.MutationID
		return r, true
	}
}

func semResposta(Mutation) (MutationResult, bool) { return MutationResult{}, false }

// filaDoAprendido são os marcadores learned.
func (p *pcQueAprende) filaDoAprendido() []storage.Marcador {
	p.t.Helper()
	ms, err := p.fila.Marcadores(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	var out []storage.Marcador
	for _, m := range ms {
		if m.EntityType == storage.EntidadeAprendido {
			out = append(out, m)
		}
	}
	return out
}

func (p *pcQueAprende) aplicadoDaNuvem(versao int64, a *Aprendido) {
	p.t.Helper()
	if d := p.receber(doPull(versao, a), false); d.Tipo != TipoAplicado && d.Tipo != TipoTrocado {
		p.t.Fatalf("o par da nuvem não foi aplicado: %+v", d)
	}
}

// ─── O pull ───

// O par ativo que não está aqui é aplicado pelas regras do PC que recebe (D2:
// a palavra é criada), com a intenção antes da gravação e o id no "veio da
// nuvem"; o eco da mesma versão não grava nada, e nada vai à fila.
func TestPercurso_OPullAplicaEOEcoNaoGrava(t *testing.T) {
	p := novoPCQueAprende(t)
	ecf := fioDoPar("sf", "ECF", false, "", false)
	if d := p.receber(doPull(1, ecf), false); d.Tipo != TipoAplicado {
		t.Fatalf("o par novo: %+v", d)
	}
	proj := p.noArquivo(idSF)
	if proj == nil || proj.Termo != "ECF" || proj.Variante != "sf" {
		t.Fatalf("o arquivo: %+v", proj)
	}
	e := p.estado(idSF)
	if e.ServerVersion == nil || *e.ServerVersion != 1 || e.BaseDesfeita || !reflect.DeepEqual(e.Projecao, proj) ||
		e.Situacao != storage.EspelhoAplicado || !e.VeioDaNuvem || e.Nuvem != nil || e.Esperada != nil || e.MudouAqui() {
		t.Errorf("o espelho: %+v", e)
	}
	antes, _ := p.fila.Espelho(context.Background())
	sementeAntes := p.semente
	if d := p.receber(doPull(1, ecf), false); d.Tipo != TipoEco {
		t.Errorf("o eco: %+v", d)
	}
	depois, _ := p.fila.Espelho(context.Background())
	if !reflect.DeepEqual(antes, depois) || !reflect.DeepEqual(sementeAntes, p.semente) {
		t.Error("o eco gravou")
	}
	if len(p.filaDoAprendido()) != 0 {
		t.Error("aplicar ecoou: o pull pôs marcador")
	}
}

// A troca: o par de antes sai e o novo entra, sem envio. A troca que não vale
// aqui tira o de antes (a restrição nunca se perde) e a linha fica "não
// aplicado", com o par guardado e a projeção sem ele (a varredura não vê um
// desfazer daqui); quando a causa some, a tentativa de novo o aplica, com a
// adaptação deste PC (o alias_restrito).
func TestPercurso_ATrocaEOQueNaoValeAqui(t *testing.T) {
	p := novoPCQueAprende(t)
	p.aplicadoDaNuvem(1, fioDoPar("sf", "ECF", false, "", false))

	if d := p.receber(doPull(2, fioDoPar("sf", "ECD", false, "", false)), false); d.Tipo != TipoTrocado {
		t.Fatalf("a troca: %+v", d)
	}
	if proj := p.noArquivo(idSF); proj == nil || proj.Termo != "ECD" {
		t.Fatalf("o arquivo depois da troca: %+v", proj)
	}
	if e := p.estado(idSF); *e.ServerVersion != 2 || e.Projecao.Termo != "ECD" || e.Situacao != storage.EspelhoAplicado {
		t.Errorf("o espelho depois da troca: %+v", e)
	}

	fiscal := fioDoPar("sf", "ECD", true, "fiscal", false)
	d := p.receber(doPull(3, fiscal), false)
	if d.Tipo != TipoNaoAplicado {
		t.Fatalf("a troca que não vale aqui: %+v", d)
	}
	if p.noArquivo(idSF) != nil {
		t.Error("o par de antes ficou no arquivo")
	}
	e := p.estado(idSF)
	if *e.ServerVersion != 3 || e.Projecao != nil || e.Situacao != storage.EspelhoNaoAplicado ||
		e.Motivo != string(dictionary.MotivoDominioAusente) || e.Nuvem == nil || *e.Nuvem.Versao != 3 ||
		e.Nuvem.Par.Dominio != "fiscal" || e.MudouAqui() || !TemPendente(e) {
		t.Errorf("o não aplicado: %+v", e)
	}
	if len(p.filaDoAprendido()) != 0 {
		t.Error("a troca pôs marcador")
	}

	// A causa some: o domínio passa a existir aqui, no termo.
	p.semente.Dominios["fiscal"] = []string{"Fiscal.exe"}
	p.semente.Entradas[0].Dominio = "fiscal"
	d, ok := p.tentarDeNovo(idSF)
	if !ok || d.Tipo != TipoAplicado {
		t.Fatalf("a tentativa de novo: %+v, %v", d, ok)
	}
	proj := p.noArquivo(idSF)
	quero := &dictionary.ParDoPainel{ID: idSF, Variante: "sf", Termo: "ECD", Restrito: true, Dominio: "fiscal", EmAliasRestrito: true}
	if !reflect.DeepEqual(proj, quero) {
		t.Fatalf("a adaptação deste PC: %+v", proj)
	}
	if e := p.estado(idSF); *e.ServerVersion != 3 || !reflect.DeepEqual(e.Projecao, quero) ||
		e.Situacao != storage.EspelhoAplicado || e.Nuvem != nil || TemPendente(e) {
		t.Errorf("o espelho depois da tentativa: %+v", e)
	}
	// A adaptação deste PC nunca volta como upsert.
	if len(p.filaDoAprendido()) != 0 {
		t.Error("a adaptação pôs marcador")
	}
}

// O par desfeito que vale aqui sai do arquivo, sem envio e sem etiqueta, e a
// linha fica só com o id.
func TestPercurso_ODesfazerDoPullSemEtiqueta(t *testing.T) {
	p := novoPCQueAprende(t)
	p.aplicadoDaNuvem(1, fioDoPar("sf", "ECF", false, "", false))
	if d := p.receber(doPull(2, fioDoPar("sf", "ECF", false, "", true)), false); d.Tipo != TipoDesfeito {
		t.Fatalf("o desfazer: %+v", d)
	}
	if p.noArquivo(idSF) != nil {
		t.Error("o par ficou no arquivo")
	}
	e := p.estado(idSF)
	if *e.ServerVersion != 2 || !e.BaseDesfeita || e.Projecao != nil || e.Esperada != nil || e.Nuvem != nil ||
		e.Situacao != storage.EspelhoNenhuma || e.MudouAqui() {
		t.Errorf("o espelho: %+v", e)
	}
	if len(p.filaDoAprendido()) != 0 {
		t.Error("o desfazer remoto pôs marcador")
	}
}

// O desfazer remoto nunca tira o que a pessoa pôs à mão, e o par ativo igual
// ao posto à mão não é adotado.
func TestPercurso_ODesfazerRemotoNaoTiraOQueAPessoaPos(t *testing.T) {
	p := novoPCQueAprende(t)
	p.semente.Entradas[0].Alias = []string{"sf"} // "sf" -> "ECD", à mão
	antes := p.semente
	if d := p.receber(doPull(1, fioDoPar("sf", "ECD", false, "", false)), false); d.Tipo != TipoJaValeAqui {
		t.Fatalf("o par ativo: %+v", d)
	}
	if d := p.receber(doPull(2, fioDoPar("sf", "ECD", false, "", true)), false); d.Tipo != TipoJaValeAqui {
		t.Fatalf("o desfazer: %+v", d)
	}
	if !reflect.DeepEqual(antes, p.semente) {
		t.Errorf("o arquivo mudou: %+v", p.semente)
	}
	if e := p.estado(idSF); *e.ServerVersion != 2 || !e.BaseDesfeita || e.Projecao != nil || e.VeioDaNuvem ||
		e.Situacao != storage.EspelhoJaValeAqui {
		t.Errorf("o espelho: %+v", e)
	}
}

// O par mudado aqui e que não subiu é pulado: nem o arquivo nem a base mudam,
// e o par fica guardado como pendente. Vale para a mudança à mão que a
// varredura ainda não viu, e para o Desfazer e o Aprender do mesmo par pelo
// painel, que deixam o arquivo como era.
func TestPercurso_OParMudadoAquiEPulado(t *testing.T) {
	for nome, mudar := range map[string]func(p *pcQueAprende){
		"a mudança à mão": func(p *pcQueAprende) {
			s, _, _ := p.semente.DesfazerVariante("sf")
			p.semente, _, _ = s.AprenderVariante("ECD", "sf")
		},
		"o Desfazer e o Aprender do mesmo par": func(p *pcQueAprende) {
			p.painel(desfazerNoPainel("sf"), idSF)
			p.painel(aprenderNoPainel("ECF", "sf"), idSF)
		},
	} {
		t.Run(nome, func(t *testing.T) {
			p := novoPCQueAprende(t)
			p.aplicadoDaNuvem(1, fioDoPar("sf", "ECF", false, "", false))
			mudar(p)
			arquivo := p.semente
			if d := p.receber(doPull(2, fioDoPar("sf", "EFD", false, "", false)), false); d.Tipo != TipoPulado {
				t.Fatalf("o pull: %+v", d)
			}
			if !reflect.DeepEqual(arquivo, p.semente) {
				t.Error("o arquivo foi sobrescrito")
			}
			e := p.estado(idSF)
			if *e.ServerVersion != 1 || e.Projecao.Termo != "ECF" || e.Nuvem == nil || *e.Nuvem.Versao != 2 ||
				e.Nuvem.Par.Termo != "EFD" || !TemPendente(e) {
				t.Errorf("o espelho: %+v", e)
			}
		})
	}
}

// O arquivo indisponível nunca segura o cursor: o par fica pendente, com o
// motivo, e entra quando o arquivo volta.
func TestPercurso_OArquivoIndisponivelNaoSeguraOCursor(t *testing.T) {
	p := novoPCQueAprende(t)
	if d := p.receber(doPull(1, fioDoPar("sf", "ECF", false, "", false)), true); d.Tipo != TipoPendente {
		t.Fatalf("o pull: %+v", d)
	}
	if c, _ := p.cursor(t); c != 1 {
		t.Errorf("o cursor não andou: %d", c)
	}
	e := p.estado(idSF)
	if e.ServerVersion != nil || e.Situacao != storage.EspelhoNaoAplicado ||
		e.Motivo != string(dictionary.MotivoArquivoIndisponivel) || e.Nuvem == nil || p.noArquivo(idSF) != nil {
		t.Fatalf("o pendente: %+v", e)
	}
	if d, ok := p.tentarDeNovo(idSF); !ok || d.Tipo != TipoAplicado {
		t.Fatalf("o arquivo voltou: %+v, %v", d, ok)
	}
	if e := p.estado(idSF); *e.ServerVersion != 1 || e.Situacao != storage.EspelhoAplicado || p.noArquivo(idSF) == nil {
		t.Errorf("o espelho: %+v", e)
	}
}

// ─── A resposta do push ───

// D4, o primeiro caso: este PC desfaz e aprende de novo o par (o arquivo fica
// como era) enquanto outro aparelho o desfaz. O upsert daqui perde na nuvem
// (o desfazer vence), a linha ganha a etiqueta "desfeito em outro aparelho",
// e o par sai daqui, seguindo a nuvem. O eco da versão não muda mais nada.
func TestPercurso_OConflictComODesfeito(t *testing.T) {
	p := novoPCQueAprende(t)
	p.aplicadoDaNuvem(1, fioDoPar("sf", "ECF", false, "", false))
	p.painel(desfazerNoPainel("sf"), idSF)
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)

	desfeito := fioDoPar("sf", "ECF", false, "", true)
	ds := p.enviar(responde(MutationResult{Status: StatusConflict, EntityVersion: 2, StateAprendido: desfeito}))
	if len(ds) != 1 || ds[0].Tipo != TipoConflito {
		t.Fatalf("a resposta: %+v", ds)
	}
	if m := p.enviadas[0]; m.Operation != OpUpsert || m.BaseVersion != 1 ||
		string(m.Payload) != `{"variant":"sf","term":"ECF","restricted":false,"domain":null}` {
		t.Errorf("o upsert: %+v %s", m, m.Payload)
	}
	e := p.estado(idSF)
	if e.Situacao != storage.EspelhoDesfeitoEmOutro || *e.ServerVersion != 1 || e.Marcador != nil || e.MudouAqui() ||
		!TemPendente(e) {
		t.Fatalf("depois do conflict: %+v", e)
	}

	// O state passa a valer aqui, pelas regras do PC que recebe, com a
	// etiqueta.
	if d, ok := p.tentarDeNovo(idSF); !ok || d.Tipo != TipoDesfeito {
		t.Fatalf("o state: %+v, %v", d, ok)
	}
	if p.noArquivo(idSF) != nil {
		t.Error("o par ficou aqui")
	}
	e = p.estado(idSF)
	if e.Situacao != storage.EspelhoDesfeitoEmOutro || *e.ServerVersion != 2 || !e.BaseDesfeita || e.Projecao != nil ||
		e.Nuvem == nil || !e.Nuvem.Par.Desfeito || TemPendente(e) || len(p.filaDoAprendido()) != 0 {
		t.Errorf("a etiqueta: %+v", e)
	}
	antes, _ := p.fila.Espelho(context.Background())
	if d := p.receber(doPull(2, desfeito), false); d.Tipo != TipoEco {
		t.Errorf("o pull da versão: %+v", d)
	}
	if depois, _ := p.fila.Espelho(context.Background()); !reflect.DeepEqual(antes, depois) {
		t.Error("o eco mexeu na etiqueta")
	}
}

// D4, o segundo caso: a mesma variante aprendida para termos diferentes em
// dois PCs. Este PC chegou depois: a troca dele perde, a linha ganha a
// etiqueta "trocado em outro aparelho", e o termo da nuvem passa a valer
// aqui. O pull da mesma versão, que chega depois na rodada, é eco.
func TestPercurso_OConflictComOutroTermo(t *testing.T) {
	p := novoPCQueAprende(t)
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)

	ecd := fioDoPar("sf", "ECD", false, "", false)
	ds := p.enviar(responde(MutationResult{Status: StatusConflict, EntityVersion: 1, StateAprendido: ecd}))
	if len(ds) != 1 || ds[0].Tipo != TipoConflito || p.enviadas[0].BaseVersion != 0 {
		t.Fatalf("a resposta: %+v %+v", ds, p.enviadas)
	}
	// A troca deste PC que perdeu fica com a etiqueta (a linha de estado do
	// painel), antes e depois de o termo da nuvem passar a valer aqui.
	perdido := storage.ParPerdido{Variante: "sf", Termo: "ECF"}
	if e := p.estado(idSF); e.Situacao != storage.EspelhoTrocadoEmOutro || e.ServerVersion != nil || !TemPendente(e) ||
		e.Perdido == nil || *e.Perdido != perdido {
		t.Fatalf("depois do conflict: %+v", e)
	}
	if d := p.receber(doPull(1, ecd), false); d.Tipo != TipoTrocado {
		t.Fatalf("o pull do state: %+v", d)
	}
	if proj := p.noArquivo(idSF); proj == nil || proj.Termo != "ECD" {
		t.Fatalf("o arquivo: %+v", proj)
	}
	e := p.estado(idSF)
	if e.Situacao != storage.EspelhoTrocadoEmOutro || *e.ServerVersion != 1 || e.Projecao.Termo != "ECD" ||
		e.Nuvem == nil || e.Nuvem.Par.Termo != "ECD" || TemPendente(e) || !e.VeioDaNuvem || len(p.filaDoAprendido()) != 0 ||
		e.Perdido == nil || *e.Perdido != perdido {
		t.Errorf("a etiqueta: %+v", e)
	}
	// A pessoa aprende de novo, sabendo: a base é a da nuvem, e ela vence.
	p.painel(desfazerNoPainel("sf"), idSF)
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)
	p.enviar(func(m Mutation) (MutationResult, bool) {
		if m.BaseVersion != 1 {
			t.Errorf("o reaprender foi com a base %d", m.BaseVersion)
		}
		return MutationResult{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 2}, true
	})
	if e := p.estado(idSF); e.Situacao != storage.EspelhoNenhuma || *e.ServerVersion != 2 || e.Nuvem != nil ||
		e.MudouAqui() || e.Projecao.Termo != "ECF" || e.Perdido != nil {
		t.Errorf("o reaprender: %+v", e)
	}
}

// O teste da ADR: um desfazer sem resposta seguido de um reaprender termina
// reaprendido na nuvem, sem etiqueta nenhuma. O envio em voo repete a mesma
// operação, com o mesmo id (o delete, mesmo com o par de volta no arquivo); a
// mudança do voo sobe depois, com outro id e a base que a resposta trouxe.
func TestPercurso_ODesfazerSemRespostaSeguidoDoReaprender(t *testing.T) {
	p := novoPCQueAprende(t)
	p.aplicadoDaNuvem(1, fioDoPar("sf", "ECF", false, "", false))
	p.painel(desfazerNoPainel("sf"), idSF)
	p.varredura(idSF)
	p.enviar(semResposta)
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)
	if e := p.estado(idSF); e.Presente() || !e.Marcador.EmVoo() {
		t.Fatalf("a projeção mudou em voo: %+v", e)
	}

	// O reenvio: o mesmo delete; a nuvem já o tinha aplicado.
	ds := p.enviar(func(m Mutation) (MutationResult, bool) {
		return MutationResult{MutationID: m.MutationID, Status: StatusDuplicate, Original: StatusApplied, EntityVersion: 2}, true
	})
	if len(ds) != 1 || ds[0].Tipo != TipoAplicado {
		t.Fatalf("o reenvio: %+v", ds)
	}
	if a, b := p.enviadas[0], p.enviadas[1]; !reflect.DeepEqual(a, b) || a.Operation != OpDelete || a.BaseVersion != 1 {
		t.Fatalf("o reenvio mudou: %+v / %+v", a, b)
	}
	e := p.estado(idSF)
	if *e.ServerVersion != 2 || !e.BaseDesfeita || e.Marcador != nil || !e.MudouAqui() {
		t.Fatalf("depois do reenvio: %+v", e)
	}

	// A varredura vê o reaprender, que sobe com a base que a resposta trouxe.
	p.varredura(idSF)
	ds = p.enviar(func(m Mutation) (MutationResult, bool) {
		// A tabela da nuvem: existe, desfeita, upsert com a base igual à
		// versão: applied, versão + 1 (reaprende).
		if m.BaseVersion != 2 || m.Operation != OpUpsert {
			return MutationResult{MutationID: m.MutationID, Status: StatusConflict, EntityVersion: 2,
				StateAprendido: fioDoPar("sf", "ECF", false, "", true)}, true
		}
		return MutationResult{MutationID: m.MutationID, Status: StatusApplied, EntityVersion: 3}, true
	})
	if len(ds) != 1 || ds[0].Tipo != TipoAplicado || p.enviadas[2].MutationID == p.enviadas[0].MutationID {
		t.Fatalf("o reaprender: %+v %+v", ds, p.enviadas)
	}
	e = p.estado(idSF)
	if *e.ServerVersion != 3 || e.BaseDesfeita || e.MudouAqui() || e.Situacao != storage.EspelhoNenhuma || !e.Presente() ||
		len(p.filaDoAprendido()) != 0 {
		t.Errorf("o fim: %+v", e)
	}
}

// O "recusado": o marcador sai, a projeção recusada fica, e a mudança não
// subiu (a versão local acima da base): o pull pula o id, e nada sobe de
// novo enquanto o par não muda (a varredura confere a projeção recusada).
func TestPercurso_ORecusadoNaoSobeDeNovoEOPullPula(t *testing.T) {
	p := novoPCQueAprende(t)
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)
	recusada := p.noArquivo(idSF)
	ds := p.enviar(responde(MutationResult{Status: StatusRejected, Code: "tipo_ainda_nao_sincroniza"}))
	if len(ds) != 1 || ds[0].Tipo != TipoRecusado {
		t.Fatalf("a recusa: %+v", ds)
	}
	e := p.estado(idSF)
	if e.Situacao != storage.EspelhoRecusado || e.Motivo != "tipo_ainda_nao_sincroniza" || !reflect.DeepEqual(e.Projecao, recusada) ||
		e.Marcador != nil || !e.MudouAqui() {
		t.Fatalf("o recusado: %+v", e)
	}
	if itens, _ := p.fila.PendentesDoAprendido(context.Background(), 10); len(itens) != 0 {
		t.Errorf("o recusado ficou na fila: %+v", itens)
	}
	arquivo := p.semente
	if d := p.receber(doPull(1, fioDoPar("sf", "ECD", false, "", false)), false); d.Tipo != TipoPulado {
		t.Errorf("o pull: %+v", d)
	}
	if !reflect.DeepEqual(arquivo, p.semente) || p.estado(idSF).Situacao != storage.EspelhoRecusado {
		t.Error("o pull sobrescreveu o recusado")
	}
}

// O nada_a_desfazer: a nuvem não tem o par; o marcador sai, a base sai e o
// desfazer fica confirmado (nada mais sobe, nem no recomeço). O base_invalida:
// a base sai, e o par sobe de novo com 0 e outro id.
func TestPercurso_NadaADesfazerEBaseInvalida(t *testing.T) {
	p := novoPCQueAprende(t)
	p.aplicadoDaNuvem(4, fioDoPar("sf", "ECF", false, "", false))
	p.painel(desfazerNoPainel("sf"), idSF)
	p.varredura(idSF)
	ds := p.enviar(responde(MutationResult{Status: StatusRejected, Code: CodigoNadaADesfazer}))
	if len(ds) != 1 || ds[0].Tipo != TipoNadaADesfazer || p.enviadas[0].Operation != OpDelete {
		t.Fatalf("o nada_a_desfazer: %+v", ds)
	}
	e := p.estado(idSF)
	if e.ServerVersion != nil || e.Marcador != nil || e.MudouAqui() || e.DesfeitoAqui() || e.Situacao != storage.EspelhoNenhuma {
		t.Fatalf("o desfazer confirmado: %+v", e)
	}

	// O par volta (aprendido aqui), com uma base que a nuvem não conhece.
	p.painel(aprenderNoPainel("ECF", "sf"), idSF)
	p.varredura(idSF)
	if err := p.fila.MudarAprendido(context.Background(), agoraDoPercurso, []string{idSF},
		func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 7}}
		}); err != nil {
		t.Fatal(err)
	}
	ds = p.enviar(responde(MutationResult{Status: StatusRejected, Code: CodigoBaseInvalida}))
	if len(ds) != 1 || ds[0].Tipo != TipoReenviar || p.enviadas[1].BaseVersion != 7 {
		t.Fatalf("o base_invalida: %+v %+v", ds, p.enviadas)
	}
	p.enviar(responde(MutationResult{Status: StatusApplied, EntityVersion: 1}))
	if m := p.enviadas[2]; m.BaseVersion != 0 || m.Operation != OpUpsert || m.MutationID == p.enviadas[1].MutationID {
		t.Errorf("o reenvio com 0: %+v", m)
	}
	if e := p.estado(idSF); *e.ServerVersion != 1 || e.MudouAqui() || e.Marcador != nil {
		t.Errorf("o fim: %+v", e)
	}
}
