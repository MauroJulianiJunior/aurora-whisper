package syncengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// A decisão do envio automático do dicionário, em tabela (ADR-023, F), e a fase
// do motor com uma nuvem de roteiro: o mesmo SHA-256 não sobe de novo, o 409
// desliga a opção, o 404 para até reiniciar, o 422 espera a cópia mudar, e os
// erros só param a fase. Nada aqui toca no sistema real.

func TestDecidirEnvioDoDicionario_Tabela(t *testing.T) {
	casos := []struct {
		nome string
		r    RespostaDoDicionario
		quer DecisaoDoDicionario
	}{
		{"200 guarda o SHA-256 e limpa o aviso",
			RespostaDoDicionario{Status: 200},
			DecisaoDoDicionario{Tipo: TipoDicionarioEnviado, Aceitar: true, LimparAviso: true}},
		{"409 de outro PC desliga e diz o nome",
			RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteOutro, Aparelho: "NOTEBOOK"}},
			DecisaoDoDicionario{Tipo: TipoDicionarioPerdeuAFonte, Desligar: true, Aviso: "O celular passou a usar o dicionário de NOTEBOOK."}},
		{"409 sem cópia desliga e diz que foi apagada",
			RespostaDoDicionario{Status: 409, Estado: &EstadoDoDicionario{Fonte: FonteNenhuma}},
			DecisaoDoDicionario{Tipo: TipoDicionarioPerdeuAFonte, Desligar: true, Aviso: "A cópia do dicionário foi apagada da nuvem."}},
		{"409 de outro PC sem nome: a frase sem o nome",
			RespostaDoDicionario{Status: 409, Estado: &EstadoDoDicionario{Fonte: FonteOutro, Aparelho: " \x00 "}},
			DecisaoDoDicionario{Tipo: TipoDicionarioPerdeuAFonte, Desligar: true, Aviso: AvisoDicionarioDeOutro}},
		{"409 sem o estado desliga do mesmo jeito",
			RespostaDoDicionario{Status: 409},
			DecisaoDoDicionario{Tipo: TipoDicionarioPerdeuAFonte, Desligar: true, Aviso: AvisoDicionarioDeOutro}},
		{"422 guarda o aviso do motivo, mantém a opção e espera a cópia mudar",
			RespostaDoDicionario{Status: 422, Codigo: CodigoDicionarioInvalid, Motivo: "termos_demais"},
			DecisaoDoDicionario{Tipo: TipoDicionarioRecusado, SoQuandoMudar: true, Aviso: FraseDoMotivoDaCopia("termos_demais")}},
		{"422 com motivo desconhecido: a frase genérica",
			RespostaDoDicionario{Status: 422, Motivo: "inventado"},
			DecisaoDoDicionario{Tipo: TipoDicionarioRecusado, SoQuandoMudar: true, Aviso: FraseDoMotivoDaCopia("")}},
		{"400 é tratado como recusa, sem repetir o mesmo pedido",
			RespostaDoDicionario{Status: 400},
			DecisaoDoDicionario{Tipo: TipoDicionarioRecusado, SoQuandoMudar: true, Aviso: AvisoDicionarioRecusado}},
		{"403 também",
			RespostaDoDicionario{Status: 403},
			DecisaoDoDicionario{Tipo: TipoDicionarioRecusado, SoQuandoMudar: true, Aviso: AvisoDicionarioRecusado}},
		{"404 avisa e não tenta mais até reiniciar",
			RespostaDoDicionario{Status: 404},
			DecisaoDoDicionario{Tipo: TipoDicionarioSemRota, NaoTentarMais: true, Aviso: "A nuvem ainda não recebe o dicionário."}},
		{"500 para a fase, sem aviso",
			RespostaDoDicionario{Status: 500},
			DecisaoDoDicionario{Tipo: TipoDicionarioAdiado, Parar: true}},
		{"503 também",
			RespostaDoDicionario{Status: 503},
			DecisaoDoDicionario{Tipo: TipoDicionarioAdiado, Parar: true}},
	}
	for _, c := range casos {
		if got := DecidirEnvioDoDicionario(c.r); got != c.quer {
			t.Errorf("%s:\n got  %+v\n want %+v", c.nome, got, c.quer)
		}
	}
}

