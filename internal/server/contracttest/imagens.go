package contracttest

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// As imagens das notas (ADR-022, N10b; imagens_routes.go e cloudstore/imagens.go),
// pela API HTTP, contra os dois Stores: o PUT idempotente do PC, o SHA-256 que não
// bate, o id de outros bytes, a lápide que não deixa ressuscitar, o máximo por
// nota, a nota da lixeira (anexar não, tirar sim), a cascata do expurgo, do
// apagar de vez e do "Apagar meus dados", as rotas do celular e o PC de antes da
// N10b, que lê o pull sem erro e não aplica a imagem. O teto do total (400 MB)
// é a regra pura (cloudstore, TestDecidirAnexo): enchê-lo aqui seriam 100
// imagens de 4 MB em cada Store.

// imagemJSON é a imagem no fio (o imageDTO): só os metadados.
type imagemJSON struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"note_id"`
	Largura   int       `json:"largura"`
	Altura    int       `json:"altura"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

// chavesDaImagem são as chaves exatas da imagem no fio.
var chavesDaImagem = []string{"altura", "bytes", "created_at", "id", "largura", "note_id", "sha256"}

// jpegDaSuite é um JPEG de w por h com as cores de semente (outra semente,
// outros bytes), sem metadados: o Conferir o devolve igual.
func jpegDaSuite(t *testing.T, w, h, semente int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x*5 + semente), G: uint8(y*3 + semente*7), B: uint8(semente * 13), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// comExif põe um APP1 com "Exif" e uma localização de mentira logo depois do SOI
// (o que a foto do celular traz e o Conferir tira).
func comExif(b []byte) []byte {
	conteudo := []byte("Exif\x00\x00GPS -23.55,-46.63 de mentira")
	seg := []byte{0xFF, 0xE1, byte((len(conteudo) + 2) >> 8), byte(len(conteudo) + 2)}
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	out = append(out, conteudo...)
	return append(out, b[2:]...)
}

// limpaDe é o que o Conferir guardaria de b (o SHA-256 é o do PUT).
func limpaDe(t *testing.T, b []byte) imagemdanota.Limpa {
	t.Helper()
	l, err := imagemdanota.Conferir(b)
	if err != nil {
		t.Fatalf("o JPEG da suíte não passa no Conferir: %v", err)
	}
	return l
}

// doBytes manda b cru, com o Content-Type e os cabeçalhos dados.
func (w *world) doBytes(method, path, token string, b []byte, tipo string, cab map[string]string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.doWith(method, path, token, string(b), func(r *http.Request) {
		r.Header.Set("Content-Type", tipo)
		r.ContentLength = int64(len(b))
		for k, v := range cab {
			r.Header.Set(k, v)
		}
	})
}

func caminhoDoSync(nota, imagem string) string {
	return "/api/v1/sync/notes/" + nota + "/images/" + imagem
}

func caminhoDoCelular(nota, imagem string) string {
	return "/api/v1/notes/" + nota + "/images/" + imagem
}

// putImagem é o PUT do PC, com o SHA-256 da cópia limpa e a hora do anexo.
func (w *world) putImagem(token, nota, imagem string, b []byte, criada time.Time) *httptest.ResponseRecorder {
	w.t.Helper()
	cab := map[string]string{"Imagem-SHA256": limpaDe(w.t, b).SHA256}
	if !criada.IsZero() {
		cab["Imagem-Criada-Em"] = criada.Format(time.RFC3339Nano)
	}
	return w.doBytes(http.MethodPut, caminhoDoSync(nota, imagem), token, b, "image/jpeg", cab)
}

// lerImagemDoFio decodifica uma imagem do JSON, com as chaves exatas.
func lerImagemDoFio(t *testing.T, raw json.RawMessage) imagemJSON {
	t.Helper()
	conferirChaves(t, raw, chavesDaImagem, "a imagem")
	var im imagemJSON
	if err := json.Unmarshal(raw, &im); err != nil {
		t.Fatalf("a imagem: %v", err)
	}
	return im
}

func querStatus(t *testing.T, o string, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("%s: %d %s, quero %d %s", o, rec.Code, rec.Body.String(), status, code)
	}
	if code != "" {
		if got, _ := errorOf(t, rec); got != code {
			t.Fatalf("%s: o código %q, quero %q (%s)", o, got, code, rec.Body.String())
		}
	}
}

