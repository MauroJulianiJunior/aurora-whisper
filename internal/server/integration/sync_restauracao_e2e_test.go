package integration_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// Restaurar um backup da nuvem MAIS VELHO que o PC (ADR-019, "Decisões da
// F11", F11d; AC-061 e 12-rollout.md, "Rollback"). A nuvem de verdade sobre o
// Store em memória, o Agent de verdade do lado do sync, e o backup como a foto
// do MemoryStore (Fotografar e Restaurar: o pg_dump da noite e o
// pg_restore). Depois de restaurar, o DepoisDeRestaurar (o que o comando
// pos-restauracao faz no Postgres) troca o epoch e revoga as sessões e os
// aparelhos; o PC conecta de novo e faz a rodada.
//
// Os testes de epoch de antes trocavam o epoch com os MESMOS dados
// (TrocarEpoch). Aqui a nuvem volta no tempo: depois do backup, o PC editou,
// pôs na lixeira e ditou um novo, o celular pôs na lixeira e excluiu de vez,
// e outro PC foi pareado.

// epochDoPC é o sync_epoch que o PC guardou (vazio: nenhum).
func (pc *pcE2E) epochDoPC() string {
	pc.t.Helper()
	var e *string
	if err := pc.db.QueryRow(`SELECT sync_epoch FROM sync_state`).Scan(&e); err != nil {
		pc.t.Fatalf("o epoch do PC: %v", err)
	}
	if e == nil {
		return ""
	}
	return *e
}

// epochDaNuvem é o sync_epoch da conta na nuvem.
func (n *nuvemE2E) epochDaNuvem() string {
	n.t.Helper()
	p, err := n.mem.Pull(context.Background(), n.conta, 0, 1)
	if err != nil {
		n.t.Fatal(err)
	}
	return p.Epoch
}

// entrarDeNovo é o celular entrando de novo depois da restauração: outra
// sessão.
func (n *nuvemE2E) entrarDeNovo() {
	n.t.Helper()
	n.sessao = cloudauth.NovoSegredo()
	if _, err := n.mem.CriarSessao(context.Background(), cloudstore.NovaSessao{
		UserID: n.conta, TokenHMAC: cloudauth.HMAC(n.segredo, n.sessao), Rotulo: "Android · Chrome",
		Agora: time.Now(), ExpiraEm: time.Now().Add(time.Hour),
	}); err != nil {
		n.t.Fatal(err)
	}
}

// restaurarOBackup volta a nuvem à foto e roda o DepoisDeRestaurar.
func (n *nuvemE2E) restaurarOBackup(foto *cloudstore.FotoDaMemoria) cloudstore.Restauracao {
	n.t.Helper()
	n.mem.Restaurar(foto)
	r, err := n.mem.DepoisDeRestaurar(context.Background(), time.Now())
	if err != nil {
		n.t.Fatal(err)
	}
	return r
}

