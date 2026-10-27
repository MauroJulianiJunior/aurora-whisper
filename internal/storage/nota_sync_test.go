package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O sync das notas no PC (ADR-022, "Sync", N7): a emissão do marcador em cada
// gravação local de nota e de marcador, na mesma transação; a lápide local; a
// capacidade; a completação; o que a fila executa das respostas da nuvem; o
// recomeço e o esquecer; e a escolha do selo da nota. Cada teste roda num
// historico.db de verdade, num arquivo da pasta temporária.

type syncDeNotas struct {
	db     *sql.DB
	hist   *storage.SQLiteTranscriptionRepository
	notas  *storage.SQLiteNotaRepository
	fila   *storage.Fila
	avisos *atomic.Int64
}

func novoSyncDeNotas(t *testing.T) *syncDeNotas {
	t.Helper()
	_, db := bancoEmArquivo(t)
	hist := storage.NewSQLiteTranscriptionRepository(db)
	notas := storage.NewSQLiteNotaRepository(db)
	var avisos atomic.Int64
	notas.AoEnfileirar(func() { avisos.Add(1) })
	return &syncDeNotas{db: db, hist: hist, notas: notas, fila: hist.Fila(donoDaFila), avisos: &avisos}
}

func (s *syncDeNotas) conectar(t *testing.T) {
	t.Helper()
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1')
		ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`)
}

func (s *syncDeNotas) capacidade(t *testing.T) {
	t.Helper()
	if _, err := s.fila.VerCapacidade(context.Background(), true, agoraDaFila); err != nil {
		t.Fatal(err)
	}
}

func (s *syncDeNotas) aceitar(t *testing.T) {
	t.Helper()
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, 'x') ON CONFLICT (nome) DO NOTHING`,
		storage.ChaveNuvemEnvioAceitoEm)
}

func (s *syncDeNotas) limparFila(t *testing.T) {
	t.Helper()
	executar(t, s.db, `DELETE FROM sync_outbox`)
}

// naNuvem dá à nota (ou ao marcador) a versão da nuvem: "a nuvem a tem".
func (s *syncDeNotas) naNuvem(t *testing.T, tabela, id string, versao int64) {
	t.Helper()
	executar(t, s.db, `UPDATE `+tabela+` SET server_version = ? WHERE id = ?`, versao, id)
}

