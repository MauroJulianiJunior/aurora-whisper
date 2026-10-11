package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O "Esquecer esta nuvem" no banco (ADR-019, "Decisões da F7c"): o que sai
// (as chaves da conexão e do envio, o cursor, a fila e, nas linhas, só o que
// é do sync) e o que fica (todo ditado, coluna a coluna, fora as três do
// sync; o dono, o install_id, as preferências e a cópia).

// colunasDoSync são as três colunas que o esquecer limpa nas linhas.
var colunasDoSync = []string{"server_version", "sync_state", "last_sync_error"}

func TestEsquecer_OQueSaiEOQueFica(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	agora := agoraDoEnvio
	// Um banco conectado e em uso: ditados daqui (um já subiu, um com selo
	// e escolha na fila, um na lixeira, um recusado, um fora da janela), um
	// de outro PC, a instalação inteira, o cursor, a fila e as preferências.
	e.semear(t, semente{id: id(1), criado: agora.Add(-time.Hour), servidor: versao(3)})
	e.semear(t, semente{id: id(2), criado: agora.Add(-2 * time.Hour), servidor: versao(5)})
	e.semear(t, semente{id: id(3), criado: agora.Add(-3 * time.Hour), lixeira: true, servidor: versao(2)})
	e.semear(t, semente{id: id(4), criado: agora.Add(-4 * time.Hour), situacao: storage.SituacaoRecusado})
	e.semear(t, semente{id: id(5), criado: agora.AddDate(0, -7, 0), situacao: storage.SituacaoForaDaJanela, servidor: versao(1)})
	e.semear(t, semente{id: id(6), criado: agora.Add(-5 * time.Hour), aparelho: id(900), servidor: versao(9)})
	e.semear(t, semente{id: id(7), criado: agora.Add(-6 * time.Hour)})
	executar(t, e.db, `UPDATE local_transcriptions SET last_sync_error = 'texto_grande' WHERE id = ?`, id(4))
	executar(t, e.db, `UPDATE local_transcriptions SET conflito_texto = 'a outra', conflito_em = ? WHERE id = ?`,
		emTextoDeTeste(agora), id(2))
	executar(t, e.db, `INSERT INTO instalacao (nome, valor) VALUES ('dono_local', ?), ('install_id', 'i1'),
		('nuvem_url', 'http://localhost:8080'), ('nuvem_conta', 'c1'), ('nuvem_email', 'voce@teste.local'),
		('nuvem_nome', 'PC'), ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1'), ('nuvem_envio_aceito_em', 'x'),
		('nuvem_envio_total', '3'), ('nuvem_copia', 'historico.db.antes-da-nuvem-x.db')`, donoDaFila)
	executar(t, e.db, `INSERT INTO local_settings (id, hotkey, hold_threshold_ms, overlay_position, asr_policy,
		protected_field_policy, local_port, updated_at, text_mode) VALUES (1, 'ctrl+win', 300, 'bottom', 'local_only', 'block', 0, 'x', 'smart')`)
	// O cursor direto no banco (o Preparar de uma primeira rodada recomeçaria
	// e apagaria as versões).
	executar(t, e.db, `INSERT INTO sync_state (user_id, device_id, last_server_seq, sync_epoch, pareado_em)
		VALUES (?, 'a1', 42, 'e1', 'p1')`, donoDaFila)
	executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, mutation_id, selo_escolha, selo_em)
		VALUES ('transcription', ?, 1, 'x', 'm1', 'trocar', 'y') ON CONFLICT DO NOTHING`, id(2))
	copia := filepath.Join(filepath.Dir(e.caminho), "historico.db.antes-da-nuvem-20261006-120000.db")
	if err := os.WriteFile(copia, []byte("a cópia"), 0o600); err != nil {
		t.Fatal(err)
	}

	antes := e.todasAsLinhas(t)
	comVersao := 0
	for _, l := range antes {
		if l["server_version"] != nil {
			comVersao++
		}
	}
	if comVersao != 5 {
		t.Fatalf("a semeadura: %d ditados com a versão da nuvem; quero 5", comVersao)
	}
	prefs := linhasDaTabela(t, e.db, "local_settings")
	if err := e.fila.EsquecerNuvem(ctx, agora); err != nil {
		t.Fatal(err)
	}

	depois := e.todasAsLinhas(t)
	if len(depois) != len(antes) {
		t.Fatalf("%d ditados antes, %d depois", len(antes), len(depois))
	}
	for i := range depois {
		if depois[i]["server_version"] != nil || depois[i]["sync_state"] != storage.SituacaoNormal || depois[i]["last_sync_error"] != nil {
			t.Errorf("ditado %v: o sync ficou: %v %v %v", depois[i]["id"], depois[i]["server_version"],
				depois[i]["sync_state"], depois[i]["last_sync_error"])
		}
		for _, c := range colunasDoSync {
			delete(depois[i], c)
			delete(antes[i], c)
		}
	}
	if !iguais(antes, depois) {
		t.Errorf("o esquecer mudou o conteúdo de um ditado:\nantes  %v\ndepois %v", antes, depois)
	}
	var chaves []string
	rows, err := e.db.Query(`SELECT nome FROM instalacao ORDER BY nome`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		chaves = append(chaves, n)
	}
	rows.Close()
	sort.Strings(chaves)
	if got := strings.Join(chaves, ","); got != "dono_local,install_id,nuvem_ditados_em_outra,nuvem_esquecida_em" {
		t.Errorf("a instalação depois: %s", got)
	}
	for _, tb := range []string{"sync_state", "sync_outbox"} {
		if n := len(linhasDaTabela(t, e.db, tb)); n != 0 {
			t.Errorf("%s ficou com %d linhas", tb, n)
		}
	}
	// A escolha do selo que estava na fila é a única mudança daqui que a
	// nuvem não recebeu: só ela fica anotada.
	if got := anotados(t, e); !reflect.DeepEqual(got, []string{id(2)}) {
		t.Errorf("o anotado como mudado aqui: %v; quero só o %s", got, id(2))
	}
	if !iguais(prefs, linhasDaTabela(t, e.db, "local_settings")) {
		t.Error("as preferências mudaram")
	}
	if _, err := os.Stat(copia); err != nil {
		t.Errorf("a cópia sumiu: %v", err)
	}
	// O selo ficou (a escolha que estava na fila, não: é só daqui agora).
	if l := e.linha(t, id(2)); l.Conflito == nil || l.Conflito.FinalText != "a outra" {
		t.Errorf("o selo: %+v", l.Conflito)
	}

	// Desconectado e esquecido: a mudança num ditado que a nuvem tinha não
	// entra mais na fila; e, conectado de novo (a outra conta), o ditado de
	// outro PC não sobe e o daqui só pelo envio (que o vê na prévia).
	if _, err := e.repo.UpdateFinalText(ctx, donoDaFila, id(1), e.linha(t, id(1)).Version, "depois de esquecer"); err != nil {
		t.Fatal(err)
	}
	if len(e.marcadores(t)) != 0 {
		t.Errorf("depois de esquecer, a edição entrou na fila: %v", e.marcadores(t))
	}
	e.conectar(t)
	if _, err := e.repo.UpdateFinalText(ctx, donoDaFila, id(6), e.linha(t, id(6)).Version, "de outro PC, editado"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.marcadores(t)[id(6)]; ok {
		t.Error("o ditado de outro PC entrou na fila da conta nova")
	}
	n, err := e.fila.Previa(ctx, agora)
	if err != nil || n != 4 {
		// id 1, 2, 4 e 7: os daqui, na janela, fora da lixeira (o 4, que a
		// nuvem antiga recusou, a nova decide de novo).
		t.Errorf("a prévia para a conta nova: %d (%v); quero 4", n, err)
	}
	r, _ := e.fila.Resumo(ctx, agora)
	if !r.OutraNuvem || r.Aceito {
		t.Errorf("o resumo depois de esquecer: %+v", r)
	}
}

// anotados devolve os ids que o esquecer anotou como mudados aqui
// (sync_mudou_aqui), em ordem.
func anotados(t *testing.T, e envioDeTeste) []string {
	t.Helper()
	var out []string
	for _, l := range linhasDaTabela(t, e.db, "sync_mudou_aqui") {
		if l["entity_type"] != storage.EntidadeDitado {
			t.Errorf("anotado com outro tipo: %v", l)
		}
		out = append(out, l["entity_id"].(string))
	}
	sort.Strings(out)
	return out
}

// mudouAqui lê, pelo aplicador, o EstadoDoItem.MudouAqui de cada id, como o
// pull de volta à mesma conta o veria.
func mudouAqui(t *testing.T, e envioDeTeste, ids ...string) map[string]bool {
	t.Helper()
	preparado(t, e.filaDeTeste)
	vistos := map[string]bool{}
	ver := func(st storage.EstadoDoItem) storage.AcaoLocal {
		vistos[st.ID] = st.MudouAqui
		return storage.AcaoLocal{}
	}
	if err := e.fila.Aplicar(context.Background(), "a1", "e1", ids, nil, ver); err != nil {
		t.Fatal(err)
	}
	return vistos
}

// A mudança feita aqui que a nuvem não recebeu (o marcador na fila na hora do
// esquecer) não pode ser sobrescrita na volta à mesma conta (revisão da F7c):
// o esquecer a anota, item por item, e ela conta como "mudou aqui". A
// referência é o instante do esquecer, sem recuo (reconferência da F7c): o
// ditado que o PC recebeu da nuvem depois da mudança mais antiga não conta,
// e não vira selo à toa. O marcador do envio inicial num ditado que não
// mudou desde o clique não anota nada. E um segundo esquecer antes da rodada
// que reconciliaria não perde o que o primeiro anotou e ainda não subiu.
func TestEsquecer_AMudancaQueNaoSubiuContaComoMudouAqui(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	pediu := agoraDoEnvio.Add(-time.Hour)                                                          // o "agora" de quem pediu a lixeira
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-3 * time.Hour), servidor: versao(3)}) // vai para a lixeira
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio.Add(-3 * time.Hour), servidor: versao(4)}) // editado
	e.semear(t, semente{id: id(3), criado: agoraDoEnvio.Add(-48 * time.Hour)})                     // do histórico, no envio inicial
	e.semear(t, semente{id: id(4), criado: agoraDoEnvio.Add(-2 * time.Hour), servidor: versao(1)}) // sem mudança
	e.semear(t, semente{id: id(5), criado: agoraDoEnvio.Add(-4 * time.Hour), servidor: versao(2)}) // veio da nuvem depois da lixeira
	executar(t, e.db, `UPDATE local_transcriptions SET updated_at = ? WHERE id = ?`,
		emTextoDeTeste(pediu.Add(30*time.Minute)), id(5))
	executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial) VALUES (?, ?, 0, ?, 1)`,
		storage.EntidadeDitado, id(3), emTextoDeTeste(agoraDoEnvio.Add(-30*time.Minute)))
	// Desconectado: as duas mudanças entram na fila (a nuvem tem os ditados).
	if err := e.repo.SoftDelete(ctx, donoDaFila, id(1), pediu); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.UpdateFinalText(ctx, donoDaFila, id(2), e.linha(t, id(2)).Version, "editado desconectado"); err != nil {
		t.Fatal(err)
	}
	if ms := e.marcadores(t); len(ms) != 3 || ms[id(1)].CriadoEm.Before(agoraDoEnvio) {
		t.Fatalf("a fila antes do esquecer: %v", ms)
	}

	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.chave(t, storage.ChaveNuvemEsquecidaEm); !ok || v != emTextoDeTeste(agoraDoEnvio) {
		t.Fatalf("a referência: %q, %v; quero o instante do esquecer", v, ok)
	}
	if got := anotados(t, e); !reflect.DeepEqual(got, []string{id(1), id(2)}) {
		t.Errorf("anotados: %v; quero a lixeira e a edição", got)
	}

	// De volta à mesma conta: as duas mudanças contam como "mudou aqui"; o
	// ditado sem mudança, o do histórico e o que veio da nuvem, não.
	todos := []string{id(1), id(2), id(3), id(4), id(5)}
	if v := mudouAqui(t, e, todos...); !v[id(1)] || !v[id(2)] || v[id(3)] || v[id(4)] || v[id(5)] {
		t.Errorf("mudou aqui: %v; quero só a 1 e a 2", v)
	}

	// Conectado de novo, o PC esquece outra vez antes da rodada que as
	// subiria (a fila está vazia): elas continuam anotadas.
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.chave(t, storage.ChaveNuvemEsquecidaEm); v != emTextoDeTeste(agoraDoEnvio.Add(time.Hour)) {
		t.Errorf("a referência do segundo esquecer: %q; quero o instante dele", v)
	}
	if v := mudouAqui(t, e, todos...); !v[id(1)] || !v[id(2)] || v[id(3)] || v[id(4)] || v[id(5)] {
		t.Errorf("depois do segundo esquecer, mudou aqui: %v; quero só a 1 e a 2", v)
	}
}

