package storage_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O aprendido no SQLite (ADR-019, "Decisões da F8", F8e): o espelho, o "veio
// da nuvem", o marcador learned, o envio que segura o upsert antes do aceite,
// o "recusado", o "aplicando" do write-ahead, o esquecer e o epoch novo. As
// decisões (a varredura, o pull, a resposta do push) são da F8f: aqui, cada
// teste monta o estado pelo executor (MudarAprendido), como elas o farão, e
// confere o que o banco guarda. Cada teste roda num historico.db de verdade,
// numa pasta temporária; nenhum toca o dicionario.json.

var agoraDoAprendido = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

// projecao é a projeção de uma variante sem domínio.
func projecao(variante, termo string) *dictionary.ParDoPainel {
	return &dictionary.ParDoPainel{ID: dictionary.IDDoPar(variante), Variante: variante, Termo: termo}
}

// nova é a projeção a gravar (nil é a ausente).
func nova(p *dictionary.ParDoPainel) *storage.NovaProjecao { return &storage.NovaProjecao{Par: p} }

func situacao(s storage.SituacaoDoEspelho, motivo string) *storage.NovaSituacao {
	return &storage.NovaSituacao{Situacao: s, Motivo: motivo}
}

// mudar executa uma ação num id, com o relógio dos testes.
func mudar(t *testing.T, f filaDeTeste, id string, a storage.AcaoDoAprendido) {
	t.Helper()
	if err := f.fila.MudarAprendido(context.Background(), agoraDoAprendido, []string{id},
		func(storage.EstadoDoAprendido) storage.AcaoDoAprendido { return a }); err != nil {
		t.Fatalf("MudarAprendido: %v", err)
	}
}

// espelhoPorID lê o espelho inteiro, pelo id.
func espelhoPorID(t *testing.T, f filaDeTeste) map[string]storage.EstadoDoAprendido {
	t.Helper()
	es, err := f.fila.Espelho(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]storage.EstadoDoAprendido{}
	for _, e := range es {
		out[e.ID] = e
	}
	return out
}

// linhaCrua lê a linha do espelho como o banco a guarda (coluna -> valor).
func linhaCrua(t *testing.T, f filaDeTeste, id string) map[string]any {
	t.Helper()
	for _, l := range linhasDaTabela(t, f.db, "sync_aprendido") {
		if l["entity_id"] == id {
			return l
		}
	}
	t.Fatalf("o id não tem linha no espelho")
	return nil
}

// colunasDeTexto são as colunas do espelho que guardam texto da troca.
var colunasDeTexto = []string{
	"variante", "termo", "dominio", "esperada_variante", "esperada_termo", "esperada_dominio",
	"nuvem_variante", "nuvem_termo", "nuvem_dominio", "perdido_variante", "perdido_termo",
}

// semTexto confere que a linha do espelho só guarda o id: nenhuma coluna de
// texto da troca.
func semTexto(t *testing.T, f filaDeTeste, id, oque string) {
	t.Helper()
	l := linhaCrua(t, f, id)
	for _, c := range colunasDeTexto {
		if l[c] != nil {
			t.Errorf("%s: a coluna %s ficou com texto", oque, c)
		}
	}
}

// veioDaNuvem lê o conjunto "veio da nuvem", em ordem.
func veioDaNuvem(t *testing.T, f filaDeTeste) []string {
	t.Helper()
	var out []string
	for _, l := range linhasDaTabela(t, f.db, "sync_veio_da_nuvem") {
		out = append(out, l["entity_id"].(string))
	}
	sort.Strings(out)
	return out
}

func aceitar(t *testing.T, f filaDeTeste) {
	t.Helper()
	executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x')
		ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`)
}

// conectarNaConta grava o pareamento com a conta dada.
func conectarNaConta(t *testing.T, f filaDeTeste, conta string) {
	t.Helper()
	executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1'),
		('nuvem_conta', ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`, conta)
}

// O marcador do aprendido: um por id, sem texto, campos 0, nunca inicial; fora
// da fila dos ditados, do progresso do envio inicial e da contagem dos
// ditados.
func TestAprendido_MarcadorSemTextoENuncaInicial(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	p := projecao("n oito n", "n8n")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Marcador: storage.MarcadorCriar})
	// Duas mudanças, um marcador.
	mudar(t, f, p.ID, storage.AcaoDoAprendido{MudouAqui: true, Marcador: storage.MarcadorCriar})

	linhas := linhasDaTabela(t, f.db, "sync_outbox")
	if len(linhas) != 1 {
		t.Fatalf("a fila: %v", linhas)
	}
	m := linhas[0]
	if m["entity_type"] != storage.EntidadeAprendido || m["entity_id"] != p.ID || m["campos"] != int64(0) ||
		m["inicial"] != int64(0) || m["mutation_id"] != nil || m["versao_enviada"] != nil ||
		m["selo_escolha"] != nil || m["selo_em"] != nil {
		t.Errorf("o marcador: %v", m)
	}
	for c, v := range m {
		if s, ok := v.(string); ok && (strings.Contains(s, "oito") || strings.Contains(s, "n8n")) {
			t.Errorf("o marcador leva texto na coluna %s", c)
		}
	}
	if itens, err := f.fila.Pendentes(ctx, 10); err != nil || len(itens) != 0 {
		t.Errorf("o aprendido entrou na fila dos ditados: %v, %v", itens, err)
	}
	if n, err := f.fila.Contar(ctx); err != nil || n != 0 {
		t.Errorf("o aprendido contou como ditado: %d, %v", n, err)
	}
	if feitos, total, err := f.fila.Progresso(ctx); err != nil || feitos != 0 || total != 0 {
		t.Errorf("o aprendido entrou no progresso do envio inicial: %d de %d, %v", feitos, total, err)
	}
	e := espelhoPorID(t, f)[p.ID]
	if e.Marcador == nil || e.Marcador.Inicial || e.VersaoLocal != 2 || e.VersaoBase != 0 || !e.Presente() {
		t.Errorf("o estado: %+v", e)
	}
}