// O cenário inteiro. O que tem de valer: o PC recomeça pelo epoch novo sem
// selo, sem duplicar e sem perder a edição que só ele tinha (a de depois do
// backup e a feita desconectado, depois da restauração); o PC pareado depois do
// backup, o PC de antes e a sessão do celular recebem 401 até conectar (ou
// entrar) de novo; a lixeira que o PC já tinha (a dele e a do celular que ele
// recebeu) fica na lixeira, porque a reconciliação do pull (F7c, D3) a sobe de
// novo. O que o Mauro aceitou em 09-out-2026 ("aceitar e deixar escrito"),
// afirmado aqui para nada ficar escondido: o excluído de vez depois do backup
// volta vivo ao PC e à nuvem, e a lixeira que só a nuvem tinha (o celular pôs
// e o PC não chegou a receber) volta fora da lixeira.
func TestSyncE2E_Restauracao_BackupMaisVelhoQueOPC(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	editado := pc.ditar("antes do backup: o PC vai editar")
	lixoDoPC := pc.ditar("antes do backup: o PC vai pôr na lixeira")
	lixoDoCel := pc.ditar("antes do backup: o celular vai pôr na lixeira")
	lixoNaoVisto := pc.ditar("antes do backup: o celular vai pôr na lixeira e o PC não vê")
	excluido := pc.ditar("antes do backup: o celular vai excluir de vez")
	mexidoFora := pc.ditar("antes do backup: o PC vai editar desconectado, depois da restauração")
	duasVezes := pc.ditar("antes do backup: o PC vai editar depois do backup e de novo desconectado")
	intocado := pc.ditar("antes do backup: fica como está")
	pc.rodadaOK()
	epochAntes := n.epochDaNuvem()
	if pc.epochDoPC() != epochAntes {
		t.Fatalf("o PC está no epoch %q, a nuvem no %q", pc.epochDoPC(), epochAntes)
	}

	backup := n.mem.Fotografar()

	// Depois do backup.
	pc.editar(editado, "editado no PC depois do backup")
	pc.editar(duasVezes, "editado no PC depois do backup, a primeira vez")
	pc.lixeira(lixoDoPC)
	novo := pc.ditar("depois do backup: novo")
	n.celLixeira(lixoDoCel)
	n.celLixeira(excluido)
	n.celExcluirDeVez(excluido)
	pc.rodadaOK()
	if pc.temLinha(excluido) || pc.linha(lixoDoCel).DeletedAt == nil || pc.naFila() != 0 {
		t.Fatalf("o estado antes da restauração não é o esperado: excluído %v, lixeira do celular %v, fila %d",
			pc.temLinha(excluido), pc.linha(lixoDoCel).DeletedAt != nil, pc.naFila())
	}
	n.celLixeira(lixoNaoVisto) // o PC não roda mais até a restauração
	pc2 := n.novoPC()          // pareado depois do backup
	pc2.rodadaOK()

	r := n.restaurarOBackup(backup)
	if r != (cloudstore.Restauracao{Contas: 1, Sessoes: 1, Aparelhos: 1}) {
		t.Errorf("o DepoisDeRestaurar = %+v; quero 1 conta, a sessão do celular e o PC (o PC2 não existe no backup)", r)
	}
	epochNovo := n.epochDaNuvem()
	if epochNovo == epochAntes || epochNovo == "" {
		t.Fatalf("o epoch da nuvem não mudou: %s", epochNovo)
	}

	// As credenciais de antes: 401. O celular tem de entrar de novo; os PCs,
	// conectar de novo (o laço os põe em "desconectado").
	if st, _ := n.celular(http.MethodGet, "/api/v1/transcriptions", nil); st != http.StatusUnauthorized {
		t.Errorf("a sessão do celular depois da restauração: %d, quero 401", st)
	}
	for nome, p := range map[string]*pcE2E{"o PC": pc, "o PC pareado depois do backup": pc2} {
		if err := p.rodada(); !errors.Is(err, syncengine.ErrNaoAutorizado) {
			t.Errorf("%s depois da restauração: %v, quero o 401", nome, err)
		}
	}
	// Desconectado, o PC edita ditados que a nuvem já tinha: as mudanças
	// entram na fila, com a base do epoch velho, e sobem quando ele conectar.
	// A do duasVezes leva uma base (a v2) MAIOR que a versão do banco
	// restaurado (a v1): a nuvem voltou no tempo.
	pc.editar(mexidoFora, "editado desconectado, depois da restauração")
	pc.editar(duasVezes, "editado de novo, desconectado, depois da restauração")
	if pc.naFila() != 2 {
		t.Fatalf("as edições desconectado não entraram na fila: %d", pc.naFila())
	}

	// O PC conecta de novo (o pareamento reaproveita o aparelho pelo
	// install_id) e roda.
	pc.conectarNa(n.conta, emailDoE2E, true)
	n.entrarDeNovo()
	for range 3 {
		pc.rodadaOK()
	}
	if got := pc.epochDoPC(); got != epochNovo {
		t.Fatalf("o PC está no epoch %q; quero o novo, %q", got, epochNovo)
	}

	// Sem duplicar, sem selo, fila vazia, e todo ditado com a versão da nuvem.
	ids := []string{editado, lixoDoPC, lixoDoCel, lixoNaoVisto, excluido, mexidoFora, duasVezes, intocado, novo}
	todos := pc.todos()
	cel := n.celLista()
	semRepetidos(t, "PC", todos, func(l storage.Transcription) string { return l.ID })
	semRepetidos(t, "celular", cel, func(i itemDoCelular) string { return i.ID })
	if len(todos) != len(ids) || len(cel) != len(ids) || pc.naFila() != 0 {
		t.Errorf("PC %d, celular %d, fila %d; quero os %d e a fila vazia", len(todos), len(cel), pc.naFila(), len(ids))
	}
	pcs := map[string]*pcE2E{"o PC": pc}
	for _, id := range ids {
		semSelo(t, n, id, pcs)
		l := pc.linha(id)
		c := n.celItem(id)
		if l.ServerVersion == nil || *l.ServerVersion != c.Version {
			t.Errorf("%s: a versão da nuvem no PC = %v, a nuvem está na %d", id, l.ServerVersion, c.Version)
		}
		if l.FinalText != c.FinalText || (l.DeletedAt == nil) != (c.DeletedAt == nil) {
			t.Errorf("%s: o PC (%q, lixeira %v) e a nuvem (%q, lixeira %v) não batem", id, l.FinalText, l.DeletedAt != nil,
				c.FinalText, c.DeletedAt != nil)
		}
	}
	// As edições que só o PC tinha (a nuvem voltou ao texto de antes) sobem e
	// valem: a de depois do backup e a feita desconectado.
	for id, want := range map[string]string{
		editado:    "editado no PC depois do backup",
		mexidoFora: "editado desconectado, depois da restauração",
		duasVezes:  "editado de novo, desconectado, depois da restauração",
		intocado:   "antes do backup: fica como está",
		novo:       "depois do backup: novo",
	} {
		if c := n.celItem(id); c.FinalText != want || c.DeletedAt != nil {
			t.Errorf("%s na nuvem: %q (lixeira %v); quero %q", id, c.FinalText, c.DeletedAt != nil, want)
		}
	}
	// A lixeira que o PC tinha (a dele e a do celular que ele recebeu) fica na
	// lixeira, nos dois lados: a reconciliação a sobe de novo.
	for _, id := range []string{lixoDoPC, lixoDoCel} {
		if pc.linha(id).DeletedAt == nil || n.celItem(id).DeletedAt == nil {
			t.Errorf("a lixeira de depois do backup que o PC tinha (%s) voltou", id)
		}
	}

	// O ACEITO (09-out-2026): o excluído de vez depois do backup volta vivo,
	// fora da lixeira, ao PC e à nuvem; e a lixeira que só a nuvem tinha volta
	// fora da lixeira.
	if l := pc.linha(excluido); l.DeletedAt != nil || l.FinalText != "antes do backup: o celular vai excluir de vez" {
		t.Errorf("o excluído de vez depois do backup: lixeira %v, %q (o aceito é ele voltar vivo)", l.DeletedAt != nil, l.FinalText)
	}
	if l := pc.linha(lixoNaoVisto); l.DeletedAt != nil {
		t.Error("a lixeira que só a nuvem tinha não voltou (o aceito é ela voltar fora da lixeira)")
	}

	// O PC2, pareado depois do backup, conecta de novo como aparelho novo e
	// recebe tudo, sem duplicar.
	pc2.conectarNa(n.conta, emailDoE2E, false)
	pc2.rodadaOK()
	if got := pc2.epochDoPC(); got != epochNovo {
		t.Errorf("o PC2 está no epoch %q", got)
	}
	verificarSemDuplicar(t, n, pc2, len(ids))
}