// Dois esquecer separados por semanas, com o PC conectado e reconciliado no
// meio (reconferência da F7c): a mudança, depois do primeiro, num ditado que
// nunca sobe (o de mais de 6 meses) ficava sem a versão da nuvem para
// sempre, e a referência do segundo recuava até ela; todo ditado recebido da
// nuvem nessas semanas passava a contar como "mudou aqui" e, mudado por
// outro aparelho com o PC esquecido, virava selo à toa na volta. Agora a
// referência é o instante do segundo esquecer, e só os próprios ditados
// mudados aqui sem a nuvem receber contam: o velho (que a nuvem não manda,
// e se mandar não sobrescreve a mudança) e o de outro PC mudado aqui (que a
// conta antiga tem). O reconciliado e o recebido, não.
func TestEsquecer_SegundoEsquecerNaoRecuaPeloQueNuncaSobe(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	primeiro := agoraDoEnvio.AddDate(0, 0, -30)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.AddDate(0, -8, 0)})                  // velho
	e.semear(t, semente{id: id(2), criado: primeiro.AddDate(0, 0, -2), servidor: versao(3)}) // vai vir da nuvem
	e.semear(t, semente{id: id(3), criado: primeiro.AddDate(0, 0, -3)})                      // reconciliado
	e.semear(t, semente{id: id(4), criado: primeiro.AddDate(0, 0, -4), aparelho: id(900)})   // de outro PC
	e.semear(t, semente{id: id(5), criado: primeiro.AddDate(0, 0, -5), servidor: versao(1)}) // sem mudança
	e.semear(t, semente{id: id(6), dono: "outro dono", criado: primeiro.AddDate(0, 0, -6)})  // fora do painel
	if err := e.fila.EsquecerNuvem(ctx, primeiro); err != nil {
		t.Fatal(err)
	}
	if got := anotados(t, e); len(got) != 0 {
		t.Fatalf("o primeiro esquecer anotou %v sem nada na fila", got)
	}

	// Nas semanas seguintes, conectado: a mudança no velho (sem a versão da
	// nuvem, para sempre), o ditado recebido da nuvem, o que mudou aqui e foi
	// reconciliado, e a edição do de outro PC.
	editar := func(x string, em time.Time, versaoDaNuvem any) {
		executar(t, e.db, `UPDATE local_transcriptions SET final_text = 'mudado em ' || ?, updated_at = ?, server_version = ?
			WHERE id = ?`, emTextoDeTeste(em), emTextoDeTeste(em), versaoDaNuvem, x)
	}
	editar(id(1), primeiro.AddDate(0, 0, 1), nil)
	editar(id(2), primeiro.AddDate(0, 0, 10), int64(4))
	editar(id(3), primeiro.AddDate(0, 0, 2), int64(7))
	editar(id(4), primeiro.AddDate(0, 0, 3), nil)
	editar(id(6), primeiro.AddDate(0, 0, 4), nil) // de outro dono: o esquecer não o anota
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 2 WHERE id = ?`, id(5))

	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.chave(t, storage.ChaveNuvemEsquecidaEm); v != emTextoDeTeste(agoraDoEnvio) {
		t.Errorf("a referência do segundo esquecer: %q; quero o instante dele, sem recuar até a mudança no velho", v)
	}
	if got := anotados(t, e); !reflect.DeepEqual(got, []string{id(1), id(4)}) {
		t.Errorf("anotados: %v; quero o velho e o de outro PC", got)
	}
	v := mudouAqui(t, e, id(1), id(2), id(3), id(4), id(5))
	if !v[id(1)] || v[id(2)] || v[id(3)] || !v[id(4)] || v[id(5)] {
		t.Errorf("mudou aqui: %v; quero só o velho e o de outro PC", v)
	}

	// Reconciliado depois (a versão de uma nuvem), o anotado sai no esquecer
	// seguinte; o que continua sem ela fica.
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 9 WHERE id = ?`, id(4))
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if got := anotados(t, e); !reflect.DeepEqual(got, []string{id(1)}) {
		t.Errorf("anotados no terceiro esquecer: %v; quero só o velho", got)
	}
}

