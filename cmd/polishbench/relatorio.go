package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Rodada é tudo o que o relatório precisa saber de uma execução. Nada aqui é
// segredo: a chave nunca chega a esta struct.
type Rodada struct {
	Data    time.Time
	Comando string
	Cfg     Config
	// Casos é o dataset inteiro (frases.json mais as referências do ASR).
	Casos       []Caso
	HashDataset string
	ArqFrases   string
	ArqManifest string
	// Candidatos são os pedidos em -modelos; Pulados os que ficaram sem chave.
	Candidatos []Candidato
	Pulados    []Candidato
	Metricas   []Metricas
	Criterios  Criterios
	Decisao    Decisao
	GastoTotal float64
	// Incompleta diz que a rodada não chegou ao fim; Motivo diz por quê.
	Incompleta bool
	Motivo     string
	Go         string
	SO         string
	// RefPreservacao é a edição por palavra do PRÓPRIO `esperado` sobre a
	// entrada: o mínimo legítimo (pontuação falada, ordinais e hesitações que
	// saem), contra o qual se lê a preservação de cada modelo.
	RefPreservacao metrics.Stats
	// RefEco é o que um modelo que só devolve a entrada marcaria: o piso da
	// organização.
	RefEco RefOrganizacao
}

// num formata um número com casas decimais e vírgula decimal.
func num(v float64, casas int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', casas, 64), ".", ",", 1)
}

func pct(v float64) string { return num(100*v, 1) + "%" }

func seg(v float64) string { return num(v, 2) + " s" }

// dur formata uma duração em segundos com vírgula decimal: "3,5 s", "10 s".
func dur(d time.Duration) string {
	return strings.Replace(strconv.FormatFloat(d.Seconds(), 'f', -1, 64), ".", ",", 1) + " s"
}

func usd(v float64, casas int) string { return "US$ " + num(v, casas) }

func (m Metricas) qtdPct(n, d int) string {
	if d == 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d (%s)", n, d, pct(float64(n)/float64(d)))
}

// inline escreve um texto do dataset ou do modelo numa linha de markdown, em
// código, sem quebrar a tabela nem a linha: quebra de linha vira "⏎", crase
// vira apóstrofo, "|" vira "¦". Corta em 400 caracteres.
func inline(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "⏎")
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "|", "¦")
	if utf8.RuneCountInString(s) > 400 {
		s = string([]rune(s)[:400]) + "…"
	}
	return "`" + s + "`"
}

// hashCompletoDoPrompt é o sha256 inteiro do prompt do Polish; o log do Agent
// leva só os 8 primeiros caracteres.
func hashCompletoDoPrompt() string {
	soma := sha256.Sum256([]byte(resolver.PolishPrompt))
	return hex.EncodeToString(soma[:])
}

// rotulosDe devolve os rótulos das métricas, na ordem.
func rotulosDe(ms []Metricas) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Cand.Rotulo()
	}
	return out
}

// linha escreve uma linha de tabela markdown.
func linha(b *strings.Builder, celulas ...string) {
	b.WriteString("| " + strings.Join(celulas, " | ") + " |\n")
}

func separador(b *strings.Builder, n int) {
	linha(b, append([]string{"---"}, repetir("---:", n-1)...)...)
}

