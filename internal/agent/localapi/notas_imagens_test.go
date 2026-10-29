package localapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/guard"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas das imagens das notas (ADR-022, N10) sobre o SQLite de verdade, com a
// pasta das imagens num diretório temporário. A conferência da imagem é do
// internal/imagemdanota, e o que segue a nota, do internal/storage: aqui, o
// contrato (o status, o código, os cabeçalhos) e o portão do token.

func servidorDeImagens(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSQLiteNotaRepository(db)
	repo.UsarPastaDasImagens(filepath.Join(t.TempDir(), "notas-imagens"))
	pref := prefsDeTeste()
	pref.Notas = repo
	pref.Imagens = repo
	srv := localapi.New("dono-1", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref)
	return srv.Handler(), db
}

// jpegDeTeste é um JPEG pequeno com um EXIF de mentira (a localização dentro).
func jpegDeTeste(t *testing.T, tom uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for i := range img.Pix {
		img.Pix[i] = tom + uint8(i%7)
	}
	img.Set(0, 0, color.RGBA{A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00GPSLatitude=-23.5")
	n := len(exif) + 2
	b := buf.Bytes()
	out := append([]byte{}, b[:2]...)
	out = append(out, 0xFF, 0xE1, byte(n>>8), byte(n))
	out = append(out, exif...)
	return append(out, b[2:]...)
}

func pedirImagem(h http.Handler, metodo, caminho, tipo string, corpo []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(metodo, caminho, bytes.NewReader(corpo))
	if tipo != "" {
		r.Header.Set("Content-Type", tipo)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type imagemJSON struct {
	ID        string `json:"id"`
	Largura   int    `json:"largura"`
	Altura    int    `json:"altura"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"created_at"`
}

type notaComImagensJSON struct {
	ID      string       `json:"id"`
	Titulo  string       `json:"titulo"`
	Corpo   string       `json:"corpo"`
	Version int64        `json:"version"`
	Imagens []imagemJSON `json:"imagens"`
}

func criarNotaJSON(t *testing.T, h http.Handler, titulo string) string {
	t.Helper()
	w := fazer(t, h, "POST", "/api/notas", map[string]any{"titulo": titulo})
	esperar(t, w, 201)
	return lerNotaJSON(t, w).ID
}

func TestImagens_AnexarLerETirar(t *testing.T) {
	h, _ := servidorDeImagens(t)
	id := criarNotaJSON(t, h, "Recibos")

	w := pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", jpegDeTeste(t, 10))
	esperar(t, w, 201)
	var im imagemJSON
	if err := json.Unmarshal(w.Body.Bytes(), &im); err != nil || im.ID == "" || im.Largura != 24 || im.Altura != 16 || im.Bytes <= 0 || im.CreatedAt == "" {
		t.Fatalf("a imagem: %s (%v)", w.Body.String(), err)
	}
	// Só os metadados: nada de sha256, de caminho ou de nome de arquivo no JSON.
	for _, proibido := range []string{"sha256", "notas-imagens", ".jpg", "\\"} {
		if strings.Contains(w.Body.String(), proibido) {
			t.Errorf("o JSON da imagem tem %q: %s", proibido, w.Body.String())
		}
	}

	w = fazer(t, h, "GET", "/api/notas/"+id, nil)
	esperar(t, w, 200)
	var n notaComImagensJSON
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || len(n.Imagens) != 1 || n.Imagens[0].ID != im.ID || n.Version != 1 {
		t.Fatalf("a nota: %s", w.Body.String())
	}
	w = fazer(t, h, "GET", "/api/notas?vista=ativas", nil)
	esperar(t, w, 200)
	if !strings.Contains(w.Body.String(), `"imagens":[{"id":"`+im.ID) {
		t.Errorf("a lista sem as imagens: %s", w.Body.String())
	}

	w = fazer(t, h, "GET", "/api/notas/"+id+"/imagens/"+im.ID, nil)
	esperar(t, w, 200)
	for cab, quero := range map[string]string{
		"Content-Type": "image/jpeg", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store",
	} {
		if got := w.Header().Get(cab); got != quero {
			t.Errorf("%s = %q, quero %q", cab, got, quero)
		}
	}
	if got := w.Body.Bytes(); int64(len(got)) != im.Bytes || bytes.Contains(got, []byte("Exif")) || got[0] != 0xFF || got[1] != 0xD8 {
		t.Errorf("os bytes: %d (quero %d), com EXIF %v", len(got), im.Bytes, bytes.Contains(got, []byte("Exif")))
	}

	esperar(t, fazer(t, h, "DELETE", "/api/notas/"+id+"/imagens/"+im.ID, nil), 204)
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+id+"/imagens/"+im.ID, nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "DELETE", "/api/notas/"+id+"/imagens/"+im.ID, nil), 404, "NOT_FOUND")
	w = fazer(t, h, "GET", "/api/notas/"+id, nil)
	if !strings.Contains(w.Body.String(), `"imagens":[]`) {
		t.Errorf("a nota sem imagem: %s", w.Body.String())
	}
}

func TestImagens_AsRecusasComCodigo(t *testing.T) {
	h, db := servidorDeImagens(t)
	id := criarNotaJSON(t, h, "Limites")
	foto := jpegDeTeste(t, 1)

	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/png", foto), 415, "IMAGE_INVALID")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "", foto), 415, "IMAGE_INVALID")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", []byte("\x89PNG\r\n\x1a\nxx")), 400, "IMAGE_INVALID")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", foto[:len(foto)/2]), 400, "IMAGE_INVALID")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg",
		append(append([]byte{}, foto...), make([]byte, imagemdanota.MaxBytes)...)), 413, "IMAGE_TOO_LARGE")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/nao-existe/imagens", "image/jpeg", foto), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+id+"/imagens/nao-existe", nil), 404, "NOT_FOUND")

	for i := 0; i < imagemdanota.MaxPorNota; i++ {
		esperar(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", jpegDeTeste(t, uint8(i))), 201)
	}
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", foto), 409, "TOO_MANY_IMAGES")

	// O teto do total: uma linha gorda, e a próxima não cabe. A mensagem é a frase
	// da tela, com o número.
	outra := criarNotaJSON(t, h, "Cheia")
	if _, err := db.Exec(`INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at)
		VALUES ('01900000-0000-7000-8000-000000000001', ?, 1, 1, ?, 'x', '2026-10-25T00:00:00.000000000Z')`,
		outra, imagemdanota.TetoDoTotal); err != nil {
		t.Fatal(err)
	}
	e := esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+outra+"/imagens", "image/jpeg", foto), 409, "IMAGES_FULL")
	if e.Error.Message != "As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras." {
		t.Errorf("a frase do teto: %q", e.Error.Message)
	}

	// A nota da lixeira não recebe imagem.
	esperar(t, fazer(t, h, "DELETE", "/api/notas/"+id, nil), 204)
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", foto), 409, "CONFLICT")
}