func TestFraseDoMotivoDaCopia_NuncaLevaTexto(t *testing.T) {
	for _, m := range []dictionary.MotivoDaCopia{dictionary.MotivoCopiaJSON, dictionary.MotivoCopiaGrandeDemais,
		dictionary.MotivoCopiaTermosDemais, dictionary.MotivoCopiaVariantesDemais, dictionary.MotivoCopiaTermoInvalido} {
		f := FraseDoMotivoDaCopia(string(m))
		if f == "" || !strings.HasSuffix(f, ".") || strings.Contains(f, string(m)+"_") {
			t.Errorf("frase do motivo %q: %q", m, f)
		}
	}
	if !strings.Contains(FraseDoMotivoDaCopia("termos_demais"), "2000") {
		t.Errorf("o limite de termos não está na frase: %q", FraseDoMotivoDaCopia("termos_demais"))
	}
}

func TestNomeDoAparelhoParaOAviso(t *testing.T) {
	if got := NomeDoAparelhoParaOAviso("  PC\x07-do-Mauro\n "); got != "PC-do-Mauro" {
		t.Errorf("controle e espaço: %q", got)
	}
	if got := NomeDoAparelhoParaOAviso(strings.Repeat("é", 200)); len([]rune(got)) != 80 {
		t.Errorf("o teto de 80 caracteres: %d", len([]rune(got)))
	}
}

// ─── A fase do motor ───

// fonteDeTeste é o dicionario.json: a semente de agora, ou o erro.
type fonteDeTeste struct {
	mu  sync.Mutex
	sem dictionary.Semente
	err error
}

func (f *fonteDeTeste) Dicionario(context.Context) (dictionary.Semente, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sem, f.err
}

func (f *fonteDeTeste) mudar(termo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sem.Entradas = append(f.sem.Entradas, dictionary.EntradaSemente{Canonica: termo})
}

func sementeDe(termos ...string) dictionary.Semente {
	s := dictionary.Semente{Usuario: "local"}
	for _, t := range termos {
		s.Entradas = append(s.Entradas, dictionary.EntradaSemente{Canonica: t, Alias: []string{"variante de " + t}})
	}
	return s
}

// opcaoDeTeste é a opção e o painel: o que a fase gravou e avisou.
type opcaoDeTeste struct {
	mu        sync.Mutex
	ligada    bool
	errLigada error
	errDesl   error
	avisos    []string
	desligou  int
	enviou    int
	termos    int
}

func (o *opcaoDeTeste) Ligada(context.Context) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ligada, o.errLigada
}

func (o *opcaoDeTeste) Desligar(_ context.Context, aviso string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.errDesl != nil {
		return o.errDesl
	}
	o.ligada, o.desligou = false, o.desligou+1
	o.avisos = append(o.avisos, aviso)
	return nil
}

func (o *opcaoDeTeste) Avisar(aviso string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.avisos = append(o.avisos, aviso)
}

func (o *opcaoDeTeste) Enviou(termos int, _ time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.enviou++
	o.termos = termos
}

func (o *opcaoDeTeste) ultimoAviso() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.avisos) == 0 {
		return ""
	}
	return o.avisos[len(o.avisos)-1]
}

// nuvemComDicionario é a nuvem de roteiro com o lado do dicionário.
type nuvemComDicionario struct {
	*nuvemDeRoteiro
	mu      sync.Mutex
	puts    []envioDoDicionario
	resp    RespostaDoDicionario
	err     error
	comoFor func(n int) (RespostaDoDicionario, error)
}

type envioDoDicionario struct {
	corpo []byte
	sha   string
	modo  ModoDoEnvio
}