// notaComImagemDoPC cria a nota pelo push do PC 1 e sobe uma imagem nela.
func (w *world) notaComImagemDoPC(t *testing.T) (nota, imagem string, b []byte) {
	t.Helper()
	nota, imagem = newID(), newID()
	w.mustNota(pcA1, criarNota(nota, nil), 1, false)
	b = jpegDaSuite(t, 32, 24, 1)
	querStatus(t, "o PUT", w.putImagem(pcA1, nota, imagem, b, now0.Add(-time.Hour)), http.StatusCreated, "")
	return nota, imagem, b
}

// O PC sobe a imagem, o outro PC a recebe no pull (só os metadados, a mudança
// note_image e a lista imagens da nota) e baixa os bytes; o reenvio é 200 sem
// gastar seq; o SHA-256 errado, o id de outros bytes, o que não é JPEG e o corpo
// grande demais são recusados sem gravar.
func testImagensSobemEDescem(t *testing.T, w *world) {
	nota, imagem := newID(), newID()
	w.mustNota(pcA1, criarNota(nota, nil), 1, false) // seq 1
	b := jpegDaSuite(t, 32, 24, 1)
	limpa := limpaDe(t, b)
	criada := now0.Add(-time.Hour)

	rec := w.putImagem(pcA1, nota, imagem, b, criada) // seq 2
	querStatus(t, "o PUT", rec, http.StatusCreated, "")
	im := lerImagemDoFio(t, rec.Body.Bytes())
	if im.ID != imagem || im.NoteID != nota || im.Largura != 32 || im.Altura != 24 || im.Bytes != int64(len(limpa.Bytes)) ||
		im.SHA256 != limpa.SHA256 || !im.CreatedAt.Equal(criada) {
		t.Fatalf("a imagem do PUT = %+v (a hora do anexo é a do PC)", im)
	}
	// O reenvio (a resposta que se perdeu): 200 com o que está gravado, sem seq.
	rec = w.putImagem(pcA1, nota, strings.ToUpper(imagem), b, criada)
	querStatus(t, "o reenvio", rec, http.StatusOK, "")
	if de := lerImagemDoFio(t, rec.Body.Bytes()); de != im {
		t.Fatalf("o reenvio = %+v, quero %+v", de, im)
	}

	p := w.pullNotas(pcA2, 0, 0)
	if len(p.Changes) != 2 || p.LastSeq != 2 {
		t.Fatalf("o pull = %+v, quero a nota e a imagem, até o seq 2 (o reenvio não gasta seq)", p)
	}
	c := p.Changes[1]
	if c.Seq != 2 || c.EntityType != "note_image" || c.EntityID != imagem || c.Operation != "upsert" || c.EntityVersion != 1 {
		t.Fatalf("a mudança da imagem = %+v", c)
	}
	if de := lerImagemDoFio(t, c.Payload); de != im {
		t.Errorf("a imagem no pull = %+v, quero %+v", de, im)
	}
	n := lerNota(t, p.Changes[0].Payload)
	if len(n.Imagens) != 1 || n.Imagens[0] != im || n.Version != 1 {
		t.Errorf("a nota no pull = %+v: quero a imagem na lista, e a versão 1 (anexar não muda a nota)", n)
	}

	for _, quem := range []struct{ token, caminho string }{{pcA2, caminhoDoSync(nota, imagem)}, {celA, caminhoDoCelular(nota, imagem)}} {
		rec = w.do(http.MethodGet, quem.caminho, quem.token, nil)
		querStatus(t, "o GET de "+quem.token, rec, http.StatusOK, "")
		if !bytes.Equal(rec.Body.Bytes(), limpa.Bytes) {
			t.Errorf("%s: os bytes não são os da cópia limpa", quem.token)
		}
		h := rec.Header()
		if h.Get("Content-Type") != "image/jpeg" || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: os cabeçalhos = %v", quem.token, h)
		}
	}

	// As recusas, nenhuma grava nem gasta seq.
	outro := jpegDaSuite(t, 32, 24, 2)
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, imagem), pcA1, outro, "image/jpeg",
		map[string]string{"Imagem-SHA256": limpaDe(t, outro).SHA256})
	querStatus(t, "o mesmo id com outros bytes", rec, http.StatusConflict, "IMAGE_CONFLICT")
	nova := newID()
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, nova), pcA1, outro, "image/jpeg",
		map[string]string{"Imagem-SHA256": limpa.SHA256})
	querStatus(t, "o SHA-256 que não é o dos bytes", rec, http.StatusBadRequest, "IMAGE_SHA_MISMATCH")
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, nova), pcA1, outro, "image/jpeg", nil)
	querStatus(t, "sem o Imagem-SHA256", rec, http.StatusBadRequest, "VALIDATION")
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, nova), pcA1, []byte("não é imagem"), "image/jpeg",
		map[string]string{"Imagem-SHA256": strings.Repeat("ab", 32)})
	querStatus(t, "o que não é JPEG", rec, http.StatusBadRequest, "IMAGE_INVALID")
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, nova), pcA1, outro, "image/png",
		map[string]string{"Imagem-SHA256": limpaDe(t, outro).SHA256})
	querStatus(t, "o Content-Type que não é image/jpeg", rec, http.StatusUnsupportedMediaType, "IMAGE_INVALID")
	grande := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, imagemdanota.MaxBytes)...)
	rec = w.doBytes(http.MethodPut, caminhoDoSync(nota, nova), pcA1, grande, "image/jpeg",
		map[string]string{"Imagem-SHA256": strings.Repeat("ab", 32)})
	querStatus(t, "o corpo acima de 4 MB", rec, http.StatusRequestEntityTooLarge, "IMAGE_TOO_LARGE")
	rec = w.putImagem(pcA1, newID(), nova, outro, time.Time{})
	querStatus(t, "a nota que não existe", rec, http.StatusNotFound, "NOT_FOUND")
	rec = w.putImagem(pcB, nota, nova, outro, time.Time{})
	querStatus(t, "a nota de outra conta", rec, http.StatusNotFound, "NOT_FOUND")
	rec = w.do(http.MethodGet, caminhoDoSync(nota, imagem), pcB, nil)
	querStatus(t, "o GET de outra conta", rec, http.StatusNotFound, "NOT_FOUND")
	if p := w.pullNotas(pcA2, 2, 0); len(p.Changes) != 0 {
		t.Fatalf("as recusas gravaram: %+v", p.Changes)
	}

	// A hora do PC além da folga para o futuro vira a de agora.
	rec = w.putImagem(pcA1, nota, nova, outro, now0.Add(48*time.Hour))
	querStatus(t, "a hora do futuro", rec, http.StatusCreated, "")
	if im := lerImagemDoFio(t, rec.Body.Bytes()); !im.CreatedAt.Equal(now0) {
		t.Errorf("a hora do anexo do futuro = %v, quero a de agora", im.CreatedAt)
	}
}

