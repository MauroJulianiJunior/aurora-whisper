package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O sync das imagens das notas no PC (N10b; nota_imagem_sync.go), contra o
// SQLite de verdade e a pasta das imagens: o marcador do anexo e o da lápide, a
// completação do envio (as imagens de antes desta versão), a espera da nota que a
// nuvem ainda não tem, a imagem que vem da nuvem "aguardando" (que a varredura
// respeita e que a API local vê "a caminho"), o download conferido, o recomeço e o
// esquecer. As decisões (o que cada resposta manda fazer) são do motor
// (internal/sync), testadas em tabela lá.

// syncDasImagens é o banco das imagens com a fila do mesmo dono, conectada.
type syncDasImagens struct {
	imagensDeTeste
	fila   *storage.Fila
	avisos int
}

func novoSyncDasImagens(t *testing.T) *syncDasImagens {
	t.Helper()
	s := &syncDasImagens{imagensDeTeste: novasImagensDeTeste(t)}
	s.fila = storage.NovaFila(s.db, dono)
	s.notas.AoEnfileirar(func() { s.avisos++ })
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1')`)
	return s
}

// comCapacidadeEAceite liga o que a completação pede: o aceite do "Enviar o
// histórico" e a capacidade das imagens.
func (s *syncDasImagens) comCapacidadeEAceite(t *testing.T) {
	t.Helper()
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x'), ('nuvem_imagens', 'x')
		ON CONFLICT (nome) DO NOTHING`)
}

// naNuvem dá à nota a versão da nuvem (o criar dela já subiu).
func (s *syncDasImagens) naNuvem(t *testing.T, notaID string) {
	t.Helper()
	executar(t, s.db, `UPDATE local_notes SET server_version = 1 WHERE id = ?`, notaID)
}

// marcadorDaImagem lê o marcador note_image da imagem: os campos e a nota.
func (s *syncDasImagens) marcadorDaImagem(t *testing.T, id string) (campos int, nota string, ok bool) {
	t.Helper()
	err := s.db.QueryRow(`SELECT campos, COALESCE(nota_id, '') FROM sync_outbox WHERE entity_type = 'note_image' AND entity_id = ?`, id).
		Scan(&campos, &nota)
	if err != nil {
		return 0, "", false
	}
	return campos, nota, true
}

func (s *syncDasImagens) colunaDaImagem(t *testing.T, id, coluna string) any {
	t.Helper()
	var v any
	if err := s.db.QueryRow(`SELECT `+coluna+` FROM local_note_images WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("lendo %s da imagem: %v", coluna, err)
	}
	return v
}