// O espelho devolve o que o executor gravou, campo por campo, e a ação vazia
// não escreve nada (a varredura ociosa).
func TestAprendido_EspelhoLeOQueGravou(t *testing.T) {
	f := novaFilaDeTeste(t)
	p := &dictionary.ParDoPainel{ID: dictionary.IDDoPar("pedro das"), Variante: "pedro das", Termo: "Pedro Dás",
		Restrito: true, Dominio: "contabil", EmAliasRestrito: true}
	esperada := &dictionary.ParDoPainel{ID: p.ID, Variante: "pedro das", Termo: "Pedro Das"}
	nuvem := storage.ParGuardado{Versao: versao(6), Par: dictionary.ParDaNuvem{Variante: "pedro das", Termo: "Pedro Das"}}
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), Base: &storage.BaseDoAprendido{ServerVersion: 5}})
	mudar(t, f, p.ID, storage.AcaoDoAprendido{
		Nuvem: &nuvem, Situacao: situacao(storage.EspelhoAplicando, ""), Esperada: nova(esperada),
		VeioDaNuvem: storage.VeioEntra,
	})
	e := espelhoPorID(t, f)[p.ID]
	if !e.TemLinha || e.ServerVersion == nil || *e.ServerVersion != 5 || e.BaseDesfeita || e.VersaoLocal != 0 ||
		!reflect.DeepEqual(e.Projecao, p) || !reflect.DeepEqual(e.Esperada, esperada) ||
		e.Nuvem == nil || *e.Nuvem.Versao != 6 || e.Nuvem.Par != nuvem.Par ||
		e.Situacao != storage.EspelhoAplicando || e.Motivo != "" || e.SituacaoEm == nil ||
		!e.SituacaoEm.Equal(agoraDoAprendido) || !e.VeioDaNuvem || e.Marcador != nil {
		t.Fatalf("o estado lido: %+v", e)
	}
	if l := linhaCrua(t, f, p.ID); l["dominio"] != "contabil" || l["esperada_dominio"] != nil {
		t.Errorf("o domínio vazio não é nulo, ou o nome não ficou: %v %v", l["dominio"], l["esperada_dominio"])
	}

	// A mesma situação gravada de novo, mais tarde, guarda a data de quando
	// ela começou (as etiquetas somem em 30 dias contados dela).
	if err := f.fila.MudarAprendido(context.Background(), agoraDoAprendido.Add(time.Hour), []string{p.ID},
		func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Situacao: situacao(storage.EspelhoAplicando, ""), Esperada: nova(esperada)}
		}); err != nil {
		t.Fatal(err)
	}
	if e := espelhoPorID(t, f)[p.ID]; e.SituacaoEm == nil || !e.SituacaoEm.Equal(agoraDoAprendido) {
		t.Errorf("a data da situação andou: %v", e.SituacaoEm)
	}

	antes := linhasDaTabela(t, f.db, "sync_aprendido")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{})
	if !iguais(antes, linhasDaTabela(t, f.db, "sync_aprendido")) {
		t.Error("a ação vazia escreveu")
	}
	// A ação vazia num id sem linha não cria linha.
	outro := projecao("sf", "ECF")
	mudar(t, f, outro.ID, storage.AcaoDoAprendido{})
	if _, ok := espelhoPorID(t, f)[outro.ID]; ok {
		t.Error("a ação vazia criou uma linha")
	}

	// A confirmação da intenção: a esperada vira a projeção guardada, e sai
	// sozinha quando a situação deixa de ser "aplicando".
	mudar(t, f, p.ID, storage.AcaoDoAprendido{
		Projecao: nova(esperada), Base: &storage.BaseDoAprendido{ServerVersion: 6}, Cobre: versao(0),
		Situacao: situacao(storage.EspelhoAplicado, ""), LimparNuvem: true,
	})
	e = espelhoPorID(t, f)[p.ID]
	if e.Esperada != nil || e.Nuvem != nil || !reflect.DeepEqual(e.Projecao, esperada) || *e.ServerVersion != 6 ||
		e.Situacao != storage.EspelhoAplicado || !e.VeioDaNuvem {
		t.Errorf("a intenção confirmada: %+v", e)
	}
	l := linhaCrua(t, f, p.ID)
	for _, c := range []string{"esperada_variante", "esperada_termo", "esperada_restrito", "esperada_dominio",
		"esperada_em_alias_restrito", "nuvem_versao", "nuvem_variante", "nuvem_termo", "nuvem_restrito",
		"nuvem_dominio", "nuvem_desfeito"} {
		if l[c] != nil {
			t.Errorf("a coluna %s ficou depois da confirmação: %v", c, l[c])
		}
	}
}

// O par perdido (a 0008) vive com a etiqueta: gravado com ela, fica na linha
// "aplicando" que aplica o par da nuvem e na etiqueta que volta depois, e numa
// ação que não grava situação; sai com qualquer outra situação gravada (a
// pessoa aprende de novo, os 30 dias, o par mais novo que o supera, a
// confirmação sem a etiqueta).
func TestAprendido_OParPerdidoViveComAEtiqueta(t *testing.T) {
	f := novaFilaDeTeste(t)
	meu := projecao("ecedê", "ECF")
	daNuvem := projecao("ecedê", "ECD")
	perdido := &storage.ParPerdido{Variante: "ecedê", Termo: "ECF"}
	g := &storage.ParGuardado{Versao: versao(4), Par: dictionary.ParDaNuvem{Variante: "ecedê", Termo: "ECD"}}
	id := meu.ID
	ler := func() *storage.ParPerdido {
		t.Helper()
		return espelhoPorID(t, f)[id].Perdido
	}
	etiquetar := func() {
		t.Helper()
		mudar(t, f, id, storage.AcaoDoAprendido{Nuvem: g, Situacao: situacao(storage.EspelhoTrocadoEmOutro, ""), Perdido: perdido})
		if p := ler(); p == nil || *p != *perdido {
			t.Fatalf("o par perdido gravado com a etiqueta: %+v", p)
		}
	}

	mudar(t, f, id, storage.AcaoDoAprendido{Projecao: nova(meu), Base: &storage.BaseDoAprendido{ServerVersion: 2}})
	etiquetar()
	if l := linhaCrua(t, f, id); l["perdido_variante"] != "ecedê" || l["perdido_termo"] != "ECF" {
		t.Errorf("a linha crua: %v %v", l["perdido_variante"], l["perdido_termo"])
	}
	// A intenção que aplica o par da nuvem, e a etiqueta que volta depois.
	mudar(t, f, id, storage.AcaoDoAprendido{Situacao: situacao(storage.EspelhoAplicando, ""), Esperada: nova(daNuvem)})
	if p := ler(); p == nil || *p != *perdido {
		t.Fatalf("a linha aplicando perdeu o par perdido: %+v", p)
	}
	mudar(t, f, id, storage.AcaoDoAprendido{Projecao: nova(daNuvem), Base: &storage.BaseDoAprendido{ServerVersion: 4},
		Situacao: situacao(storage.EspelhoTrocadoEmOutro, "")})
	if p := ler(); p == nil || *p != *perdido {
		t.Fatalf("a etiqueta de volta perdeu o par perdido: %+v", p)
	}
	mudar(t, f, id, storage.AcaoDoAprendido{MudouAqui: true})
	if p := ler(); p == nil {
		t.Fatal("uma ação sem situação tirou o par perdido")
	}

	for _, s := range []*storage.NovaSituacao{
		situacao(storage.EspelhoNenhuma, ""),
		situacao(storage.EspelhoAplicado, ""),
		situacao(storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)),
		situacao(storage.EspelhoJaValeAqui, ""),
		situacao(storage.EspelhoRecusado, "campo_invalido"),
	} {
		etiquetar()
		mudar(t, f, id, storage.AcaoDoAprendido{Situacao: s})
		if p := ler(); p != nil {
			t.Errorf("a situação %q deixou o par perdido: %+v", s.Situacao, p)
		}
		if l := linhaCrua(t, f, id); l["perdido_variante"] != nil || l["perdido_termo"] != nil {
			t.Errorf("a situação %q deixou o texto do par perdido na linha", s.Situacao)
		}
	}
}

