package main

// O modo -historico: em vez do dataset sintético, os ditados LONGOS do próprio
// histórico de quem roda (os do PC e os do celular que o sync trouxe), para
// descobrir POR QUE a guarda de palavras recusa o Polish neles. Pedido do
// Mauro (16-out-2026): no log do Agent, nenhum ditado com menos de 600
// caracteres caiu na guarda, e os de 1.000 ou mais caíram todos; o log só
// guarda o motivo, nunca o texto (AC-048), então não diz qual palavra mudou.
//
// PRIVACIDADE: o texto dos ditados aparece só no terminal de quem roda. Neste
// modo o comando não grava relatório nem arquivo nenhum, e nada vai a log. O
// que sai da máquina é o mesmo que o Polish do Agent já manda ao provedor: o
// texto do Smart, o prompt fixo e o modelo (AC-047).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"

	// O driver do SQLite (modernc.org/sqlite, puro Go): o pacote storage não o
	// importa (ADR-008); quem abre o banco registra.
	_ "modernc.org/sqlite"
)

// CatHistorico é a categoria dos casos vindos do histórico.
const CatHistorico = "historico"

// historicoLocal é o valor de -historico que aponta o banco do Agent
// instalado: %LOCALAPPDATA%\AuroraWhisper\historico.db.
const historicoLocal = "local"

// caminhoDoHistorico resolve o valor de -historico.
func caminhoDoHistorico(valor string, getenv func(string) string) (string, error) {
	if valor != historicoLocal {
		return valor, nil
	}
	base := getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("-historico local: o LOCALAPPDATA não está definido; passe o caminho do historico.db")
	}
	return filepath.Join(base, pastas.NomeDaPastaDeDados, pastas.BancoDoHistorico), nil
}

// abrirHistorico abre o banco do histórico só para leitura: o comando nunca
// escreve nele, e o Agent pode estar rodando ao mesmo tempo.
func abrirHistorico(caminho string) (*sql.DB, error) {
	return storage.AbrirSomenteLeitura(caminho)
}

// Ditado é um ditado do histórico que vira caso.
type Ditado struct {
	ID       string
	Texto    string
	Origem   string
	CriadoEm string
}

// lerDitadosLongos lê os ditados com o modo smart (a entrada exata do Polish:
// o texto do Smart, com o Polish recusado ou desligado), sem os apagados,
// com pelo menos minCaracteres, os mais recentes primeiro, até max.
//
// Os ditados com modo polish ficam de fora: neles o texto guardado já é a
// saída do Polish, não a entrada.
func lerDitadosLongos(ctx context.Context, db *sql.DB, minCaracteres, max int) ([]Ditado, error) {
	linhas, err := db.QueryContext(ctx, `
SELECT id, final_text, source_platform, created_at
  FROM local_transcriptions
 WHERE deleted_at IS NULL AND mode = 'smart' AND length(final_text) >= ?
 ORDER BY created_at DESC
 LIMIT ?`, minCaracteres, max)
	if err != nil {
		return nil, fmt.Errorf("lendo o histórico: %w", err)
	}
	defer linhas.Close()
	var out []Ditado
	for linhas.Next() {
		var d Ditado
		if err := linhas.Scan(&d.ID, &d.Texto, &d.Origem, &d.CriadoEm); err != nil {
			return nil, fmt.Errorf("lendo o histórico: %w", err)
		}
		out = append(out, d)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("lendo o histórico: %w", err)
	}
	return out, nil
}

// casosDoHistorico transforma os ditados em casos. O ID do caso é "h-" mais
// os 8 primeiros caracteres do id do ditado: dá para achar o ditado no
// painel sem pôr o texto em lugar nenhum. Sem esperado (não há gabarito) e
// sem termos do dicionário (a guarda de termos fica mais frouxa aqui; o
// alvo do modo é a guarda de palavras).
func casosDoHistorico(ditados []Ditado) []Caso {
	casos := make([]Caso, 0, len(ditados))
	for _, d := range ditados {
		id := d.ID
		if len(id) > 8 {
			id = id[:8]
		}
		casos = append(casos, Caso{ID: "h-" + id, Categoria: CatHistorico, Entrada: d.Texto})
	}
	return casos
}

// rodadaHistorico são os parâmetros do modo -historico.
type rodadaHistorico struct {
	Caminho                   string
	MinCaracteres, MaxDitados int
	Cands                     []Candidato
	Precos                    map[string]Preco
	// Cfg traz rodadas, prazos, guardas e limite; os casos vêm do histórico.
	Cfg Config
}

