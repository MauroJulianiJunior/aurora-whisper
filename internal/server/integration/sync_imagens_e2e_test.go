package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A ponta a ponta das imagens das notas (ADR-022, N10b): dois PCs e o celular
// contra a nuvem de verdade (o palco de sync_harness_test.go), com a pasta das
// imagens de cada PC. O que só se vê com os dois lados juntos: a imagem anexada
// num PC chega ao outro (o PUT, os metadados no pull, o GET conferido); a do
// celular chega ao PC; o "Tirar da nota" sai de todo lado; a imagem de antes
// desta versão sobe depois do aceite; e um arquivo estragado não prende o resto.

// imagensDoPC é o repositório das notas do PC com a pasta das imagens (uma por
// PC, na pasta temporária), e o laço refeito com elas.
func (pc *pcE2E) comImagens() *storage.SQLiteNotaRepository {
	pc.t.Helper()
	r := storage.NewSQLiteNotaRepository(pc.db)
	r.UsarPastaDasImagens(filepath.Join(filepath.Dir(pc.caminho), "notas-imagens"))
	pc.laco = syncengine.NovoLaco(syncengine.ConfigDoLaco{
		Fila: pc.repo.Fila(pc.dono), Conexao: conexaoDoTeste{pc.servico}, NovoID: storage.NewID, Log: pc.logger,
		Intervalo: 3600e9, Debounce: 10e6, Espera: syncengine.BackoffPolicy{Base: 20e6, Max: 50e6},
		Imagens: r,
	})
	return r
}