func (n *nuvemComDicionario) EstadoDoDicionario(context.Context) (RespostaDoDicionario, error) {
	return RespostaDoDicionario{Status: 200}, nil
}

func (n *nuvemComDicionario) SubirDicionario(_ context.Context, corpo []byte, sha string, modo ModoDoEnvio) (RespostaDoDicionario, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.puts = append(n.puts, envioDoDicionario{corpo: corpo, sha: sha, modo: modo})
	if n.comoFor != nil {
		return n.comoFor(len(n.puts))
	}
	return n.resp, n.err
}

func (n *nuvemComDicionario) ApagarDicionario(context.Context) (RespostaDoDicionario, error) {
	return RespostaDoDicionario{Status: 204}, nil
}

func (n *nuvemComDicionario) quantos() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.puts)
}

func (n *nuvemComDicionario) responder(r RespostaDoDicionario, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resp, n.err, n.comoFor = r, err, nil
}

// motorDoDicionario monta o motor com a nuvem de roteiro e a fase do dicionário.
func motorDoDicionario(t *testing.T, ligada bool, termos ...string) (*Motor, *nuvemComDicionario, *fonteDeTeste, *opcaoDeTeste) {
	t.Helper()
	p := novoPC(t)
	nuvem := &nuvemComDicionario{
		nuvemDeRoteiro: &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")},
		resp:           RespostaDoDicionario{Status: 200},
	}
	fonte := &fonteDeTeste{sem: sementeDe(termos...)}
	opcao := &opcaoDeTeste{ligada: ligada}
	m := novoMotor(p, nuvem)
	m.Copia = &CopiaDoDicionario{Fonte: fonte, Opcao: opcao}
	return m, nuvem, fonte, opcao
}