func TestImagens_CriarNotaComImagem(t *testing.T) {
	h, _ := servidorDeImagens(t)
	w := pedirImagem(h, "POST", "/api/notas/com-imagem", "image/jpeg", jpegDeTeste(t, 5))
	esperar(t, w, 201)
	var n notaComImagensJSON
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || n.ID == "" || n.Titulo != "" || n.Corpo != "" || len(n.Imagens) != 1 {
		t.Fatalf("a nota só com imagem: %s", w.Body.String())
	}
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/com-imagem", "image/jpeg", []byte("nada")), 400, "IMAGE_INVALID")
	if l := lerLista(t, fazer(t, h, "GET", "/api/notas", nil)); len(l) != 1 {
		t.Errorf("a recusa criou nota: %d notas", len(l))
	}
}

// TestImagens_SemORepositorio: o Agent sem a pasta das imagens responde 503 nas
// rotas, e a nota continua com a lista (vazia) no JSON.
func TestImagens_SemORepositorio(t *testing.T) {
	h, _ := servidorDeNotas(t)
	id := criarNotaJSON(t, h, "Sem imagens")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", jpegDeTeste(t, 1)), 503, "UNAVAILABLE")
	esperarErro(t, pedirImagem(h, "POST", "/api/notas/com-imagem", "image/jpeg", jpegDeTeste(t, 1)), 503, "UNAVAILABLE")
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+id+"/imagens/x", nil), 503, "UNAVAILABLE")
	esperarErro(t, fazer(t, h, "DELETE", "/api/notas/"+id+"/imagens/x", nil), 503, "UNAVAILABLE")
	if w := fazer(t, h, "GET", "/api/notas/"+id, nil); !strings.Contains(w.Body.String(), `"imagens":[]`) {
		t.Errorf("a nota: %s", w.Body.String())
	}
}