// Os casos do que o esquecer anota, um por vez, com a referência sempre no
// instante dele: o marcador do envio inicial num ditado que não mudou desde
// o clique não anota (o ditado é velho, e a nuvem não o tem); o mesmo ditado
// editado depois do clique anota; a mudança num ditado que a nuvem tem
// anota, inclusive o marcador sem campo (a escolha do selo); a fila vazia
// não anota; e a mudança com a hora depois do esquecer (o relógio voltou)
// anota, sem levar a referência para o futuro.
func TestEsquecer_AnotadoPorCaso(t *testing.T) {
	clique := agoraDoEnvio.Add(-3 * time.Hour)
	editado := agoraDoEnvio.Add(-90 * time.Minute)
	for _, c := range []struct {
		nome    string
		preparo func(t *testing.T, e envioDeTeste)
		anota   bool
	}{
		{"a fila vazia", func(*testing.T, envioDeTeste) {}, false},
		{"o envio inicial sem mudança desde o clique", func(t *testing.T, e envioDeTeste) {
			executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial) VALUES (?, ?, 0, ?, 1)`,
				storage.EntidadeDitado, id(1), emTextoDeTeste(clique))
		}, false},
		{"o envio inicial editado depois do clique", func(t *testing.T, e envioDeTeste) {
			executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial) VALUES (?, ?, 0, ?, 1)`,
				storage.EntidadeDitado, id(1), emTextoDeTeste(clique))
			executar(t, e.db, `UPDATE local_transcriptions SET updated_at = ? WHERE id = ?`, emTextoDeTeste(editado), id(1))
		}, true},
		{"a escolha do selo num ditado que a nuvem tem", func(t *testing.T, e envioDeTeste) {
			executar(t, e.db, `UPDATE local_transcriptions SET server_version = 2, updated_at = ? WHERE id = ?`, emTextoDeTeste(editado), id(1))
			executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, selo_escolha, selo_em) VALUES (?, ?, 0, ?, 'manter', ?)`,
				storage.EntidadeDitado, id(1), emTextoDeTeste(editado), emTextoDeTeste(clique))
		}, true},
		{"a mudança com a hora depois do esquecer (o relógio voltou)", func(t *testing.T, e envioDeTeste) {
			executar(t, e.db, `UPDATE local_transcriptions SET server_version = 2, updated_at = ? WHERE id = ?`,
				emTextoDeTeste(agoraDoEnvio.Add(time.Hour)), id(1))
			executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, 1, ?)`,
				storage.EntidadeDitado, id(1), emTextoDeTeste(agoraDoEnvio.Add(time.Hour)))
		}, true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			e := novoEnvioDeTeste(t)
			e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-48 * time.Hour)})
			c.preparo(t, e)
			if err := e.fila.EsquecerNuvem(context.Background(), agoraDoEnvio); err != nil {
				t.Fatal(err)
			}
			if v, _ := e.chave(t, storage.ChaveNuvemEsquecidaEm); v != emTextoDeTeste(agoraDoEnvio) {
				t.Errorf("a referência: %q; quero o instante do esquecer", v)
			}
			if got := len(anotados(t, e)) == 1; got != c.anota {
				t.Errorf("anotado: %v; quero %v", got, c.anota)
			}
			if v := mudouAqui(t, e, id(1)); v[id(1)] != c.anota {
				t.Errorf("mudou aqui: %v; quero %v", v[id(1)], c.anota)
			}
		})
	}
}