func TestMotorDicionario_DesligadaNaoMandaNada(t *testing.T) {
	m, nuvem, _, _ := motorDoDicionario(t, false, "Kubernetes")
	rel, err := m.Rodada(context.Background())
	if err != nil || nuvem.quantos() != 0 || soma(rel.Dicionario) != 0 {
		t.Fatalf("desligada: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
}

func TestMotorDicionario_ManterComOsBytesDaCopiaEOMesmoSHANaoSobeDeNovo(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true, "Kubernetes", "Aurora")
	ctx := context.Background()

	rel, err := m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 1 || rel.Dicionario[TipoDicionarioEnviado] != 1 || !rel.Teve() {
		t.Fatalf("primeira rodada: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
	// O que sobe é exatamente a cópia (BytesDaCopia), com o SHA-256 dela, em modo
	// manter, e sem o usuário local.
	semente, _ := fonte.Dicionario(ctx)
	esperado, sha, err := semente.BytesDaCopia()
	if err != nil {
		t.Fatal(err)
	}
	if got := nuvem.puts[0]; got.modo != EnvioManter || got.sha != sha || string(got.corpo) != string(esperado) ||
		strings.Contains(string(got.corpo), `"local"`) {
		t.Fatalf("o PUT: modo %q, sha %q (quero %q), corpo igual %v", got.modo, got.sha, sha, string(got.corpo) == string(esperado))
	}
	if opcao.enviou != 1 || opcao.termos != 2 {
		t.Errorf("a opção soube do envio %d vez(es), com %d termos", opcao.enviou, opcao.termos)
	}

	// A mesma cópia: nada sobe.
	rel, err = m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 1 || rel.Dicionario[TipoDicionarioIgual] != 1 {
		t.Fatalf("segunda rodada: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}

	// O arquivo mudou: sobe de novo.
	fonte.mudar("Whisper")
	rel, err = m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 2 || rel.Dicionario[TipoDicionarioEnviado] != 1 || nuvem.puts[1].sha == nuvem.puts[0].sha {
		t.Fatalf("terceira rodada: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
}

func TestMotorDicionario_OutroPareamentoComecaDoZero(t *testing.T) {
	m, nuvem, _, _ := motorDoDicionario(t, true, "Kubernetes")
	ctx := context.Background()
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	// O PC reconectou (outro PareadoEm): o primeiro envio é um 200 sem gravar.
	m.PareadoEm = "p2"
	rel := novoRelatorio()
	if err := m.EnviarDicionario(ctx, &rel); err != nil || nuvem.quantos() != 2 || rel.Dicionario[TipoDicionarioEnviado] != 1 {
		t.Fatalf("outro pareamento: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
}

func TestMotorDicionario_409DesligaAOpcaoEGuardaOAviso(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		nome  string
		resp  RespostaDoDicionario
		aviso string
	}{
		{"outro PC", RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteOutro, Aparelho: "NOTEBOOK"}},
			"O celular passou a usar o dicionário de NOTEBOOK."},
		{"sem cópia", RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteNenhuma}},
			"A cópia do dicionário foi apagada da nuvem."},
	} {
		m, nuvem, _, opcao := motorDoDicionario(t, true, "Kubernetes")
		nuvem.responder(c.resp, nil)
		rel, err := m.Rodada(ctx)
		if err != nil || rel.Dicionario[TipoDicionarioPerdeuAFonte] != 1 {
			t.Fatalf("%s: relatório %v, err %v", c.nome, rel.Dicionario, err)
		}
		if opcao.ligada || opcao.desligou != 1 || opcao.ultimoAviso() != c.aviso {
			t.Errorf("%s: ligada %v, desligou %d, aviso %q", c.nome, opcao.ligada, opcao.desligou, opcao.ultimoAviso())
		}
		// Desligada, a rodada seguinte não manda mais nada.
		if _, err := m.Rodada(ctx); err != nil || nuvem.quantos() != 1 {
			t.Errorf("%s: depois de desligar, puts %d, err %v", c.nome, nuvem.quantos(), err)
		}
	}
}

func TestMotorDicionario_422MantemAOpcaoESoTentaDeNovoQuandoOArquivoMuda(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true, "Kubernetes")
	ctx := context.Background()
	nuvem.responder(RespostaDoDicionario{Status: 422, Codigo: CodigoDicionarioInvalid, Motivo: "termo_invalido"}, nil)

	rel, err := m.Rodada(ctx)
	if err != nil || rel.Dicionario[TipoDicionarioRecusado] != 1 || !opcao.ligada || opcao.ultimoAviso() != FraseDoMotivoDaCopia("termo_invalido") {
		t.Fatalf("422: relatório %v, ligada %v, aviso %q, err %v", rel.Dicionario, opcao.ligada, opcao.ultimoAviso(), err)
	}
	// A mesma cópia: nem tenta.
	rel, err = m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 1 || rel.Dicionario[TipoDicionarioEsperando] != 1 {
		t.Fatalf("mesma cópia: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
	// O arquivo mudou: tenta, e agora a nuvem aceita (o aviso sai).
	fonte.mudar("Whisper")
	nuvem.responder(RespostaDoDicionario{Status: 200}, nil)
	rel, err = m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 2 || rel.Dicionario[TipoDicionarioEnviado] != 1 || opcao.enviou != 1 {
		t.Fatalf("cópia nova: puts %d, relatório %v, err %v", nuvem.quantos(), rel.Dicionario, err)
	}
}

func TestMotorDicionario_404ParaAteReiniciar(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true, "Kubernetes")
	ctx := context.Background()
	nuvem.responder(RespostaDoDicionario{Status: 404}, nil)

	rel, err := m.Rodada(ctx)
	if err != nil || rel.Dicionario[TipoDicionarioSemRota] != 1 || !opcao.ligada || opcao.ultimoAviso() != "A nuvem ainda não recebe o dicionário." {
		t.Fatalf("404: relatório %v, ligada %v, aviso %q, err %v", rel.Dicionario, opcao.ligada, opcao.ultimoAviso(), err)
	}
	// Nem com o arquivo mudando.
	fonte.mudar("Whisper")
	for range 3 {
		if _, err := m.Rodada(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if nuvem.quantos() != 1 {
		t.Fatalf("depois do 404 foram %d PUTs, quero 1", nuvem.quantos())
	}
	// "Reiniciar" é uma memória nova (o laço novo).
	m.Copia = &CopiaDoDicionario{Fonte: fonte, Opcao: opcao}
	nuvem.responder(RespostaDoDicionario{Status: 200}, nil)
	if _, err := m.Rodada(ctx); err != nil || nuvem.quantos() != 2 {
		t.Fatalf("depois de reiniciar: puts %d, err %v", nuvem.quantos(), err)
	}
}

func TestMotorDicionario_OsErrosSoParamAFase(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		nome string
		err  error
		resp RespostaDoDicionario
	}{
		{"rede", transitorio(CodigoRede), RespostaDoDicionario{}},
		{"prazo", transitorio(CodigoPrazo), RespostaDoDicionario{}},
		{"redirecionamento", transitorio(CodigoRedirecionamento), RespostaDoDicionario{Status: 302}},
		{"429", &ErrEspere{Espera: time.Minute}, RespostaDoDicionario{Status: 429}},
	} {
		m, nuvem, _, opcao := motorDoDicionario(t, true, "Kubernetes")
		nuvem.responder(c.resp, c.err)
		rel, err := m.Rodada(ctx)
		if err != nil || rel.Dicionario[TipoDicionarioAdiado] != 1 || !opcao.ligada || len(opcao.avisos) != 0 {
			t.Errorf("%s: a rodada deveria terminar sem erro e sem aviso: relatório %v, avisos %v, err %v", c.nome, rel.Dicionario, opcao.avisos, err)
		}
		// A rodada seguinte tenta de novo, com a mesma cópia.
		nuvem.responder(RespostaDoDicionario{Status: 200}, nil)
		if rel, err = m.Rodada(ctx); err != nil || nuvem.quantos() != 2 || rel.Dicionario[TipoDicionarioEnviado] != 1 {
			t.Errorf("%s: a rodada seguinte: puts %d, relatório %v, err %v", c.nome, nuvem.quantos(), rel.Dicionario, err)
		}
	}

	// Um 5xx também só para a fase, sem aviso.
	m, nuvem, _, opcao := motorDoDicionario(t, true, "Kubernetes")
	nuvem.responder(RespostaDoDicionario{Status: 503}, nil)
	if rel, err := m.Rodada(ctx); err != nil || rel.Dicionario[TipoDicionarioAdiado] != 1 || len(opcao.avisos) != 0 {
		t.Errorf("503: relatório %v, avisos %v, err %v", rel.Dicionario, opcao.avisos, err)
	}

	// O 401 derruba a rodada (o token caiu), e o contexto cancelado também.
	m, nuvem, _, _ = motorDoDicionario(t, true, "Kubernetes")
	nuvem.responder(RespostaDoDicionario{Status: 401}, ErrNaoAutorizado)
	if _, err := m.Rodada(ctx); !errors.Is(err, ErrNaoAutorizado) {
		t.Errorf("401: %v", err)
	}
	m, nuvem, _, _ = motorDoDicionario(t, true, "Kubernetes")
	nuvem.responder(RespostaDoDicionario{}, context.Canceled)
	if _, err := m.Rodada(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelado: %v", err)
	}
}

func TestMotorDicionario_OArquivoForaDosLimitesNaoVaiParaARede(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true)
	ctx := context.Background()
	fonte.sem = sementeDe()
	for i := 0; i <= dictionary.MaxTermosDaCopia; i++ {
		fonte.sem.Entradas = append(fonte.sem.Entradas, dictionary.EntradaSemente{Canonica: fmt.Sprintf("termo-%d", i)})
	}
	rel, err := m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 0 || rel.Dicionario[TipoDicionarioForaDoLimite] != 1 ||
		opcao.ultimoAviso() != FraseDoMotivoDaCopia("termos_demais") || !opcao.ligada {
		t.Fatalf("fora do limite: puts %d, relatório %v, aviso %q, err %v", nuvem.quantos(), rel.Dicionario, opcao.ultimoAviso(), err)
	}
}

func TestMotorDicionario_SemArquivoNaoFazNada(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true, "Kubernetes")
	fonte.err = dictionary.ErrSemArquivo
	rel, err := m.Rodada(context.Background())
	if err != nil || nuvem.quantos() != 0 || rel.Dicionario[TipoDicionarioSemArquivo] != 1 || len(opcao.avisos) != 0 {
		t.Fatalf("sem arquivo: puts %d, relatório %v, avisos %v, err %v", nuvem.quantos(), rel.Dicionario, opcao.avisos, err)
	}
}

// A fase só roda numa rodada sem erro: o pull que falha deixa o dicionário para a
// rodada seguinte.
func TestMotorDicionario_ORoundComErroNaoEnviaODicionario(t *testing.T) {
	m, nuvem, _, _ := motorDoDicionario(t, true, "Kubernetes")
	nuvem.nuvemDeRoteiro.pull = func(int64) (ChangePage, error) { return ChangePage{}, transitorio(CodigoRede) }
	if _, err := m.Rodada(context.Background()); err == nil {
		t.Fatal("o pull que falhou deveria falhar a rodada")
	}
	if nuvem.quantos() != 0 {
		t.Fatalf("o dicionário subiu numa rodada com erro: %d PUTs", nuvem.quantos())
	}
}

// Sem o lado do dicionário no cliente (um SyncClient de antes), a fase é nula.
func TestMotorDicionario_ClienteSemODicionarioDeixaDeFora(t *testing.T) {
	m, _, fonte, opcao := motorDoDicionario(t, true, "Kubernetes")
	m.Cliente = &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	if rel, err := m.Rodada(context.Background()); err != nil || soma(rel.Dicionario) != 0 {
		t.Fatalf("cliente sem o dicionário: %v, %v", rel.Dicionario, err)
	}
	_, _ = fonte, opcao
}

// A opção que não se lê é erro local da rodada (como as imagens com o banco).
func TestMotorDicionario_OpcaoIlegivelFalhaARodada(t *testing.T) {
	m, _, _, opcao := motorDoDicionario(t, true, "Kubernetes")
	opcao.errLigada = errors.New("banco fora")
	if _, err := m.Rodada(context.Background()); err == nil {
		t.Fatal("a opção que não se lê deveria voltar como erro")
	}
}

// O aviso de "fora dos limites" sai quando a cópia volta a caber, mesmo que seja
// a que a nuvem já tinha aceitado.
func TestMotorDicionario_OAvisoDeForaDoLimiteSaiQuandoACopiaVoltaACaber(t *testing.T) {
	m, nuvem, fonte, opcao := motorDoDicionario(t, true, "Kubernetes")
	ctx := context.Background()
	original := fonte.sem
	if _, err := m.Rodada(ctx); err != nil || nuvem.quantos() != 1 {
		t.Fatalf("envio inicial: puts %d, err %v", nuvem.quantos(), err)
	}
	grande := sementeDe()
	for i := 0; i <= dictionary.MaxTermosDaCopia; i++ {
		grande.Entradas = append(grande.Entradas, dictionary.EntradaSemente{Canonica: fmt.Sprintf("termo-%d", i)})
	}
	fonte.sem = grande
	if _, err := m.Rodada(ctx); err != nil || opcao.ultimoAviso() != FraseDoMotivoDaCopia("termos_demais") {
		t.Fatalf("fora do limite: aviso %q, err %v", opcao.ultimoAviso(), err)
	}
	fonte.sem = original
	rel, err := m.Rodada(ctx)
	if err != nil || nuvem.quantos() != 1 || rel.Dicionario[TipoDicionarioIgual] != 1 || opcao.ultimoAviso() != "" {
		t.Fatalf("de volta: puts %d, relatório %v, aviso %q, err %v", nuvem.quantos(), rel.Dicionario, opcao.ultimoAviso(), err)
	}
}
