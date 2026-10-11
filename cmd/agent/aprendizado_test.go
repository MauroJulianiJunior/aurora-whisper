package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/aprendiz"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Tudo aqui roda em t.TempDir(), com um runtime FALSO: nenhum teste toca o
// bin\dicionario.json, abre áudio, digita ou usa a área de transferência.

// dicionarioDoAprendizado está no formato que dictionary.Semente.Gravar
// escreve, para o teste provar o "idêntico byte a byte" do Desfazer.
const dicionarioDoAprendizado = `{
  "usuario": "mauro",
  "dominios": {
    "contabil": [
      "SistemaContabil.exe"
    ]
  },
  "alias_origem": {
    "DAAS": "cont-04"
  },
  "entradas": [
    {
      "canonica": "ECF",
      "prioridade": 90,
      "dominio": "contabil"
    },
    {
      "canonica": "DAS",
      "prioridade": 90,
      "dominio": "contabil",
      "alias": [
        "DAAS"
      ]
    }
  ]
}
`

// trocadorFalso faz o papel do runtime: guarda cada dicionário pedido, na
// ordem em que os pedidos terminam de chegar.
type trocadorFalso struct {
	mu      sync.Mutex
	pedidos []winruntime.Dicionario

	// Só para o teste que força uma troca lenta: se segurar não é nil, o
	// PRIMEIRO pedido avisa em entrou e espera segurar fechar antes de
	// valer.
	segurar  chan struct{}
	entrou   chan struct{}
	jaEntrou bool
}

func (f *trocadorFalso) TrocarDicionario(d winruntime.Dicionario) {
	f.mu.Lock()
	primeiro := !f.jaEntrou
	f.jaEntrou = true
	f.mu.Unlock()
	if primeiro && f.segurar != nil {
		f.entrou <- struct{}{}
		<-f.segurar
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.pedidos = append(f.pedidos, d)
}

func (f *trocadorFalso) quantos() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pedidos)
}

func (f *trocadorFalso) ultimo(t *testing.T) winruntime.Dicionario {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pedidos) == 0 {
		t.Fatal("o runtime não recebeu dicionário nenhum")
	}
	return f.pedidos[len(f.pedidos)-1]
}

func (f *trocadorFalso) todos() []winruntime.Dicionario {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]winruntime.Dicionario(nil), f.pedidos...)
}

type montagem struct {
	a       *aprendizadoDoAgent
	rt      *trocadorFalso
	caminho string
	log     *bytes.Buffer
}

func montarAprendizado(t *testing.T, conteudo, usuario string) montagem {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	lg := slog.New(slog.NewTextHandler(&log, nil))
	polish := novoPolishDoAgent(resolver.NewSmartResolver(nil), chaves.NovaMemoria(), &ia.Cliente{}, lg)
	rt := &trocadorFalso{}
	return montagem{a: novoAprendizado(caminho, usuario, rt, polish, lg), rt: rt, caminho: caminho, log: &log}
}

func (m montagem) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(m.caminho)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// resolve passa texto pelo Smart do dicionário pedido.
func resolve(t *testing.T, d winruntime.Dicionario, texto string) string {
	t.Helper()
	out, err := d.Resolver.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:    "mauro",
		ASRResult: asr.ASRResult{Text: texto},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return out.FinalText
}

// smartDasGuardas devolve o Smart que as guardas do Polish daquele
// dicionário consultam.
func smartDasGuardas(t *testing.T, d winruntime.Dicionario) *resolver.SmartResolver {
	t.Helper()
	pol, _ := d.AbrirPolidor("openai", "modelo-x", nil)
	pr, ok := pol.(*resolver.PolishResolver)
	if !ok {
		t.Fatalf("o Polidor é %T", pol)
	}
	return pr.Smart
}

func achaTermo(t *testing.T, s *resolver.SmartResolver, termo string) bool {
	t.Helper()
	cands, err := s.Dictionary.Search(context.Background(), dictionary.DictionaryQuery{UserID: "mauro", Term: termo})
	if err != nil {
		t.Fatal(err)
	}
	return len(cands) > 0
}