// Tirar da nota deixa a lápide na linha: o pull leva o tombstone, o GET é 404, e
// o PUT atrasado do mesmo id é 410, sem ressuscitar. Tirar de novo é 204 sem
// gastar seq. O id que a nuvem nunca teve também ganha a lápide (o PUT que ainda
// estava na rede chega depois e recebe 410).
func testImagensTirarELapide(t *testing.T, w *world) {
	nota, imagem, b := w.notaComImagemDoPC(t) // seqs 1 e 2

	querStatus(t, "o DELETE", w.do(http.MethodDelete, caminhoDoSync(nota, imagem), pcA1, nil), http.StatusNoContent, "") // seq 3
	p := w.pullNotas(pcA2, 2, 0)
	if len(p.Changes) != 1 || p.Changes[0].Seq != 3 || p.Changes[0].EntityType != "note_image" ||
		p.Changes[0].EntityID != imagem || p.Changes[0].Operation != "tombstone" || len(p.Changes[0].Payload) != 0 {
		t.Fatalf("o pull depois de tirar = %+v, quero o tombstone da imagem no seq 3", p.Changes)
	}
	querStatus(t, "o GET da imagem tirada", w.do(http.MethodGet, caminhoDoSync(nota, imagem), pcA2, nil), http.StatusNotFound, "NOT_FOUND")
	querStatus(t, "o PUT atrasado", w.putImagem(pcA1, nota, imagem, b, time.Time{}), http.StatusGone, "IMAGE_GONE")
	querStatus(t, "tirar de novo", w.do(http.MethodDelete, caminhoDoSync(nota, imagem), pcA2, nil), http.StatusNoContent, "")
	if n := w.notaNoPull(pcA2, nota); len(n.Imagens) != 0 || n.Version != 1 {
		t.Errorf("a nota depois de tirar = %+v: quero sem imagens, e a versão 1", n)
	}

	nunca := newID()
	querStatus(t, "tirar o que nunca chegou", w.do(http.MethodDelete, caminhoDoSync(nota, nunca), pcA1, nil), http.StatusNoContent, "") // seq 4
	querStatus(t, "o PUT que estava na rede", w.putImagem(pcA1, nota, nunca, b, time.Time{}), http.StatusGone, "IMAGE_GONE")
	p = w.pullNotas(pcA2, 3, 0)
	if len(p.Changes) != 1 || p.Changes[0].Seq != 4 || p.Changes[0].Operation != "tombstone" || p.Changes[0].EntityID != nunca {
		t.Fatalf("a lápide do id que nunca chegou = %+v", p.Changes)
	}
	querStatus(t, "tirar de uma nota que não existe", w.do(http.MethodDelete, caminhoDoSync(newID(), newID()), pcA1, nil),
		http.StatusNotFound, "NOT_FOUND")
	querStatus(t, "tirar o id que é de outra nota", w.do(http.MethodDelete, caminhoDoSync(newID(), nunca), pcA1, nil),
		http.StatusNotFound, "NOT_FOUND")
}