// O LIMITE QUE A F7 DEIXOU PARA A F11, CONFIRMADO AQUI E PENDENTE DO MAURO
// (ADR-019, "Decisões da F11", F11d; "Decisões da F7a/b", decisão 2: "com a
// base vazia, a nuvem compara contra tudo, e um ditado cujo texto difere do
// backup e cujo último texto veio de outro aparelho vira selo").
//
// Antes do backup, o celular editou o ditado (o último texto, no backup, é do
// celular). Depois do backup, o PC editou em cima do texto do celular, e a
// edição subiu. A nuvem volta ao backup; o PC recomeça e sobe o texto dele com
// a base vazia; a nuvem não sabe que a edição do PC partiu do texto do
// celular, e faz um SELO SEM CONFLITO DE VERDADE: o texto principal volta ao
// do celular, e o do PC fica no selo, nos dois lados. A edição não some (a
// pessoa a recupera com "trocar"), mas o PC perde a edição como texto
// principal, sem aviso além do selo.
//
// Este teste fixa o comportamento de HOJE para nada ficar escondido. Não é a
// decisão: a F11d parou aqui, sem consertar calada, e levou as opções ao
// Mauro. Se ele escolher consertar, este teste muda junto (o selo some e o
// texto do PC vale).
func TestSyncE2E_Restauracao_LimiteDaF7_SeloSobreOTextoDoCelular(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	id := pc.ditar("antes do backup: ditado no PC")
	pc.rodadaOK()
	n.celEditar(id, "o celular editou antes do backup")
	pc.rodadaOK()
	if pc.linha(id).FinalText != "o celular editou antes do backup" {
		t.Fatalf("a edição do celular não chegou ao PC: %q", pc.linha(id).FinalText)
	}
	backup := n.mem.Fotografar()
	const doPC = "o PC editou depois do backup, em cima do texto do celular"
	pc.editar(id, doPC)
	pc.rodadaOK()
	if c := n.celItem(id); c.FinalText != doPC || c.VersaoEmConflito != nil {
		t.Fatalf("antes da restauração, a nuvem: %q, selo %v", c.FinalText, c.VersaoEmConflito != nil)
	}

	n.restaurarOBackup(backup)
	if err := pc.rodada(); !errors.Is(err, syncengine.ErrNaoAutorizado) {
		t.Fatalf("a rodada depois da restauração: %v, quero o 401", err)
	}
	pc.conectarNa(n.conta, emailDoE2E, true)
	n.entrarDeNovo()
	for range 3 {
		pc.rodadaOK()
	}
	l, c := pc.linha(id), n.celItem(id)
	// Convergem, sem duplicar e com a fila vazia...
	if l.FinalText != c.FinalText || pc.naFila() != 0 || len(n.celLista()) != 1 || len(pc.todos()) != 1 {
		t.Fatalf("o PC (%q) e a nuvem (%q) não convergiram; fila %d", l.FinalText, c.FinalText, pc.naFila())
	}
	// ...mas com o selo falso: o texto do celular no principal, o do PC no selo,
	// nos dois lados. (O comportamento de hoje; ver o comentário acima.)
	if c.FinalText != "o celular editou antes do backup" || c.VersaoEmConflito == nil || c.VersaoEmConflito.FinalText != doPC {
		t.Errorf("a nuvem: %q, selo %+v; o comportamento de hoje é o texto do celular com o do PC no selo", c.FinalText, c.VersaoEmConflito)
	}
	if l.Conflito == nil || l.Conflito.FinalText != doPC {
		t.Errorf("o PC: selo %+v; o comportamento de hoje é o texto do PC no selo", l.Conflito)
	}
}