// A ação que deixaria o espelho incoerente é recusada, e nada da transação
// fica (nem o que um id anterior do mesmo lote fez).
func TestAprendido_AcaoInvalidaNaoGravaNada(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	p := projecao("n oito n", "n8n")
	outro := projecao("sf", "ECF")
	casos := map[string]storage.AcaoDoAprendido{
		"a projeção de outro id":    {Projecao: nova(outro)},
		"a projeção sem o id certo": {Projecao: nova(&dictionary.ParDoPainel{ID: outro.ID, Variante: "n oito n", Termo: "n8n"})},
		"a projeção que a nuvem recusaria": {Projecao: nova(&dictionary.ParDoPainel{ID: p.ID, Variante: "n oito n",
			Termo: strings.Repeat("x", 81)})},
		"o restrito sem domínio": {Projecao: nova(&dictionary.ParDoPainel{ID: p.ID, Variante: "n oito n", Termo: "n8n",
			Restrito: true})},
		"o par da nuvem de outro id":    {Nuvem: &storage.ParGuardado{Par: dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF"}}},
		"o par da nuvem com nulo":       {Nuvem: &storage.ParGuardado{Par: dictionary.ParDaNuvem{Variante: "n oito n", Termo: "n8n\x00"}}},
		"a versão do par menor que 1":   {Nuvem: &storage.ParGuardado{Versao: versao(0), Par: dictionary.ParDaNuvem{Variante: "n oito n", Termo: "n8n"}}},
		"o par gravado e tirado":        {Nuvem: &storage.ParGuardado{Par: dictionary.ParDaNuvem{Variante: "n oito n", Termo: "n8n"}}, LimparNuvem: true},
		"a base gravada e zerada":       {Base: &storage.BaseDoAprendido{ServerVersion: 2}, ZerarBase: true},
		"a base com versão 0":           {Base: &storage.BaseDoAprendido{}},
		"a versão coberta acima":        {Cobre: versao(1)},
		"a versão coberta negativa":     {Cobre: versao(-1)},
		"a esperada sem aplicando":      {Esperada: nova(p)},
		"aplicando sem a esperada":      {Situacao: situacao(storage.EspelhoAplicando, "")},
		"a situação fora da lista":      {Situacao: situacao("pendente", "")},
		"o motivo fora da lista":        {Situacao: situacao(storage.EspelhoNaoAplicado, "sei_la")},
		"o não aplicado sem motivo":     {Situacao: situacao(storage.EspelhoNaoAplicado, "")},
		"o código da recusa com texto":  {Situacao: situacao(storage.EspelhoRecusado, "Campo Inválido")},
		"o recusado sem código":         {Situacao: situacao(storage.EspelhoRecusado, "")},
		"motivo onde não há":            {Situacao: situacao(storage.EspelhoAplicado, "dominio_ausente")},
		"o marcador fora da lista":      {Marcador: 9},
		"o veio da nuvem fora da lista": {VeioDaNuvem: 9},
		"o perdido sem a etiqueta":      {Perdido: &storage.ParPerdido{Variante: "n oito n", Termo: "n8n"}},
		"o perdido com aplicando": {Situacao: situacao(storage.EspelhoAplicando, ""), Esperada: nova(p),
			Perdido: &storage.ParPerdido{Variante: "n oito n", Termo: "n8n"}},
		"o perdido de outro id": {Situacao: situacao(storage.EspelhoTrocadoEmOutro, ""),
			Perdido: &storage.ParPerdido{Variante: "sf", Termo: "ECF"}},
		"o perdido sem termo": {Situacao: situacao(storage.EspelhoTrocadoEmOutro, ""),
			Perdido: &storage.ParPerdido{Variante: "n oito n"}},
	}
	for nome, a := range casos {
		err := f.fila.MudarAprendido(ctx, agoraDoAprendido, []string{outro.ID, p.ID},
			func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
				if e.ID == outro.ID {
					// O primeiro id do lote grava; a recusa do segundo o desfaz.
					return storage.AcaoDoAprendido{Projecao: nova(outro), MudouAqui: true, Marcador: storage.MarcadorCriar}
				}
				return a
			})
		if !errors.Is(err, storage.ErrAcaoDoAprendido) {
			t.Errorf("%s: %v", nome, err)
		}
		if err != nil && strings.Contains(err.Error(), "oito") {
			t.Errorf("%s: o erro traz o texto: %v", nome, err)
		}
		for _, tb := range []string{"sync_aprendido", "sync_outbox", "sync_veio_da_nuvem"} {
			if n := len(linhasDaTabela(t, f.db, tb)); n != 0 {
				t.Errorf("%s: %s ficou com %d linhas", nome, tb, n)
			}
		}
	}
	// A versão coberta pode ir até a local que a mesma ação sobe.
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Cobre: versao(1)})
	if e := espelhoPorID(t, f)[p.ID]; e.VersaoLocal != 1 || e.VersaoBase != 1 {
		t.Errorf("a versão coberta com a mudança: %+v", e)
	}
}

// Durante o voo, a projeção guardada é a do envio: a ação que a troca é
// recusada (o reenvio repete a operação e o payload), a mudança só sobe a
// versão local, e a resposta que tira o marcador ou limpa o envio pode
// trocá-la.
func TestAprendido_ProjecaoNaoMudaEmVoo(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	p := projecao("n oito n", "n8n")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Marcador: storage.MarcadorCriar})
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: p.ID,
		MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	err := f.fila.MudarAprendido(ctx, agoraDoAprendido, []string{p.ID}, func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Projecao: nova(nil), MudouAqui: true}
	})
	if !errors.Is(err, storage.ErrProjecaoEmVoo) {
		t.Fatalf("a projeção trocada em voo: %v", err)
	}
	// A mudança feita no voo: só a versão local sobe (o "mudou durante o voo").
	mudar(t, f, p.ID, storage.AcaoDoAprendido{MudouAqui: true})
	e := espelhoPorID(t, f)[p.ID]
	if !e.Presente() || e.VersaoLocal != 2 || !e.Marcador.EmVoo() || e.Marcador.VersaoEnviada != 1 {
		t.Fatalf("depois da mudança em voo: %+v %+v", e, e.Marcador)
	}
	// O reenvio repete a mesma operação: o upsert, com o mesmo id.
	itens, err := f.fila.PendentesDoAprendido(ctx, 10)
	if err != nil || len(itens) != 1 || !itens[0].Presente() || itens[0].Marcador.MutationID != "m1" {
		t.Fatalf("o reenvio: %+v, %v", itens, err)
	}
	// A resposta (applied, com mudança no voo): a base anda até a enviada, o
	// envio é limpo, e a projeção pode mudar na mesma ação.
	mudar(t, f, p.ID, storage.AcaoDoAprendido{
		Base: &storage.BaseDoAprendido{ServerVersion: 1}, Cobre: versao(1), Marcador: storage.MarcadorReenviar,
		Projecao: nova(nil),
	})
	e = espelhoPorID(t, f)[p.ID]
	if e.Presente() || e.Marcador == nil || e.Marcador.EmVoo() || !e.MudouAqui() || !e.DesfeitoAqui() {
		t.Errorf("depois da resposta: %+v", e)
	}
}