// O máximo por nota (10) e a nota da lixeira: anexar não (nem o PC, nem o
// celular), tirar sim.
func testImagensLimitesELixeira(t *testing.T, w *world) {
	nota := newID()
	w.mustNota(pcA1, criarNota(nota, nil), 1, false)
	for i := 0; i < imagemdanota.MaxPorNota; i++ {
		querStatus(t, "a imagem "+strconv.Itoa(i+1), w.putImagem(pcA1, nota, newID(), jpegDaSuite(t, 8, 8, i), time.Time{}),
			http.StatusCreated, "")
	}
	antes := w.pullNotas(pcA2, 0, 0).LastSeq
	querStatus(t, "a 11ª pelo PC", w.putImagem(pcA1, nota, newID(), jpegDaSuite(t, 8, 8, 99), time.Time{}),
		http.StatusConflict, "TOO_MANY_IMAGES")
	rec := w.doBytes(http.MethodPost, "/api/v1/notes/"+nota+"/images", celA, jpegDaSuite(t, 8, 8, 98), "image/jpeg", nil)
	querStatus(t, "a 11ª pelo celular", rec, http.StatusConflict, "TOO_MANY_IMAGES")
	if depois := w.pullNotas(pcA2, 0, 0).LastSeq; depois != antes {
		t.Fatalf("a recusa gastou seq: %d -> %d", antes, depois)
	}

	lixo, imagem, _ := w.notaComImagemDoPC(t)
	querStatus(t, "a nota para a lixeira", w.do(http.MethodDelete, "/api/v1/notes/"+lixo, celA, nil), http.StatusNoContent, "")
	querStatus(t, "o PC anexa na lixeira", w.putImagem(pcA1, lixo, newID(), jpegDaSuite(t, 8, 8, 50), time.Time{}),
		http.StatusConflict, "CONFLICT")
	rec = w.doBytes(http.MethodPost, "/api/v1/notes/"+lixo+"/images", celA, jpegDaSuite(t, 8, 8, 51), "image/jpeg", nil)
	querStatus(t, "o celular anexa na lixeira", rec, http.StatusConflict, "CONFLICT")
	querStatus(t, "a imagem da nota da lixeira se lê", w.do(http.MethodGet, caminhoDoCelular(lixo, imagem), celA, nil), http.StatusOK, "")
	querStatus(t, "tirar na lixeira", w.do(http.MethodDelete, caminhoDoCelular(lixo, imagem), celA, nil), http.StatusNoContent, "")
	if ns, _ := w.listarNotas(celA, "lixeira"); len(ns) != 1 || len(ns[0].Imagens) != 0 {
		t.Errorf("a lixeira = %+v, quero a nota sem a imagem", ns)
	}
}

