package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O estado dos 6 meses para a tela e as cópias ao lado do banco (ADR-019,
// "Decisões da F10", F10d e F10e), num historico.db de verdade na pasta
// temporária. Nenhum teste abre o bin\ de ninguém nem apaga fora de t.TempDir().

const (
	copiaDoDono   = "historico.db.antes-do-dono-20260929-101500.db"
	copiaDaNuvem  = "historico.db.antes-da-nuvem-20261006-153000.db"
	copiaDos6     = "historico.db.antes-dos-6-meses-20261008-153000.db"
	copiaDaNuvem2 = "historico.db.antes-da-nuvem-20260101-090000.db"
)

// escrever grava um arquivo de teste ao lado do banco.
func (s seisMesesDeTeste) escrever(t *testing.T, nome, conteudo string) string {
	t.Helper()
	caminho := filepath.Join(filepath.Dir(s.caminho), nome)
	if err := os.WriteFile(caminho, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	return caminho
}

func (s seisMesesDeTeste) existe(nome string) bool {
	_, err := os.Lstat(filepath.Join(filepath.Dir(s.caminho), nome))
	return err == nil
}

func (s seisMesesDeTeste) arquivos(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(s.caminho))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (s seisMesesDeTeste) apagarCopias(t *testing.T) storage.ResultadoDasCopias {
	t.Helper()
	r, err := s.sm.ApagarCopias(context.Background())
	if err != nil {
		t.Fatalf("ApagarCopias: %v", err)
	}
	return r
}

// só o nome que casa exatamente com <banco>.antes-(do-dono|da-nuvem|dos-6-meses)-
// AAAAMMDD-HHMMSS.db, e só arquivo regular, é cópia: o banco, o -wal, o -shm,
// outro .db, um nome parecido e uma pasta com o nome da cópia ficam.
func TestCopias_SoOQueCasaExatamenteECopia(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	copias := []string{copiaDoDono, copiaDaNuvem, copiaDos6}
	for _, n := range copias {
		s.escrever(t, n, "cópia "+n)
	}
	parecidos := []string{
		"historico.db-wal",
		"historico.db-shm",
		"outro.db",
		"outro.db.antes-do-dono-20260929-101500.db",
		"historico.db.antes-do-dono-20260929-101500.db.bak",
		"historico.db.antes-do-dono-20260929-101500.db-journal",
		"historico.db.antes-do-dono-20260929-101500.dbx",
		"historico.db.antes-do-dono-2026-09-29.db",
		"historico.db.antes-do-dono-20260929-1015.db",
		"historico.db.antes-qualquer-20260929-101500.db",
		"historico.db.antes-da-nuvem-.db",
		"x-historico.db.antes-do-dono-20260929-101500.db",
		"anotacao.txt",
	}
	for _, n := range parecidos {
		s.escrever(t, n, "não é cópia: "+n)
	}
	// Uma pasta com o nome de uma cópia, com um arquivo dentro.
	pasta := filepath.Join(filepath.Dir(s.caminho), "historico.db.antes-do-dono-20260101-000000.db")
	if err := os.Mkdir(pasta, 0o700); err != nil {
		t.Fatal(err)
	}
	dentro := filepath.Join(pasta, "dentro.txt")
	if err := os.WriteFile(dentro, []byte("dentro"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := storage.CopiasAoLado(s.caminho)
	if err != nil {
		t.Fatal(err)
	}
	quer := append([]string(nil), copias...)
	sort.Strings(quer)
	if strings.Join(got, "|") != strings.Join(quer, "|") {
		t.Fatalf("as cópias = %v; quero só %v", got, quer)
	}
	if e, err := s.sm.Estado(context.Background(), agoraSM); err != nil || e.Copias != 3 {
		t.Fatalf("o Estado conta %d cópias, %v; quero 3", e.Copias, err)
	}

	r := s.apagarCopias(t)
	if r.Apagadas != 3 || r.Ficaram != 0 {
		t.Fatalf("o ApagarCopias = %+v; quero 3 apagadas", r)
	}
	for _, n := range copias {
		if s.existe(n) {
			t.Errorf("a cópia %s não saiu", n)
		}
	}
	for _, n := range append(parecidos, "historico.db") {
		if !s.existe(n) {
			t.Errorf("%s saiu, e não é cópia", n)
		}
	}
	if b, err := os.ReadFile(dentro); err != nil || string(b) != "dentro" {
		t.Errorf("a pasta com o nome da cópia foi mexida: %v", err)
	}
	// O banco continua funcionando.
	if n := s.contar(t, `SELECT COUNT(*) FROM instalacao`); n == 0 {
		t.Error("o banco ficou sem a tabela instalacao")
	}
	if e, err := s.sm.Estado(context.Background(), agoraSM); err != nil || e.Copias != 0 {
		t.Fatalf("depois de apagar, o Estado conta %d, %v", e.Copias, err)
	}
}

// Um link com o nome de uma cópia não é cópia: nem é contado, nem é seguido
// para apagar o alvo.
func TestCopias_UmLinkNaoEApagadoNemSeguido(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	alvo := s.escrever(t, "importante.txt", "não pode sair")
	if err := os.Symlink(alvo, filepath.Join(filepath.Dir(s.caminho), copiaDoDono)); err != nil {
		t.Skipf("este sistema não deixa criar um link: %v", err)
	}
	if got, err := storage.CopiasAoLado(s.caminho); err != nil || len(got) != 0 {
		t.Fatalf("o link foi contado como cópia: %v, %v", got, err)
	}
	if r := s.apagarCopias(t); r.Apagadas != 0 || r.Ficaram != 0 {
		t.Fatalf("o ApagarCopias = %+v; quero nada", r)
	}
	if !s.existe("importante.txt") || !s.existe(copiaDoDono) {
		t.Fatal("o link ou o alvo dele saiu")
	}
}

// A cópia do envio que ainda não terminou (a nuvem_copia sem o aceite) não
// sai: o EnviarHistorico não copia de novo quando a chave existe. Só ela: as
// outras saem. Com o aceite, sai.
func TestCopias_ACopiaDoEnvioEmCursoNaoSai(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.escrever(t, copiaDoDono, "a")
	registrada := s.escrever(t, copiaDaNuvem, "b")
	s.escrever(t, copiaDaNuvem2, "c")
	s.escrever(t, copiaDos6, "d")
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveNuvemCopia, registrada)

	r := s.apagarCopias(t)
	if r.Apagadas != 3 || r.Ficaram != 1 {
		t.Fatalf("o ApagarCopias = %+v; quero 3 apagadas e 1 que fica", r)
	}
	if !s.existe(copiaDaNuvem) {
		t.Fatal("a cópia do envio que ainda não terminou saiu")
	}
	for _, n := range []string{copiaDoDono, copiaDaNuvem2, copiaDos6} {
		if s.existe(n) {
			t.Errorf("%s não saiu", n)
		}
	}
	if _, ok := s.chave(t, storage.ChaveNuvemCopia); !ok {
		t.Error("a nuvem_copia saiu: o clique seguinte do envio seguiria sem a cópia")
	}
	if e, err := s.sm.Estado(context.Background(), agoraSM); err != nil || e.Copias != 1 {
		t.Fatalf("o Estado conta %d, %v; quero 1", e.Copias, err)
	}

	// O envio terminou: o aceite está gravado, e a cópia sai.
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveNuvemEnvioAceitoEm, "2026-10-08T12:00:00Z")
	if r := s.apagarCopias(t); r.Apagadas != 1 || r.Ficaram != 0 || s.existe(copiaDaNuvem) {
		t.Fatalf("depois do aceite: %+v, existe=%v", r, s.existe(copiaDaNuvem))
	}
}

// A nuvem_copia com o nome ilegível (vazio): errar para o lado de guardar.
// As do envio ficam; as outras saem.
func TestCopias_NuvemCopiaVaziaGuardaAsDoEnvio(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.escrever(t, copiaDaNuvem, "b")
	s.escrever(t, copiaDaNuvem2, "c")
	s.escrever(t, copiaDos6, "d")
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, '')`, storage.ChaveNuvemCopia)
	if r := s.apagarCopias(t); r.Apagadas != 1 || r.Ficaram != 2 || !s.existe(copiaDaNuvem) || !s.existe(copiaDaNuvem2) {
		t.Fatalf("o ApagarCopias = %+v", r)
	}
}

// Apagada a cópia dos 6 meses, a chave dela sai e o próximo OK copia de novo;
// o OK e a rodada não são tocados. A chave que ficou para trás (a cópia
// apagada à mão) também sai.
func TestCopias_ApagadaADosSeisMesesATiraAChave(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 3, agoraSM)
	a := s.ligarComOK(t, agoraSM, 3)
	if !s.existe(a.Copia) {
		t.Fatal("o Ligar não deixou a cópia")
	}
	if r := s.apagarCopias(t); r.Apagadas != 1 || r.Ficaram != 0 {
		t.Fatalf("o ApagarCopias = %+v", r)
	}
	if s.existe(a.Copia) {
		t.Fatal("a cópia dos 6 meses não saiu")
	}
	if _, ok := s.chave(t, storage.ChaveRetencaoCopia); ok {
		t.Fatal("a retencao_6m_copia ficou")
	}
	for _, c := range []string{storage.ChaveRetencaoAceitaEm, storage.ChaveRetencaoRodadaEm} {
		if _, ok := s.chave(t, c); !ok {
			t.Errorf("a chave %s saiu junto", c)
		}
	}
	if e, err := s.sm.Estado(context.Background(), agoraSM); err != nil || !e.Ligada || e.Copias != 0 {
		t.Fatalf("o Estado = %+v, %v; quero ligada e sem cópias", e, err)
	}

	// A regra desligada e ligada de novo, com mais ditados vencidos: a cópia
	// é refeita.
	if err := s.sm.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	depois := agoraSM.Add(time.Hour)
	s.velhos(t, 10, 2, depois)
	b := s.ligarComOK(t, depois, 2)
	if b.CopiaJaExistia || b.Copia == "" || !s.existe(b.Copia) {
		t.Fatalf("o OK depois de apagar as cópias = %+v; quero uma cópia nova", b)
	}

	// A chave de uma cópia que alguém apagou à mão também sai.
	if err := os.Remove(filepath.Join(filepath.Dir(s.caminho), b.Copia)); err != nil {
		t.Fatal(err)
	}
	if r := s.apagarCopias(t); r.Apagadas != 0 || r.Ficaram != 0 {
		t.Fatalf("o ApagarCopias sem cópias = %+v", r)
	}
	if _, ok := s.chave(t, storage.ChaveRetencaoCopia); ok {
		t.Fatal("a chave de uma cópia que não existe mais ficou")
	}
}

// O ApagarCopias é idempotente e o banco fica como estava (nenhum ditado).
func TestCopias_SemNadaAApagarNaoFazNada(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.ditado(t, idSM(1), agoraSM.AddDate(0, -1, 0))
	antes := s.arquivos(t)
	if r := s.apagarCopias(t); r.Apagadas != 0 || r.Ficaram != 0 {
		t.Fatalf("o ApagarCopias sem cópias = %+v", r)
	}
	if got := s.arquivos(t); strings.Join(got, "|") != strings.Join(antes, "|") {
		t.Fatalf("a pasta mudou: %v -> %v", antes, got)
	}
	if n := s.contar(t, `SELECT COUNT(*) FROM local_transcriptions`); n != 1 {
		t.Fatalf("sobraram %d ditados", n)
	}
}

// Sem o arquivo do banco, não há onde procurar.
func TestCopias_SemOCaminhoDoBanco(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	sm := storage.NovosSeisMeses(s.db, "")
	if _, err := sm.ApagarCopias(context.Background()); !errors.Is(err, storage.ErrSemCaminhoDoBanco) {
		t.Fatalf("o ApagarCopias sem o caminho = %v", err)
	}
	if _, err := storage.CopiasAoLado(""); !errors.Is(err, storage.ErrSemCaminhoDoBanco) {
		t.Fatalf("o CopiasAoLado sem o caminho = %v", err)
	}
	// O Estado não precisa dele: zero cópias.
	if e, err := sm.Estado(context.Background(), agoraSM); err != nil || e.Copias != 0 {
		t.Fatalf("o Estado sem o caminho = %+v, %v", e, err)
	}
}

// O arquivo que o sistema não deixa apagar não para os outros: o resultado
// conta o que saiu e o que ficou, e o erro diz o motivo sem o caminho (que leva
// o nome de usuário do Windows). Só no Windows, onde abrir um arquivo o trava.
func TestCopias_UmaQueNaoSaiNaoParaAsOutras(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("só o Windows trava a exclusão de um arquivo aberto")
	}
	s := novosSeisMesesDeTeste(t)
	travada := s.escrever(t, copiaDoDono, "a")
	s.escrever(t, copiaDaNuvem, "b")
	s.escrever(t, copiaDos6, "c")
	f, err := os.Open(travada)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, err := s.sm.ApagarCopias(context.Background())
	if err == nil {
		t.Fatal("o ApagarCopias não disse que uma cópia não saiu")
	}
	if r.Apagadas != 2 || r.Ficaram != 1 || !s.existe(copiaDoDono) {
		t.Fatalf("o ApagarCopias = %+v; quero 2 apagadas e a travada que fica", r)
	}
	if strings.Contains(err.Error(), filepath.Dir(s.caminho)) || strings.Contains(err.Error(), copiaDoDono) {
		t.Fatalf("o erro tem o caminho ou o nome: %v", err)
	}
}

// O Estado: a contagem da prévia (total, lixeira, selo, outro dono), o OK e a
// pausa, com o agora dado.
func TestEstado_ContagemOKEPausa(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 4, agoraSM)
	s.ditado(t, idSM(900), agoraSM.AddDate(0, -1, 0))
	executar(t, s.db, `UPDATE local_transcriptions SET deleted_at = ? WHERE id = ?`, emTextoDeTeste(agoraSM.AddDate(0, -6, -20)), idSM(0))
	executar(t, s.db, `UPDATE local_transcriptions SET conflito_texto = 'outra versão' WHERE id IN (?, ?)`, idSM(1), idSM(2))
	executar(t, s.db, `INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status, created_at, updated_at)
		VALUES (?, 'outro-dono', 'windows', 'x', 'y', 'smart', 'local', 'done', ?, ?)`,
		idSM(800), emTextoDeTeste(agoraSM.AddDate(0, -8, 0)), emTextoDeTeste(agoraSM.AddDate(0, -8, 0)))

	e, err := s.sm.Estado(context.Background(), agoraSM)
	if err != nil {
		t.Fatal(err)
	}
	if e.Total != 4 || e.NaLixeira != 1 || e.ComSelo != 2 || e.DeOutroDono != 1 || e.Ligada || e.Pausada || e.Copias != 0 || e.SemDonoFixo {
		t.Fatalf("o Estado = %+v", e)
	}

	// Pausada: o freio tirou o OK e gravou a pausa.
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveRetencaoPausadaEm, emTextoDeTeste(agoraSM))
	if e, err = s.sm.Estado(context.Background(), agoraSM); err != nil || !e.Pausada || e.Ligada || e.Total != 4 {
		t.Fatalf("o Estado pausado = %+v, %v", e, err)
	}

	// Ligada: o OK vale, e a pausa que sobrou não aparece.
	s.ligar(t)
	if e, err = s.sm.Estado(context.Background(), agoraSM); err != nil || !e.Ligada || e.Pausada {
		t.Fatalf("o Estado ligado = %+v, %v", e, err)
	}
}

// Sem o dono fixo, o Estado não é erro: contagem zero, e a tela não pergunta.
func TestEstado_SemDonoFixo(t *testing.T) {
	caminho, db := bancoEmArquivo(t)
	sm := storage.NovosSeisMeses(db, caminho)
	e, err := sm.Estado(context.Background(), agoraSM)
	if err != nil || !e.SemDonoFixo || e.Total != 0 || e.Ligada {
		t.Fatalf("o Estado sem o dono fixo = %+v, %v", e, err)
	}
}

// O Estado conta, além das cópias, as que o botão apaga: a do envio à nuvem
// que ainda não terminou fica de fora (a tela não promete apagá-la, e sem
// nenhuma apagável não oferece o botão). Com o nome registrado ilegível, todas
// as do envio ficam de fora; com o aceite, todas contam.
func TestEstado_AsCopiasQueOBotaoApaga(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.escrever(t, copiaDoDono, "a")
	registrada := s.escrever(t, copiaDaNuvem, "b")
	s.escrever(t, copiaDaNuvem2, "c")
	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveNuvemCopia, registrada)
	estado := func() storage.EstadoDosSeisMeses {
		t.Helper()
		e, err := s.sm.Estado(context.Background(), agoraSM)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if e := estado(); e.Copias != 3 || e.CopiasApagaveis != 2 {
		t.Fatalf("o Estado = %+v; quero 3 cópias, 2 apagáveis (a do envio em curso fica)", e)
	}
	// O botão apaga exatamente as que o Estado disse.
	if r := s.apagarCopias(t); r.Apagadas != 2 || r.Ficaram != 1 {
		t.Fatalf("o ApagarCopias = %+v", r)
	}
	if e := estado(); e.Copias != 1 || e.CopiasApagaveis != 0 {
		t.Fatalf("o Estado com só a do envio = %+v; quero 1 cópia e nenhuma apagável", e)
	}

	executar(t, s.db, `UPDATE instalacao SET valor = '' WHERE nome = ?`, storage.ChaveNuvemCopia)
	s.escrever(t, copiaDaNuvem2, "c")
	s.escrever(t, copiaDos6, "d")
	if e := estado(); e.Copias != 3 || e.CopiasApagaveis != 1 {
		t.Fatalf("o Estado com o nome ilegível = %+v; quero só a dos 6 meses apagável", e)
	}

	executar(t, s.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveNuvemEnvioAceitoEm, "2026-10-08T12:00:00Z")
	if e := estado(); e.Copias != 3 || e.CopiasApagaveis != 3 {
		t.Fatalf("o Estado depois do aceite = %+v; quero as 3 apagáveis", e)
	}
}

// O Estado diz se a cópia dos 6 meses de um OK anterior está ao lado do banco:
// com ela, o próximo OK não copia de novo, e o que nasceu depois dela não está
// nela (a tela não pode prometer que "só a cópia guarda"). Sem ela (nunca
// ligada, apagada pelo botão ou à mão), o OK copia.
func TestEstado_ACopiaDosSeisMesesQueJaExiste(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	s.velhos(t, 0, 3, agoraSM)
	estado := func() storage.EstadoDosSeisMeses {
		t.Helper()
		e, err := s.sm.Estado(context.Background(), agoraSM)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if estado().CopiaDosSeisMeses {
		t.Fatal("antes do primeiro OK, o Estado diz que a cópia já existe")
	}
	a := s.ligarComOK(t, agoraSM, 3)
	if err := s.sm.Desligar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !estado().CopiaDosSeisMeses {
		t.Fatal("depois do OK e do Desligar, o Estado não diz que a cópia já existe")
	}
	// O OK seguinte não copia: é o que o Estado disse.
	depois := agoraSM.Add(time.Hour)
	s.velhos(t, 10, 2, depois)
	if b := s.ligarComOK(t, depois, 2); !b.CopiaJaExistia {
		t.Fatalf("o OK seguinte = %+v; quero sem cópia nova", b)
	}
	// Apagada à mão, o Estado deixa de dizer (o próximo OK copia).
	if err := os.Remove(filepath.Join(filepath.Dir(s.caminho), a.Copia)); err != nil {
		t.Fatal(err)
	}
	if estado().CopiaDosSeisMeses {
		t.Fatal("com a cópia apagada à mão, o Estado ainda diz que ela existe")
	}
}