// A reconexão (outro pareamento da mesma instalação) no meio de um envio do
// aprendido: o marcador fica a repetir (sem o mutation_id, que o aparelho
// novo troca, e com a versão enviada), a projeção continua congelada, e o
// envio seguinte ganha o id novo e fica com a versão enviada de antes (é com
// ela que a resposta vê a mudança feita no voo). O recomeço (o epoch novo)
// esquece o envio inteiro.
func TestReconexao_OEnvioDoAprendidoFicaARepetir(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	p := projecao("sf", "ECF")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Marcador: storage.MarcadorCriar})
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: p.ID,
		MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := f.fila.SubirVersaoLocal(ctx, []string{p.ID}); err != nil { // a mudança no voo
		t.Fatal(err)
	}
	if _, inicio, err := f.fila.Preparar(ctx, "a1", "p2", agoraDaFila); err != nil || inicio != storage.InicioReconexao {
		t.Fatalf("a reconexão: %v, %v", inicio, err)
	}
	m := espelhoPorID(t, f)[p.ID].Marcador
	if m == nil || m.EmVoo() || !m.Repetir || !m.Congelado() || m.VersaoEnviada != 1 {
		t.Fatalf("o marcador depois da reconexão: %+v", m)
	}
	err := f.fila.MudarAprendido(ctx, agoraDoAprendido, []string{p.ID}, func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Projecao: nova(nil), MudouAqui: true}
	})
	if !errors.Is(err, storage.ErrProjecaoEmVoo) {
		t.Fatalf("a projeção trocada no envio a repetir: %v", err)
	}
	if itens, err := f.fila.PendentesDoAprendido(ctx, 10); err != nil || len(itens) != 1 || !itens[0].Presente() {
		t.Fatalf("o envio a repetir: %+v, %v", itens, err)
	}
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: p.ID,
		MutationID: "m2", Versao: 2}}); err != nil {
		t.Fatal(err)
	}
	m = espelhoPorID(t, f)[p.ID].Marcador
	if m.MutationID != "m2" || m.VersaoEnviada != 1 || m.Repetir {
		t.Fatalf("o envio repetido: %+v", m)
	}
	if err := f.fila.Recomecar(ctx, "a1", "p2", agoraDaFila); err != nil {
		t.Fatal(err)
	}
	if m := espelhoPorID(t, f)[p.ID].Marcador; m == nil || m.Congelado() || m.VersaoEnviada != 0 {
		t.Errorf("o recomeço não esqueceu o envio: %+v", m)
	}
}