// rodarHistorico é o modo -historico: lê os ditados longos, mostra a
// estimativa de custo ANTES de pedir a chave, roda cada um pelo mesmo
// caminho do Agent e escreve o diagnóstico no terminal. Não grava nada.
func rodarHistorico(ctx context.Context, r rodadaHistorico, stdout, stderr io.Writer, d deps) int {
	getenv, abrir := d.Getenv, d.AbrirHistorico
	if getenv == nil {
		getenv = os.Getenv
	}
	if abrir == nil {
		abrir = abrirHistorico
	}
	caminho, err := caminhoDoHistorico(r.Caminho, getenv)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 2
	}
	// Sem o arquivo, o SQLite só diz "unable to open database file (14)". O
	// Agent rodado de uma pasta (modo portátil, o PC de desenvolvimento) guarda
	// o histórico ao lado do executável, e não no LOCALAPPDATA.
	if _, err := os.Stat(caminho); err != nil {
		fmt.Fprintf(stderr, "polishbench: não achei o histórico em %s. Se o Agent roda de uma pasta (modo portátil, como no PC de desenvolvimento), o historico.db fica ao lado do executável: passe esse caminho em -historico.\n", caminho)
		return 1
	}
	db, err := abrir(caminho)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 1
	}
	defer db.Close()
	ditados, err := lerDitadosLongos(ctx, db, r.MinCaracteres, r.MaxDitados)
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: %v (confira o caminho do historico.db)\n", err)
		return 1
	}
	if len(ditados) == 0 {
		fmt.Fprintf(stdout, "nenhum ditado do modo smart com %d caracteres ou mais no histórico: nada a medir, nenhuma chave pedida.\n", r.MinCaracteres)
		return 0
	}
	casos := casosDoHistorico(ditados)
	cfg := r.Cfg
	cfg.Casos = casos
	exec := &Executor{Cfg: cfg, Agora: d.Agora, Progresso: stderr}

	menor, maior := 1<<30, 0
	var estimativa float64
	for _, c := range casos {
		n := len([]rune(c.Entrada))
		menor, maior = min(menor, n), max(maior, n)
		// O prompt tem cerca de 1.000 tokens; o ditado, uns 3,5 caracteres
		// por token, na ida e na volta.
		t := ia.Tokens{Entrada: tokensEntradaEstimados + n*10/35, Saida: n*10/35 + 20}
		for _, cand := range r.Cands {
			estimativa += float64(cfg.Rodadas) * custoDe(t, r.Precos[cand.ChavePreco()])
		}
	}
	fmt.Fprintf(stdout, "polishbench — diagnóstico do histórico — %s\n", d.Agora().Format("2006-01-02 15:04"))
	fmt.Fprintf(stdout, "ditados: %d, do modo smart, os mais recentes, de %d a %d caracteres\n", len(casos), menor, maior)
	fmt.Fprintf(stdout, "prompt: %s (hash %s)\n", resolver.PolishPromptRef, resolver.PolishPromptHash())
	for _, c := range r.Cands {
		fmt.Fprintf(stdout, "modelo: %s (esforço: %s)\n", c.Rotulo(), c.EsforcoEfetivo())
	}
	fmt.Fprintf(stdout, "%d rodada(s); estimativa de custo cerca de %s (limite: %s)\n", cfg.Rodadas, usd(estimativa, 2), usd(cfg.LimiteUSD, 2))
	fmt.Fprintln(stdout, "o texto dos ditados aparece SÓ neste terminal: nada é gravado em arquivo, relatório ou log.")
	if estimativa > cfg.LimiteUSD {
		fmt.Fprintf(stderr, "polishbench: recusado: a estimativa (%s) já passa do limite (%s). Reduza -max-ditados ou -rodadas, ou suba -limite-usd. Nenhuma chave foi pedida.\n",
			usd(estimativa, 2), usd(cfg.LimiteUSD, 2))
		return 2
	}

	ex, cod := pedirChavesERodar(ctx, r.Cands, r.Precos, exec, stdout, stderr, d)
	if cod != 0 {
		return cod
	}
	var buf bytes.Buffer
	escreverDiagnostico(&buf, ditados, casos, ex.saidas)
	var gasto float64
	for _, s := range ex.saidas {
		for _, res := range s.Resultados {
			gasto += custoDe(res.Tokens, s.Preco)
		}
	}
	fmt.Fprintf(&buf, "\ngasto: cerca de %s\n", usd(gasto, 3))
	// Tudo o que sai passa pela máscara das chaves, como o resumo do modo normal.
	fmt.Fprint(stdout, mascarar(buf.String(), ex.segredos))
	if ex.incompleta != "" {
		fmt.Fprintf(stdout, "rodada incompleta: %s\n", ex.incompleta)
		return 1
	}
	fmt.Fprintln(stdout, "nada foi gravado: o diagnóstico existe só neste terminal.")
	return 0
}

// origemLegivel diz de onde veio o ditado, para o terminal.
func origemLegivel(origem string) string {
	switch strings.ToLower(origem) {
	case "web":
		return "celular"
	case "windows":
		return "PC"
	}
	return origem
}