// TestImagens_AtrasDoToken: as rotas das imagens estão atrás da mesma cadeia do
// guard que as outras. Sem o token, nada: nem ler, nem anexar, nem tirar.
func TestImagens_AtrasDoToken(t *testing.T) {
	h, _ := servidorDeImagens(t)
	const porta = 51234
	protegido := guard.Protect(porta, "tok")(h)
	id := criarNotaJSON(t, h, "Protegida")
	w := pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", jpegDeTeste(t, 3))
	esperar(t, w, 201)
	var im imagemJSON
	_ = json.Unmarshal(w.Body.Bytes(), &im)

	for _, c := range []struct{ metodo, caminho string }{
		{"GET", "/api/notas/" + id + "/imagens/" + im.ID},
		{"DELETE", "/api/notas/" + id + "/imagens/" + im.ID},
		{"POST", "/api/notas/" + id + "/imagens"},
		{"POST", "/api/notas/com-imagem"},
	} {
		for _, token := range []string{"", "Bearer outro"} {
			r := httptest.NewRequest(c.metodo, c.caminho, bytes.NewReader(jpegDeTeste(t, 4)))
			r.Host = "127.0.0.1:51234"
			r.Header.Set("Content-Type", "image/jpeg")
			if token != "" {
				r.Header.Set("Authorization", token)
			}
			rec := httptest.NewRecorder()
			protegido.ServeHTTP(rec, r)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s com %q: %d, quero 401", c.metodo, c.caminho, token, rec.Code)
			}
		}
	}
	// Com o token, passa.
	r := httptest.NewRequest("GET", "/api/notas/"+id+"/imagens/"+im.ID, nil)
	r.Host = "127.0.0.1:51234"
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	protegido.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Errorf("com o token: %d", rec.Code)
	}
	// A imagem continua lá: os pedidos sem token não tiraram nada.
	if w := fazer(t, h, "GET", "/api/notas/"+id, nil); !strings.Contains(w.Body.String(), im.ID) {
		t.Error("um pedido sem token mexeu na imagem")
	}
}

// A imagem que veio da nuvem e ainda não baixou (N10b): o item da nota leva
// "estado": "a_caminho" (a presente não leva a chave), e a leitura dos bytes é
// 409 IMAGE_PENDING, não um 404 qualquer. Tirar da nota vale do mesmo jeito.
func TestImagens_ACaminhoDaNuvem(t *testing.T) {
	h, db := servidorDeImagens(t)
	id := criarNotaJSON(t, h, "Do celular")
	presente := pedirImagem(h, "POST", "/api/notas/"+id+"/imagens", "image/jpeg", jpegDeTeste(t, 10))
	esperar(t, presente, 201)
	if strings.Contains(presente.Body.String(), "estado") {
		t.Errorf("a imagem presente leva o estado: %s", presente.Body.String())
	}
	const aCaminho = "0199a000-0000-7000-8000-00000000f0f0"
	if _, err := db.Exec(`INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at, na_nuvem, sync_state)
		VALUES (?, ?, 30, 20, 1234, 'aa', '2026-10-28T23:00:00.000000000Z', 1, 'aguardando')`, aCaminho, id); err != nil {
		t.Fatal(err)
	}
	w := fazer(t, h, "GET", "/api/notas/"+id, nil)
	esperar(t, w, 200)
	var n struct {
		Imagens []struct {
			ID     string `json:"id"`
			Estado string `json:"estado"`
		} `json:"imagens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || len(n.Imagens) != 2 {
		t.Fatalf("a nota com a imagem a caminho: %s", w.Body.String())
	}
	for _, im := range n.Imagens {
		if quero := map[bool]string{true: "a_caminho", false: ""}[im.ID == aCaminho]; im.Estado != quero {
			t.Errorf("a imagem %s: estado %q, quero %q", im.ID, im.Estado, quero)
		}
	}
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+id+"/imagens/"+aCaminho, nil), 409, "IMAGE_PENDING")
	esperar(t, fazer(t, h, "DELETE", "/api/notas/"+id+"/imagens/"+aCaminho, nil), 204)
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+id+"/imagens/"+aCaminho, nil), 404, "NOT_FOUND")
}