func (s *syncDeNotas) nota(t *testing.T, titulo string) storage.Nota {
	t.Helper()
	n, err := s.notas.Criar(context.Background(), storage.NovaNota{UserID: donoDaFila, Titulo: titulo, Corpo: "corpo de " + titulo})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (s *syncDeNotas) marcadorDeNota(t *testing.T, nome string) storage.MarcadorDeNota {
	t.Helper()
	m, err := s.notas.CriarMarcador(context.Background(), donoDaFila, nome)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (s *syncDeNotas) ditado(t *testing.T, id string, versaoDaNuvem int64) {
	t.Helper()
	tr := newTranscription(id, donoDaFila, agoraDaFila.Add(-time.Hour))
	if err := s.hist.Put(tr); err != nil {
		t.Fatal(err)
	}
	if versaoDaNuvem > 0 {
		executar(t, s.db, `UPDATE local_transcriptions SET server_version = ? WHERE id = ?`, versaoDaNuvem, id)
	}
}

func (s *syncDeNotas) get(t *testing.T, id string) storage.Nota {
	t.Helper()
	n, err := s.notas.Get(context.Background(), donoDaFila, id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return n
}

// marca é um marcador da fila, resumido: o tipo, o id e os bits.
type marca struct {
	tipo, id string
	campos   int
}

func (m marca) String() string { return fmt.Sprintf("%s/%s=%d", m.tipo, m.id, m.campos) }

func (s *syncDeNotas) marcas(t *testing.T) []marca {
	t.Helper()
	ms, err := s.fila.Marcadores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []marca
	for _, m := range ms {
		out = append(out, marca{m.EntityType, m.EntityID, m.Campos})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func (s *syncDeNotas) marcadorDaFila(t *testing.T, tipo, id string) (storage.Marcador, bool) {
	t.Helper()
	ms, err := s.fila.Marcadores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.EntityType == tipo && m.EntityID == id {
			return m, true
		}
	}
	return storage.Marcador{}, false
}

func (s *syncDeNotas) contar(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// foto lê as tabelas que as gravações das notas tocam, linha por linha, como
// texto: a prova de que uma gravação que falhou não deixou nada.
func (s *syncDeNotas) foto(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, tb := range []string{"local_notes", "local_labels", "local_note_labels", "local_transcriptions", "sync_outbox"} {
		rows, err := s.db.Query(`SELECT * FROM ` + tb + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%s %v\n", tb, vals)
		}
		rows.Close()
	}
	return b.String()
}

// tempoDoBanco é um instante no formato das colunas de tempo.
const tempoDoBanco = "2026-10-20T10:00:00.000000000Z"

// ─── A emissão: a tabela de todos os caminhos ───

// casoDeEmissao prepara o cenário (conectado, com a fila limpa no fim) e
// devolve a gravação a testar e os marcadores que ela tem de deixar.
type casoDeEmissao struct {
	nome     string
	preparar func(t *testing.T, s *syncDeNotas) (gravar func() error, quer []marca)
}

func casosDeEmissao() []casoDeEmissao {
	ctx := context.Background()
	notaNaNuvem := func(t *testing.T, s *syncDeNotas) storage.Nota {
		n := s.nota(t, "n")
		s.naNuvem(t, "local_notes", n.ID, 5)
		return s.get(t, n.ID)
	}
	return []casoDeEmissao{
		{"Criar", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			return func() error {
				_, err := s.notas.Criar(ctx, storage.NovaNota{UserID: donoDaFila, Corpo: "x"})
				return err
			}, []marca{{storage.EntidadeNota, "<nova>", 0}}
		}},
		{"Atualizar o conteúdo", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{Titulo: ptr(n.Titulo), Corpo: ptr("novo")})
				return err
			}, []marca{{storage.EntidadeNota, n.ID, storage.CampoNotaConteudo}}
		}},
		{"Atualizar a cor", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{MudarCor: true, Cor: "azul"})
				return err
			}, []marca{{storage.EntidadeNota, n.ID, storage.CampoNotaCor}}
		}},
		{"Atualizar: fixar muda de grupo", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{Fixada: ptr(true)})
				return err
			}, []marca{{storage.EntidadeNota, n.ID, storage.CampoNotaFixada | storage.CampoNotaPosicao}}
		}},
		{"Atualizar: arquivar muda de grupo", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{Arquivada: ptr(true)})
				return err
			}, []marca{{storage.EntidadeNota, n.ID, storage.CampoNotaArquivada | storage.CampoNotaPosicao}}
		}},
		{"SoftDelete", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			s.limparFila(t)
			return func() error { return s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila) },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoNotaLixeira}}
		}},
		{"Restaurar", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
				t.Fatal(err)
			}
			s.limparFila(t)
			return func() error { _, err := s.notas.Restaurar(ctx, donoDaFila, n.ID); return err },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoNotaLixeira | storage.CampoNotaPosicao}}
		}},
		{"ApagarDeVez", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
				t.Fatal(err)
			}
			s.limparFila(t)
			return func() error { return s.notas.ApagarDeVez(ctx, donoDaFila, n.ID) },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoApagado}}
		}},
		{"PurgeExpired", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
				t.Fatal(err)
			}
			s.limparFila(t)
			return func() error {
				k, err := s.notas.PurgeExpired(ctx, agoraDaFila.AddDate(0, 0, 31))
				if err == nil && k != 1 {
					return fmt.Errorf("expurgou %d", k)
				}
				return err
			}, []marca{{storage.EntidadeNota, n.ID, storage.CampoApagado}}
		}},
		{"Mover", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			a := notaNaNuvem(t, s)
			b := s.nota(t, "b") // b fica acima de a
			s.limparFila(t)
			return func() error { _, err := s.notas.Mover(ctx, donoDaFila, a.ID, "", b.ID); return err },
				[]marca{{storage.EntidadeNota, a.ID, storage.CampoNotaPosicao}}
		}},
		{"Marcar", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			m := s.marcadorDeNota(t, "casa")
			s.limparFila(t)
			return func() error { _, err := s.notas.Marcar(ctx, donoDaFila, n.ID, m.ID); return err },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoNotaMarcadores}}
		}},
		{"Desmarcar", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			m := s.marcadorDeNota(t, "casa")
			if _, err := s.notas.Marcar(ctx, donoDaFila, n.ID, m.ID); err != nil {
				t.Fatal(err)
			}
			s.limparFila(t)
			return func() error { _, err := s.notas.Desmarcar(ctx, donoDaFila, n.ID, m.ID); return err },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoNotaMarcadores}}
		}},
		{"CriarMarcador", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			return func() error { _, err := s.notas.CriarMarcador(ctx, donoDaFila, "novo"); return err },
				[]marca{{storage.EntidadeMarcadorDeNota, "<nova>", 0}}
		}},
		{"RenomearMarcador", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			m := s.marcadorDeNota(t, "casa")
			s.naNuvem(t, "local_labels", m.ID, 2)
			s.limparFila(t)
			return func() error { _, err := s.notas.RenomearMarcador(ctx, donoDaFila, m.ID, m.Version, "lar"); return err },
				[]marca{{storage.EntidadeMarcadorDeNota, m.ID, storage.CampoMarcadorNome}}
		}},
		{"ApagarMarcador: a lápide e as notas que o tinham", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			m := s.marcadorDeNota(t, "casa")
			s.naNuvem(t, "local_labels", m.ID, 2)
			if _, err := s.notas.Marcar(ctx, donoDaFila, n.ID, m.ID); err != nil {
				t.Fatal(err)
			}
			s.limparFila(t)
			return func() error { return s.notas.ApagarMarcador(ctx, donoDaFila, m.ID) },
				[]marca{{storage.EntidadeMarcadorDeNota, m.ID, storage.CampoApagado}, {storage.EntidadeNota, n.ID, storage.CampoNotaMarcadores}}
		}},
		{"VirarNota", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			s.ditado(t, "d-virar", 3)
			s.limparFila(t)
			return func() error { _, err := s.notas.VirarNota(ctx, donoDaFila, "d-virar"); return err },
				[]marca{{storage.EntidadeNota, "<nova>", 0}, {storage.EntidadeDitado, "d-virar", storage.CampoVirouNota}}
		}},
		{"DesfazerVirarNota (a nota que subiu sai: a lápide)", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			s.ditado(t, "d-desfazer", 3)
			n, err := s.notas.VirarNota(ctx, donoDaFila, "d-desfazer")
			if err != nil {
				t.Fatal(err)
			}
			s.naNuvem(t, "local_notes", n.ID, 1)
			s.limparFila(t)
			return func() error { _, err := s.notas.DesfazerVirarNota(ctx, donoDaFila, n.ID); return err },
				[]marca{{storage.EntidadeNota, n.ID, storage.CampoApagado}, {storage.EntidadeDitado, "d-desfazer", storage.CampoVirouNota}}
		}},
		{"Criar com Token (os ditados da caixa)", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			tr := newTranscription("d-caixa", donoDaFila, agoraDaFila.Add(-time.Hour))
			tr.VirouNota = ptr(storage.PrefixoDaNotaNova + "tk")
			if err := s.hist.Put(tr); err != nil {
				t.Fatal(err)
			}
			executar(t, s.db, `UPDATE local_transcriptions SET server_version = 2 WHERE id = 'd-caixa'`)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.Criar(ctx, storage.NovaNota{UserID: donoDaFila, Corpo: "x", Token: "tk"})
				return err
			}, []marca{{storage.EntidadeNota, "<nova>", 0},
				{storage.EntidadeDitado, "d-caixa", storage.CampoVirouNota}}
		}},
		{"ResolverConflitoDaNota (a escolha do selo)", func(t *testing.T, s *syncDeNotas) (func() error, []marca) {
			n := notaNaNuvem(t, s)
			executar(t, s.db, `UPDATE local_notes SET conflito_titulo = 't2', conflito_corpo = 'c2', conflito_em = ? WHERE id = ?`,
				"2026-10-20T10:00:00.000000000Z", n.ID)
			s.limparFila(t)
			return func() error {
				_, err := s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, n.Version, storage.EscolhaTrocar)
				return err
			}, []marca{{storage.EntidadeNota, n.ID, 0}}
		}},
	}
}