// Antes do aceite do "Enviar o histórico" (D3), o envio só leva o delete: o
// upsert fica na fila. O em voo sai sempre, primeiro. Depois do aceite, tudo.
func TestAprendido_PendentesSeguraOUpsertAntesDoAceite(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	novo := projecao("n oito n", "n8n")
	desfeito := projecao("sf", "ECF")
	emVoo := projecao("dctf web", "DCTFWeb")
	mudar(t, f, novo.ID, storage.AcaoDoAprendido{Projecao: nova(novo), MudouAqui: true, Marcador: storage.MarcadorCriar})
	mudar(t, f, desfeito.ID, storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 3},
		Projecao: nova(nil), MudouAqui: true, Marcador: storage.MarcadorCriar})
	mudar(t, f, emVoo.ID, storage.AcaoDoAprendido{Projecao: nova(emVoo), MudouAqui: true, Marcador: storage.MarcadorCriar})
	executar(t, f.db, `UPDATE sync_outbox SET mutation_id = 'm9', versao_enviada = 1 WHERE entity_id = ?`, emVoo.ID)

	ids := func() []string {
		t.Helper()
		itens, err := f.fila.PendentesDoAprendido(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range itens {
			out = append(out, it.ID)
		}
		return out
	}
	if got := ids(); !reflect.DeepEqual(got, []string{emVoo.ID, desfeito.ID}) {
		t.Errorf("antes do aceite: %v; quero o em voo e o delete", got)
	}
	if _, ok := espelhoPorID(t, f)[novo.ID]; !ok || len(f.marcadores(t)) != 3 {
		t.Error("o upsert segurado saiu da fila")
	}
	aceitar(t, f)
	got := ids()
	if len(got) != 3 || got[0] != emVoo.ID {
		t.Errorf("depois do aceite: %v", got)
	}
	// O limite vale.
	if itens, _ := f.fila.PendentesDoAprendido(ctx, 1); len(itens) != 1 || itens[0].ID != emVoo.ID {
		t.Errorf("com o limite 1: %v", itens)
	}
	// O marcador sem linha no espelho (um banco mexido à mão) sai da fila.
	executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('learned', 'sem-linha', 0, ?)`,
		emTextoDeTeste(agoraDoAprendido))
	ids()
	if _, ok := f.marcadores(t)["sem-linha"]; ok {
		t.Error("o marcador sem espelho ficou")
	}
}

// O "recusado": o marcador sai, a linha fica com o código e com a projeção
// recusada (a guardada), para a varredura não mandá-la de novo.
func TestAprendido_RecusadoGuardaAProjecaoRecusada(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	p := projecao("n oito n", "n8n")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Marcador: storage.MarcadorCriar})
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: p.ID,
		MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Situacao: situacao(storage.EspelhoRecusado, "tipo_ainda_nao_sincroniza"),
		Marcador: storage.MarcadorSai})
	e := espelhoPorID(t, f)[p.ID]
	if e.Marcador != nil || e.Situacao != storage.EspelhoRecusado || e.Motivo != "tipo_ainda_nao_sincroniza" ||
		!reflect.DeepEqual(e.Projecao, p) || e.ServerVersion != nil || !e.MudouAqui() {
		t.Errorf("o recusado: %+v", e)
	}
	if len(f.marcadores(t)) != 0 {
		t.Error("o marcador do recusado ficou")
	}
}

// O Aprender e o Desfazer do painel sobem a versão local no ato, só de quem
// já tem linha no espelho.
func TestAprendido_SubirVersaoLocal(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	p := projecao("n oito n", "n8n")
	semLinha := projecao("sf", "ECF")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), Base: &storage.BaseDoAprendido{ServerVersion: 2}})
	if err := f.fila.SubirVersaoLocal(ctx, []string{p.ID, semLinha.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.fila.SubirVersaoLocal(ctx, []string{p.ID}); err != nil {
		t.Fatal(err)
	}
	es := espelhoPorID(t, f)
	if e := es[p.ID]; e.VersaoLocal != 2 || e.VersaoBase != 0 || !e.MudouAqui() || !e.Presente() {
		t.Errorf("depois do Desfazer e do Aprender: %+v", e)
	}
	if _, ok := es[semLinha.ID]; ok {
		t.Error("o id sem linha ganhou linha")
	}
}

// A resposta da nuvem aplica o aprendido na mesma transação dos ditados e do
// cursor: o epoch de outra nuvem não aplica nada; a ação do aprendido que
// falha desfaz também o ditado e o cursor.
func TestAprendido_AplicarComAprendidoNaMesmaTransacao(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraDaFila)
	preparado(t, f)
	p := projecao("n oito n", "n8n")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Marcador: storage.MarcadorCriar})

	apagar := func(storage.EstadoDoItem) storage.AcaoLocal { return storage.AcaoLocal{ApagarLinha: true} }
	aplicado := storage.RespostaDoAprendido{IDs: []string{p.ID}, Agora: agoraDoAprendido,
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 1}, Cobre: &e.VersaoLocal,
				Marcador: storage.MarcadorSai}
		}}
	cursor := int64(9)
	if err := f.fila.AplicarComAprendido(ctx, "a1", "e1", []string{idA}, &cursor, apagar, aplicado); err != nil {
		t.Fatal(err)
	}
	if e := espelhoPorID(t, f)[p.ID]; e.ServerVersion == nil || *e.ServerVersion != 1 || e.MudouAqui() || e.Marcador != nil {
		t.Errorf("o aprendido aplicado: %+v", e)
	}
	// Outro epoch: nada.
	recusar := storage.RespostaDoAprendido{IDs: []string{p.ID}, Agora: agoraDoAprendido,
		Decidir: func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Situacao: situacao(storage.EspelhoRecusado, "campo_invalido")}
		}}
	if err := f.fila.AplicarComAprendido(ctx, "a1", "e2", nil, nil, nil, recusar); !errors.Is(err, storage.ErrEpochDiferente) {
		t.Fatalf("outro epoch: %v", err)
	}
	if e := espelhoPorID(t, f)[p.ID]; e.Situacao != storage.EspelhoNenhuma {
		t.Errorf("outro epoch aplicou: %+v", e)
	}
	// A ação do aprendido que falha: o ditado e o cursor também voltam.
	f.ditar(t, idB, agoraDaFila)
	ruim := storage.RespostaDoAprendido{IDs: []string{p.ID}, Agora: agoraDoAprendido,
		Decidir: func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Situacao: situacao("inventada", "")}
		}}
	outro := int64(20)
	if err := f.fila.AplicarComAprendido(ctx, "a1", "e1", []string{idB}, &outro, apagar, ruim); !errors.Is(err, storage.ErrAcaoDoAprendido) {
		t.Fatalf("a ação ruim: %v", err)
	}
	if _, err := f.repo.Get(ctx, donoDaFila, idB); err != nil {
		t.Errorf("o ditado foi apagado com a ação ruim: %v", err)
	}
	if est, _, err := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila); err != nil || est.Cursor != 9 {
		t.Errorf("o cursor andou com a ação ruim: %+v, %v", est, err)
	}
	// Os ids do aprendido sem a decisão são recusados antes da transação.
	if err := f.fila.AplicarComAprendido(ctx, "a1", "e1", nil, nil, nil,
		storage.RespostaDoAprendido{IDs: []string{p.ID}}); !errors.Is(err, storage.ErrAcaoDoAprendido) {
		t.Errorf("sem a decisão: %v", err)
	}
}

// O cenário do esquecer e do epoch novo: cada tipo de linha do espelho, como
// as decisões da F8f as deixam.
type cenarioDoAprendido struct {
	aplicado, desfeitoAqui, naoAplicado, etiqueta, aplicando, recusado, desfeitoConfirmado, nuncaSubiu, segurado string
	esperadaDoAplicando                                                                                          *dictionary.ParDoPainel
}

func semearCenario(t *testing.T, f filaDeTeste) cenarioDoAprendido {
	t.Helper()
	ctx := context.Background()
	var c cenarioDoAprendido
	base := func(v int64, desfeita bool) *storage.BaseDoAprendido {
		return &storage.BaseDoAprendido{ServerVersion: v, Desfeita: desfeita}
	}

	// Aplicado: veio da nuvem e vale no arquivo.
	p := projecao("sf", "ECF")
	c.aplicado = p.ID
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), Base: base(3, false),
		Situacao: situacao(storage.EspelhoAplicado, ""), VeioDaNuvem: storage.VeioEntra})

	// Desfeito aqui: aprendido aqui, confirmado, e desfeito; o delete na fila.
	p = projecao("dctf web", "DCTFWeb")
	c.desfeitoAqui = p.ID
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Base: base(4, false), Cobre: versao(1)})
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(nil), MudouAqui: true, Marcador: storage.MarcadorCriar,
		Situacao: situacao(storage.EspelhoDesfeitoAqui, "")})

	// Não aplicado: o par da nuvem, preso a um domínio que este PC não tem.
	c.naoAplicado = dictionary.IDDoPar("lalur")
	mudar(t, f, c.naoAplicado, storage.AcaoDoAprendido{
		Nuvem: &storage.ParGuardado{Versao: versao(7), Par: dictionary.ParDaNuvem{Variante: "lalur", Termo: "LALUR",
			Restrito: true, Dominio: "contabil"}},
		Situacao: situacao(storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)),
	})

	// A etiqueta "desfeito em outro aparelho": a mudança daqui (o termo que
	// este PC tinha, o par perdido) perdeu.
	c.etiqueta = dictionary.IDDoPar("e social")
	mudar(t, f, c.etiqueta, storage.AcaoDoAprendido{
		Base:     base(9, true),
		Nuvem:    &storage.ParGuardado{Versao: versao(9), Par: dictionary.ParDaNuvem{Variante: "e social", Termo: "eSocial", Desfeito: true}},
		Situacao: situacao(storage.EspelhoDesfeitoEmOutro, ""),
		Perdido:  &storage.ParPerdido{Variante: "e social", Termo: "E-Social"},
	})

	// Aplicando: a intenção (trocar o termo) gravada antes do arquivo.
	p = projecao("n oito n", "N8N")
	c.aplicando = p.ID
	c.esperadaDoAplicando = projecao("n oito n", "n8n")
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), Base: base(5, false)})
	mudar(t, f, p.ID, storage.AcaoDoAprendido{
		Nuvem:    &storage.ParGuardado{Versao: versao(6), Par: dictionary.ParDaNuvem{Variante: "n oito n", Termo: "n8n"}},
		Situacao: situacao(storage.EspelhoAplicando, ""), Esperada: nova(c.esperadaDoAplicando),
		VeioDaNuvem: storage.VeioEntra,
	})

	// Recusado: a nuvem recusou o upsert daqui.
	p = projecao("simples nacional", "Simples Nacional")
	c.recusado = p.ID
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true, Base: base(2, false),
		Situacao: situacao(storage.EspelhoRecusado, "campo_invalido")})

	// O desfazer confirmado pela nuvem: só o id.
	c.desfeitoConfirmado = dictionary.IDDoPar("efd reinf")
	mudar(t, f, c.desfeitoConfirmado, storage.AcaoDoAprendido{Base: base(8, true), MudouAqui: true, Cobre: versao(1)})

	// Aprendido aqui, nunca subiu, e desfeito: nenhuma versão da nuvem.
	p = projecao("pis cofins", "PIS/COFINS")
	c.nuncaSubiu = p.ID
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true})
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(nil), MudouAqui: true})

	// Aprendido aqui, sem marcador (segurado antes do aceite).
	p = projecao("ir pj", "IRPJ")
	c.segurado = p.ID
	mudar(t, f, p.ID, storage.AcaoDoAprendido{Projecao: nova(p), MudouAqui: true})

	if es, err := f.fila.Espelho(ctx); err != nil || len(es) != 9 {
		t.Fatalf("o cenário: %d linhas, %v", len(es), err)
	}
	return c
}

// O "Esquecer esta nuvem" no espelho: a base e a situação saem (menos na
// linha "aplicando", que fica com a projeção esperada), o "desfeito aqui" fica
// só com o id e a conta, o texto do não aplicado, do pendente e das
// etiquetas sai (fica o id), e o "veio da nuvem" fica inteiro. A pergunta do
// esquecer conta o aprendido que não subiu.
func TestEsquecer_Aprendido(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	c := semearCenario(t, f)
	veioAntes := veioDaNuvem(t, f)
	if len(veioAntes) != 2 {
		t.Fatalf("o veio da nuvem do cenário: %v", veioAntes)
	}
	// Um upsert também na fila: a pergunta conta os dois marcadores.
	mudar(t, f, c.segurado, storage.AcaoDoAprendido{Marcador: storage.MarcadorCriar})
	r, err := e.fila.Resumo(ctx, agoraDoEnvio)
	if err != nil || r.Mudancas != 2 || r.Pendentes != 0 {
		t.Fatalf("a pergunta do esquecer antes: mudanças %d, pendentes %d, %v; quero 2 e 0", r.Mudancas, r.Pendentes, err)
	}
	antes := espelhoPorID(t, f)

	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	depois := espelhoPorID(t, f)
	if len(depois) != len(antes) {
		t.Fatalf("o espelho perdeu linhas: %d antes, %d depois", len(antes), len(depois))
	}
	for id, d := range depois {
		if d.ServerVersion != nil || d.BaseDesfeita || d.Nuvem != nil || d.Marcador != nil {
			t.Errorf("%s: a base, o par da nuvem ou o marcador ficou: %+v", id, d)
		}
		if id != c.aplicando && (d.Situacao != storage.EspelhoNenhuma || d.Motivo != "" || d.SituacaoEm != nil || d.Esperada != nil) {
			t.Errorf("%s: a situação ficou: %+v", id, d)
		}
		// As versões locais ficam: o "mudou aqui" sobrevive.
		if d.VersaoLocal != antes[id].VersaoLocal || d.VersaoBase != antes[id].VersaoBase {
			t.Errorf("%s: as versões locais mudaram", id)
		}
		// A projeção guardada é o que o arquivo tem: fica.
		if !reflect.DeepEqual(d.Projecao, antes[id].Projecao) {
			t.Errorf("%s: a projeção guardada mudou", id)
		}
	}
	// A linha "aplicando" fica com a situação e a projeção esperada.
	if d := depois[c.aplicando]; d.Situacao != storage.EspelhoAplicando || !reflect.DeepEqual(d.Esperada, c.esperadaDoAplicando) {
		t.Errorf("a linha aplicando: %+v", d)
	}
	// O "desfeito aqui", o não aplicado, a etiqueta e o desfazer confirmado:
	// só o id.
	for nome, id := range map[string]string{"desfeito aqui": c.desfeitoAqui, "não aplicado": c.naoAplicado,
		"etiqueta": c.etiqueta, "desfeito confirmado": c.desfeitoConfirmado, "nunca subiu": c.nuncaSubiu} {
		semTexto(t, f, id, nome)
	}
	// O "desfeito aqui" guarda a conta a que pertence, e a linha presente
	// mudada aqui que a nuvem tinha (o recusado; também o Desfazer que a
	// varredura não viu) também; o que não mudou aqui, ou que a nuvem nunca
	// teve, não.
	for id, l := range map[string]map[string]any{c.desfeitoAqui: linhaCrua(t, f, c.desfeitoAqui),
		c.nuncaSubiu: linhaCrua(t, f, c.nuncaSubiu), c.recusado: linhaCrua(t, f, c.recusado)} {
		if l["conta_esquecida"] != "c1" {
			t.Errorf("%s: a mudança daqui não guardou a conta: %v", id, l["conta_esquecida"])
		}
	}
	for _, id := range []string{c.aplicado, c.naoAplicado, c.etiqueta, c.aplicando, c.desfeitoConfirmado, c.segurado} {
		if l := linhaCrua(t, f, id); l["conta_esquecida"] != nil {
			t.Errorf("%s: guardou a conta sem ser mudança daqui que a nuvem tinha", id)
		}
	}
	if !reflect.DeepEqual(veioDaNuvem(t, f), veioAntes) {
		t.Errorf("o veio da nuvem mudou: %v", veioDaNuvem(t, f))
	}
	if n := len(linhasDaTabela(t, f.db, "sync_outbox")); n != 0 {
		t.Errorf("a fila ficou com %d marcadores", n)
	}
	if r, _ := e.fila.Resumo(ctx, agoraDoEnvio); r.Mudancas != 0 {
		t.Errorf("a pergunta depois do esquecer: %d", r.Mudancas)
	}
}

// O esquecer entre o arquivo e o espelho: a linha "aplicando" sobrevive com
// a projeção esperada e o id no "veio da nuvem"; na conta nova, a primeira
// rodada não a marca, a reconciliação da varredura (a confirmação, que a
// F8f3 manda quando o arquivo é igual à esperada) não põe marcador, e o id
// continua no "veio da nuvem": o par da conta esquecida não sobe para a nova,
// nem depois do clique. O "desfeito aqui" da conta antiga é solto.
func TestEsquecer_AplicandoSobreviveENaoSobeParaAContaNova(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	c := semearCenario(t, f)
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}

	conectarNaConta(t, f, "c2")
	if _, inicio, err := f.fila.Preparar(ctx, "a1", "p1", agoraDoEnvio); err != nil || inicio != storage.InicioRecomeco {
		t.Fatalf("a primeira rodada da conta nova: %v, %v", inicio, err)
	}
	es := espelhoPorID(t, f)
	if a := es[c.aplicando]; a.Situacao != storage.EspelhoAplicando || !reflect.DeepEqual(a.Esperada, c.esperadaDoAplicando) ||
		!a.VeioDaNuvem || a.Marcador != nil {
		t.Fatalf("a linha aplicando na conta nova: %+v", a)
	}
	// O desfeito aqui da conta antiga é solto: não é mais mudança daqui.
	for _, id := range []string{c.desfeitoAqui, c.nuncaSubiu} {
		if d := es[id]; d.MudouAqui() || linhaCrua(t, f, id)["conta_esquecida"] != nil {
			t.Errorf("%s: o desfeito da conta antiga não foi solto: %+v", id, d)
		}
	}
	// O que foi aprendido ou mudado aqui continua mudado aqui: sobe com o
	// clique. A marca do esquecer sai (a conta é outra).
	for _, id := range []string{c.segurado, c.recusado} {
		if d := es[id]; !d.MudouAqui() || !d.Presente() || d.MesmaContaEsquecida ||
			linhaCrua(t, f, id)["conta_esquecida"] != nil {
			t.Errorf("o mudado aqui na conta nova: %+v", d)
		}
	}

	// A varredura acha o arquivo igual à esperada e confirma, sem envio.
	if err := f.fila.MudarAprendido(ctx, agoraDoEnvio, []string{c.aplicando}, func(a storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Projecao: nova(a.Esperada), Cobre: &a.VersaoLocal,
			Situacao: situacao(storage.EspelhoAplicado, ""), LimparNuvem: true}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	a := espelhoPorID(t, f)[c.aplicando]
	if !a.VeioDaNuvem || a.Marcador != nil || a.ServerVersion != nil || a.Situacao != storage.EspelhoAplicado ||
		!reflect.DeepEqual(a.Projecao, c.esperadaDoAplicando) {
		t.Errorf("a linha confirmada: %+v", a)
	}
	if itens, err := f.fila.PendentesDoAprendido(ctx, 10); err != nil || len(itens) != 0 {
		t.Errorf("subiu para a conta nova: %v, %v", itens, err)
	}
}

// De volta à mesma conta, o "desfeito aqui" que o esquecer guardou continua
// mudado aqui (a varredura põe o delete quando o pull trouxer a versão).
func TestEsquecer_DesfeitoAquiVoltaAMesmaConta(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	c := semearCenario(t, f)
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	conectarNaConta(t, f, "c1")
	preparado(t, f)
	d := espelhoPorID(t, f)[c.desfeitoAqui]
	if !d.DesfeitoAqui() || d.Presente() || linhaCrua(t, f, c.desfeitoAqui)["conta_esquecida"] != nil {
		t.Errorf("o desfeito aqui de volta à mesma conta: %+v", d)
	}
	semTexto(t, f, c.desfeitoAqui, "desfeito aqui")
	// Sem a versão da nuvem, nada nasce na fila: o delete espera o pull.
	if n := len(linhasDaTabela(t, f.db, "sync_outbox")); n != 0 {
		t.Errorf("a primeira rodada pôs %d marcadores", n)
	}
	// A linha presente mudada aqui que a nuvem tinha fica com a marca (a
	// varredura não a assenta se vir o par sair), até ganhar uma projeção
	// vista agora; as outras, não.
	es := espelhoPorID(t, f)
	for id, d := range es {
		if d.MesmaContaEsquecida != (id == c.recusado) {
			t.Errorf("%s: a marca do esquecer de volta à mesma conta: %v", id, d.MesmaContaEsquecida)
		}
	}
	if r := es[c.recusado]; !r.MudouAqui() || !r.Presente() {
		t.Errorf("o recusado de volta à mesma conta: %+v", r)
	}
	mudar(t, f, c.recusado, storage.AcaoDoAprendido{Projecao: nova(nil), MudouAqui: true})
	if r := espelhoPorID(t, f)[c.recusado]; r.MesmaContaEsquecida || linhaCrua(t, f, c.recusado)["conta_esquecida"] != nil {
		t.Errorf("a projeção vista agora não tirou a marca: %+v", r)
	}
}

// O par aprendido aqui que a conta esquecida confirmou: numa conta nova, ele
// passa a ser mudança daqui (sobe com o clique, e o pull da conta nova o pula
// até lá); de volta à mesma conta, não (o pull do cursor 0 o reconcilia). O
// par do "veio da nuvem" nunca. Nos dois casos, a marca do esquecer sai.
func TestEsquecer_OAprendidoAquiNaContaNova(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		conta  string
		mudado bool
	}{{"c2", true}, {"c1", false}} {
		t.Run(c.conta, func(t *testing.T) {
			e := novoEnvioDeTeste(t)
			f := e.filaDeTeste
			conectarNaConta(t, f, "c1")
			aqui := projecao("sf", "ECF")
			mudar(t, f, aqui.ID, storage.AcaoDoAprendido{Projecao: nova(aqui), MudouAqui: true,
				Base: &storage.BaseDoAprendido{ServerVersion: 1}, Cobre: versao(1)})
			veio := projecao("n oito n", "n8n")
			mudar(t, f, veio.ID, storage.AcaoDoAprendido{Projecao: nova(veio), Base: &storage.BaseDoAprendido{ServerVersion: 2},
				Situacao: situacao(storage.EspelhoAplicado, ""), VeioDaNuvem: storage.VeioEntra})
			if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
				t.Fatal(err)
			}
			if linhaCrua(t, f, aqui.ID)["conta_esquecida"] != "c1" || linhaCrua(t, f, veio.ID)["conta_esquecida"] != nil {
				t.Fatal("o esquecer não guardou a conta do aprendido aqui (ou guardou a do veio da nuvem)")
			}
			conectarNaConta(t, f, c.conta)
			preparado(t, f)
			es := espelhoPorID(t, f)
			if d := es[aqui.ID]; d.MudouAqui() != c.mudado || !d.Presente() {
				t.Errorf("o aprendido aqui na conta %s: mudou aqui %v, quero %v", c.conta, d.MudouAqui(), c.mudado)
			}
			if d := es[veio.ID]; d.MudouAqui() {
				t.Errorf("o par do veio da nuvem virou mudança daqui: %+v", d)
			}
			for _, id := range []string{aqui.ID, veio.ID} {
				if linhaCrua(t, f, id)["conta_esquecida"] != nil || es[id].MesmaContaEsquecida {
					t.Errorf("%s: a marca do esquecer ficou", id)
				}
			}
		})
	}
}

// O epoch novo (um backup restaurado): todo par do espelho é marcado de novo
// (o presente vira upsert com a base 0, o desfeito vira delete), as versões
// somem e o "recusado" é liberado. A linha "aplicando", o par do "veio da
// nuvem" e o que a nuvem nunca teve ficam de fora. O nada_a_desfazer tira o
// marcador, e o recomeço seguinte não o põe de novo.
func TestRecomecar_Aprendido(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	preparado(t, f)
	c := semearCenario(t, f)
	aceitar(t, f)
	// O delete daqui estava em voo.
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeAprendido, EntityID: c.desfeitoAqui,
		MutationID: "m1", Versao: 2}}); err != nil {
		t.Fatal(err)
	}

	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	es := espelhoPorID(t, f)
	marcados := map[string]bool{}
	for id, d := range es {
		if d.ServerVersion != nil || (d.Nuvem != nil && d.Nuvem.Versao != nil) {
			t.Errorf("%s: uma versão do epoch velho ficou: %+v", id, d)
		}
		if d.Marcador != nil {
			marcados[id] = true
			if d.Marcador.EmVoo() || d.Marcador.Inicial || d.Marcador.Campos != 0 {
				t.Errorf("%s: o marcador do recomeço: %+v", id, d.Marcador)
			}
		}
	}
	// O aplicado está no "veio da nuvem": quem o sobe de novo é o PC de
	// origem, no epoch novo dele.
	quero := map[string]bool{
		c.recusado: true, c.segurado: true, // presentes: upsert
		c.desfeitoAqui: true, c.desfeitoConfirmado: true, c.etiqueta: true, // desfeitos: delete
	}
	if !reflect.DeepEqual(marcados, quero) {
		t.Errorf("marcados: %v\nquero:    %v", marcados, quero)
	}
	for id := range quero {
		if presente := es[id].Presente(); presente != (id == c.recusado || id == c.segurado) {
			t.Errorf("%s: a operação do marcador está errada (presente %v)", id, presente)
		}
	}
	if d := es[c.recusado]; d.Situacao != storage.EspelhoNenhuma || d.Motivo != "" {
		t.Errorf("o recusado não foi liberado: %+v", d)
	}
	if d := es[c.aplicando]; d.Situacao != storage.EspelhoAplicando || d.Esperada == nil || d.Nuvem == nil {
		t.Errorf("a linha aplicando mudou: %+v", d)
	}
	if d := es[c.naoAplicado]; d.Situacao != storage.EspelhoNaoAplicado || d.Nuvem == nil || d.Nuvem.Par.Termo != "LALUR" {
		t.Errorf("o não aplicado perdeu o par (a conta é a mesma): %+v", d)
	}

	// O nada_a_desfazer (o backup não tem a linha): o marcador sai, e o
	// desfazer fica confirmado.
	nada := storage.RespostaDoAprendido{IDs: []string{c.desfeitoAqui, c.desfeitoConfirmado}, Agora: agoraDoEnvio,
		Decidir: func(a storage.EstadoDoAprendido) storage.AcaoDoAprendido {
			return storage.AcaoDoAprendido{Marcador: storage.MarcadorSai, Cobre: &a.VersaoLocal,
				Situacao: situacao(storage.EspelhoNenhuma, "")}
		}}
	if err := f.fila.AplicarComAprendido(ctx, "a1", "e2", nil, nil, nil, nada); err != nil {
		t.Fatal(err)
	}
	itens, err := f.fila.PendentesDoAprendido(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range itens {
		if it.ID == c.desfeitoAqui || it.ID == c.desfeitoConfirmado {
			t.Errorf("%s: o marcador ficou depois do nada_a_desfazer", it.ID)
		}
	}
	if d := espelhoPorID(t, f)[c.desfeitoAqui]; d.DesfeitoAqui() {
		t.Errorf("o desfazer não ficou confirmado: %+v", d)
	}
	// Sem laço: nem um outro recomeço o põe de novo. (O desfazer que a nuvem
	// tinha confirmado antes, com a base desfeita, vai uma vez em cada epoch
	// novo, e nunca a cada rodada.)
	preparado(t, f)
	if _, ok := f.marcadores(t)[c.desfeitoAqui]; ok {
		t.Error("o delete voltou à fila na rodada seguinte")
	}
	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	ms := f.marcadores(t)
	if _, ok := ms[c.desfeitoAqui]; ok {
		t.Error("o delete voltou à fila no recomeço seguinte")
	}
	if _, ok := ms[c.desfeitoConfirmado]; !ok {
		t.Error("o desfazer confirmado não foi de novo no epoch novo")
	}
}

// O epoch novo antes do aceite (D3): o par presente não ganha o upsert (a
// varredura o conta, e o clique o solta), e o desfeito que a nuvem tinha
// ganha o delete.
func TestRecomecar_AprendidoAntesDoAceite(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	presente := projecao("sf", "ECF")
	desfeito := projecao("dctf web", "DCTFWeb")
	mudar(t, f, presente.ID, storage.AcaoDoAprendido{Projecao: nova(presente),
		Base: &storage.BaseDoAprendido{ServerVersion: 3}, VeioDaNuvem: storage.VeioEntra})
	mudar(t, f, desfeito.ID, storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 4},
		Projecao: nova(nil), MudouAqui: true})
	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
	ms := f.marcadores(t)
	if _, ok := ms[presente.ID]; ok {
		t.Error("o upsert nasceu antes do aceite")
	}
	if _, ok := ms[desfeito.ID]; !ok {
		t.Error("o delete do que a nuvem tinha não nasceu")
	}
}

// A pergunta do esquecer conta só a troca mudada aqui que não subiu (a versão
// local acima da base), como no ditado: o marcador que o epoch novo põe num
// par que a nuvem já tinha confirmado (aprendido aqui, ou desfeito por outro
// aparelho) não é mudança feita aqui.
func TestEsquecer_APerguntaNaoContaORecomeco(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	preparado(t, f)
	aceitar(t, f)
	confirmado := projecao("sf", "ECF")
	mudar(t, f, confirmado.ID, storage.AcaoDoAprendido{Projecao: nova(confirmado), MudouAqui: true,
		Base: &storage.BaseDoAprendido{ServerVersion: 1}, Cobre: versao(1)})
	desfeitoLa := dictionary.IDDoPar("efd reinf")
	mudar(t, f, desfeitoLa, storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 8, Desfeita: true}})
	mudado := projecao("ir pj", "IRPJ")
	mudar(t, f, mudado.ID, storage.AcaoDoAprendido{Projecao: nova(mudado), MudouAqui: true, Marcador: storage.MarcadorCriar})

	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 3 {
		t.Fatalf("o recomeço marcou %d, quero 3", len(ms))
	}
	if r, err := e.fila.Resumo(ctx, agoraDoEnvio); err != nil || r.Mudancas != 1 {
		t.Errorf("a pergunta do esquecer: %d mudanças, %v; quero 1 (só a mudada aqui)", r.Mudancas, err)
	}
}

// O epoch novo não sobe o par que veio da nuvem (no "veio da nuvem"): ele
// pode ser de uma conta esquecida, ou estar com a adaptação deste PC, e cada
// PC de origem marca de novo as próprias trocas no epoch novo dele. O par
// chega da conta c1, o PC esquece c1, conecta c2, o envio é aceito, e c2 ganha
// um epoch novo (um backup restaurado): o par de c1 não sobe para c2, e o
// aprendido aqui sobe.
func TestRecomecar_OVeioDaNuvemNaoSobe(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	f := e.filaDeTeste
	conectarNaConta(t, f, "c1")
	preparado(t, f)
	daC1 := projecao("n oito n", "n8n")
	mudar(t, f, daC1.ID, storage.AcaoDoAprendido{Projecao: nova(daC1), Base: &storage.BaseDoAprendido{ServerVersion: 3},
		Situacao: situacao(storage.EspelhoAplicado, ""), VeioDaNuvem: storage.VeioEntra})
	aqui := projecao("sf", "ECF")
	mudar(t, f, aqui.ID, storage.AcaoDoAprendido{Projecao: nova(aqui), MudouAqui: true,
		Base: &storage.BaseDoAprendido{ServerVersion: 1}, Cobre: versao(1)})
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	conectarNaConta(t, f, "c2")
	preparado(t, f)
	aceitar(t, f)

	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	itens, err := f.fila.PendentesDoAprendido(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	subiriam := map[string]bool{}
	for _, it := range itens {
		subiriam[it.ID] = true
	}
	if subiriam[daC1.ID] {
		t.Error("o par da conta esquecida subiria para a conta nova no epoch novo")
	}
	if !subiriam[aqui.ID] {
		t.Error("o par aprendido aqui não subiria no epoch novo")
	}
}

// O CompletarEnvio sem ditado elegível só grava o aceite sozinho sem troca a
// subir (M = 0): com M > 0, a rodada espera o clique, e o clique com N = 0
// grava o aceite sem cópia.
func TestEnvio_CompletarComTrocasEsperaOClique(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(9), criado: agoraDoEnvio.Add(-time.Hour), lixeira: true}) // inelegível
	if n, err := e.fila.CompletarEnvio(ctx, agoraDoEnvio, 2); err != nil || n != 0 {
		t.Fatalf("com M = 2: %d, %v", n, err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); ok {
		t.Fatal("o aceite foi gravado com trocas a subir")
	}
	if r, _ := e.fila.Resumo(ctx, agoraDoEnvio); r.Aceito || r.Previa != 0 {
		t.Errorf("o resumo com M > 0: %+v", r)
	}
	r, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio)
	if err != nil || r.Marcados != 0 || r.Copia != "" || r.JaAceito {
		t.Fatalf("o clique com N = 0: %+v, %v", r, err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); !ok {
		t.Error("o clique com N = 0 não gravou o aceite")
	}
	if len(copiasDoEnvio(t, e.caminho)) != 0 {
		t.Error("o clique com N = 0 fez cópia")
	}

	// Com M = 0, sem ditado elegível: o aceite sozinho, como antes.
	e2 := novoEnvioDeTeste(t)
	if _, err := e2.fila.CompletarEnvio(ctx, agoraDoEnvio, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := e2.chave(t, storage.ChaveNuvemEnvioAceitoEm); !ok {
		t.Error("sem nada a subir, o aceite não foi gravado")
	}
}