// A imagem sai com a nota: o apagar de vez do celular, a lápide do PC e o
// "Apagar meus dados". O PUT atrasado de uma imagem da nota apagada de vez é 410.
func testImagensSaemComANota(t *testing.T, w *world) {
	nota, imagem, b := w.notaComImagemDoPC(t)
	querStatus(t, "a lixeira", w.do(http.MethodDelete, "/api/v1/notes/"+nota, celA, nil), http.StatusNoContent, "")
	querStatus(t, "apagar de vez", w.do(http.MethodPost, "/api/v1/notes/"+nota+"/purge", celA, nil), http.StatusNoContent, "")
	querStatus(t, "o GET depois do apagar de vez", w.do(http.MethodGet, caminhoDoSync(nota, imagem), pcA2, nil), http.StatusNotFound, "NOT_FOUND")
	querStatus(t, "o PUT atrasado da nota apagada", w.putImagem(pcA1, nota, imagem, b, time.Time{}), http.StatusGone, "IMAGE_GONE")
	querStatus(t, "o PUT de outra imagem da nota apagada", w.putImagem(pcA1, nota, newID(), b, time.Time{}), http.StatusGone, "IMAGE_GONE")
	querStatus(t, "tirar da nota apagada", w.do(http.MethodDelete, caminhoDoSync(nota, imagem), pcA1, nil), http.StatusNoContent, "")
	for _, c := range w.pullNotas(pcA2, 0, 0).Changes {
		if c.EntityType == "note_image" {
			t.Errorf("a imagem ficou no pull depois do apagar de vez: %+v", c)
		}
	}

	pelaLapide, outra, _ := w.notaComImagemDoPC(t)
	w.mustNota(pcA1, apagarNota(pelaLapide), 2, false)
	querStatus(t, "o GET depois da lápide do PC", w.do(http.MethodGet, caminhoDoSync(pelaLapide, outra), pcA2, nil), http.StatusNotFound, "NOT_FOUND")

	ficou, guardada, _ := w.notaComImagemDoPC(t)
	querStatus(t, "apagar meus dados", w.do(http.MethodDelete, "/api/v1/me/data", celA, map[string]any{"confirmar": "apagar"}),
		http.StatusNoContent, "")
	querStatus(t, "o GET depois do apagar meus dados", w.do(http.MethodGet, caminhoDoCelular(ficou, guardada), celA, nil), http.StatusNotFound, "NOT_FOUND")
	if ns, _ := w.listarNotas(celA, "ativas"); len(ns) != 0 {
		t.Errorf("as notas depois do apagar meus dados = %+v", ns)
	}
}

// O celular: a nota nova só com a imagem, o anexo (o id é do servidor), a leitura
// sem cache e sem os metadados da foto, e o tirar; o PC recebe tudo pelo pull, a
// nota antes da imagem. O token do PC não usa as rotas do celular, e a sessão
// não usa as do sync.
func testImagensDoCelular(t *testing.T, w *world) {
	foto := comExif(jpegDaSuite(t, 40, 30, 7))
	rec := w.doBytes(http.MethodPost, "/api/v1/notes/with-image", celA, foto, "image/jpeg", nil)
	querStatus(t, "a nota nova com a imagem", rec, http.StatusCreated, "")
	n := lerNota(t, rec.Body.Bytes())
	if n.Titulo != "" || n.Corpo != "" || n.Version != 1 || len(n.Imagens) != 1 || n.Imagens[0].Largura != 40 {
		t.Fatalf("a nota nova = %+v", n)
	}
	primeira := n.Imagens[0]
	rec = w.do(http.MethodGet, caminhoDoCelular(n.ID, primeira.ID), celA, nil)
	querStatus(t, "o GET do celular", rec, http.StatusOK, "")
	if bytes.Contains(rec.Body.Bytes(), []byte("Exif")) || bytes.Contains(rec.Body.Bytes(), []byte("GPS")) ||
		int64(rec.Body.Len()) != primeira.Bytes || limpaDe(t, rec.Body.Bytes()).SHA256 != primeira.SHA256 {
		t.Errorf("os bytes guardados ainda têm os metadados da foto, ou não são os da lista")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("o Cache-Control = %q, quero no-store (o celular nunca guarda a imagem)", rec.Header().Get("Cache-Control"))
	}

	// Um segundo depois: a ordem das imagens é a da hora do anexo.
	w.clk.Advance(time.Second)
	rec = w.doBytes(http.MethodPost, "/api/v1/notes/"+n.ID+"/images", celA, jpegDaSuite(t, 20, 20, 8), "image/jpeg", nil)
	querStatus(t, "o anexo do celular", rec, http.StatusCreated, "")
	segunda := lerImagemDoFio(t, rec.Body.Bytes())
	if segunda.NoteID != n.ID || segunda.ID == "" || !segunda.CreatedAt.Equal(now0.Add(time.Second)) {
		t.Fatalf("o anexo do celular = %+v", segunda)
	}

	p := w.pullNotas(pcA1, 0, 0)
	var tipos []string
	for _, c := range p.Changes {
		tipos = append(tipos, c.EntityType)
	}
	if strings.Join(tipos, ",") != "note,note_image,note_image" {
		t.Fatalf("o pull do PC = %v, quero a nota antes das duas imagens", tipos)
	}
	if de := w.notaNoPull(pcA1, n.ID); len(de.Imagens) != 2 || de.Imagens[0].ID != primeira.ID || de.Imagens[1].ID != segunda.ID {
		t.Errorf("as imagens da nota no pull = %+v, quero as duas na ordem do anexo", de.Imagens)
	}

	querStatus(t, "o token do PC na rota do celular",
		w.doBytes(http.MethodPost, "/api/v1/notes/"+n.ID+"/images", pcA1, foto, "image/jpeg", nil), http.StatusForbidden, "FORBIDDEN")
	querStatus(t, "o token do PC na nota nova do celular",
		w.doBytes(http.MethodPost, "/api/v1/notes/with-image", pcA1, foto, "image/jpeg", nil), http.StatusForbidden, "FORBIDDEN")
	querStatus(t, "o token do PC no GET do celular", w.do(http.MethodGet, caminhoDoCelular(n.ID, primeira.ID), pcA1, nil),
		http.StatusForbidden, "FORBIDDEN")
	querStatus(t, "a sessão no PUT do sync", w.putImagem(celA, n.ID, newID(), foto, time.Time{}), http.StatusForbidden, "FORBIDDEN")
	querStatus(t, "a sessão no GET do sync", w.do(http.MethodGet, caminhoDoSync(n.ID, primeira.ID), celA, nil), http.StatusForbidden, "FORBIDDEN")

	querStatus(t, "o celular tira", w.do(http.MethodDelete, caminhoDoCelular(n.ID, segunda.ID), celA, nil), http.StatusNoContent, "")
	if c, ok := mudancaDe(w.pullNotas(pcA1, p.LastSeq, 0), segunda.ID); !ok || c.Operation != "tombstone" {
		t.Errorf("o tirar do celular no pull do PC = %+v, %v", c, ok)
	}
}