// TestEmissaoDasNotas_TodosOsCaminhos: cada gravação, conectada, deixa os
// marcadores certos (o tipo, o id e os bits), e avisa o laço depois do commit.
// E a prova de que é na MESMA transação: com a fila recusando a escrita (um
// gatilho que aborta), a gravação falha e nada dela fica, nem a linha.
func TestEmissaoDasNotas_TodosOsCaminhos(t *testing.T) {
	for _, c := range casosDeEmissao() {
		t.Run(c.nome, func(t *testing.T) {
			s := novoSyncDeNotas(t)
			s.conectar(t)
			gravar, quer := c.preparar(t, s)
			s.avisos.Store(0)
			if err := gravar(); err != nil {
				t.Fatalf("a gravação: %v", err)
			}
			tem := s.marcas(t)
			// "<nova>" é o id que a gravação gerou: casa com o marcador do tipo que
			// não está entre os ids conhecidos.
			conhecidos := map[string]bool{}
			for _, q := range quer {
				conhecidos[q.id] = true
			}
			for i, q := range quer {
				if q.id != "<nova>" {
					continue
				}
				for _, m := range tem {
					if m.tipo == q.tipo && !conhecidos[m.id] {
						quer[i].id = m.id
					}
				}
			}
			sort.Slice(quer, func(i, j int) bool { return quer[i].String() < quer[j].String() })
			if !reflect.DeepEqual(tem, quer) {
				t.Errorf("a fila:\n tem  %v\n quer %v", tem, quer)
			}
			if s.avisos.Load() == 0 {
				t.Error("a gravação pôs marcador e não avisou o laço")
			}
		})
		t.Run(c.nome+": na mesma transação", func(t *testing.T) {
			s := novoSyncDeNotas(t)
			s.conectar(t)
			gravar, _ := c.preparar(t, s)
			antes := s.foto(t)
			executar(t, s.db, `CREATE TEMP TRIGGER falha_insert BEFORE INSERT ON sync_outbox BEGIN SELECT RAISE(ABORT, 'disco cheio'); END`)
			executar(t, s.db, `CREATE TEMP TRIGGER falha_update BEFORE UPDATE ON sync_outbox BEGIN SELECT RAISE(ABORT, 'disco cheio'); END`)
			s.avisos.Store(0)
			if err := gravar(); err == nil {
				t.Fatal("a gravação passou com a fila recusando a escrita")
			}
			executar(t, s.db, `DROP TRIGGER falha_insert`)
			executar(t, s.db, `DROP TRIGGER falha_update`)
			if depois := s.foto(t); depois != antes {
				t.Errorf("a gravação que falhou deixou mudança:\nantes\n%s\ndepois\n%s", antes, depois)
			}
			if s.avisos.Load() != 0 {
				t.Error("avisou o laço sem commit")
			}
		})
	}
}

// Atualizar sem mudança de verdade (o mesmo título e corpo, a mesma cor) não
// põe bit: um bit à toa vira conflito à toa na nuvem.
func TestEmissaoDasNotas_AtualizarSemMudancaNaoPoeBit(t *testing.T) {
	s := novoSyncDeNotas(t)
	s.conectar(t)
	n := s.nota(t, "n")
	s.naNuvem(t, "local_notes", n.ID, 5)
	s.limparFila(t)
	if _, err := s.notas.Atualizar(context.Background(), donoDaFila, n.ID, n.Version,
		storage.MudancaDaNota{Titulo: ptr(n.Titulo), Corpo: ptr(n.Corpo), MudarCor: true, Fixada: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("a fila: %v", m)
	}
	// Renomear para o mesmo nome também não.
	l := s.marcadorDeNota(t, "casa")
	s.naNuvem(t, "local_labels", l.ID, 1)
	s.limparFila(t)
	if _, err := s.notas.RenomearMarcador(context.Background(), donoDaFila, l.ID, l.Version, "  casa "); err != nil {
		t.Fatal(err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("a fila depois do mesmo nome: %v", m)
	}
}

// Conectado, tudo entra; desconectado, só o que a nuvem já tem (a versão
// dela), e os bits se juntam num marcador só.
func TestEmissaoDasNotas_ConectadoDesconectadoEVersao(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)

	// Desconectado: a nota e o marcador novos não entram, nem a edição deles.
	n := s.nota(t, "n")
	l := s.marcadorDeNota(t, "casa")
	n, _ = s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{Corpo: ptr("outro")})
	if m := s.marcas(t); len(m) != 0 {
		t.Fatalf("desconectado, sem versão da nuvem: %v", m)
	}
	// Desconectado, com a versão da nuvem: entra, e junta os bits.
	s.naNuvem(t, "local_notes", n.ID, 2)
	n, _ = s.notas.Atualizar(ctx, donoDaFila, n.ID, n.Version, storage.MudancaDaNota{Corpo: ptr("mais")})
	if _, err := s.notas.Marcar(ctx, donoDaFila, n.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	quer := []marca{{storage.EntidadeNota, n.ID, storage.CampoNotaConteudo | storage.CampoNotaMarcadores}}
	if m := s.marcas(t); !reflect.DeepEqual(m, quer) {
		t.Errorf("desconectado, com versão: %v; quero %v", m, quer)
	}
	// Conectado: a nota nova entra (campos 0), e a edição de uma nota que a
	// nuvem ainda não tem leva o bit (o envio de criar pode estar no voo).
	s.conectar(t)
	s.limparFila(t)
	o := s.nota(t, "o")
	if _, err := s.notas.Atualizar(ctx, donoDaFila, o.ID, o.Version, storage.MudancaDaNota{MudarCor: true, Cor: "rosa"}); err != nil {
		t.Fatal(err)
	}
	if m, ok := s.marcadorDaFila(t, storage.EntidadeNota, o.ID); !ok || m.Campos != storage.CampoNotaCor {
		t.Errorf("conectado: %+v %v", m, ok)
	}
	// O "marcador que espera" nunca entra.
	executar(t, s.db, `INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at, deleted_at, sync_state)
		VALUES ('espera', ?, '', '', ?, ?, ?, 'aguardando')`, donoDaFila, tempoDoBanco, tempoDoBanco, tempoDoBanco)
	executar(t, s.db, `UPDATE local_labels SET deleted_at = NULL WHERE id = 'espera'`) // nem se ficasse visível
	if _, err := s.notas.RenomearMarcador(ctx, donoDaFila, "espera", 1, "x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.marcadorDaFila(t, storage.EntidadeMarcadorDeNota, "espera"); ok {
		t.Error("o marcador que espera entrou na fila")
	}
}

// ─── A lápide local ───

func TestLapideDaNota_NuncaSubiuNaoDeixaNada(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	n := s.nota(t, "n")
	if _, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID); !ok {
		t.Fatal("a nota nova não entrou")
	}
	// A lixeira tira o marcador de criar parado (a nota que nunca subiu não sobe
	// na lixeira).
	if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Fatalf("a lixeira de uma nota que nunca subiu: %v", m)
	}
	if err := s.notas.ApagarDeVez(ctx, donoDaFila, n.ID); err != nil {
		t.Fatal(err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("apagar de vez uma nota que nunca subiu deixou %v", m)
	}
	// O expurgo, do mesmo jeito; e o marcador nunca subido também.
	o := s.nota(t, "o")
	_ = s.notas.SoftDelete(ctx, donoDaFila, o.ID, agoraDaFila)
	if k, err := s.notas.PurgeExpired(ctx, agoraDaFila.AddDate(0, 0, 31)); err != nil || k != 1 {
		t.Fatalf("expurgo: %d %v", k, err)
	}
	l := s.marcadorDeNota(t, "casa")
	if err := s.notas.ApagarMarcador(ctx, donoDaFila, l.ID); err != nil {
		t.Fatal(err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("o expurgo e o marcador que nunca subiram deixaram %v", m)
	}
}

func TestLapideDaNota_EmVooOuNaNuvemFicaComoApagado(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	// Em voo: o envio de criar saiu e não teve resposta. A lixeira não o tira
	// (a nuvem pode ter a nota), e o apagar de vez vira a lápide, com o envio
	// esquecido.
	n := s.nota(t, "n")
	if err := s.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeNota, EntityID: n.ID, MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	m, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID)
	if !ok || m.MutationID != "m1" || m.Campos != storage.CampoNotaLixeira {
		t.Fatalf("o em voo depois da lixeira: %+v %v", m, ok)
	}
	executar(t, s.db, `UPDATE sync_outbox SET selo_escolha = 'manter', selo_em = '2026-10-20T10:00:00.000000000Z' WHERE entity_id = ?`, n.ID)
	if err := s.notas.ApagarDeVez(ctx, donoDaFila, n.ID); err != nil {
		t.Fatal(err)
	}
	m, ok = s.marcadorDaFila(t, storage.EntidadeNota, n.ID)
	if !ok || m.Campos != storage.CampoApagado || m.MutationID != "" || m.VersaoEnviada != 0 || m.SeloEscolha != "" {
		t.Errorf("a lápide do em voo: %+v %v", m, ok)
	}
	// Na nuvem, desconectado: a lápide fica do mesmo jeito (a nuvem tem a nota).
	executar(t, s.db, `DELETE FROM instalacao WHERE nome = 'nuvem_aparelho'`)
	o := s.nota(t, "o")
	s.naNuvem(t, "local_notes", o.ID, 4)
	_ = s.notas.SoftDelete(ctx, donoDaFila, o.ID, agoraDaFila)
	if k, err := s.notas.PurgeExpired(ctx, agoraDaFila.AddDate(0, 0, 31)); err != nil || k != 1 {
		t.Fatalf("expurgo: %d %v", k, err)
	}
	if m, ok := s.marcadorDaFila(t, storage.EntidadeNota, o.ID); !ok || m.Campos != storage.CampoApagado {
		t.Errorf("a lápide do expurgo: %+v %v", m, ok)
	}
	if c := s.contar(t, `SELECT COUNT(*) FROM local_notes`); c != 0 {
		t.Errorf("sobraram %d notas", c)
	}
}

