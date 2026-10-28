package storage_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As imagens das notas no PC (ADR-022, N10): a linha na tabela, os bytes em
// arquivo na pasta, e a imagem que segue a nota (lixeira, restaurar, expurgo,
// apagar de vez). As fotos nascem do codificador do Go: nenhum arquivo de foto
// de verdade no repositório.

type imagensDeTeste struct {
	db    *sql.DB
	notas *storage.SQLiteNotaRepository
	rel   *relogioDeNotas
	pasta string
	// caminhoDoBanco: o historico.db, para provar que os bytes não estão nele.
	caminhoDoBanco string
}

func novasImagensDeTeste(t *testing.T) imagensDeTeste {
	t.Helper()
	caminho, db := bancoEmArquivo(t)
	notas := storage.NewSQLiteNotaRepository(db)
	rel := &relogioDeNotas{t: inicioDasNotas}
	notas.UsarRelogio(rel.Now)
	pasta := filepath.Join(filepath.Dir(caminho), "notas-imagens")
	notas.UsarPastaDasImagens(pasta)
	return imagensDeTeste{db: db, notas: notas, rel: rel, pasta: pasta, caminhoDoBanco: caminho}
}

// fotoDeTeste é um JPEG w x h com um EXIF de mentira (com a localização) logo
// depois do SOI: o que a foto do celular leva, e o que não pode ser guardado.
func fotoDeTeste(t *testing.T, w, h int, tom uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: tom, G: uint8(x * 3), B: uint8(y * 5), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00GPSLatitude=-23.5505")
	n := len(exif) + 2
	b := buf.Bytes()
	out := append([]byte{}, b[:2]...)
	out = append(out, 0xFF, 0xE1, byte(n>>8), byte(n))
	out = append(out, exif...)
	return append(out, b[2:]...)
}

func (s imagensDeTeste) arquivos(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(s.pasta)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	nomes := []string{}
	for _, e := range es {
		nomes = append(nomes, e.Name())
	}
	sort.Strings(nomes)
	return nomes
}