// O PC de antes da N10b (o Cliente da N10a) lê o pull com as imagens sem erro:
// a nota com a chave imagens (ele a ignora) e a mudança note_image, que vira o
// Item do ditado e é "não aplicado" (o cursor anda; o PC novo, ao ver a
// capacidade note_image, lê tudo de novo).
func testImagensNoPCDeAntes(t *testing.T, w *world) {
	w.notaComImagemDoPC(t)
	rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pcA2, nil)
	var page syncengine.ChangePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("o PC de antes não lê o pull com a imagem: %v\n%s", err, rec.Body.String())
	}
	if len(page.Changes) != 2 || page.Changes[1].EntityType != "note_image" {
		t.Fatalf("a página = %+v", page)
	}
	c := page.Changes[1]
	if d := syncengine.DecidirMudanca(storage.EstadoDoItem{ID: c.EntityID}, c); d.Tipo != syncengine.TipoNaoAplicado {
		t.Errorf("o PC de antes decidiu %q para a imagem, quero %q", d.Tipo, syncengine.TipoNaoAplicado)
	}
}

// Nenhuma linha de log leva os bytes, o SHA-256, o tamanho de uma imagem ou o
// texto da nota.
func testImagensNadaNoLog(t *testing.T, w *world) {
	nota, imagem := newID(), newID()
	w.mustNota(pcA1, criarNota(nota, map[string]any{"titulo": "segredo do título"}), 1, false)
	b := jpegDaSuite(t, 64, 48, 3)
	limpa := limpaDe(t, b)
	querStatus(t, "o PUT", w.putImagem(pcA1, nota, imagem, b, time.Time{}), http.StatusCreated, "")
	querStatus(t, "o GET", w.do(http.MethodGet, caminhoDoSync(nota, imagem), pcA2, nil), http.StatusOK, "")
	querStatus(t, "o GET do celular", w.do(http.MethodGet, caminhoDoCelular(nota, imagem), celA, nil), http.StatusOK, "")
	logs := w.logs.String()
	tamanho := "bytes=" + strconv.Itoa(len(limpa.Bytes))
	for _, proibido := range []string{limpa.SHA256, tamanho, "segredo do título", imagem, nota} {
		if strings.Contains(logs, proibido) {
			t.Errorf("o log leva %q:\n%s", proibido, logs)
		}
	}
	if !strings.Contains(logs, `rota="GET /api/v1/sync/notes/{id}/images/{imagem}"`) {
		t.Errorf("o log de acesso não tem o padrão da rota:\n%s", logs)
	}
}