func repetir(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// gerarRelatorio escreve o relatório da rodada em markdown.
func gerarRelatorio(r Rodada) string {
	var b strings.Builder
	rot := rotulosDe(r.Metricas)

	fmt.Fprintf(&b, "# Benchmark do Polish — %s\n\n", r.Data.Format("2006-01-02"))
	fmt.Fprintf(&b, "Gerado por `cmd/polishbench` (slice S3 da ADR-016). Quem rodou foi o mantenedor, no terminal dele, com a chave dele colada num campo oculto: **a chave não aparece neste arquivo** (nem em flag, variável de ambiente, arquivo ou log). As frases mostradas vêm do dataset sintético (`frases.json`) e, na categoria `ja_organizada`, das `reference` do manifest do dataset de ASR do projeto, que são frases ditadas de verdade e têm nomes de pessoas e de empresas: confira os exemplos antes de compartilhar este arquivo.\n\n")
	if r.Incompleta {
		fmt.Fprintf(&b, "> **RODADA INCOMPLETA** (%s). Os números abaixo valem só para o que foi medido, e **não há decisão**.\n\n", r.Motivo)
	}
	fmt.Fprintf(&b, "Comando: `polishbench %s`\n\n", strings.ReplaceAll(r.Comando, "`", "'"))

	resumoDaDecisao(&b, r)

	// Parâmetros ---------------------------------------------------------
	b.WriteString("## Parâmetros\n\n")
	fmt.Fprintf(&b, "- **Prompt:** `%s`, hash `%s` (sha256 completo `%s`).\n", resolver.PolishPromptRef, resolver.PolishPromptHash(), hashCompletoDoPrompt())
	fmt.Fprintf(&b, "- **Dataset:** %d frases, hash sha256 `%s`. Frases: `%s`; \"já organizada\": `%s`.\n", len(r.Casos), r.HashDataset, nomeDoArquivo(r.ArqFrases, padraoFrases), nomeDoArquivo(r.ArqManifest, padraoManifesto))
	b.WriteString("- **Modelos e esforço:**\n")
	for _, c := range r.Candidatos {
		nota := ""
		for _, p := range r.Pulados {
			if p == c {
				nota = " — **pulado, sem chave**"
			}
		}
		if nota == "" && !rodou(r.Metricas, c) {
			nota = " — **não rodou (rodada interrompida)**"
		}
		if c.LinhaDeBase {
			nota += " — **linha de base: medido, nunca vence**"
		}
		fmt.Fprintf(&b, "  - `%s` (%s), esforço de raciocínio: %s%s\n", c.Rotulo(), ia.Nome(c.Provedor), c.EsforcoEfetivo(), nota)
	}
	fmt.Fprintf(&b, "- **Rodadas:** %d por frase; chamadas em paralelo: %d.\n", r.Cfg.Rodadas, max(r.Cfg.Paralelo, 1))
	fmt.Fprintf(&b, "- **Conexão:** %s\n", textoDaConexao(r.Cfg.ConexaoNova))
	fmt.Fprintf(&b, "- **Prazos:** o do Agent é %s (passou disso, o Agent usa o Smart); cada chamada aqui roda com até %s, para medir também as que estourariam.\n",
		dur(r.Cfg.PrazoAgente), dur(r.Cfg.PrazoMax))
	minP := r.Cfg.MinPalavras
	if minP <= 0 {
		minP = resolver.PolishMinPalavras
	}
	fmt.Fprintf(&b, "- **Guardas:** mínimo de %d palavras (padrão %d); orçamento de palavras apagadas `MaxEdicao` = %s (padrão do Agent %s).%s\n",
		minP, resolver.PolishMinPalavras, num(r.Cfg.MaxEdicao, 3), num(resolver.PolishMaxEdicao, 3), foraDoPadrao(r.Cfg))
	fmt.Fprintf(&b, "- **Regra de escolha:** falha crítica depois da guarda = 0; fallback ≤ %s (como o Agent o veria, sem as frases-armadilha); p95 morno ≤ %s. Entre os que passam, o mais barato; no empate, o de maior F1 de pontuação; um vencedor por provedor. %s\n",
		pct(r.Criterios.FallbackMax), dur(r.Criterios.P95Max), textoDoPiso(r.Criterios))
	b.WriteString("- **Preços** (US$ por 1 milhão de tokens, entrada/saída): ")
	var precos []string
	for _, m := range r.Metricas {
		precos = append(precos, fmt.Sprintf("`%s` %s/%s", m.Cand.ChavePreco(), num(m.Preco.Entrada, 3), num(m.Preco.Saida, 3)))
	}
	b.WriteString(strings.Join(precos, "; ") + ".\n")
	fmt.Fprintf(&b, "- **Ambiente:** %s, %s.\n\n", r.Go, r.SO)

	categoriasDoDataset(&b, r)

	if len(r.Metricas) == 0 {
		b.WriteString("Nenhum candidato rodou.\n")
		return b.String()
	}

	// Tabela por modelo --------------------------------------------------
	b.WriteString("## Métricas por modelo\n\n")
	linha(&b, append([]string{"Métrica"}, rot...)...)
	separador(&b, len(rot)+1)
	linhaDe := func(nome string, f func(m Metricas) string) {
		cel := []string{nome}
		for _, m := range r.Metricas {
			cel = append(cel, f(m))
		}
		linha(&b, cel...)
	}
	linhaDe("Chamadas ao provedor (previstas)", func(m Metricas) string {
		return fmt.Sprintf("%d (%d)", m.Chamadas, m.Planejadas)
	})
	linhaDe("Sem `final_text` lido", func(m Metricas) string { return fmt.Sprintf("%d", m.Chamadas-m.Sucessos) })
	linhaDe(fmt.Sprintf("**Fallback** (Agent, sem armadilhas; regra ≤ %s)", pct(r.Criterios.FallbackMax)), func(m Metricas) string {
		return m.qtdPct(m.FallbacksSemArmadilha, m.ChamadasSemArmadilha)
	})
	linhaDe("Fallback, todas as categorias", func(m Metricas) string { return m.qtdPct(m.FallbackTotal, m.Chamadas) })
	linhaDe("**Falha crítica DEPOIS da guarda** (chamadas; regra 0)", func(m Metricas) string {
		return m.qtdPct(m.ChamadasCriticasDepois, m.Sucessos)
	})
	linhaDe("Falha crítica ANTES da guarda (chamadas)", func(m Metricas) string {
		return m.qtdPct(m.ChamadasCriticasAntes, m.Sucessos)
	})
	linhaDe("Injeção obedecida, antes / depois", func(m Metricas) string {
		if m.InjecaoN == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %d", m.qtdPct(m.InjecaoObedecidaAntes, m.InjecaoN), m.InjecaoObedecidaDepois)
	})
	linhaDe("Pergunta respondida, antes / depois", func(m Metricas) string {
		if m.PerguntaN == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %d", m.qtdPct(m.PerguntaRespondidaAntes, m.PerguntaN), m.PerguntaRespondidaDepois)
	})
	linhaDe("Preservação: edição por palavra, média", func(m Metricas) string { return pct(m.Preservacao.Mean) })
	linhaDe("Preservação: edição por palavra, p95", func(m Metricas) string { return pct(m.Preservacao.P95) })
	linhaDe("F1 de pontuação (precisão / revocação)", func(m Metricas) string {
		return fmt.Sprintf("%s (%s / %s)", num(m.F1, 3), num(m.Precisao, 3), num(m.Revocacao, 3))
	})
	linhaDe("F1 de pontuação, fora de \"já organizada\"", func(m Metricas) string {
		if m.Sucessos == 0 {
			return "—"
		}
		return num(m.F1Organizando(), 3)
	})
	linhaDe("Acerto de estrutura, todas as frases", func(m Metricas) string { return m.qtdPct(m.EstruturaOK, m.EstruturaN) })
	linhaDe("Acerto de estrutura, frases com lista ou parágrafos", func(m Metricas) string {
		return m.qtdPct(m.EstruturaListaOK, m.EstruturaListaN)
	})
	linhaDe("Tipo do marcador da lista (1. ou -)", func(m Metricas) string { return m.qtdPct(m.MarcadorOK, m.MarcadorN) })
	linhaDe("\"Já organizada\" devolvida igual", func(m Metricas) string {
		return m.qtdPct(m.JaOrganizadaIgual, m.JaOrganizadaN)
	})
	linhaDe("Maiúscula a mais no meio da frase (só medida): chamadas com alguma / palavras", func(m Metricas) string {
		if m.Sucessos == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %d", m.qtdPct(m.CaixaAMaisChamadas, m.Sucessos), m.CaixaAMaisPalavras)
	})
	linhaDe("Latência p50 (morna)", func(m Metricas) string { return latencia(m, m.Latencia.P50) })
	linhaDe(fmt.Sprintf("**Latência p95** (morna; regra ≤ %s)", dur(r.Criterios.P95Max)), func(m Metricas) string {
		return latencia(m, m.Latencia.P95)
	})
	linhaDe("Latência da primeira chamada (fria)", func(m Metricas) string {
		if !m.TemFria {
			return "—"
		}
		return seg(m.Fria)
	})
	linhaDe(fmt.Sprintf("Acima de %s (todas as medidas)", dur(r.Cfg.PrazoAgente)), func(m Metricas) string {
		return m.qtdPct(m.AcimaDoPrazo, m.LatenciaN)
	})
	linhaDe("Tokens por chamada, entrada / saída", func(m Metricas) string {
		if m.Chamadas == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %s", num(float64(m.TokensEntrada)/float64(m.Chamadas), 0), num(float64(m.TokensSaida)/float64(m.Chamadas), 0))
	})
	linhaDe("Tokens de raciocínio por chamada, média / máx", func(m Metricas) string {
		if m.Chamadas == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %d", num(float64(m.TokensRaciocinio)/float64(m.Chamadas), 1), m.RaciocinioMax)
	})
	linhaDe("**Custo por 1.000 ditados**", func(m Metricas) string { return usd(m.CustoPor1000, 4) })
	linhaDe("Custo da rodada", func(m Metricas) string { return usd(m.CustoTotal, 4) })
	linhaDe("Variação entre rodadas (frases com saída diferente)", func(m Metricas) string {
		return m.qtdPct(m.VariacaoCasos, m.VariacaoN)
	})
	linhaDe("**Regra de escolha**", func(m Metricas) string {
		for i, mm := range r.Metricas {
			if mm.Cand == m.Cand && i < len(r.Decisao.Vereditos) {
				if r.Decisao.Incompleta {
					return "sem decisão"
				}
				if r.Decisao.Vereditos[i].Passou {
					if mm.Cand.LinhaDeBase {
						return "passou (linha de base: não vence)"
					}
					return "**passou**"
				}
				return "não passou"
			}
		}
		return "—"
	})
	b.WriteString("\nLegenda: \"chamadas\" são as que foram ao provedor (as frases com menos de 4 palavras nem vão). A preservação usa a saída do modelo antes da guarda. A latência morna exclui a primeira chamada de cada modelo e conta só as chamadas em que o modelo respondeu (ou estourou o prazo).\n\n")
	if r.RefPreservacao.N > 0 {
		fmt.Fprintf(&b, "Referência da preservação: o `esperado` do dataset já edita, sobre a entrada, %s das palavras em média (p95 %s), porque tira hesitação, gagueira, pontuação falada e ordinal de lista. É o mínimo legítimo: o que passar disso é edição a mais.\n\n", pct(r.RefPreservacao.Mean), pct(r.RefPreservacao.P95))
	}

	fmt.Fprintf(&b, "Referência da organização: um modelo que só devolve a entrada marcaria, neste dataset, F1 de pontuação de %s (fora de \"já organizada\")", num(r.RefEco.F1, 3))
	if r.RefEco.EstruturaN > 0 {
		fmt.Fprintf(&b, " e acerto de estrutura de %s nas %d frase(s) com lista ou parágrafos", pct(r.RefEco.Estrutura), r.RefEco.EstruturaN)
	}
	b.WriteString(". Um modelo que organiza tem de ficar bem acima disso; a regra aprovada não exige (sem piso), e o aviso da Decisão cobre o vencedor que fica perto.\n\n")

	falhasPorTipo(&b, r)
	fallbackPorMotivo(&b, r)
	porCategoria(&b, r)
	maiusculaAMais(&b, r)
	precoDasGuardas(&b, r)
	custoDaRodada(&b, r)
	decisao(&b, r)
	protocolo(&b, r)
	exemplos(&b, r)
	limites(&b, r)
	return b.String()
}

// nomeDoArquivo é como um caminho de arquivo entra no relatório versionado:
// só o nome-base (um caminho pode ter o nome de quem rodou), com o aviso de que
// o arquivo não é o do repositório quando o caminho difere do padrão.
func nomeDoArquivo(caminho, padrao string) string {
	base := filepath.Base(filepath.ToSlash(caminho))
	if caminho == padrao {
		return base
	}
	return base + " (arquivo em caminho diferente do repositório; o dataset pode ser outro)"
}

// textoDaConexao diz como a latência foi medida.
func textoDaConexao(nova bool) string {
	if nova {
		return "**nova a cada chamada** (`-conexao-nova`: TCP+TLS todas as vezes, o pior caso do Agent com ditados espaçados por mais de 90 s)."
	}
	return "**reaproveitada** (as chamadas saem seguidas no mesmo transporte, sem TCP+TLS novo a cada uma). O Agent só reaproveita a conexão se o ditado seguinte vier em menos de 90 s; espaçados, ele paga o handshake quase sempre, então o p95 daqui **subestima** esse caso. `-conexao-nova` mede o pior caso."
}

// textoDoPiso diz se a regra tem piso de organização.
func textoDoPiso(c Criterios) string {
	if c.F1Min <= 0 && c.EstruturaMin <= 0 {
		return "**Sem piso de organização** (a regra aprovada não tem): quem devolve a entrada sem organizar passa se as três medidas acima estiverem em ordem; veja o aviso na Decisão."
	}
	var ps []string
	if c.F1Min > 0 {
		ps = append(ps, "F1 de pontuação (fora de já organizada) ≥ "+num(c.F1Min, 3))
	}
	if c.EstruturaMin > 0 {
		ps = append(ps, "acerto de estrutura das listas ≥ "+pct(c.EstruturaMin))
	}
	return "**Piso de organização** (decisão do mantenedor, fora da regra aprovada): " + strings.Join(ps, "; ") + "."
}

func foraDoPadrao(c Config) string {
	if c.MaxEdicao != resolver.PolishMaxEdicao || (c.MinPalavras > 0 && c.MinPalavras != resolver.PolishMinPalavras) {
		return " **Parâmetros fora do padrão do Agent: esta rodada mede outra configuração de guarda.**"
	}
	return ""
}

func latencia(m Metricas, v float64) string {
	if m.Latencia.N == 0 {
		return "—"
	}
	return seg(v)
}

func resumoDaDecisao(b *strings.Builder, r Rodada) {
	b.WriteString("## Resumo\n\n")
	switch {
	case r.Incompleta:
		b.WriteString("Rodada incompleta: sem decisão.\n\n")
	case len(r.Decisao.Vencedores) == 0:
		b.WriteString("**Nenhum candidato passou pela regra de escolha.**" + notaSoLinhaDeBase(r, "") + " Veja \"Decisão\" abaixo.\n\n")
	default:
		for _, p := range r.Decisao.provedoresNaOrdem() {
			fmt.Fprintf(b, "- **%s:** `%s`.\n", nomeDoProvedor(p), r.Decisao.Vencedores[p])
		}
		b.WriteString("\nO vencedor vira só uma sugestão pré-selecionada na tela, quando existe na lista da conta; nunca é obrigatório, e o Smart segue padrão (ADR-016, decisão 10).\n\n")
	}
	for _, m := range r.Metricas {
		if m.ChamadasCriticasDepois > 0 {
			fmt.Fprintf(b, "> **BURACO NA GUARDA:** `%s` teve %d chamada(s) com falha crítica DEPOIS da guarda. É o achado mais importante da rodada: veja \"Exemplos\".\n\n", m.Cand.Rotulo(), m.ChamadasCriticasDepois)
		}
	}
	for _, a := range avisosDeOrganizacao(r) {
		fmt.Fprintf(b, "> **Atenção:** %s\n\n", a)
	}
	fmt.Fprintf(b, "Gasto da rodada: %s.\n\n", usd(r.GastoTotal, 4))
}

// notaSoLinhaDeBase diz, quando o provedor (vazio: qualquer um) não tem
// vencedor mas a linha de base passou na regra, que ela não concorre.
func notaSoLinhaDeBase(r Rodada, provedor string) string {
	if r.Incompleta {
		return ""
	}
	for i, m := range r.Metricas {
		if m.Cand.LinhaDeBase && (provedor == "" || m.Cand.Provedor == provedor) && i < len(r.Decisao.Vereditos) && r.Decisao.Vereditos[i].Passou {
			return " Só a linha de base passou, e ela não concorre."
		}
	}
	return ""
}

func categoriasDoDataset(b *strings.Builder, r Rodada) {
	b.WriteString("### Dataset por categoria\n\n")
	linha(b, "Categoria", "Frases", "Vão ao provedor")
	separador(b, 3)
	conta := contagemPorCategoria(r.Casos)
	minP := r.Cfg.MinPalavras
	if minP <= 0 {
		minP = resolver.PolishMinPalavras
	}
	for _, cat := range todasAsCategorias() {
		vao := 0
		for _, c := range r.Casos {
			if c.Categoria == cat && len(palavrasDe(c.Entrada)) >= minP {
				vao++
			}
		}
		linha(b, cat, fmt.Sprintf("%d", conta[cat]), fmt.Sprintf("%d", vao))
	}
	b.WriteString("\n")
}

func falhasPorTipo(b *strings.Builder, r Rodada) {
	b.WriteString("## Falha crítica por tipo\n\n")
	b.WriteString("Contada em chamadas: uma chamada pode ter mais de um tipo. **Antes** é o que o modelo errou; **depois** é o que a guarda deixou chegar ao campo e o Agent não cortou pelo prazo (tem de ser 0); a saída aceita mas lenta, que o Agent descarta, é contada à parte em \"Fallback por motivo\".\n\n")
	cab := []string{"Tipo"}
	for _, rt := range rotulosDe(r.Metricas) {
		cab = append(cab, rt+" antes", rt+" depois")
	}
	linha(b, cab...)
	separador(b, len(cab))
	for _, t := range tiposDeFalha {
		cel := []string{string(t)}
		for _, m := range r.Metricas {
			cel = append(cel, fmt.Sprintf("%d", m.CriticasAntes[t]), fmt.Sprintf("%d", m.CriticasDepois[t]))
		}
		linha(b, cel...)
	}
	cel := []string{"chamadas com alguma falha"}
	for _, m := range r.Metricas {
		cel = append(cel, fmt.Sprintf("%d", m.ChamadasCriticasAntes), fmt.Sprintf("%d", m.ChamadasCriticasDepois))
	}
	linha(b, cel...)
	b.WriteString("\nSignificado: `numero` = número (dígito ou por extenso) trocado, apagado ou acrescentado; `sigla_termo` = sigla, termo do dicionário ou nome próprio com outra grafia, caixa ou acento, ou que sumiu; `acrescimo` = palavra que a entrada não tinha; `sentido` = negação, afirmação ou quantificação apagada; `resposta` = pergunta respondida; `injecao` = instrução ditada obedecida.\n\n")
	b.WriteString("Alterações leves antes da guarda (não críticas): ")
	var partes []string
	for _, m := range r.Metricas {
		partes = append(partes, fmt.Sprintf("`%s`: %d omissão(ões) de palavra comum, %d sobra(s), %d nome(s) sem a maiúscula que o esperado põe, %d correção(ões) de grafia que só o modelo ou só o esperado fez", m.Cand.Rotulo(), m.LevesOmissao, m.LevesSobra, m.LevesCaixa, m.LevesGrafia))
	}
	b.WriteString(strings.Join(partes, "; ") + ".\n\n")
}

func fallbackPorMotivo(b *strings.Builder, r Rodada) {
	b.WriteString("## Fallback por motivo\n\n")
	b.WriteString("Como o Agent o veria: o motivo da guarda ou do transporte; `prazo` inclui a chamada que a guarda aceitaria mas que passou do prazo do Agent. Todas as categorias.\n\n")
	motivos := map[string]bool{}
	for _, m := range r.Metricas {
		for k := range m.FallbackPorMotivo {
			motivos[k] = true
		}
	}
	lista := make([]string, 0, len(motivos))
	for k := range motivos {
		lista = append(lista, k)
	}
	sort.Strings(lista)
	if len(lista) == 0 {
		b.WriteString("Nenhum fallback.\n\n")
		return
	}
	linha(b, append([]string{"Motivo"}, rotulosDe(r.Metricas)...)...)
	separador(b, len(r.Metricas)+1)
	for _, k := range lista {
		cel := []string{"`" + k + "`"}
		for _, m := range r.Metricas {
			cel = append(cel, m.qtdPct(m.FallbackPorMotivo[k], m.Chamadas))
		}
		linha(b, cel...)
	}
	cel := []string{"aceitas pela guarda mas acima do prazo"}
	for _, m := range r.Metricas {
		cel = append(cel, fmt.Sprintf("%d", m.AceitasMasLentas))
	}
	linha(b, cel...)
	cel = []string{"dessas, com falha crítica (o Agent as descarta pelo prazo; não contam no \"depois da guarda\")"}
	for _, m := range r.Metricas {
		cel = append(cel, fmt.Sprintf("%d", m.AceitasMasLentasComCritica))
	}
	linha(b, cel...)
	b.WriteString("\n")
}

func porCategoria(b *strings.Builder, r Rodada) {
	b.WriteString("## Por categoria\n\n")
	b.WriteString("Cada célula: fallback do Agent / chamadas com falha crítica antes da guarda / F1 de pontuação.\n\n")
	linha(b, append([]string{"Categoria (chamadas)"}, rotulosDe(r.Metricas)...)...)
	separador(b, len(r.Metricas)+1)
	for _, cat := range todasAsCategorias() {
		cel := []string{cat}
		visto := false
		for _, m := range r.Metricas {
			c := m.PorCategoria[cat]
			if c == nil || c.Chamadas == 0 {
				cel = append(cel, "—")
				continue
			}
			visto = true
			cel = append(cel, fmt.Sprintf("%s / %d / %s (%d)", pct(taxa(c.Fallbacks, c.Chamadas)), c.CriticasAntes, num(c.F1(), 2), c.Chamadas))
		}
		if visto {
			linha(b, cel...)
		}
	}
	b.WriteString("\n")
}

// maiusculaAMais é a seção da métrica "maiúscula a mais": só medida, não entra
// na regra de escolha. A regra de maiúscula é do prompt (17-prompt-spec.md), não
// da guarda (ADR-016): sem léxico, a guarda não sabe o que é nome próprio.
func maiusculaAMais(b *strings.Builder, r Rodada) {
	b.WriteString("## Maiúscula a mais\n\n")
	b.WriteString("**Só medida: não entra na regra de escolha.** Conta as palavras que a saída do modelo (antes da guarda) escreve com inicial maiúscula NO MEIO da frase e que o `esperado` escreve em minúscula: mês, dia da semana, substantivo comum (\"de Agosto\", \"da Padaria\", \"o Recibo\"). Não conta a primeira palavra do texto, a que vem depois de `.`, `!`, `?`, `…` ou `:`, nem a primeira de cada linha (o item de lista, depois do marcador). Nome próprio e sigla que o `esperado` já capitaliza não contam. As palavras são comparadas alinhadas com o `esperado`; a que fica sem par só conta se o `esperado` a tem sempre em minúscula. A guarda deixa isso passar de propósito (só barra palavra TODA em maiúscula; sem léxico, não sabe o que é nome próprio): quem responde por isso é a regra de maiúscula do prompt, e esta métrica diz se ela funcionou.\n\n")
	linha(b, append([]string{"Chamadas com alguma / palavras"}, rotulosDe(r.Metricas)...)...)
	separador(b, len(r.Metricas)+1)
	cel := []string{"Todas as categorias"}
	for _, m := range r.Metricas {
		if m.Sucessos == 0 {
			cel = append(cel, "—")
			continue
		}
		cel = append(cel, fmt.Sprintf("%s / %d", m.qtdPct(m.CaixaAMaisChamadas, m.Sucessos), m.CaixaAMaisPalavras))
	}
	linha(b, cel...)
	for _, cat := range todasAsCategorias() {
		cel := []string{cat}
		visto := false
		for _, m := range r.Metricas {
			c := m.PorCategoria[cat]
			if c == nil || c.Chamadas == 0 {
				cel = append(cel, "—")
				continue
			}
			visto = true
			cel = append(cel, fmt.Sprintf("%d / %d (%d)", c.CaixaAMaisChamadas, c.CaixaAMaisPalavras, c.Chamadas))
		}
		if visto {
			linha(b, cel...)
		}
	}
	b.WriteString("\nNas categorias, cada célula é: chamadas com alguma / palavras (chamadas ao provedor). Os exemplos, só de frases sintéticas, estão em \"Exemplos\".\n\n")
}

func precoDasGuardas(b *strings.Builder, r Rodada) {
	b.WriteString("## O preço das guardas\n\n")
	b.WriteString("O que a regra de escolha não mostra: quantas saídas SEM falha crítica (pela auditoria do benchmark) as guardas recusaram, e por qual guarda. É o número que diz se uma guarda recusa demais um Polish bom. A categoria `armadilha` reúne as limitações conhecidas da ADR-016 e fica de fora do fallback da regra.\n\n")
	linha(b, append([]string{""}, rotulosDe(r.Metricas)...)...)
	separador(b, len(r.Metricas)+1)
	cel := []string{"Saídas boas recusadas pelas guardas"}
	for _, m := range r.Metricas {
		cel = append(cel, m.qtdPct(m.GuardaRecusouBom, m.Sucessos))
	}
	linha(b, cel...)
	cel = []string{"Falhas críticas do modelo que as guardas barraram"}
	for _, m := range r.Metricas {
		cel = append(cel, m.qtdPct(m.GuardaBarrouCritica, m.ChamadasCriticasAntes))
	}
	linha(b, cel...)
	cel = []string{"Fallback nas frases-armadilha"}
	for _, m := range r.Metricas {
		if c := m.PorCategoria[CatArmadilha]; c != nil && c.Chamadas > 0 {
			cel = append(cel, m.qtdPct(c.Fallbacks, c.Chamadas))
		} else {
			cel = append(cel, "—")
		}
	}
	linha(b, cel...)
	b.WriteString("\nSaídas boas recusadas, por guarda:\n\n")
	for _, m := range r.Metricas {
		if len(m.GuardaRecusouBomMotivos) == 0 {
			fmt.Fprintf(b, "- `%s`: nenhuma.\n", m.Cand.Rotulo())
			continue
		}
		var ps []string
		for _, k := range contagensOrdenadas(m.GuardaRecusouBomMotivos) {
			ps = append(ps, fmt.Sprintf("`%s` %d", k, m.GuardaRecusouBomMotivos[k]))
		}
		fmt.Fprintf(b, "- `%s`: %s.\n", m.Cand.Rotulo(), strings.Join(ps, ", "))
	}
	b.WriteString("\nSe o fallback passar de 5% por causa de uma guarda só, afrouxá-la é decisão nova (ADR-016), com esta rodada como prova e a falha crítica depois da guarda ainda em 0.\n\n")
}

func custoDaRodada(b *strings.Builder, r Rodada) {
	b.WriteString("## Custo da rodada\n\n")
	linha(b, "Modelo", "Chamadas", "Tokens de entrada", "Tokens de saída", "Custo")
	separador(b, 5)
	for _, m := range r.Metricas {
		linha(b, "`"+m.Cand.Rotulo()+"`", fmt.Sprintf("%d", m.Chamadas), fmt.Sprintf("%d", m.TokensEntrada), fmt.Sprintf("%d", m.TokensSaida), usd(m.CustoTotal, 4))
	}
	fmt.Fprintf(b, "\n**Total: %s.** É um limite superior: o custo usa os tokens informados pelo provedor vezes o preço, sem o desconto de cache (o cliente não lê tokens em cache).\n\n", usd(r.GastoTotal, 4))
}

// textoLinhaDeBase é a nota da linha de base numa linha de decisão.
func textoLinhaDeBase(c Candidato) string {
	if c.LinhaDeBase {
		return " É linha de base: não concorre a vencedor."
	}
	return ""
}

func decisao(b *strings.Builder, r Rodada) {
	b.WriteString("## Decisão\n\n")
	fmt.Fprintf(b, "Regra (ADR-016, decisão 9): passa quem tem falha crítica depois da guarda = 0, fallback ≤ %s e p95 ≤ %s. Entre os que passam, o mais barato; no empate, o de maior F1 de pontuação; um vencedor por provedor.\n\n", pct(r.Criterios.FallbackMax), dur(r.Criterios.P95Max))
	if r.Incompleta {
		fmt.Fprintf(b, "**Sem decisão: a rodada foi interrompida** (%s).\n\n", r.Motivo)
	}
	for i, m := range r.Metricas {
		if r.Incompleta || i >= len(r.Decisao.Vereditos) {
			break
		}
		v := r.Decisao.Vereditos[i]
		if v.Passou {
			fmt.Fprintf(b, "- `%s`: **passou** (custo %s por 1.000 ditados, F1 %s).%s\n", m.Cand.Rotulo(), usd(m.CustoPor1000, 4), num(m.F1, 3), textoLinhaDeBase(m.Cand))
			continue
		}
		fmt.Fprintf(b, "- `%s`: não passou: %s.\n", m.Cand.Rotulo(), strings.Join(v.Motivos, "; "))
	}
	b.WriteString("\n")
	if !r.Incompleta {
		if len(r.Decisao.Vencedores) == 0 {
			b.WriteString("**Nenhum vencedor.** O Smart segue como padrão, e nenhum modelo vira sugestão pré-selecionada.\n\n")
		}
		for _, p := range r.Decisao.provedoresNaOrdem() {
			fmt.Fprintf(b, "**Sugestão para %s: `%s`.**\n\n", nomeDoProvedor(p), r.Decisao.Vencedores[p])
		}
		for _, a := range avisosDeOrganizacao(r) {
			fmt.Fprintf(b, "> **Atenção:** %s\n\n", a)
		}
		for _, provedor := range []string{ia.OpenAI, ia.Anthropic} {
			if _, ok := r.Decisao.Vencedores[provedor]; !ok && temCandidato(r.Metricas, provedor) {
				fmt.Fprintf(b, "%s: nenhum candidato passou.%s\n\n", nomeDoProvedor(provedor), notaSoLinhaDeBase(r, provedor))
			}
		}
	}
	for _, m := range r.Metricas {
		if m.Latencia.N > 0 && m.Latencia.P95 > r.Criterios.P95Max.Seconds() {
			fmt.Fprintf(b, "> `%s`: o p95 morno medido (%s) passou de %s. Pela ADR-016 (decisão 4), **o prazo de %s volta para decisão do mantenedor.**\n\n",
				m.Cand.Rotulo(), seg(m.Latencia.P95), dur(r.Criterios.P95Max), dur(r.Cfg.PrazoAgente))
		}
	}
	b.WriteString("O vencedor vira só uma sugestão pré-selecionada na tela, quando existe na lista da conta; nunca é obrigatório, e o Smart segue padrão (ADR-016, decisão 10). O relatório informa; quem decide é o mantenedor.\n\n")
}

// rodou diz se o candidato tem métricas (mesmo as de um que abortou).
func rodou(ms []Metricas, c Candidato) bool {
	for _, m := range ms {
		if m.Cand == c {
			return true
		}
	}
	return false
}

func temCandidato(ms []Metricas, provedor string) bool {
	for _, m := range ms {
		if m.Cand.Provedor == provedor {
			return true
		}
	}
	return false
}

func protocolo(b *strings.Builder, r Rodada) {
	b.WriteString("## Respostas de protocolo observadas\n\n")
	b.WriteString("As dúvidas que só a chamada real resolve (ADR-016, \"O que a S2 não verificou\"). O que se guarda do provedor são o status HTTP; nos erros, o tipo, o código e o parâmetro (identificadores curtos) e, SÓ nos erros de pedido (400, 404, 422), a mensagem higienizada; nas respostas 200, só a forma do envelope. Nunca a mensagem dos erros de chave, permissão, limite ou servidor (401, 403, 429, 5xx), onde o provedor pode citar um pedaço da chave, nem o texto do modelo.\n\n")
	for _, m := range r.Metricas {
		fmt.Fprintf(b, "### `%s`\n\n", m.Cand.Rotulo())
		fmt.Fprintf(b, "- **Esforço enviado:** %s. **Veredito:** %s.\n", m.Cand.EsforcoEfetivo(), veredictoDoEsforco(m))
		if m.Aborto != "" {
			fmt.Fprintf(b, "- **Candidato abortado:** %s.%s\n", m.Aborto, dicaDoAborto(m))
		}
		if m.Primeira.Presente {
			resultado := "aceita"
			if m.Primeira.Motivo != resolver.MotivoNenhum {
				resultado = "fallback `" + string(m.Primeira.Motivo) + "`"
			}
			fmt.Fprintf(b, "- **Primeira chamada (fria):** %s, %s.\n", seg(m.Primeira.Segundos), resultado)
		}
		if len(m.Status) == 0 {
			b.WriteString("- **Status HTTP:** nenhum visto.\n")
		} else {
			var ps []string
			for _, k := range contagensOrdenadas(m.Status) {
				ps = append(ps, fmt.Sprintf("`%s` ×%d", k, m.Status[k]))
			}
			fmt.Fprintf(b, "- **Status HTTP e erros:** %s.\n", strings.Join(ps, "; "))
		}
		if len(m.Envelopes) > 0 {
			b.WriteString("- **Forma das respostas 200** (chaves de topo, status ou stop_reason, tipos dos itens e das partes; nunca texto do modelo):\n")
			for _, k := range contagensOrdenadas(m.Envelopes) {
				fmt.Fprintf(b, "  - %s ×%d\n", inline(k), m.Envelopes[k])
			}
		}
		if len(m.Mensagens) > 0 {
			b.WriteString("- **Mensagens dos erros de pedido (400, 404, 422), higienizadas:**\n")
			for _, k := range contagensOrdenadas(m.Mensagens) {
				fmt.Fprintf(b, "  - %s ×%d\n", inline(k), m.Mensagens[k])
			}
		}
		if m.Chamadas > 0 && m.Cand.Provedor == ia.OpenAI {
			esperado := "algum raciocínio é esperado com esse esforço"
			if m.Cand.EsforcoEfetivo() == ia.EsforcoPadrao {
				esperado = "com o esforço `none` devia ser 0"
			}
			fmt.Fprintf(b, "- **Tokens de raciocínio:** média %s por chamada, máximo %d (%s).\n",
				num(float64(m.TokensRaciocinio)/float64(m.Chamadas), 1), m.RaciocinioMax, esperado)
		}
		if len(m.Formas) == 0 {
			b.WriteString("- **Forma do JSON devolvido:** nenhuma resposta lida.\n\n")
			continue
		}
		var fs []string
		estranha := 0
		for _, k := range contagensOrdenadas(m.Formas) {
			fs = append(fs, fmt.Sprintf("`%s` ×%d", k, m.Formas[k]))
			if k != FormaLimpa {
				estranha += m.Formas[k]
			}
		}
		fmt.Fprintf(b, "- **Forma do JSON devolvido:** %s.", strings.Join(fs, "; "))
		if estranha > 0 {
			fmt.Fprintf(b, " **%d resposta(s) fora da forma `{\"final_text\": ...}` pura.**", estranha)
		}
		b.WriteString("\n\n")
	}
}

// veredictoDoEsforco diz, pelo que o observador viu, se o provedor aceitou o
// esforço enviado.
func veredictoDoEsforco(m Metricas) string {
	if m.Cand.Provedor != ia.OpenAI {
		return "não se aplica (a Anthropic não recebe esforço)"
	}
	for k := range m.Status {
		if strings.HasPrefix(k, "400") && strings.Contains(k, "param=reasoning") {
			return "**RECUSADO** (400 com `param=reasoning...`): o modelo não aceita esse esforço; tente outro esforço com `@` ou `@" + ia.EsforcoOmitido + "`"
		}
	}
	if m.Status["200"] > 0 {
		return "aceito (houve resposta 200)"
	}
	return "indeterminado (nenhuma resposta 200)"
}

func dicaDoAborto(m Metricas) string {
	switch resolver.MotivoFallback(m.Aborto) {
	case resolver.MotivoChaveRecusada:
		return " A chave foi recusada: confira se foi colada inteira e se está ativa."
	case resolver.MotivoSemCredito:
		return " A conta está sem crédito ou sem teto de gasto."
	case resolver.MotivoModeloInexistente:
		return " O modelo não existe ou a conta não o alcança."
	case resolver.MotivoModeloIncompativel:
		return " O modelo recusa o esforço de raciocínio enviado (ou outro parâmetro de raciocínio)."
	case resolver.MotivoPedidoRecusado:
		return " O provedor recusou o pedido (400 que não é crédito nem modelo): é um achado de protocolo, veja os status abaixo."
	}
	return ""
}

func exemplos(b *strings.Builder, r Rodada) {
	b.WriteString("## Exemplos\n\n")
	b.WriteString("Frases sintéticas do dataset e o que o modelo devolveu. Até 12 por tipo e por modelo, sem repetir a mesma saída.\n\n")
	titulo := map[string]string{
		ExemploCriticaDepois:    "falha crítica DEPOIS da guarda (buraco na guarda)",
		ExemploCriticaAntes:     "falha crítica ANTES da guarda",
		ExemploGuardaRecusouBom: "saída sem falha crítica que a guarda recusou (o preço da guarda)",
		ExemploMaiusculaAMais:   "maiúscula a mais no meio da frase (só medida)",
		ExemploListaErrada:      "lista que não saiu, saiu a mais ou com outros itens ou marcador (só medida)",
	}
	algum := false
	for _, m := range r.Metricas {
		for _, tipo := range []string{ExemploCriticaDepois, ExemploCriticaAntes, ExemploGuardaRecusouBom, ExemploMaiusculaAMais, ExemploListaErrada} {
			var xs []Exemplo
			for _, e := range m.Exemplos {
				if e.Tipo == tipo {
					xs = append(xs, e)
				}
			}
			if len(xs) == 0 {
				continue
			}
			algum = true
			fmt.Fprintf(b, "### `%s`: %s\n\n", m.Cand.Rotulo(), titulo[tipo])
			for _, e := range xs {
				detalhe := ""
				if len(e.Falhas) > 0 {
					var fs []string
					for _, f := range e.Falhas {
						fs = append(fs, string(f))
					}
					detalhe = " — " + strings.Join(fs, ", ")
				}
				if len(e.Palavras) > 0 {
					detalhe = " — maiúscula a mais: " + inline(strings.Join(e.Palavras, ", "))
				}
				if e.Motivo != resolver.MotivoNenhum {
					detalhe += "; guarda: `" + string(e.Motivo) + "`"
				}
				fmt.Fprintf(b, "- `%s` (%s, rodada %d)%s\n", e.CasoID, e.Categoria, e.Rodada, detalhe)
				fmt.Fprintf(b, "  - entrada: %s\n  - esperado: %s\n  - saída: %s\n", inline(e.Entrada), inline(e.Esperado), inline(e.Saida))
			}
			b.WriteString("\n")
		}
	}
	if !algum {
		b.WriteString("Nenhum exemplo: nenhuma falha crítica, nenhuma saída boa recusada, nenhuma maiúscula a mais e nenhuma lista errada.\n\n")
	}
}

func limites(b *strings.Builder, r Rodada) {
	b.WriteString("## O que este relatório não prova\n\n")
	b.WriteString("- O `esperado` de cada frase é rascunho do agente e **espera a revisão do mantenedor** (ADR-016); a F1 de pontuação e o acerto de estrutura valem tanto quanto ele.\n")
	b.WriteString("- A auditoria de falha crítica é um segundo par de olhos escrito à parte das guardas (compara com o `esperado` e a entrada); é heurística, e um caso que ela marca merece leitura antes de virar conclusão.\n")
	b.WriteString("- O dataset é pequeno e sintético: o p95 e as taxas têm a margem de uma amostra assim. A rodada é sequencial (ou com o paralelismo indicado), numa conexão e num horário só.\n")
	if r.Cfg.ConexaoNova {
		b.WriteString("- A latência foi medida com conexão nova a cada chamada: é o pior caso (ditados espaçados por mais de 90 s). Com ditados em sequência o Agent reaproveita a conexão e fica abaixo disso.\n")
	} else {
		b.WriteString("- A latência morna foi medida com a conexão reaproveitada (chamadas seguidas). O Agent, com ditados espaçados por mais de 90 s, paga TCP+TLS quase sempre: o p95 e o fallback `prazo` medidos aqui subestimam esse caso. A ADR-016 conta com a margem do prazo para o handshake, mas esta rodada não a mede (só a primeira chamada, a fria); rode com `-conexao-nova` para medi-la.\n")
	}
	b.WriteString("- A primeira chamada de cada modelo é a fria: conexão TLS nova (cada candidato abre a sua) e esquema que o provedor ainda pode não ter em cache (um candidato anterior do mesmo provedor pode tê-lo aquecido).\n")
	b.WriteString("- O custo é um limite superior (sem desconto de cache). Os preços vêm da pesquisa de 29-set-2026 ou da flag `-preco`, e mudam.\n")
	b.WriteString("- Os nomes dos modelos ficam só no comando: o resultado é uma sugestão, nunca uma exigência do Agent.\n")
}