// limpaDoTeste é o que o Conferir guarda de b.
func limpaDoTeste(t *testing.T, b []byte) imagemdanota.Limpa {
	t.Helper()
	l, err := imagemdanota.Conferir(b)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSyncDasImagens_AnexarPoeOMarcadorETirarPoeALapide(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Recibos", "")
	im := s.anexar(t, n.ID, 1)
	if campos, nota, ok := s.marcadorDaImagem(t, im.ID); !ok || campos != 0 || nota != n.ID {
		t.Fatalf("o marcador do anexo: %d %q %v", campos, nota, ok)
	}
	if s.avisos == 0 {
		t.Error("o anexo não acordou o laço")
	}

	// Tirar a que a nuvem nunca viu: o marcador sai, nada sobe.
	if err := s.notas.TirarImagem(ctx, dono, n.ID, im.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.marcadorDaImagem(t, im.ID); ok {
		t.Error("a imagem que a nuvem nunca viu deixou marcador ao sair")
	}

	// Tirar a que a nuvem pode ter (o PUT já saiu uma vez): a lápide, com a nota.
	outra := s.anexar(t, n.ID, 2)
	if err := s.fila.MarcarEnvioDaImagem(ctx, outra.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.notas.TirarImagem(ctx, dono, n.ID, outra.ID); err != nil {
		t.Fatal(err)
	}
	if campos, nota, ok := s.marcadorDaImagem(t, outra.ID); !ok || campos != storage.CampoApagado || nota != n.ID {
		t.Fatalf("a lápide da imagem: %d %q %v", campos, nota, ok)
	}

	// Desconectado, o anexo não põe marcador (a completação o põe depois).
	executar(t, s.db, `DELETE FROM instalacao WHERE nome = 'nuvem_aparelho'`)
	terceira := s.anexar(t, n.ID, 3)
	if _, _, ok := s.marcadorDaImagem(t, terceira.ID); ok {
		t.Error("o anexo desconectado pôs marcador")
	}
	// A nota que sai de vez leva os marcadores das imagens dela, a lápide também.
	if err := s.notas.SoftDelete(ctx, dono, n.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.notas.ApagarDeVez(ctx, dono, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.marcadorDaImagem(t, outra.ID); ok {
		t.Error("a lápide da imagem ficou depois de a nota sair de vez")
	}
}

func TestSyncDasImagens_ACompletacaoLevaAsDeAntesDestaVersao(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	agora := time.Now()
	executar(t, s.db, `DELETE FROM instalacao WHERE nome = 'nuvem_aparelho'`)
	n := criarNota(t, s.notas, "Viagem", "")
	lixo := criarNota(t, s.notas, "Lixo", "")
	a, b := s.anexar(t, n.ID, 1), s.anexar(t, n.ID, 2)
	naLixeira := s.anexar(t, lixo.ID, 3)
	if err := s.notas.SoftDelete(ctx, dono, lixo.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	executar(t, s.db, `UPDATE local_note_images SET sync_state = 'recusado' WHERE id = ?`, b.ID)

	// Sem o aceite ou sem a capacidade, nada.
	if k, err := s.fila.CompletarImagens(ctx, agora); err != nil || k != 0 {
		t.Fatalf("sem o aceite: %d, %v", k, err)
	}
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x')`)
	if k, err := s.fila.CompletarImagens(ctx, agora); err != nil || k != 0 {
		t.Fatalf("sem a capacidade: %d, %v", k, err)
	}
	s.comCapacidadeEAceite(t)
	if k, err := s.fila.CompletarImagens(ctx, agora); err != nil || k != 1 {
		t.Fatalf("a completação marcou %d (%v), quero 1: a normal; a recusada e a da lixeira ficam", k, err)
	}
	if campos, nota, ok := s.marcadorDaImagem(t, a.ID); !ok || campos != 0 || nota != n.ID {
		t.Errorf("o marcador da completação: %d %q %v", campos, nota, ok)
	}
	if _, _, ok := s.marcadorDaImagem(t, naLixeira.ID); ok {
		t.Error("a imagem da nota da lixeira entrou na fila")
	}
	if k, _ := s.fila.CompletarImagens(ctx, agora); k != 0 {
		t.Errorf("a segunda completação marcou %d", k)
	}
}

func TestSyncDasImagens_PendentesEsperamANotaEAVez(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	agora := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	espera := func(int) time.Duration { return time.Hour }
	n := criarNota(t, s.notas, "Quadro", "")
	im := s.anexar(t, n.ID, 1)

	pendentes := func() []storage.ItemDeImagemNaFila {
		t.Helper()
		its, err := s.fila.PendentesDeImagens(ctx, 10, agora, espera)
		if err != nil {
			t.Fatal(err)
		}
		return its
	}
	if its := pendentes(); len(its) != 0 {
		t.Fatalf("sem a capacidade, subiu: %+v", its)
	}
	s.comCapacidadeEAceite(t)
	if its := pendentes(); len(its) != 0 {
		t.Fatalf("a imagem subiu antes da nota: %+v", its)
	}
	s.naNuvem(t, n.ID)
	its := pendentes()
	if len(its) != 1 || its[0].ID != im.ID || its[0].NotaID != n.ID || its[0].Lapide || its[0].Linha == nil ||
		its[0].Linha.SHA256 != im.SHA256 {
		t.Fatalf("o pendente: %+v", its)
	}
	// A espera de uma recusa que pode passar: tentou há 30 min, espera 1 h.
	if err := s.fila.AplicarEnvioDaImagem(ctx, its[0], storage.AcaoDaImagemNoEnvio{Esperar: true, Erro: "TOO_MANY_IMAGES"},
		agora.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if its := pendentes(); len(its) != 0 {
		t.Fatalf("subiu antes da vez: %+v", its)
	}
	if s.colunaDaImagem(t, im.ID, "tentativas") != int64(1) || s.colunaDaImagem(t, im.ID, "last_sync_error") != "TOO_MANY_IMAGES" {
		t.Error("a espera não foi gravada")
	}
	agora = agora.Add(time.Hour)
	if its := pendentes(); len(its) != 1 {
		t.Fatalf("a vez chegou e não subiu: %+v", its)
	}
	// A nota que vai para a lixeira segura a imagem (ela sobe se a nota voltar).
	if err := s.notas.SoftDelete(ctx, dono, n.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if its := pendentes(); len(its) != 0 {
		t.Fatalf("a imagem da nota da lixeira subiu: %+v", its)
	}
	if _, err := s.notas.Restaurar(ctx, dono, n.ID); err != nil {
		t.Fatal(err)
	}
	its = pendentes()
	if len(its) != 1 {
		t.Fatalf("a nota restaurada não soltou a imagem: %+v", its)
	}

	// Subiu: o marcador sai e a espera zera; o criar que virou lápide no meio
	// fica.
	if err := s.fila.AplicarEnvioDaImagem(ctx, its[0], storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Subiu: true}, agora); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.marcadorDaImagem(t, im.ID); ok {
		t.Error("o marcador ficou depois de subir")
	}
	if s.colunaDaImagem(t, im.ID, "na_nuvem") != int64(1) || s.colunaDaImagem(t, im.ID, "tentativas") != int64(0) {
		t.Error("o subir não zerou a espera nem gravou na_nuvem")
	}
	outra := s.anexar(t, n.ID, 2)
	its = pendentes()
	if err := s.notas.TirarImagem(ctx, dono, n.ID, outra.ID); err != nil {
		t.Fatal(err)
	}
	// (na_nuvem 0: o tirar não deixou lápide; marca o envio e tira de novo.)
	outra = s.anexar(t, n.ID, 3)
	if err := s.fila.MarcarEnvioDaImagem(ctx, outra.ID); err != nil {
		t.Fatal(err)
	}
	its = pendentes()
	if len(its) != 1 || its[0].ID != outra.ID {
		t.Fatalf("o pendente da outra: %+v", its)
	}
	if err := s.notas.TirarImagem(ctx, dono, n.ID, outra.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.fila.AplicarEnvioDaImagem(ctx, its[0], storage.AcaoDaImagemNoEnvio{TirarMarcador: true, Subiu: true}, agora); err != nil {
		t.Fatal(err)
	}
	if campos, _, ok := s.marcadorDaImagem(t, outra.ID); !ok || campos != storage.CampoApagado {
		t.Fatalf("a resposta do PUT tirou a lápide feita no meio: %d %v", campos, ok)
	}
	its = pendentes()
	if len(its) != 1 || !its[0].Lapide || its[0].NotaID != n.ID {
		t.Fatalf("a lápide pendente: %+v", its)
	}
}

// A imagem que vem da nuvem nasce "aguardando": a nota a lista "a caminho", a
// leitura dos bytes é ErrImagemACaminho, a varredura da subida não a apaga, e o
// download conferido a põe normal, com o arquivo. Bytes que não conferem não
// mudam nada. A da nuvem entra mesmo acima do teto daqui.
func TestSyncDasImagens_AQueVemDaNuvemEsperaOArquivo(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	s.comCapacidadeEAceite(t)
	if _, _, err := s.fila.Preparar(ctx, "a1", "p1", time.Now()); err != nil {
		t.Fatal(err)
	}
	n := criarNota(t, s.notas, "Do celular", "")
	foto := limpaDoTeste(t, fotoDeTeste(t, 30, 20, 9))
	id := "0199a000-0000-7000-8000-00000000f001"
	grande := "0199a000-0000-7000-8000-00000000f002"
	aplicar := func(ims []storage.ImagemNoPull, acao func(storage.EstadoDaImagem) storage.AcaoDaImagem) {
		t.Helper()
		if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{},
			storage.RespostaDasNotas{Imagens: ims, DecidirImagem: acao}); err != nil {
			t.Fatal(err)
		}
	}
	quando := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	aplicar([]storage.ImagemNoPull{{ID: id, NotaID: n.ID}, {ID: grande, NotaID: n.ID}}, func(e storage.EstadoDaImagem) storage.AcaoDaImagem {
		if !e.NotaAqui || e.Linha != nil {
			t.Errorf("o estado da imagem nova: %+v", e)
		}
		im := &storage.ImagemDaNota{ID: e.ID, NotaID: n.ID, Largura: 30, Altura: 20, Bytes: int64(len(foto.Bytes)), SHA256: foto.SHA256, CreatedAt: quando}
		if e.ID == grande {
			// Uma imagem da nuvem que sozinha passa do teto daqui: entra.
			im.Bytes, im.SHA256 = imagemdanota.TetoDoTotal+1, "ab"+foto.SHA256[2:]
		}
		return storage.AcaoDaImagem{Inserir: im}
	})
	lida, err := s.notas.Get(ctx, dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lida.Imagens) != 2 || lida.Imagens[0].Estado != storage.EstadoACaminho || lida.Imagens[0].SHA256 != foto.SHA256 {
		t.Fatalf("a nota com a imagem a caminho: %+v", lida.Imagens)
	}
	if _, _, err := s.notas.LerImagem(ctx, dono, n.ID, id); !errors.Is(err, storage.ErrImagemACaminho) {
		t.Errorf("ler a imagem a caminho: %v", err)
	}
	// O teto daqui conta as da nuvem: o anexo novo é recusado.
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, fotoDeTeste(t, 8, 8, 1)); !errors.Is(err, storage.ErrTetoDasImagens) {
		t.Errorf("o anexo acima do teto com a da nuvem: %v", err)
	}
	v, err := s.notas.VarrerImagens(ctx)
	if err != nil || !v.Vazia() || s.linhas(t) != 2 {
		t.Fatalf("a varredura da subida mexeu na que espera: %+v, %v, %d linhas", v, err, s.linhas(t))
	}
	baixar, err := s.fila.ImagensABaixar(ctx, 10, time.Now(), nil)
	if err != nil || len(baixar) != 2 || baixar[0].ID != id || baixar[0].NotaID != n.ID {
		t.Fatalf("a baixar: %+v, %v", baixar, err)
	}

	// Os bytes errados: nada muda.
	if err := s.notas.GuardarBaixada(ctx, id, fotoDeTeste(t, 30, 20, 9)); !errors.Is(err, storage.ErrImagemEstragada) {
		t.Errorf("os bytes com EXIF (não são os guardados): %v", err)
	}
	if err := s.notas.GuardarBaixada(ctx, id, foto.Bytes[:len(foto.Bytes)-1]); !errors.Is(err, storage.ErrImagemEstragada) {
		t.Errorf("os bytes cortados: %v", err)
	}
	if len(s.arquivos(t)) != 0 {
		t.Fatalf("os bytes errados deixaram arquivo: %v", s.arquivos(t))
	}
	// Os certos: o arquivo, e a imagem normal.
	if err := s.notas.GuardarBaixada(ctx, id, foto.Bytes); err != nil {
		t.Fatal(err)
	}
	if arqs := s.arquivos(t); len(arqs) != 1 || arqs[0] != id+".jpg" {
		t.Fatalf("o arquivo baixado: %v", arqs)
	}
	_, b, err := s.notas.LerImagem(ctx, dono, n.ID, id)
	soma := sha256.Sum256(b)
	if err != nil || hex.EncodeToString(soma[:]) != foto.SHA256 {
		t.Fatalf("ler a baixada: %v", err)
	}
	if err := s.notas.GuardarBaixada(ctx, id, foto.Bytes); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("baixar de novo a que já está aqui: %v", err)
	}

	// A espera do download e o que sumiu da nuvem.
	if err := s.fila.AplicarBaixaDaImagem(ctx, grande, storage.AcaoDaImagemNaBaixa{Esperar: true, Erro: "status"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.fila.ImagensABaixar(ctx, 10, time.Now(), func(int) time.Duration { return time.Hour }); len(b) != 0 {
		t.Errorf("a que espera a vez foi devolvida: %+v", b)
	}
	if err := s.fila.AplicarBaixaDaImagem(ctx, grande, storage.AcaoDaImagemNaBaixa{Sumiu: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.linhas(t) != 1 {
		t.Errorf("a que sumiu da nuvem ficou: %d linhas", s.linhas(t))
	}

	// A lápide da nuvem tira a linha; o arquivo sai na varredura sem linhas.
	aplicar([]storage.ImagemNoPull{{ID: id, Tombstone: true}}, func(e storage.EstadoDaImagem) storage.AcaoDaImagem {
		if e.Linha == nil || !e.Linha.NaNuvem || e.Linha.Situacao != storage.SituacaoNormal {
			t.Errorf("o estado da imagem baixada: %+v", e.Linha)
		}
		return storage.AcaoDaImagem{ApagarLinha: true, TirarMarcador: true}
	})
	if n, err := s.notas.TirarArquivosSemLinha(ctx); err != nil || n != 1 || len(s.arquivos(t)) != 0 || s.linhas(t) != 0 {
		t.Fatalf("o arquivo da imagem tirada na nuvem: %d, %v, %v", n, err, s.arquivos(t))
	}
}

// O eco: a imagem daqui que o pull mostra (o PUT cuja resposta se perdeu) ganha
// na_nuvem, e o marcador de criar sai; a lápide local fica.
func TestSyncDasImagens_OEcoTiraOCriarEDeixaALapide(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	if _, _, err := s.fila.Preparar(ctx, "a1", "p1", time.Now()); err != nil {
		t.Fatal(err)
	}
	n := criarNota(t, s.notas, "Eco", "")
	im := s.anexar(t, n.ID, 1)
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{},
		storage.RespostaDasNotas{Imagens: []storage.ImagemNoPull{{ID: im.ID, NotaID: n.ID}},
			DecidirImagem: func(e storage.EstadoDaImagem) storage.AcaoDaImagem {
				if e.Marcador == nil || e.Marcador.Campos != 0 || e.Linha == nil {
					t.Errorf("o estado do eco: %+v", e)
				}
				return storage.AcaoDaImagem{NaNuvem: true, TirarCriar: true}
			}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.marcadorDaImagem(t, im.ID); ok || s.colunaDaImagem(t, im.ID, "na_nuvem") != int64(1) {
		t.Fatalf("o eco: o marcador %v, na_nuvem %v", ok, s.colunaDaImagem(t, im.ID, "na_nuvem"))
	}
	if err := s.notas.TirarImagem(ctx, dono, n.ID, im.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.fila.AplicarTudo(ctx, "a1", "e1", nil, nil, nil, storage.RespostaDoAprendido{},
		storage.RespostaDasNotas{Imagens: []storage.ImagemNoPull{{ID: im.ID, NotaID: n.ID}},
			DecidirImagem: func(storage.EstadoDaImagem) storage.AcaoDaImagem { return storage.AcaoDaImagem{TirarCriar: true} }}); err != nil {
		t.Fatal(err)
	}
	if campos, _, ok := s.marcadorDaImagem(t, im.ID); !ok || campos != storage.CampoApagado {
		t.Fatalf("o TirarCriar tirou a lápide: %d %v", campos, ok)
	}
}

// O recomeço (o epoch novo) põe de volta na fila as imagens que a nuvem tinha,
// e deixa as que esperam o download esperando; o esquecer tira as que esperavam
// e zera o que era da nuvem.
func TestSyncDasImagens_RecomecoEEsquecer(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	s.comCapacidadeEAceite(t)
	if _, _, err := s.fila.Preparar(ctx, "a1", "p1", time.Now()); err != nil {
		t.Fatal(err)
	}
	n := criarNota(t, s.notas, "Recomeço", "")
	daqui := s.anexar(t, n.ID, 1)
	executar(t, s.db, `DELETE FROM sync_outbox`)
	executar(t, s.db, `UPDATE local_note_images SET na_nuvem = 1, tentativas = 3, tentou_em = '2026-10-28T00:00:00.000000000Z'`)
	executar(t, s.db, `INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at, na_nuvem, sync_state)
		VALUES ('0199a000-0000-7000-8000-00000000f010', ?, 1, 1, 10, 'aa', '2026-10-28T00:00:00.000000000Z', 1, 'aguardando')`, n.ID)
	if err := s.fila.Recomecar(ctx, "a1", "p1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if campos, nota, ok := s.marcadorDaImagem(t, daqui.ID); !ok || campos != 0 || nota != n.ID {
		t.Fatalf("o recomeço não pôs a imagem na fila: %d %q %v", campos, nota, ok)
	}
	if _, _, ok := s.marcadorDaImagem(t, "0199a000-0000-7000-8000-00000000f010"); ok {
		t.Error("o recomeço pôs na fila a imagem que espera o download")
	}
	if s.colunaDaImagem(t, daqui.ID, "na_nuvem") != int64(1) || s.colunaDaImagem(t, daqui.ID, "tentativas") != int64(0) {
		t.Error("o recomeço esqueceu que a nuvem pode ter a imagem, ou não zerou a espera")
	}
	if err := s.fila.EsquecerNuvem(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.linhas(t) != 1 || s.colunaDaImagem(t, daqui.ID, "na_nuvem") != int64(0) {
		t.Errorf("o esquecer: %d linhas, na_nuvem %v", s.linhas(t), s.colunaDaImagem(t, daqui.ID, "na_nuvem"))
	}
	if ok, err := s.fila.TemCapacidadeDeImagens(ctx); err != nil || ok {
		t.Errorf("a capacidade das imagens ficou depois do esquecer: %v, %v", ok, err)
	}
}

func TestSyncDasImagens_ACapacidade(t *testing.T) {
	s := novoSyncDasImagens(t)
	ctx := context.Background()
	agora := time.Now()
	for _, c := range []struct {
		tem, primeira, guardada bool
	}{
		{false, false, false}, {true, true, true}, {true, false, true}, {false, false, false}, {true, true, true},
	} {
		primeira, err := s.fila.VerCapacidadeDasImagens(ctx, c.tem, agora)
		if err != nil || primeira != c.primeira {
			t.Fatalf("com %v: primeira = %v (%v), quero %v", c.tem, primeira, err, c.primeira)
		}
		if ok, _ := s.fila.TemCapacidadeDeImagens(ctx); ok != c.guardada {
			t.Fatalf("com %v: a chave = %v, quero %v", c.tem, ok, c.guardada)
		}
	}
}