// O selo de verdade que o backup guarda (o celular e o PC 1 mudaram o mesmo
// ditado antes do backup; o texto do PC 1 está no selo) continua depois da
// restauração, com os dois PCs recomeçando pela base vazia: o recomeço não
// troca o selo pendente pelo texto de um PC (o outro limite que a F7 deixou
// para a F11), e nada duplica.
func TestSyncE2E_Restauracao_OSeloDeVerdadeDoBackupFica(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	x := selar(t, n, pc1, pc2, "x")
	backup := n.mem.Fotografar()
	n.restaurarOBackup(backup)
	for _, pc := range []*pcE2E{pc1, pc2} {
		if err := pc.rodada(); !errors.Is(err, syncengine.ErrNaoAutorizado) {
			t.Fatalf("a rodada depois da restauração: %v, quero o 401", err)
		}
	}
	n.entrarDeNovo()
	for _, pc := range []*pcE2E{pc2, pc1, pc2, pc1} {
		if pc.epochDoPC() != n.epochDaNuvem() {
			pc.conectarNa(n.conta, emailDoE2E, true)
		}
		pc.rodadaOK()
	}
	c := n.celItem(x)
	if c.FinalText != "x do celular" || c.VersaoEmConflito == nil || c.VersaoEmConflito.FinalText != "x do PC 1" {
		t.Errorf("a nuvem: %q, selo %+v; quero o texto do celular e o do PC 1 no selo", c.FinalText, c.VersaoEmConflito)
	}
	for nome, pc := range map[string]*pcE2E{"o PC 1": pc1, "o PC 2": pc2} {
		l := pc.linha(x)
		if l.FinalText != "x do celular" || l.Conflito == nil || l.Conflito.FinalText != "x do PC 1" || pc.naFila() != 0 {
			t.Errorf("%s: %q, selo %+v, fila %d", nome, l.FinalText, l.Conflito, pc.naFila())
		}
		verificarSemDuplicar(t, n, pc, 1)
	}
}