func TestAprendizado_AprenderGravaETrocaNaHora(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	antes, err := aprendiz.SmartDaSemente(mustSemente(t, dicionarioDoAprendizado))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := antes.Resolve(context.Background(), resolver.ResolveRequest{UserID: "mauro", ASRResult: asr.ASRResult{Text: "mandei o SF hoje"}})
	if out.FinalText != "mandei o SF hoje" {
		t.Fatalf("pré-condição: o dicionário de antes já trocava SF: %q", out.FinalText)
	}

	ap, err := m.a.Aprender(context.Background(), "ecf", "SF")
	if err != nil {
		t.Fatalf("Aprender: %v", err)
	}
	if ap != (dictionary.Aprendida{Termo: "ECF", Variante: "SF"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if m.rt.quantos() != 1 {
		t.Fatalf("o runtime recebeu %d dicionários, quero 1", m.rt.quantos())
	}
	d := m.rt.ultimo(t)

	// O Smart do próximo ditado.
	if got := resolve(t, d, "mandei o SF hoje"); got != "mandei o ECF hoje" {
		t.Errorf("o Smart novo resolveu %q, quero %q", got, "mandei o ECF hoje")
	}
	// As guardas do Polish do próximo ditado consultam o MESMO dicionário.
	if smartDasGuardas(t, d) != d.Resolver.(*resolver.SmartResolver) {
		t.Error("as guardas do Polish consultam outro dicionário que não o do Smart novo")
	}
	// O que já valia continua valendo, e o domínio vem do arquivo.
	if got := resolve(t, d, "paguei o DAAS"); got != "paguei o DAS" {
		t.Errorf("o que já valia deixou de valer: %q", got)
	}
	if dom := d.DominioDoApp("SistemaContabil.exe"); dom == nil || *dom != "contabil" {
		t.Errorf("DominioDoApp(SistemaContabil.exe) = %v, quero contabil", dom)
	}

	// Gravou, com a origem.
	if got := string(m.bytes(t)); !strings.Contains(got, `"SF": "painel"`) || !strings.Contains(got, `"SF"`) {
		t.Errorf("o arquivo não guarda o aprendido:\n%s", got)
	}
}

func TestAprendizado_TermoNovoEntraNasGuardasEDesfazerTira(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")

	if _, err := m.a.Aprender(context.Background(), "Wispr Flow", "Whisperflow"); err != nil {
		t.Fatalf("Aprender: %v", err)
	}
	d := m.rt.ultimo(t)
	if got := resolve(t, d, "uso o Whisperflow todo dia"); got != "uso o Wispr Flow todo dia" {
		t.Errorf("Smart novo = %q", got)
	}
	if !achaTermo(t, smartDasGuardas(t, d), "Wispr Flow") {
		t.Error("as guardas do Polish não veem o termo novo")
	}

	ap, err := m.a.Desfazer(context.Background(), "Whisperflow")
	if err != nil {
		t.Fatalf("Desfazer: %v", err)
	}
	if ap != (dictionary.Aprendida{Termo: "Wispr Flow", Variante: "Whisperflow"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if m.rt.quantos() != 2 {
		t.Fatalf("o runtime recebeu %d dicionários, quero 2", m.rt.quantos())
	}
	d = m.rt.ultimo(t)
	if got := resolve(t, d, "uso o Whisperflow todo dia"); got != "uso o Whisperflow todo dia" {
		t.Errorf("depois de desfazer o Smart ainda troca: %q", got)
	}
	if achaTermo(t, smartDasGuardas(t, d), "Wispr Flow") {
		t.Error("depois de desfazer, as guardas do Polish ainda veem o termo")
	}
	if got := string(m.bytes(t)); got != dicionarioDoAprendizado {
		t.Errorf("desfazer não deixou o arquivo idêntico:\n%s", got)
	}
}

func TestAprendizado_AprendidasListaOQueOPainelEnsinou(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if got, err := m.a.Aprendidas(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("antes de aprender: %#v, %v", got, err)
	}
	if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); err != nil {
		t.Fatal(err)
	}
	got, err := m.a.Aprendidas(context.Background())
	if err != nil || len(got) != 1 || got[0] != (dictionary.Aprendida{Termo: "ECF", Variante: "SF"}) {
		t.Fatalf("Aprendidas = %+v, %v", got, err)
	}
}

// TestAprendizado_RecusaNaoGravaNemTroca: o que a regra recusa deixa o
// arquivo como estava e não troca o dicionário do runtime.
func TestAprendizado_RecusaNaoGravaNemTroca(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	antes := m.bytes(t)

	if _, err := m.a.Aprender(context.Background(), "ECF", "DAAS"); !errors.Is(err, dictionary.ErrVarianteDeOutroTermo) {
		t.Errorf("variante de outro termo: erro = %v", err)
	}
	if _, err := m.a.Aprender(context.Background(), "ECF", "ecf"); !errors.Is(err, dictionary.ErrSemNovidade) {
		t.Errorf("só a caixa: erro = %v", err)
	}
	if _, err := m.a.Desfazer(context.Background(), "DAAS"); !errors.Is(err, dictionary.ErrNaoAprendida) {
		t.Errorf("desfazer o que não é do painel: erro = %v", err)
	}
	ctx, cancela := context.WithCancel(context.Background())
	cancela()
	if _, err := m.a.Aprender(ctx, "ECF", "SF"); !errors.Is(err, context.Canceled) {
		t.Errorf("contexto cancelado: erro = %v", err)
	}

	if m.rt.quantos() != 0 {
		t.Errorf("uma recusa trocou o dicionário do runtime (%d pedidos)", m.rt.quantos())
	}
	if !bytes.Equal(m.bytes(t), antes) {
		t.Error("uma recusa mudou o arquivo")
	}
}

func TestAprendizado_ArquivoInvalidoOuDeOutroUsuarioNaoEToado(t *testing.T) {
	casos := map[string]struct {
		conteudo string
		quer     error
	}{
		"arquivo inválido": {`{"usuario":"mauro","entradas":[`, dictionary.ErrArquivoInvalido},
		"outro usuário":    {strings.Replace(dicionarioDoAprendizado, `"mauro"`, `"outro"`, 1), dictionary.ErrUsuarioDiferente},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			m := montarAprendizado(t, c.conteudo, "mauro")
			if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); !errors.Is(err, c.quer) {
				t.Errorf("erro = %v, quero %v", err, c.quer)
			}
			if m.rt.quantos() != 0 || string(m.bytes(t)) != c.conteudo {
				t.Error("tocou no arquivo ou trocou o dicionário")
			}
		})
	}
}