// fotoDoE2E é um JPEG com um EXIF de mentira (a localização) depois do SOI.
func fotoDoE2E(t *testing.T, tom uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			img.SetRGBA(x, y, color.RGBA{R: tom, G: uint8(x * 5), B: uint8(y * 7), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00GPSLatitude=-23.5505")
	b := buf.Bytes()
	out := append([]byte{}, b[:2]...)
	out = append(out, 0xFF, 0xE1, byte((len(exif)+2)>>8), byte(len(exif)+2))
	out = append(out, exif...)
	return append(out, b[2:]...)
}

func (n *nuvemE2E) celularBytes(metodo, caminho string, b []byte) (int, []byte) {
	n.t.Helper()
	req, _ := http.NewRequest(metodo, n.base+caminho, bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: n.sessao})
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "image/jpeg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.t.Fatalf("celular %s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func bytesDaImagem(t *testing.T, r *storage.SQLiteNotaRepository, dono, nota, id string) []byte {
	t.Helper()
	_, b, err := r.LerImagem(context.Background(), dono, nota, id)
	if err != nil {
		t.Fatalf("ler a imagem %s: %v", id, err)
	}
	return b
}

func TestSyncImagensE2E_DoPCAoOutroPCDoCelularAoPCETirar(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	img1, img2 := pc1.comImagens(), pc2.comImagens()
	nota, err := img1.Criar(ctx, storage.NovaNota{UserID: pc1.dono, Titulo: "Recibos"})
	if err != nil {
		t.Fatal(err)
	}
	im, err := img1.AnexarImagem(ctx, pc1.dono, nota.ID, fotoDoE2E(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	pc1.duasRodadas()
	pc2.duasRodadas()

	// O PC2 tem a nota com a imagem, com os bytes de verdade (sem o EXIF).
	got := pc2.nota(nota.ID)
	if len(got.Imagens) != 1 || got.Imagens[0].ID != im.ID || got.Imagens[0].Estado != "" || got.Imagens[0].SHA256 != im.SHA256 {
		t.Fatalf("a imagem no PC2: %+v\n%s", got.Imagens, pc2.log.String())
	}
	b := bytesDaImagem(t, img2, pc2.dono, nota.ID, im.ID)
	if !bytes.Equal(b, bytesDaImagem(t, img1, pc1.dono, nota.ID, im.ID)) || bytes.Contains(b, []byte("GPS")) {
		t.Fatal("os bytes no PC2 não são os do PC1 (ou têm o EXIF)")
	}
	if got.Imagens[0].CreatedAt.Sub(im.CreatedAt).Abs() > 1e3 {
		t.Errorf("a hora do anexo no PC2: %v, no PC1 %v", got.Imagens[0].CreatedAt, im.CreatedAt)
	}
	// A versão da nota não mudou com o anexo.
	if got.Version != pc1.nota(nota.ID).Version && got.Version != 1 {
		t.Errorf("a versão da nota no PC2: %d", got.Version)
	}

	// O celular anexa; o PC1 recebe e baixa.
	st, corpo := n.celularBytes(http.MethodPost, "/api/v1/notes/"+nota.ID+"/images", fotoDoE2E(t, 20))
	if st != http.StatusCreated {
		t.Fatalf("o anexo do celular: %d %s", st, corpo)
	}
	var doCelular struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(corpo, &doCelular); err != nil || doCelular.ID == "" {
		t.Fatalf("a resposta do anexo do celular: %s", corpo)
	}
	pc1.rodadaOK()
	if got := pc1.nota(nota.ID); len(got.Imagens) != 2 || got.Imagens[1].ID != doCelular.ID || got.Imagens[1].Estado != "" {
		t.Fatalf("a imagem do celular no PC1: %+v\n%s", got.Imagens, pc1.log.String())
	}
	_ = bytesDaImagem(t, img1, pc1.dono, nota.ID, doCelular.ID)

	// O PC2 tira a do PC1: sai do PC1 (a linha e o arquivo) e do celular.
	pc2.rodadaOK()
	if err := img2.TirarImagem(ctx, pc2.dono, nota.ID, im.ID); err != nil {
		t.Fatal(err)
	}
	pc2.rodadaOK()
	pc1.rodadaOK()
	if got := pc1.nota(nota.ID); len(got.Imagens) != 1 || got.Imagens[0].ID != doCelular.ID {
		t.Fatalf("a imagem tirada no PC2 ficou no PC1: %+v", got.Imagens)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(pc1.caminho), "notas-imagens", im.ID+".jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("o arquivo da imagem tirada ficou no PC1: %v", err)
	}
	if st, _ := n.celular(http.MethodGet, "/api/v1/notes/"+nota.ID+"/images/"+im.ID, nil); st != http.StatusNotFound {
		t.Errorf("a imagem tirada no celular: %d", st)
	}

	// Ninguém ecoa: sem mudança, nenhum pedido de imagem.
	n.porteiro.zerar()
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "imagem:") {
		t.Errorf("houve pedido de imagem sem mudança: %v", pedidos)
	}

	// Apagar a nota de vez no celular leva a imagem dos dois PCs.
	if st, b := n.celular(http.MethodDelete, "/api/v1/notes/"+nota.ID, nil); st != http.StatusNoContent {
		t.Fatalf("a lixeira no celular: %d %s", st, b)
	}
	if st, b := n.celular(http.MethodPost, "/api/v1/notes/"+nota.ID+"/purge", nil); st != http.StatusNoContent {
		t.Fatalf("apagar de vez no celular: %d %s", st, b)
	}
	pc1.rodadaOK()
	pc2.rodadaOK()
	for _, pc := range []*pcE2E{pc1, pc2} {
		var linhas int
		if err := pc.db.QueryRow(`SELECT COUNT(*) FROM local_note_images`).Scan(&linhas); err != nil || linhas != 0 {
			t.Errorf("as imagens da nota apagada de vez ficaram: %d (%v)", linhas, err)
		}
		if es, _ := os.ReadDir(filepath.Join(filepath.Dir(pc.caminho), "notas-imagens")); len(es) != 0 {
			t.Errorf("os arquivos da nota apagada de vez ficaram: %d", len(es))
		}
	}
}

// A imagem anexada antes desta versão (sem marcador na fila: a N10a não punha)
// sobe pela completação, com o aceite do "Enviar o histórico", como as notas da
// N7; e um arquivo estragado não prende a rodada nem a outra imagem.
func TestSyncImagensE2E_AsDeAntesSobemEOEstragadoNaoPrende(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	img1, img2 := pc1.comImagens(), pc2.comImagens()
	nota, err := img1.Criar(ctx, storage.NovaNota{UserID: pc1.dono, Corpo: "a viagem"})
	if err != nil {
		t.Fatal(err)
	}
	boa, err := img1.AnexarImagem(ctx, pc1.dono, nota.ID, fotoDoE2E(t, 30))
	if err != nil {
		t.Fatal(err)
	}
	ruim, err := img1.AnexarImagem(ctx, pc1.dono, nota.ID, fotoDoE2E(t, 40))
	if err != nil {
		t.Fatal(err)
	}
	// O que a N10a deixou: as imagens sem marcador; e o arquivo de uma, estragado.
	if _, err := pc1.db.Exec(`DELETE FROM sync_outbox WHERE entity_type = 'note_image'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(pc1.caminho), "notas-imagens", ruim.ID+".jpg"), []byte("estragado"), 0o600); err != nil {
		t.Fatal(err)
	}
	// O aceite: aqui, o PC sem ditado nenhum a mandar o grava sozinho na
	// primeira rodada (CompletarEnvio); sem ele, nada das imagens de antes sobe
	// (o teste do storage, TestSyncDasImagens_ACompletacaoLevaAsDeAntesDestaVersao).
	pc1.duasRodadas()
	pc2.duasRodadas()
	got := pc2.nota(nota.ID)
	if len(got.Imagens) != 1 || got.Imagens[0].ID != boa.ID {
		t.Fatalf("a imagem de antes não chegou ao PC2, ou a estragada subiu: %+v\n%s", got.Imagens, pc1.log.String())
	}
	_ = bytesDaImagem(t, img2, pc2.dono, nota.ID, boa.ID)
	if !strings.Contains(pc1.log.String(), "imagens_esperando=1") {
		t.Errorf("o log da rodada não contou a estragada esperando:\n%s", pc1.log.String())
	}
	// Nada de bytes, tamanho ou SHA-256 no log dos PCs nem da nuvem.
	for _, l := range []string{pc1.log.String(), pc2.log.String(), n.log.String()} {
		if strings.Contains(l, boa.SHA256) || strings.Contains(l, boa.ID) {
			t.Errorf("o log leva a imagem:\n%s", l)
		}
	}
}