// A lixeira de uma nota que a nuvem ainda não tem, com o marcador da
// reconciliação (MarcadorCriar: a nuvem tem a nota), não o tira: a lixeira
// sobe.
func TestLixeiraDaNota_ReconciliacaoFica(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	n := s.nota(t, "n")
	s.limparFila(t)
	preparar(t, s.fila)
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Notas: []string{n.ID},
		DecidirNota: func(storage.EstadoDaNota) storage.AcaoDaNota {
			return storage.AcaoDaNota{Marcador: storage.MarcadorCriar}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.notas.SoftDelete(ctx, donoDaFila, n.ID, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID); !ok {
		t.Error("a lixeira tirou o marcador da reconciliação")
	}
}

// ─── A capacidade ───

func preparar(t *testing.T, f *storage.Fila) {
	t.Helper()
	if _, _, err := f.Preparar(context.Background(), "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
}

func TestCapacidade_SemElaNadaDeNotaSobe(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	// Um ditado que a nuvem tem virou nota: o marcador dele só tem o bit do
	// virou_nota.
	s.ditado(t, "d1", 2)
	s.limparFila(t)
	n := s.nota(t, "n")
	l := s.marcadorDeNota(t, "casa")
	v, err := s.notas.VirarNota(ctx, donoDaFila, "d1")
	if err != nil {
		t.Fatal(err)
	}
	// E outro ditado com o texto mudado e o virou_nota: esse sobe (sem a chave).
	s.ditado(t, "d2", 2)
	executar(t, s.db, `DELETE FROM sync_outbox WHERE entity_id = 'd2'`)
	tr, _ := s.hist.Get(ctx, donoDaFila, "d2")
	if _, err := s.hist.UpdateFinalText(ctx, donoDaFila, "d2", tr.Version, "editado"); err != nil {
		t.Fatal(err)
	}
	executar(t, s.db, `UPDATE sync_outbox SET campos = campos | 4 WHERE entity_id = 'd2'`)

	if ok, _ := s.fila.TemCapacidadeDeNotas(ctx); ok {
		t.Fatal("a capacidade sem ninguém gravar")
	}
	if its, err := s.fila.PendentesDeNotas(ctx, 10); err != nil || len(its) != 0 {
		t.Errorf("as notas sem a capacidade: %d %v", len(its), err)
	}
	if its, err := s.fila.PendentesDeMarcadoresDeNota(ctx, 10); err != nil || len(its) != 0 {
		t.Errorf("os marcadores sem a capacidade: %d %v", len(its), err)
	}
	its, err := s.fila.Pendentes(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(its) != 1 || its[0].Linha.ID != "d2" {
		t.Errorf("os ditados sem a capacidade: %v (quero só o d2)", idsDosItens(its))
	}
	// Os marcadores ficam na fila.
	for _, q := range []marca{{storage.EntidadeNota, n.ID, 0}, {storage.EntidadeNota, v.ID, 0},
		{storage.EntidadeMarcadorDeNota, l.ID, 0}, {storage.EntidadeDitado, "d1", storage.CampoVirouNota}} {
		if m, ok := s.marcadorDaFila(t, q.tipo, q.id); !ok || m.Campos != q.campos {
			t.Errorf("%v: %+v %v", q, m, ok)
		}
	}

	// Com a capacidade: tudo sai, em voo primeiro.
	s.capacidade(t)
	notas, _ := s.fila.PendentesDeNotas(ctx, 10)
	marcadores, _ := s.fila.PendentesDeMarcadoresDeNota(ctx, 10)
	ditados, _ := s.fila.Pendentes(ctx, 10)
	if len(notas) != 2 || len(marcadores) != 1 || len(ditados) != 2 {
		t.Errorf("com a capacidade: %d notas, %d marcadores, %d ditados", len(notas), len(marcadores), len(ditados))
	}
	if marcadores[0].Linha == nil || marcadores[0].Linha.Nome != "casa" {
		t.Errorf("o marcador: %+v", marcadores[0])
	}
	// O limite vale, e o em voo vem antes.
	if err := s.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeNota, EntityID: v.ID, MutationID: "m", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	if um, _ := s.fila.PendentesDeNotas(ctx, 1); len(um) != 1 || um[0].Linha.ID != v.ID {
		t.Errorf("o em voo primeiro: %+v", um)
	}
}

func idsDosItens(its []storage.ItemDaFila) []string {
	var out []string
	for _, it := range its {
		out = append(out, it.Linha.ID)
	}
	return out
}

func TestCapacidade_PrimeiraVezPoeOVirouNota(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	notaID := storage.NewID()
	s.ditado(t, "com-versao", 3)
	s.ditado(t, "sem-versao", 0)
	s.ditado(t, "da-caixa", 3)
	s.ditado(t, "comum", 3)
	s.ditado(t, "com-marcador", 3)
	executar(t, s.db, `UPDATE local_transcriptions SET virou_nota = ? WHERE id IN ('com-versao', 'sem-versao', 'com-marcador')`, notaID)
	executar(t, s.db, `UPDATE local_transcriptions SET virou_nota = 'nova:tk' WHERE id = 'da-caixa'`)
	s.limparFila(t)
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('transcription', 'com-marcador', 1, '2026-10-20T10:00:00.000000000Z')`)

	primeira, err := s.fila.VerCapacidade(ctx, true, agoraDaFila)
	if err != nil || !primeira {
		t.Fatalf("a primeira vez: %v %v", primeira, err)
	}
	quer := []marca{{storage.EntidadeDitado, "com-marcador", storage.CampoTexto | storage.CampoVirouNota},
		{storage.EntidadeDitado, "com-versao", storage.CampoVirouNota}}
	if m := s.marcas(t); !reflect.DeepEqual(m, quer) {
		t.Errorf("a fila depois da primeira vez: %v; quero %v", m, quer)
	}
	if ok, _ := s.fila.TemCapacidadeDeNotas(ctx); !ok {
		t.Error("a chave não foi gravada")
	}
	// A segunda vez não faz nada.
	s.limparFila(t)
	if primeira, err := s.fila.VerCapacidade(ctx, true, agoraDaFila); err != nil || primeira {
		t.Errorf("a segunda vez: %v %v", primeira, err)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("a segunda vez marcou %v", m)
	}
	// A nuvem que deixou de conhecer notas: a chave sai, e voltar é outra
	// primeira vez.
	if primeira, err := s.fila.VerCapacidade(ctx, false, agoraDaFila); err != nil || primeira {
		t.Fatalf("sem notas: %v %v", primeira, err)
	}
	if ok, _ := s.fila.TemCapacidadeDeNotas(ctx); ok {
		t.Error("a chave ficou")
	}
	if primeira, _ := s.fila.VerCapacidade(ctx, true, agoraDaFila); !primeira {
		t.Error("a volta não foi uma primeira vez")
	}
}

// ─── A completação ───

func TestCompletarNotas(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	// Desconectado: nada entra na hora.
	viva := s.nota(t, "viva")
	lixo := s.nota(t, "lixo")
	_ = s.notas.SoftDelete(ctx, donoDaFila, lixo.ID, agoraDaFila)
	subiu := s.nota(t, "subiu")
	s.naNuvem(t, "local_notes", subiu.ID, 1)
	comMarcador := s.nota(t, "com marcador")
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('note', ?, 4, '2026-10-20T10:00:00.000000000Z')`, comMarcador.ID)
	recusada := s.nota(t, "recusada")
	executar(t, s.db, `UPDATE local_notes SET sync_state = 'recusado' WHERE id = ?`, recusada.ID)
	l := s.marcadorDeNota(t, "casa")
	apagado := s.marcadorDeNota(t, "velho")
	_ = s.notas.ApagarMarcador(ctx, donoDaFila, apagado.ID)
	executar(t, s.db, `INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at, deleted_at, sync_state)
		VALUES ('espera', ?, '', '', ?, ?, ?, 'aguardando')`, donoDaFila, tempoDoBanco, tempoDoBanco, tempoDoBanco)

	// Sem o aceite, ou sem a capacidade: nada.
	if k, err := s.fila.CompletarNotas(ctx, agoraDaFila); err != nil || k != 0 {
		t.Fatalf("sem nada: %d %v", k, err)
	}
	s.aceitar(t)
	if k, _ := s.fila.CompletarNotas(ctx, agoraDaFila); k != 0 {
		t.Fatalf("sem a capacidade: %d", k)
	}
	s.capacidade(t)
	k, err := s.fila.CompletarNotas(ctx, agoraDaFila)
	if err != nil || k != 2 {
		t.Fatalf("com o aceite e a capacidade: %d %v", k, err)
	}
	quer := []marca{{storage.EntidadeMarcadorDeNota, l.ID, 0}, {storage.EntidadeNota, comMarcador.ID, 4}, {storage.EntidadeNota, viva.ID, 0}}
	sort.Slice(quer, func(i, j int) bool { return quer[i].String() < quer[j].String() })
	if m := s.marcas(t); !reflect.DeepEqual(m, quer) {
		t.Errorf("a fila: %v; quero %v", m, quer)
	}
	if k, _ := s.fila.CompletarNotas(ctx, agoraDaFila); k != 0 {
		t.Errorf("a segunda rodada marcou %d", k)
	}
}

// ─── A resposta da nuvem ───

func TestAplicarNotas_GravarMarcadoresQueEsperamEColisao(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.capacidade(t)
	preparar(t, s.fila)
	local := s.marcadorDeNota(t, "Mercado")
	s.limparFila(t)
	notaID, marcNuvem, marcEspera := storage.NewID(), storage.NewID(), storage.NewID()
	criada := agoraDaFila.Add(-48 * time.Hour)
	apagadaEm := agoraDaFila.Add(-time.Hour)
	daNuvem := storage.Nota{Titulo: "lista", Corpo: "- [ ] pão", Cor: ptr("verde"), Fixada: true, Posicao: "V",
		Marcadores: []string{marcEspera, local.ID}, CreatedAt: criada, UpdatedAt: criada.Add(time.Hour),
		Conflito: &storage.ConflitoDaNota{Titulo: "lista", Corpo: "- [ ] leite", DeviceID: ptr("outro"), Em: criada}}
	v7 := int64(7)
	var visto storage.EstadoDaNota
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Notas: []string{notaID},
		DecidirNota: func(e storage.EstadoDaNota) storage.AcaoDaNota {
			visto = e
			return storage.AcaoDaNota{Gravar: &daNuvem, ServerVersion: &v7}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if visto.Linha != nil || visto.Marcador != nil {
		t.Errorf("o estado da nota nova: %+v", visto)
	}
	n := s.get(t, notaID)
	if n.Titulo != "lista" || n.Corpo != "- [ ] pão" || n.Cor == nil || *n.Cor != "verde" || !n.Fixada || n.Posicao != "V" ||
		n.Version != 1 || n.ServerVersion == nil || *n.ServerVersion != 7 || n.CorpoNuvem == nil || *n.CorpoNuvem != n.Corpo ||
		n.Conflito == nil || n.Conflito.Corpo != "- [ ] leite" || !n.Conflito.Em.Equal(criada) || n.SyncState != storage.SituacaoNormal {
		t.Errorf("a nota gravada: %+v", n)
	}
	// O marcador que o PC não tem espera, invisível: a nota mostra só o local.
	if !reflect.DeepEqual(n.Marcadores, []string{local.ID}) {
		t.Errorf("os marcadores visíveis: %v", n.Marcadores)
	}
	if ls, _ := s.notas.ListarMarcadores(ctx, donoDaFila); len(ls) != 1 {
		t.Errorf("o que espera aparece na lista: %v", ls)
	}
	// O sync vê o conjunto inteiro, em ordem de id.
	var estado storage.EstadoDaNota
	_ = s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Notas: []string{notaID}, DecidirNota: func(e storage.EstadoDaNota) storage.AcaoDaNota { estado = e; return storage.AcaoDaNota{} },
	})
	quer := []string{marcEspera, local.ID}
	sort.Strings(quer)
	if estado.Linha == nil || !reflect.DeepEqual(estado.Linha.Marcadores, quer) {
		t.Errorf("o sync vê: %+v", estado.Linha)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("aplicar pôs marcador: %v", m)
	}

	// Chegam os dois marcadores: o que esperava vira de verdade, e o "mercado"
	// da nuvem colide com o local: " (2)". E a nota muda (vai para a lixeira) na
	// mesma página: marcadores antes das notas.
	v1 := int64(1)
	daNuvem2 := daNuvem
	daNuvem2.Marcadores = []string{marcEspera, marcNuvem}
	daNuvem2.DeletedAt = &apagadaEm
	daNuvem2.Conflito = nil
	daNuvem2.Corpo = "- [x] pão"
	var estadoEspera storage.EstadoDoMarcadorDeNota
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Marcadores: []string{marcEspera, marcNuvem},
		DecidirMarcador: func(e storage.EstadoDoMarcadorDeNota) storage.AcaoDoMarcadorDeNota {
			nome := "MERCADO"
			if e.ID == marcEspera {
				estadoEspera = e
				nome = "trabalho"
			}
			return storage.AcaoDoMarcadorDeNota{Gravar: &storage.MarcadorDeNota{Nome: nome, CreatedAt: criada, UpdatedAt: criada}, ServerVersion: &v1}
		},
		Notas:       []string{notaID},
		DecidirNota: func(storage.EstadoDaNota) storage.AcaoDaNota { return storage.AcaoDaNota{Gravar: &daNuvem2} },
	}); err != nil {
		t.Fatal(err)
	}
	if estadoEspera.Linha != nil {
		t.Errorf("o que espera apareceu como linha: %+v", estadoEspera.Linha)
	}
	ls, _ := s.notas.ListarMarcadores(ctx, donoDaFila)
	nomes := map[string]string{}
	for _, l := range ls {
		nomes[l.ID] = l.Nome
	}
	if len(ls) != 3 || nomes[marcEspera] != "trabalho" || nomes[marcNuvem] != "MERCADO (2)" || nomes[local.ID] != "Mercado" {
		t.Errorf("os marcadores: %v", nomes)
	}
	n = s.get(t, notaID)
	queroMarc := []string{marcEspera, marcNuvem}
	sort.Strings(queroMarc)
	got := append([]string(nil), n.Marcadores...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, queroMarc) || n.DeletedAt == nil || n.PurgeAt == nil ||
		!n.PurgeAt.Equal(apagadaEm.Add(storage.PrazoDaLixeira)) || n.Version != 2 || n.Conflito != nil ||
		*n.CorpoNuvem != "- [x] pão" || *n.ServerVersion != 7 {
		t.Errorf("a nota atualizada: %+v", n)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("aplicar pôs marcador: %v", m)
	}

	// As lápides: a do marcador tira a ligação; a da nota apaga a linha, as
	// ligações e o marcador da fila.
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('note', ?, 1, '2026-10-20T10:00:00.000000000Z')`, notaID)
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Marcadores: []string{marcNuvem},
		DecidirMarcador: func(storage.EstadoDoMarcadorDeNota) storage.AcaoDoMarcadorDeNota {
			return storage.AcaoDoMarcadorDeNota{ApagarLinha: true}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if n := s.get(t, notaID); len(n.Marcadores) != 1 || n.Marcadores[0] != marcEspera {
		t.Errorf("depois da lápide do marcador: %v", n.Marcadores)
	}
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
		Notas:       []string{notaID},
		DecidirNota: func(storage.EstadoDaNota) storage.AcaoDaNota { return storage.AcaoDaNota{ApagarLinha: true} },
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.notas.Get(ctx, donoDaFila, notaID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a nota depois da lápide: %v", err)
	}
	if c := s.contar(t, `SELECT COUNT(*) FROM local_note_labels`); c != 0 {
		t.Errorf("sobraram %d ligações", c)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("a fila depois da lápide: %v", m)
	}
}

// As ações do sync sobre uma nota que existe: a versão, o corpo_nuvem, a
// situação e o marcador (sai, reenviar, criar). Um erro desfaz a página
// inteira, cursor junto.
func TestAplicarNotas_AcoesEOCursor(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	s.capacidade(t)
	preparar(t, s.fila)
	n := s.nota(t, "n")
	if err := s.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeNota, EntityID: n.ID, MutationID: "m1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	v3 := int64(3)
	cursor := int64(40)
	aplicar := func(a storage.AcaoDaNota, c *int64) error {
		return s.fila.AplicarTudo(ctx, "a1", "e1", nil, c, nil, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{
			Notas: []string{n.ID}, DecidirNota: func(storage.EstadoDaNota) storage.AcaoDaNota { return a },
		})
	}
	if err := aplicar(storage.AcaoDaNota{ServerVersion: &v3, CorpoNuvemAtual: true, Marcador: storage.MarcadorReenviar}, &cursor); err != nil {
		t.Fatal(err)
	}
	g := s.get(t, n.ID)
	if g.ServerVersion == nil || *g.ServerVersion != 3 || g.CorpoNuvem == nil || *g.CorpoNuvem != g.Corpo {
		t.Errorf("a versão e o corpo_nuvem: %+v", g)
	}
	if m, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID); !ok || m.MutationID != "" {
		t.Errorf("o reenviar: %+v %v", m, ok)
	}
	est, _, _ := s.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if est.Cursor != 40 {
		t.Errorf("o cursor: %d", est.Cursor)
	}
	if err := aplicar(storage.AcaoDaNota{EsquecerCorpoNuvem: true, ZerarServerVersion: true, Situacao: storage.SituacaoRecusado,
		Erro: "grande", Marcador: storage.MarcadorSai}, nil); err != nil {
		t.Fatal(err)
	}
	g = s.get(t, n.ID)
	if g.ServerVersion != nil || g.CorpoNuvem != nil || g.SyncState != storage.SituacaoRecusado || g.LastSyncError == nil || *g.LastSyncError != "grande" {
		t.Errorf("zerar, esquecer e recusar: %+v", g)
	}
	if _, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID); ok {
		t.Error("o marcador não saiu")
	}
	// A reconciliação põe o marcador com os campos todos.
	if err := aplicar(storage.AcaoDaNota{Marcador: storage.MarcadorCriar}, nil); err != nil {
		t.Fatal(err)
	}
	if m, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID); !ok || m.Campos != 127 {
		t.Errorf("o MarcadorCriar: %+v %v", m, ok)
	}
	// A ação inválida desfaz tudo, cursor junto.
	outro := int64(99)
	if err := aplicar(storage.AcaoDaNota{CorpoNuvemAtual: true, EsquecerCorpoNuvem: true}, &outro); err == nil {
		t.Error("a ação inválida passou")
	}
	if est, _, _ := s.fila.Preparar(ctx, "a1", "p1", agoraDaFila); est.Cursor != 40 {
		t.Errorf("o cursor andou num erro: %d", est.Cursor)
	}
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{},
		storage.RespostaDasNotas{Notas: []string{n.ID}}); err == nil {
		t.Error("ids sem a decisão passaram")
	}
}

// O virou_nota do ditado que vem da nuvem: só com a capacidade, e o nulo dela
// não tira o "nova:" daqui (a nuvem nunca o recebe).
func TestAplicarDitado_VirouNotaComAGuardaDoNova(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	preparar(t, s.fila)
	notaID := storage.NewID()
	s.ditado(t, "virou", 2)
	s.ditado(t, "caixa", 2)
	s.ditado(t, "eco", 2)
	executar(t, s.db, `UPDATE local_transcriptions SET virou_nota = ? WHERE id = 'virou'`, notaID)
	executar(t, s.db, `UPDATE local_transcriptions SET virou_nota = 'nova:tk' WHERE id IN ('caixa', 'eco')`)
	// As linhas lidas antes: a decisão roda dentro da transação e não toca o
	// banco.
	linhas := map[string]storage.Transcription{}
	for _, id := range []string{"virou", "caixa", "eco"} {
		linhas[id], _ = s.hist.Get(ctx, donoDaFila, id)
	}
	gravarComo := func(id string, virou *string) *storage.Transcription {
		tr := linhas[id]
		tr.VirouNota = virou
		return &tr
	}
	aplicar := func(decidir func(storage.EstadoDoItem) storage.AcaoLocal) {
		t.Helper()
		if err := s.fila.AplicarTudo(ctx, "a1", "e1", []string{"virou", "caixa", "eco"}, nil, decidir,
			storage.RespostaDoAprendido{}, storage.RespostaDasNotas{}); err != nil {
			t.Fatal(err)
		}
	}
	virouDe := func(id string) *string { tr, _ := s.hist.Get(ctx, donoDaFila, id); return tr.VirouNota }

	// Sem a capacidade: a nuvem não sabe do virou_nota, e o nulo dela não vale.
	aplicar(func(e storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{Gravar: gravarComo(e.ID, nil), MudarVirouNota: true}
	})
	if v := virouDe("virou"); v == nil || *v != notaID {
		t.Errorf("sem a capacidade, o virou_nota mudou: %v", v)
	}
	// Com a capacidade: o nulo da nuvem vale, menos sobre o "nova:". (A
	// primeira vez põe o bit no "virou", que tem a versão da nuvem: sai aqui,
	// para a fila provar no fim que aplicar não põe marcador.)
	s.capacidade(t)
	s.limparFila(t)
	aplicar(func(e storage.EstadoDoItem) storage.AcaoLocal {
		if e.ID == "eco" {
			return storage.AcaoLocal{MudarVirouNota: true} // o eco: só o virou_nota (nulo)
		}
		return storage.AcaoLocal{Gravar: gravarComo(e.ID, nil)}
	})
	if v := virouDe("virou"); v != nil {
		t.Errorf("o nulo da nuvem não tirou a nota: %v", *v)
	}
	for _, id := range []string{"caixa", "eco"} {
		if v := virouDe(id); v == nil || *v != "nova:tk" {
			t.Errorf("%s: o nulo da nuvem tirou o ditado da caixa: %v", id, v)
		}
	}
	// E o id da nuvem vale, também por cima do "nova:", e no eco.
	aplicar(func(e storage.EstadoDoItem) storage.AcaoLocal {
		if e.ID == "eco" {
			return storage.AcaoLocal{MudarVirouNota: true, VirouNota: &notaID}
		}
		return storage.AcaoLocal{Gravar: gravarComo(e.ID, &notaID)}
	})
	for _, id := range []string{"virou", "caixa", "eco"} {
		if v := virouDe(id); v == nil || *v != notaID {
			t.Errorf("%s: o id da nuvem: %v", id, v)
		}
	}
	// O ditado novo que vem da nuvem com o virou_nota já entra escondido.
	novo := newTranscription("novo", donoDaFila, agoraDaFila.Add(-time.Hour))
	novo.VirouNota = &notaID
	v1 := int64(1)
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", []string{"novo"}, nil, func(storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{Gravar: &novo, ServerVersion: &v1}
	}, storage.RespostaDoAprendido{}, storage.RespostaDasNotas{}); err != nil {
		t.Fatal(err)
	}
	if v := virouDe("novo"); v == nil || *v != notaID {
		t.Errorf("o ditado novo: %v", v)
	}
	if m := (&syncDeNotas{db: s.db, fila: s.fila}).marcas(t); len(m) != 0 {
		t.Errorf("aplicar pôs marcador: %v", m)
	}
}

// ─── O recomeço e o esquecer ───

func TestRecomecarEEsquecer_Notas(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.capacidade(t)
	preparar(t, s.fila)
	subiu := s.nota(t, "subiu")
	naLixeira := s.nota(t, "lixeira")
	nunca := s.nota(t, "nunca")
	l := s.marcadorDeNota(t, "casa")
	for _, id := range []string{subiu.ID, naLixeira.ID} {
		executar(t, s.db, `UPDATE local_notes SET server_version = 4, corpo_nuvem = corpo WHERE id = ?`, id)
	}
	s.naNuvem(t, "local_labels", l.ID, 2)
	_ = s.notas.SoftDelete(ctx, donoDaFila, naLixeira.ID, agoraDaFila)
	s.limparFila(t)
	executar(t, s.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, mutation_id, versao_enviada)
		VALUES ('note', ?, 2, '2026-10-20T10:00:00.000000000Z', 'm1', 3)`, naLixeira.ID)

	if err := s.fila.Recomecar(ctx, "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
	quer := []marca{{storage.EntidadeMarcadorDeNota, l.ID, 0}, {storage.EntidadeNota, naLixeira.ID, 2}, {storage.EntidadeNota, subiu.ID, 0}}
	sort.Slice(quer, func(i, j int) bool { return quer[i].String() < quer[j].String() })
	if m := s.marcas(t); !reflect.DeepEqual(m, quer) {
		t.Errorf("a fila do recomeço: %v; quero %v", m, quer)
	}
	if m, _ := s.marcadorDaFila(t, storage.EntidadeNota, naLixeira.ID); m.MutationID != "" || m.VersaoEnviada != 0 {
		t.Errorf("o envio em curso não foi esquecido: %+v", m)
	}
	if c := s.contar(t, `SELECT COUNT(*) FROM local_notes WHERE server_version IS NOT NULL OR corpo_nuvem IS NOT NULL`); c != 0 {
		t.Errorf("%d notas com a versão velha", c)
	}
	if c := s.contar(t, `SELECT COUNT(*) FROM local_labels WHERE server_version IS NOT NULL`); c != 0 {
		t.Errorf("%d marcadores com a versão velha", c)
	}
	_ = nunca

	// O esquecer: a fila sai, as versões e a situação também, o selo fica, e a
	// capacidade vai junto com a conexão.
	executar(t, s.db, `UPDATE local_notes SET server_version = 9, sync_state = 'recusado', last_sync_error = 'x',
		conflito_titulo = 't', conflito_corpo = 'c', conflito_em = ? WHERE id = ?`, "2026-10-20T10:00:00.000000000Z", subiu.ID)
	s.naNuvem(t, "local_labels", l.ID, 3)
	if err := s.fila.EsquecerNuvem(ctx, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	g := s.get(t, subiu.ID)
	if g.ServerVersion != nil || g.SyncState != storage.SituacaoNormal || g.LastSyncError != nil || g.Conflito == nil {
		t.Errorf("a nota depois do esquecer: %+v", g)
	}
	if c := s.contar(t, `SELECT COUNT(*) FROM local_labels WHERE server_version IS NOT NULL`); c != 0 {
		t.Errorf("%d marcadores com a versão", c)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("a fila depois do esquecer: %v", m)
	}
	if ok, _ := s.fila.TemCapacidadeDeNotas(ctx); ok {
		t.Error("a capacidade ficou depois do esquecer")
	}
}

// ─── A escolha do selo ───

func TestResolverConflitoDaNota(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	n := s.nota(t, "meu")
	selo := func(id string) {
		executar(t, s.db, `UPDATE local_notes SET conflito_titulo = 'deles', conflito_corpo = 'corpo deles',
			conflito_device_id = 'outro', conflito_em = ? WHERE id = ?`, "2026-10-20T10:00:00.000000000Z", id)
	}
	// Sem selo: conflito de versão.
	if _, err := s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, n.Version, storage.EscolhaManter); !errors.Is(err, storage.ErrVersionConflict) {
		t.Errorf("sem selo: %v", err)
	}
	selo(n.ID)
	if _, err := s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, n.Version, "talvez"); !errors.Is(err, storage.ErrEscolhaInvalida) {
		t.Errorf("escolha inválida: %v", err)
	}
	if _, err := s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, n.Version+1, storage.EscolhaManter); !errors.Is(err, storage.ErrVersionConflict) {
		t.Errorf("versão velha: %v", err)
	}
	// Manter: o texto fica, o selo sai; sem a versão da nuvem, nada na fila.
	g, err := s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, n.Version, storage.EscolhaManter)
	if err != nil {
		t.Fatal(err)
	}
	if g.Titulo != "meu" || g.Conflito != nil || g.Version != n.Version+1 {
		t.Errorf("manter: %+v", g)
	}
	if m := s.marcas(t); len(m) != 0 {
		t.Errorf("sem a versão da nuvem, a escolha entrou na fila: %v", m)
	}
	// Trocar, numa nota que a nuvem tem (desconectado): o título e o corpo do
	// selo, e a escolha vai para a fila com o conflict_at.
	selo(n.ID)
	s.naNuvem(t, "local_notes", n.ID, 6)
	s.avisos.Store(0)
	g, err = s.notas.ResolverConflitoDaNota(ctx, donoDaFila, n.ID, g.Version, storage.EscolhaTrocar)
	if err != nil {
		t.Fatal(err)
	}
	if g.Titulo != "deles" || g.Corpo != "corpo deles" || g.Conflito != nil {
		t.Errorf("trocar: %+v", g)
	}
	m, ok := s.marcadorDaFila(t, storage.EntidadeNota, n.ID)
	if !ok || m.SeloEscolha != storage.EscolhaTrocar || m.Campos != 0 ||
		!m.SeloEm.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("a escolha na fila: %+v %v", m, ok)
	}
	if s.avisos.Load() != 1 {
		t.Errorf("avisos: %d", s.avisos.Load())
	}
}