// TestAprendizado_SemDicionarioNaSubida: sem dicionário na subida o usuário
// é vazio, e nada é gravado nem trocado — mesmo que um arquivo apareça
// depois, o resolver buscaria por um usuário que ele não tem. Hoje o Agent cria
// um dicionário vazio na subida (dicionario_vazio_test.go); este é o caso em
// que a criação falhou.
func TestAprendizado_SemDicionarioNaSubida(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "")
	if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("Aprender: erro = %v, quero ErrSemArquivo", err)
	}
	if _, err := m.a.Desfazer(context.Background(), "SF"); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("Desfazer: erro = %v, quero ErrSemArquivo", err)
	}
	if got, err := m.a.Aprendidas(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Errorf("Aprendidas = %#v, %v; quero lista vazia", got, err)
	}
	if m.rt.quantos() != 0 || string(m.bytes(t)) != dicionarioDoAprendizado {
		t.Error("tocou no arquivo ou trocou o dicionário")
	}

	// E com usuário mas sem o arquivo (apagado com o Agent rodando): a lista
	// é vazia, e aprender diz que não há dicionário.
	m = montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if err := os.Remove(m.caminho); err != nil {
		t.Fatal(err)
	}
	if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("arquivo apagado: erro = %v, quero ErrSemArquivo", err)
	}
	if got, err := m.a.Aprendidas(context.Background()); err != nil || len(got) != 0 {
		t.Errorf("arquivo apagado: Aprendidas = %#v, %v", got, err)
	}
}

// TestAprendizado_TrocaOQueEstaNoDisco: o dictlearn gravou outro alias
// depois da subida do Agent. O dicionário que vai ao runtime é o do arquivo
// relido, com o que os dois acrescentaram.
func TestAprendizado_TrocaOQueEstaNoDisco(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")

	doDisco := mustSemente(t, string(m.bytes(t)))
	comExterno, err := doDisco.AcrescentarAlias("ECF", "EFC", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := comExterno.Gravar(m.caminho); err != nil {
		t.Fatal(err)
	}

	if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); err != nil {
		t.Fatal(err)
	}
	d := m.rt.ultimo(t)
	if got := resolve(t, d, "o SF e o EFC"); got != "o ECF e o ECF" {
		t.Errorf("o dicionário trocado não tem o que o outro processo gravou: %q", got)
	}
}