// Sem nada daqui confirmado pela nuvem esquecida, a pergunta seguinte não
// diz que os ditados "já foram para outra nuvem" (a referência da
// reconciliação é gravada do mesmo jeito).
func TestEsquecer_SemNadaConfirmadoNaoDizOutraNuvem(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour)})
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio.Add(-time.Hour), aparelho: id(900), servidor: versao(2)})
	executar(t, e.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_url', 'u'), ('nuvem_envio_aceito_em', 'x')`)
	if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemDitadosEmOutra); ok {
		t.Error("anotou a outra nuvem sem nenhum ditado daqui confirmado nela")
	}
	if v, ok := e.chave(t, storage.ChaveNuvemEsquecidaEm); !ok || v != emTextoDeTeste(agoraDoEnvio) {
		t.Errorf("a referência da reconciliação: %q, %v", v, ok)
	}
}

// O ditado que o celular criou na nuvem e o PC recebeu (sem aparelho, com a
// versão da nuvem) não é "daqui": confirmado na nuvem esquecida, ele não faz a
// pergunta seguinte dizer que os ditados deste PC "já foram para outra nuvem"
// (ADR-021). O do PC, com a mesma forma, faz: é o controle.
func TestEsquecer_DitadoDoAppNaoContaComoDestePC(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		nome   string
		origem string
		quero  bool
	}{
		{"do PC", "windows", true},
		{"pelo app", storage.PlataformaWeb, false},
		{"pelo app nativo", storage.PlataformaAndroid, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			e := novoEnvioDeTeste(t)
			e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour), servidor: versao(2), origem: c.origem})
			executar(t, e.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_url', 'u'), ('nuvem_envio_aceito_em', 'x')`)
			if err := e.fila.EsquecerNuvem(ctx, agoraDoEnvio); err != nil {
				t.Fatal(err)
			}
			if _, ok := e.chave(t, storage.ChaveNuvemDitadosEmOutra); ok != c.quero {
				t.Errorf("anotou a outra nuvem = %v; quero %v", ok, c.quero)
			}
		})
	}
}