// ─── O resumo ───

func TestResumo_MudancasContaAsNotas(t *testing.T) {
	ctx := context.Background()
	s := novoSyncDeNotas(t)
	s.conectar(t)
	nova := s.nota(t, "nova") // marcador de criar: não conta
	_ = nova
	velha := s.nota(t, "velha")
	s.naNuvem(t, "local_notes", velha.ID, 2)
	if _, err := s.notas.Atualizar(ctx, donoDaFila, velha.ID, velha.Version, storage.MudancaDaNota{Corpo: ptr("x")}); err != nil {
		t.Fatal(err) // conta
	}
	apagada := s.nota(t, "apagada")
	s.naNuvem(t, "local_notes", apagada.ID, 2)
	_ = s.notas.SoftDelete(ctx, donoDaFila, apagada.ID, agoraDaFila)
	_ = s.notas.ApagarDeVez(ctx, donoDaFila, apagada.ID) // a lápide conta
	l := s.marcadorDeNota(t, "casa")
	s.naNuvem(t, "local_labels", l.ID, 1)
	if _, err := s.notas.RenomearMarcador(ctx, donoDaFila, l.ID, l.Version, "lar"); err != nil {
		t.Fatal(err) // conta
	}
	_ = s.marcadorDeNota(t, "novo") // não conta
	r, err := s.fila.Resumo(ctx, agoraDaFila)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mudancas != 3 {
		t.Errorf("Mudancas = %d; quero 3", r.Mudancas)
	}
}