func (s imagensDeTeste) linhas(t *testing.T) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM local_note_images`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (s imagensDeTeste) anexar(t *testing.T, notaID string, tom uint8) storage.ImagemDaNota {
	t.Helper()
	im, err := s.notas.AnexarImagem(context.Background(), dono, notaID, fotoDeTeste(t, 40, 30, tom))
	if err != nil {
		t.Fatalf("AnexarImagem: %v", err)
	}
	return im
}

func TestImagens_AnexarGuardaOArquivoLimpoEAListaMostra(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Recibos", "táxi")

	im := s.anexar(t, n.ID, 10)
	if im.NotaID != n.ID || im.Largura != 40 || im.Altura != 30 || im.Bytes <= 0 || len(im.SHA256) != 64 {
		t.Fatalf("a imagem: %+v", im)
	}
	if got := s.arquivos(t); len(got) != 1 || got[0] != im.ID+".jpg" {
		t.Fatalf("os arquivos da pasta: %v", got)
	}
	noDisco, err := os.ReadFile(filepath.Join(s.pasta, im.ID+".jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(noDisco, []byte("Exif")) || bytes.Contains(noDisco, []byte("GPS")) {
		t.Error("o arquivo guardado ainda tem o EXIF")
	}
	if int64(len(noDisco)) != im.Bytes {
		t.Errorf("o arquivo tem %d bytes; a linha diz %d", len(noDisco), im.Bytes)
	}
	// Os bytes não estão no banco: as cópias do historico.db não levam as fotos.
	banco, err := os.ReadFile(s.caminhoDoBanco)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(banco, noDisco[len(noDisco)/2:len(noDisco)/2+64]) {
		t.Error("os bytes da imagem estão dentro do historico.db")
	}

	got, err := s.notas.Get(ctx, dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Imagens) != 1 || got.Imagens[0].ID != im.ID || !got.Imagens[0].CreatedAt.Equal(inicioDasNotas) {
		t.Fatalf("Get: as imagens %+v", got.Imagens)
	}
	// Anexar não é editar: a versão e a hora de edição da nota ficam.
	if got.Version != n.Version || !got.UpdatedAt.Equal(n.UpdatedAt) {
		t.Errorf("anexar mexeu na nota: versão %d→%d, hora %v→%v", n.Version, got.Version, n.UpdatedAt, got.UpdatedAt)
	}
	lista, err := s.notas.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 1 || len(lista[0].Imagens) != 1 {
		t.Fatalf("Listar: %+v", lista)
	}
	meta, b, err := s.notas.LerImagem(ctx, dono, n.ID, im.ID)
	if err != nil || !bytes.Equal(b, noDisco) || meta.ID != im.ID {
		t.Fatalf("LerImagem: %v (%d bytes)", err, len(b))
	}

	// A nota sem imagem também vem com a lista (vazia, nunca nil).
	outra := criarNota(t, s.notas, "Sem imagem", "texto")
	if got, _ := s.notas.Get(ctx, dono, outra.ID); got.Imagens == nil || len(got.Imagens) != 0 {
		t.Errorf("a nota sem imagem: %#v", got.Imagens)
	}
}

func TestImagens_AOrdemEADaCriacao(t *testing.T) {
	s := novasImagensDeTeste(t)
	n := criarNota(t, s.notas, "Quadro", "")
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, s.anexar(t, n.ID, uint8(i*40)).ID)
		if i == 0 {
			s.rel.Andar(time.Minute)
		}
	}
	got, err := s.notas.Get(context.Background(), dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, im := range got.Imagens {
		if im.ID != ids[i] {
			t.Fatalf("a ordem: %v, quero %v", got.Imagens, ids)
		}
	}
}

func TestImagens_AsRecusas(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Limites", "")
	foto := fotoDeTeste(t, 20, 20, 1)

	if _, err := s.notas.AnexarImagem(ctx, dono, "nao-existe", foto); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("nota que não existe: %v", err)
	}
	if _, err := s.notas.AnexarImagem(ctx, "outra-pessoa", n.ID, foto); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("nota de outra pessoa: %v", err)
	}
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, []byte("\x89PNG\r\n\x1a\n")); !errors.Is(err, imagemdanota.ErrNaoEJPEG) {
		t.Errorf("um PNG: %v", err)
	}
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, foto[:len(foto)/2]); !errors.Is(err, imagemdanota.ErrInvalida) {
		t.Errorf("um JPEG cortado: %v", err)
	}

	// Dez por nota; a décima primeira é recusada e não deixa arquivo.
	for i := 0; i < imagemdanota.MaxPorNota; i++ {
		s.anexar(t, n.ID, uint8(i))
	}
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, foto); !errors.Is(err, storage.ErrImagensDemais) {
		t.Errorf("a décima primeira: %v", err)
	}
	if got := len(s.arquivos(t)); got != imagemdanota.MaxPorNota || s.linhas(t) != imagemdanota.MaxPorNota {
		t.Errorf("%d arquivos e %d linhas depois da recusa", got, s.linhas(t))
	}

	// A nota da lixeira não recebe imagem.
	lixo := criarNota(t, s.notas, "Lixo", "x")
	if err := s.notas.SoftDelete(ctx, dono, lixo.ID, inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	if _, err := s.notas.AnexarImagem(ctx, dono, lixo.ID, foto); !errors.Is(err, storage.ErrNotaNaLixeira) {
		t.Errorf("na lixeira: %v", err)
	}

	// Sem a pasta montada, nada se grava.
	sem := storage.NewSQLiteNotaRepository(s.db)
	if _, err := sem.AnexarImagem(ctx, dono, lixo.ID, foto); !errors.Is(err, storage.ErrImagensSemPasta) {
		t.Errorf("sem a pasta: %v", err)
	}
}

// TestImagens_OTetoDoTotal: com o total das imagens da pessoa perto do teto, a
// que passaria é recusada (e não deixa arquivo); as de outra pessoa não contam.
func TestImagens_OTetoDoTotal(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Cheia", "")
	foto := fotoDeTeste(t, 20, 20, 1)
	limpa, err := imagemdanota.Conferir(foto)
	if err != nil {
		t.Fatal(err)
	}
	// Uma linha gorda, sem arquivo (o teste não grava 400 MB): o total já está a
	// um byte a menos do que a foto precisa.
	executar(t, s.db, `INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at)
		VALUES ('01900000-0000-7000-8000-000000000001', ?, 1, 1, ?, 'x', '2026-10-25T00:00:00.000000000Z')`,
		n.ID, imagemdanota.TetoDoTotal-len(limpa.Bytes)+1)
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, foto); !errors.Is(err, storage.ErrTetoDasImagens) {
		t.Fatalf("acima do teto: %v", err)
	}
	if got := s.arquivos(t); len(got) != 0 {
		t.Errorf("a recusa deixou arquivo: %v", got)
	}
	// Um byte a menos na linha gorda, e a foto cabe exatamente.
	executar(t, s.db, `UPDATE local_note_images SET bytes = bytes - 1`)
	if _, err := s.notas.AnexarImagem(ctx, dono, n.ID, foto); err != nil {
		t.Fatalf("no teto exato: %v", err)
	}
	// A de outra pessoa não conta para o teto desta.
	if _, err := s.notas.Criar(ctx, storage.NovaNota{UserID: "outra", Titulo: "dela"}); err != nil {
		t.Fatal(err)
	}
	ns, err := s.notas.Listar(ctx, storage.FiltroDeNotas{UserID: "outra"})
	if err != nil || len(ns) != 1 {
		t.Fatal(err)
	}
	if _, err := s.notas.AnexarImagem(ctx, "outra", ns[0].ID, foto); err != nil {
		t.Errorf("a outra pessoa: %v", err)
	}
}

// TestImagens_SeguemANota: a imagem vai para a lixeira com a nota e volta com
// ela; o "Apagar para sempre" e o expurgo de 30 dias a apagam (linha e arquivo).
func TestImagens_SeguemANota(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	a := criarNota(t, s.notas, "Apagar de vez", "")
	b := criarNota(t, s.notas, "Expurgo", "")
	c := criarNota(t, s.notas, "Fica", "")
	imA, imB, imC := s.anexar(t, a.ID, 1), s.anexar(t, b.ID, 2), s.anexar(t, c.ID, 3)

	for _, n := range []storage.Nota{a, b} {
		if err := s.notas.SoftDelete(ctx, dono, n.ID, inicioDasNotas); err != nil {
			t.Fatal(err)
		}
	}
	lixeira, err := s.notas.Listar(ctx, storage.FiltroDeNotas{UserID: dono, Vista: storage.VistaLixeira})
	if err != nil || len(lixeira) != 2 || len(lixeira[0].Imagens) != 1 || len(lixeira[1].Imagens) != 1 {
		t.Fatalf("na lixeira, as notas e as imagens: %v %+v", err, lixeira)
	}
	if len(s.arquivos(t)) != 3 {
		t.Fatalf("a lixeira apagou arquivo: %v", s.arquivos(t))
	}
	// A imagem da nota da lixeira ainda se lê (a nota volta com ela).
	if _, _, err := s.notas.LerImagem(ctx, dono, a.ID, imA.ID); err != nil {
		t.Errorf("ler a imagem da nota da lixeira: %v", err)
	}
	if _, err := s.notas.Restaurar(ctx, dono, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.notas.Get(ctx, dono, a.ID); len(got.Imagens) != 1 || got.Imagens[0].ID != imA.ID {
		t.Fatalf("restaurada, sem a imagem: %+v", got.Imagens)
	}
	if err := s.notas.SoftDelete(ctx, dono, a.ID, inicioDasNotas); err != nil {
		t.Fatal(err)
	}

	if err := s.notas.ApagarDeVez(ctx, dono, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.pasta, imA.ID+".jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("o Apagar para sempre deixou o arquivo: %v", err)
	}
	if n, err := s.notas.PurgeExpired(ctx, inicioDasNotas.AddDate(0, 0, 31)); err != nil || n != 1 {
		t.Fatalf("expurgo: %d, %v", n, err)
	}
	if got := s.arquivos(t); len(got) != 1 || got[0] != imC.ID+".jpg" {
		t.Errorf("depois do expurgo, os arquivos: %v (a de %s devia sair)", got, imB.ID)
	}
	if s.linhas(t) != 1 {
		t.Errorf("%d linhas; só a da nota que ficou devia sobrar", s.linhas(t))
	}
}

func TestImagens_TirarDaNota(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Tirar", "")
	outra := criarNota(t, s.notas, "Outra", "")
	im, fica := s.anexar(t, n.ID, 1), s.anexar(t, n.ID, 2)

	if err := s.notas.TirarImagem(ctx, dono, outra.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("tirar pela nota errada: %v", err)
	}
	if err := s.notas.TirarImagem(ctx, "outra-pessoa", n.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("tirar por outra pessoa: %v", err)
	}
	if err := s.notas.TirarImagem(ctx, dono, n.ID, im.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.arquivos(t); len(got) != 1 || got[0] != fica.ID+".jpg" {
		t.Errorf("depois de tirar, os arquivos: %v", got)
	}
	if err := s.notas.TirarImagem(ctx, dono, n.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("tirar de novo: %v", err)
	}
	// Vale na nota da lixeira (o Desfazer do painel que vence depois de a nota ir
	// para lá), e a versão da nota não muda.
	if err := s.notas.SoftDelete(ctx, dono, n.ID, inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	antes, _ := s.notas.Get(ctx, dono, n.ID)
	if err := s.notas.TirarImagem(ctx, dono, n.ID, fica.ID); err != nil {
		t.Fatalf("tirar na lixeira: %v", err)
	}
	if depois, _ := s.notas.Get(ctx, dono, n.ID); depois.Version != antes.Version || len(depois.Imagens) != 0 {
		t.Errorf("tirar na lixeira: versão %d→%d, %d imagens", antes.Version, depois.Version, len(depois.Imagens))
	}
}

func TestImagens_LerConfereOArquivo(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Ler", "")
	outra := criarNota(t, s.notas, "Outra", "")
	im := s.anexar(t, n.ID, 1)

	if _, _, err := s.notas.LerImagem(ctx, dono, outra.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("pela nota errada: %v", err)
	}
	if _, _, err := s.notas.LerImagem(ctx, "outra-pessoa", n.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("por outra pessoa: %v", err)
	}
	caminho := filepath.Join(s.pasta, im.ID+".jpg")
	b, _ := os.ReadFile(caminho)
	b[len(b)/2] ^= 0xFF
	if err := os.WriteFile(caminho, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.notas.LerImagem(ctx, dono, n.ID, im.ID); !errors.Is(err, storage.ErrImagemEstragada) {
		t.Errorf("o arquivo trocado: %v", err)
	}
	if err := os.Remove(caminho); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.notas.LerImagem(ctx, dono, n.ID, im.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("o arquivo que sumiu: %v", err)
	}
}

// TestImagens_AVarreduraDaSubida: o arquivo sem linha, a linha sem arquivo e o
// temporário que sobrou saem; o que não é da gravação (um desktop.ini) e as
// imagens em ordem ficam.
func TestImagens_AVarreduraDaSubida(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Varrer", "")
	fica := s.anexar(t, n.ID, 1)
	semArquivo := s.anexar(t, n.ID, 2)
	if err := os.Remove(filepath.Join(s.pasta, semArquivo.ID+".jpg")); err != nil {
		t.Fatal(err)
	}
	orfa := "01900000-0000-7000-8000-0000000000aa"
	for _, nome := range []string{orfa + ".jpg", ".gravando-" + orfa + "-123.tmp", "desktop.ini"} {
		if err := os.WriteFile(filepath.Join(s.pasta, nome), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	v, err := s.notas.VarrerImagens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.ArquivosSemLinha != 1 || v.LinhasSemArquivo != 1 || v.Temporarios != 1 {
		t.Errorf("a varredura: %+v", v)
	}
	if got := s.arquivos(t); strings.Join(got, ",") != strings.Join([]string{fica.ID + ".jpg", "desktop.ini"}, ",") {
		t.Errorf("os arquivos que ficaram: %v", got)
	}
	if got, _ := s.notas.Get(ctx, dono, n.ID); len(got.Imagens) != 1 || got.Imagens[0].ID != fica.ID {
		t.Errorf("as imagens da nota: %+v", got.Imagens)
	}
	if v, err := s.notas.VarrerImagens(ctx); err != nil || !v.Vazia() {
		t.Errorf("a segunda varredura: %+v, %v", v, err)
	}
	// Sem a pasta (nenhuma imagem ainda), a varredura não acha nada nem falha.
	limpo := novasImagensDeTeste(t)
	if v, err := limpo.notas.VarrerImagens(ctx); err != nil || !v.Vazia() {
		t.Errorf("sem a pasta: %+v, %v", v, err)
	}
}

// TestImagens_ARodadaDoExpurgoTiraOArquivoQueOSyncDeixou: a lápide da nuvem
// apaga a nota pela Fila, que não conhece a pasta; a rodada do expurgo tira o
// arquivo que ficou. A linha sem arquivo fica para a subida.
func TestImagens_ARodadaDoExpurgoTiraOArquivoQueOSyncDeixou(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n := criarNota(t, s.notas, "Apagada na nuvem", "")
	m := criarNota(t, s.notas, "Sem arquivo", "")
	im := s.anexar(t, n.ID, 1)
	semArquivo := s.anexar(t, m.ID, 2)
	executar(t, s.db, `DELETE FROM local_note_images WHERE id = ?`, im.ID)
	if err := os.Remove(filepath.Join(s.pasta, semArquivo.ID+".jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.notas.PurgeExpired(ctx, inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	if got := s.arquivos(t); len(got) != 0 {
		t.Errorf("o arquivo sem linha ficou: %v", got)
	}
	if s.linhas(t) != 1 {
		t.Errorf("a rodada do expurgo mexeu na linha sem arquivo: %d linhas", s.linhas(t))
	}
}

func TestImagens_CriarNotaComImagem(t *testing.T) {
	s := novasImagensDeTeste(t)
	ctx := context.Background()
	n, err := s.notas.CriarNotaComImagem(ctx, storage.NovaNota{UserID: dono}, fotoDeTeste(t, 30, 20, 9))
	if err != nil {
		t.Fatal(err)
	}
	if n.Titulo != "" || n.Corpo != "" || n.Version != 1 || len(n.Imagens) != 1 || n.Imagens[0].Largura != 30 {
		t.Fatalf("a nota só com imagem: %+v", n)
	}
	// Ela entra no topo das outras, como toda nota nova.
	velha := criarNota(t, s.notas, "Velha", "")
	s.rel.Andar(time.Second)
	n2, err := s.notas.CriarNotaComImagem(ctx, storage.NovaNota{UserID: dono}, fotoDeTeste(t, 30, 20, 3))
	if err != nil {
		t.Fatal(err)
	}
	lista, _ := s.notas.Listar(ctx, storage.FiltroDeNotas{UserID: dono})
	if len(lista) != 3 || lista[0].ID != n2.ID || lista[1].ID != velha.ID {
		t.Errorf("a ordem: %v", titulos(lista))
	}

	// O que não passa não cria nada: nem a nota, nem o arquivo.
	if _, err := s.notas.CriarNotaComImagem(ctx, storage.NovaNota{UserID: dono}, []byte("não")); !errors.Is(err, imagemdanota.ErrNaoEJPEG) {
		t.Errorf("os bytes errados: %v", err)
	}
	if _, err := s.notas.CriarNotaComImagem(ctx, storage.NovaNota{UserID: dono, Token: "abc"}, fotoDeTeste(t, 8, 8, 1)); !errors.Is(err, storage.ErrTokenInvalido) {
		t.Errorf("com token: %v", err)
	}
	if lista, _ := s.notas.Listar(ctx, storage.FiltroDeNotas{UserID: dono}); len(lista) != 3 || len(s.arquivos(t)) != 2 {
		t.Errorf("uma recusa deixou nota ou arquivo: %d notas, %v", len(lista), s.arquivos(t))
	}
}

// TestSchema_VarreduraVeAsColunasDa0014 prova que a varredura de nomes
// proibidos (TestSchema_NoAudioOrContextColumns) lê as colunas da tabela das
// imagens: cada coluna do SQLite migrado aparece como linha de coluna na 0014.
func TestSchema_VarreduraVeAsColunasDa0014(t *testing.T) {
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	conteudo, err := os.ReadFile(filepath.Join("migrations", "0014_notas_imagens.sql"))
	if err != nil {
		t.Fatal(err)
	}
	vistas := map[string]bool{}
	for _, linha := range strings.Split(string(conteudo), "\n") {
		if m := linhaDeColuna.FindStringSubmatch(linha); m != nil {
			vistas[strings.ToLower(m[1])] = true
		}
	}
	cols, err := db.Query(`SELECT name FROM pragma_table_info('local_note_images')`)
	if err != nil {
		t.Fatal(err)
	}
	defer cols.Close()
	total := 0
	for cols.Next() {
		var c string
		if err := cols.Scan(&c); err != nil {
			t.Fatal(err)
		}
		total++
		if !vistas[c] {
			t.Errorf("local_note_images.%s não é vista pela varredura de nomes proibidos", c)
		}
	}
	if total != 7 {
		t.Errorf("a tabela das imagens tem %d colunas, esperava 7", total)
	}
}

// TestImagens_ODesfazerDoVirarNotaNaoLevaANotaComImagem: anexar não sobe a
// versão, e o Desfazer do "Virar nota" apaga a nota que está na versão 1. A nota
// com imagem fica (a pessoa já mexeu nela); só o ditado volta ao histórico.
func TestImagens_ODesfazerDoVirarNotaNaoLevaANotaComImagem(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.notas.UsarPastaDasImagens(t.TempDir())
	v.ditado(t, "d1", "bruto", "final")
	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.AnexarImagem(ctx, v.dono, n.ID, fotoDeTeste(t, 10, 10, 1)); err != nil {
		t.Fatal(err)
	}
	d, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.NotaApagada {
		t.Fatal("o Desfazer apagou a nota que tinha imagem")
	}
	if got, err := v.notas.Get(ctx, v.dono, n.ID); err != nil || len(got.Imagens) != 1 {
		t.Errorf("a nota depois do Desfazer: %v, %+v", err, got.Imagens)
	}
}