// TestAprendizado_LogSoTemContagens: o termo e a variante são texto que a
// pessoa ditou, e nunca vão a log.
func TestAprendizado_LogSoTemContagens(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if _, err := m.a.Aprender(context.Background(), "Wispr Flow", "Whisperflow"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.a.Desfazer(context.Background(), "Whisperflow"); err != nil {
		t.Fatal(err)
	}
	log := m.log.String()
	if strings.Count(log, "aprendizado: dicionário atualizado") != 2 {
		t.Fatalf("o log não tem as duas linhas:\n%s", log)
	}
	for _, proibido := range []string{"Whisperflow", "whisperflow", "Wispr", "Flow"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
	for _, quer := range []string{"acao=aprender", "acao=desfazer", "termos=3", "variantes_do_painel=1", "variantes_do_painel=0"} {
		if !strings.Contains(log, quer) {
			t.Errorf("o log não traz %q:\n%s", quer, log)
		}
	}
}

// TestAprendizado_Concorrencia: várias operações ao mesmo tempo. A cada uma
// o runtime recebe o dicionário do arquivo que acabou de ser gravado, e a
// ordem dos pedidos é a das gravações: o k-ésimo pedido tem exatamente k
// variantes. Sem a trava que junta gravar e pedir a troca, o pedido de uma
// gravação antiga chegaria depois e desfaria o aprendizado da mais nova.
//
// Não depende do detector de corrida (sem cgo aqui). Rodar com -count=20
// -cpu=1,2,8.
func TestAprendizado_Concorrencia(t *testing.T) {
	const n = 16
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")

	variantes := make([]string, n)
	partes := make([]string, n)
	for i := range variantes {
		variantes[i] = fmt.Sprintf("v%02d", i)
		partes[i] = variantes[i]
	}
	texto := strings.Join(partes, " depois ")
	quantasTrocam := func(d winruntime.Dicionario) int {
		return strings.Count(resolve(t, d, texto), "ECF")
	}

	var wg sync.WaitGroup
	erros := make(chan error, 3*n)
	for i := range variantes {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := m.a.Aprender(context.Background(), "ECF", variantes[i]); err != nil {
				erros <- fmt.Errorf("Aprender %d: %w", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := m.a.Aprendidas(context.Background()); err != nil {
				erros <- fmt.Errorf("Aprendidas %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Error(err)
	}

	pedidos := m.rt.todos()
	if len(pedidos) != n {
		t.Fatalf("o runtime recebeu %d pedidos, quero %d", len(pedidos), n)
	}
	for k, d := range pedidos {
		if got := quantasTrocam(d); got != k+1 {
			t.Fatalf("o pedido %d troca %d variantes, quero %d: os pedidos chegaram fora da ordem das gravações", k+1, got, k+1)
		}
	}

	// E desfazendo tudo, também ao mesmo tempo: o último pedido não troca
	// mais nada, e o arquivo volta ao que era.
	erros = make(chan error, n)
	for i := range variantes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.a.Desfazer(context.Background(), variantes[i]); err != nil {
				erros <- fmt.Errorf("Desfazer %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Error(err)
	}
	if got := quantasTrocam(m.rt.ultimo(t)); got != 0 {
		t.Errorf("o último dicionário pedido ainda troca %d variantes", got)
	}
	if got := string(m.bytes(t)); got != dicionarioDoAprendizado {
		t.Errorf("depois de desfazer tudo o arquivo não voltou ao que era:\n%s", got)
	}
}

// TestAprendizado_OPedidoDeUmaGravacaoAntigaNaoChegaDepoisDaNova força a
// corrida que a trava evita, sem depender de sorte: a primeira operação
// grava e fica presa no meio da troca (um runtime ocupado); a segunda
// começa nesse meio tempo. Com a trava ela espera a primeira; sem ela
// gravaria, pediria a troca e terminaria ANTES — e o pedido velho, com uma
// variante só, chegaria depois e desfaria o aprendizado da mais nova.
func TestAprendizado_OPedidoDeUmaGravacaoAntigaNaoChegaDepoisDaNova(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	m.rt.segurar, m.rt.entrou = make(chan struct{}), make(chan struct{}, 1)

	var wg sync.WaitGroup
	erros := make(chan error, 2)
	ensinar := func(variante string, feito chan<- struct{}) {
		defer wg.Done()
		if _, err := m.a.Aprender(context.Background(), "ECF", variante); err != nil {
			erros <- err
		}
		if feito != nil {
			close(feito)
		}
	}

	wg.Add(1)
	go ensinar("v00", nil)
	<-m.rt.entrou // a primeira gravou e está no meio da troca

	segundaTerminou := make(chan struct{})
	wg.Add(1)
	go ensinar("v01", segundaTerminou)
	select {
	case <-segundaTerminou: // sem a trava, termina aqui
	case <-time.After(300 * time.Millisecond): // com a trava, espera a primeira
	}
	close(m.rt.segurar)
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Fatal(err)
	}

	if got := strings.Count(resolve(t, m.rt.ultimo(t), "v00 depois v01"), "ECF"); got != 2 {
		t.Fatalf("o último dicionário pedido troca %d das 2 variantes: um pedido de gravação antiga chegou depois do da nova", got)
	}
}

// TestPolishDoAgent_ComPreservaAPoliticaDeEsforco: trocar o dicionário não
// pode esquecer quais modelos recusaram o esforço de raciocínio.
func TestPolishDoAgent_ComPreservaAPoliticaDeEsforco(t *testing.T) {
	a := novoPolishDoAgent(resolver.NewSmartResolver(nil), chaves.NovaMemoria(), &ia.Cliente{}, slog.New(slog.DiscardHandler))
	novo := resolver.NewSmartResolver(nil)
	b := a.com(novo)
	if b == a || b.smart != novo || a.smart == novo {
		t.Fatal("com não devolveu uma cópia sobre o Smart novo")
	}
	if b.esforco != a.esforco || b.cofre != a.cofre || b.cliente != a.cliente {
		t.Error("com perdeu o cofre, o cliente ou a política de esforço")
	}
}

func mustSemente(t *testing.T, conteudo string) dictionary.Semente {
	t.Helper()
	s, err := dictionary.CarregarSemente(strings.NewReader(conteudo))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestAprendizado_TermoCriadoPeloPainelNaoReescreveAcento é o caso da
// revisão de 30-set-2026, de ponta a ponta (Arquivo, Popular, Smart): criar o
// termo para aprender a variante não cria, junto, uma regra de acento. A
// pessoa confirmou a variante; o texto certo dos ditados seguintes não muda.
func TestAprendizado_TermoCriadoPeloPainelNaoReescreveAcento(t *testing.T) {
	casos := []struct {
		termo, variante, entra, quer string
	}{
		{"esta", "ista", "a casa está pronta", "a casa está pronta"},
		{"e", "i", "a casa é nova", "a casa é nova"},
		{"está", "istá", "esta casa", "esta casa"},
		{"é", "eh", "esta casa e nova", "esta casa e nova"},
	}
	for _, c := range casos {
		t.Run(c.termo, func(t *testing.T) {
			m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
			if _, err := m.a.Aprender(context.Background(), c.termo, c.variante); err != nil {
				t.Fatalf("Aprender: %v", err)
			}
			d := m.rt.ultimo(t)
			if got := resolve(t, d, c.entra); got != c.quer {
				t.Errorf("o termo criado pelo painel reescreveu texto certo: %q virou %q", c.entra, got)
			}
			// A variante que a pessoa confirmou age.
			if got := resolve(t, d, "veja "+c.variante+" aqui"); got != "veja "+c.termo+" aqui" {
				t.Errorf("a variante confirmada não age: %q", got)
			}
			// O repositório ainda acha o termo (com a marca AliasOnly); é o Smart
			// que o ignora, e o Polish só o protege quando a variante age no
			// ditado (internal/resolver, painel_aliasonly_revisao_test.go).
			if !achaTermo(t, smartDasGuardas(t, d), c.termo) {
				t.Error("o repositório não acha o termo criado")
			}
		})
	}
}

// TestAprendizado_RecusaDeRegraDaRevisaoNaoGravaNemTroca: o termo novo que já
// é variante de outro termo, e a variante que o Smart nunca procuraria, são
// recusados sem tocar no arquivo e sem pedir troca ao runtime.
func TestAprendizado_RecusaDeRegraDaRevisaoNaoGravaNemTroca(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")

	_, err := m.a.Aprender(context.Background(), "DAAS", "outra coisa")
	var e *dictionary.ErroTermoEVarianteDeOutro
	if !errors.Is(err, dictionary.ErrTermoEVarianteDeOutro) || !errors.As(err, &e) || e.Dono != "DAS" {
		t.Errorf("termo que é variante de outro: erro = %#v", err)
	}
	if _, err := m.a.Aprender(context.Background(), "ECF", "um dois tres quatro cinco"); !errors.Is(err, dictionary.ErrInvalido) {
		t.Errorf("variante de 5 palavras: erro = %v", err)
	}
	if m.rt.quantos() != 0 {
		t.Errorf("o runtime recebeu %d dicionários por um pedido recusado", m.rt.quantos())
	}
	if got := string(m.bytes(t)); got != dicionarioDoAprendizado {
		t.Errorf("uma recusa mexeu no arquivo:\n%s", got)
	}
}
