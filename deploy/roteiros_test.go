package deploy

import (
	"regexp"
	"strings"
	"testing"
)

var regexpRecebido = regexp.MustCompile(`(?m)^RECEBIDO="([^"]+)"$`)

// Os roteiros da Parte 2 (docs/deploy-f11.md e os outros que o dono da nuvem
// segue na VPS): os comandos prontos para colar têm de dizer o mesmo que os scripts e o
// compose. Estes testes leem o texto; nada roda.

const roteiroDeploy = "docs/deploy-f11.md"

// Os arquivos com comandos do compose para a VPS.
var arquivosComComandosDoCompose = []string{
	roteiroDeploy, "deploy/README.md", "docs/teste-final-f11.md", "docs/backup-b2-f11.md", composeYml,
	subirSh, restaurarSh, "docs/restauracao-f11.md",
}

// Todo "docker compose ... -f <compose>" leva o --project-name: um
// COMPOSE_PROJECT_NAME exportado na sessão vale mais que o name: do arquivo, e
// o "stop" do roteiro procuraria outro projeto sem parar nada.
func TestRoteiros_ComposeSempreComOProjeto(t *testing.T) {
	achou := 0
	for _, arq := range arquivosComComandosDoCompose {
		for i, l := range strings.Split(ler(t, arq), "\n") {
			if !strings.Contains(l, "docker compose ") || !strings.Contains(l, " -f ") {
				continue
			}
			achou++
			if !strings.Contains(l, "--project-name aurora-whisper ") && !strings.Contains(l, `--project-name "$PROJETO"`) &&
				!strings.Contains(l, "--project-name $PROJETO ") {
				t.Errorf("%s, linha %d: docker compose sem o --project-name: %s", arq, i+1, strings.TrimSpace(l))
			}
		}
	}
	if achou < 4 {
		t.Errorf("achei só %d comandos do compose nos roteiros: o teste não está olhando o lugar certo", achou)
	}
}

// trecho devolve o texto entre o começo e o fim (o fim não incluído).
func trecho(t *testing.T, nome, texto, comeco, fim string) string {
	t.Helper()
	i := strings.Index(texto, comeco)
	if i < 0 {
		t.Fatalf("%s não tem %q", nome, comeco)
	}
	resto := texto[i:]
	if fim == "" {
		return resto
	}
	j := strings.Index(resto, fim)
	if j < 0 {
		t.Fatalf("%s não tem %q depois de %q", nome, fim, comeco)
	}
	return resto[:j]
}

// secaoDoRoteiro devolve a seção "## N." ou "### N." do roteiro do deploy,
// até o próximo título.
func secaoDoRoteiro(t *testing.T, numero string) string {
	t.Helper()
	texto := ler(t, roteiroDeploy)
	m := regexp.MustCompile(`(?m)^#{2,3} ` + regexp.QuoteMeta(numero) + `\. .*$`).FindStringIndex(texto)
	if m == nil {
		t.Fatalf("o %s não tem o passo %s", roteiroDeploy, numero)
	}
	resto := texto[m[1]:]
	if f := regexp.MustCompile(`(?m)^#{2,3} `).FindStringIndex(resto); f != nil {
		resto = resto[:f[0]]
	}
	return resto
}

// O roteiro do B2 aponta para o passo certo do roteiro do deploy: o passo
// citado ao lado de um script é o que roda esse script.
func TestRoteiros_OsPassosCitadosPeloB2(t *testing.T) {
	texto := strings.Join(strings.Fields(ler(t, "docs/backup-b2-f11.md")), " ")
	refs := regexp.MustCompile("`([a-z0-9-]+\\.sh)`[^`]*?\\(o passo ([0-9.]+) do `docs/deploy-f11\\.md`\\)").FindAllStringSubmatch(texto, -1)
	if len(refs) < 2 {
		t.Fatalf("achei %d referências a um passo do roteiro do deploy no do B2", len(refs))
	}
	for _, r := range refs {
		if !strings.Contains(secaoDoRoteiro(t, r[2]), "deploy/scripts/"+r[1]) {
			t.Errorf("o docs/backup-b2-f11.md manda o %s para o passo %s do roteiro do deploy, que não o roda", r[1], r[2])
		}
	}
}

// A prova do log de acesso desligado (passo 8) conta só as linhas do log de
// acesso no formato comum do Traefik (o de --accesslog=true sem format), e
// não as do log do próprio Traefik, que saem no mesmo stdout e citam o router
// (o ERR do certificado traz routerName=...).
func TestRoteiros_AContagemDoLogDeAcesso(t *testing.T) {
	passo8 := secaoDoRoteiro(t, "8")
	exigirContem(t, "o passo 8", passo8, `docker inspect traefik --format '{{json .Args}}'`,
		"--accesslog.filepath", "--accesslog.format")
	padroes := regexp.MustCompile(`grep -cE '([^']+)'`).FindAllStringSubmatch(passo8, -1)
	if len(padroes) != 2 {
		t.Fatalf("quero as duas contagens com grep -cE no passo 8, achei %d", len(padroes))
	}
	nosso, todos := regexp.MustCompile(padroes[0][1]), regexp.MustCompile(padroes[1][1])
	const (
		acessoNosso = `203.0.113.7 - - [09/Oct/2026:12:00:00 +0000] "GET /auth/google/callback?code=x&state=y HTTP/2.0" 302 0 "-" "Mozilla/5.0" 1234 "aurora-whisper-api@docker" "http://172.18.0.9:8080" 3ms`
		acessoOutro = `203.0.113.8 - - [09/Oct/2026:12:00:01 +0000] "GET / HTTP/2.0" 200 512 "-" "Mozilla/5.0" 1235 "outro-site@docker" "http://172.19.0.3:3000" 12ms`
		semRouter   = `203.0.113.9 - - [09/Oct/2026:12:00:02 +0000] "GET / HTTP/2.0" 404 19 "-" "curl/8.9" 1236 "-" "-" 0ms`
		erroDoACME  = `2026-10-09T12:00:03Z ERR Unable to obtain ACME certificate for domains error="desafio falhou" ` +
			`providerName=letsencrypt.acme routerName=aurora-whisper-api@docker rule=Host(` + "`aurora-whisper.mjj.net.br`" + `)`
		erroAspas = `2026-10-09T12:00:04Z ERR erro routerName="aurora-whisper-api@docker"`
	)
	for _, c := range []struct {
		linha                string
		querNosso, querTodos bool
	}{
		{acessoNosso, true, true},
		{acessoOutro, false, true},
		{semRouter, false, false},
		{erroDoACME, false, false},
		{erroAspas, false, false},
	} {
		if nosso.MatchString(c.linha) != c.querNosso || todos.MatchString(c.linha) != c.querTodos {
			t.Errorf("as contagens do passo 8 erram nesta linha (nosso %v, todos %v): %s", c.querNosso, c.querTodos, c.linha)
		}
	}
	exigirNaoContem(t, "o passo 8", passo8, `grep -c "aurora-whisper-api@docker"`, `grep -c "@docker"`)
}

// O digest da distroless vem do registro: a imagem de base de um build do
// BuildKit não entra na lista de imagens, e o docker image inspect dela falha.
func TestRoteiros_ODigestDaDistroless(t *testing.T) {
	passo4 := secaoDoRoteiro(t, "4")
	exigirContem(t, "o passo 4", passo4, "docker buildx imagetools inspect gcr.io/distroless/static-debian12:nonroot")
	exigirNaoContem(t, "o passo 4", passo4, "docker image inspect gcr.io/distroless")
	if !strings.Contains(ler(t, "deploy/Dockerfile"), "gcr.io/distroless/static-debian12:nonroot") {
		t.Error("o Dockerfile não usa mais a distroless static-debian12:nonroot que o roteiro cita")
	}
}

const roteiroRestauracao = "docs/restauracao-f11.md"

// O roteiro da restauração na VPS (o restaurar.sh e a ADR contam com ele): a
// decifra no PC pelo cmd, ligada ao receber; a sessão ssh aberta antes; a API
// e o backup parados; o aplicar; o subir.sh; os avisos da ADR antes do
// RESTAURAR; e o DROP dos 7 dias.
func TestRoteiros_ARestauracao(t *testing.T) {
	texto := ler(t, roteiroRestauracao)
	mensal := ler(t, "scripts/teste-restauracao-mensal.ps1")
	chave := regexp.MustCompile(`\$NomeDaChave = '([^']+)'`).FindStringSubmatch(mensal)
	pasta := regexp.MustCompile(`\(Join-Path \$env:USERPROFILE '([^']+)'\)`).FindStringSubmatch(mensal)
	if chave == nil || pasta == nil {
		t.Fatal("não achei o nome e a pasta da chave no teste-restauracao-mensal.ps1")
	}
	imagem := constanteDoScript(t, restaurarSh, "IMAGEM_BACKUP")
	decifra := `cmd /d /c "docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges` +
		` -v %USERPROFILE%\` + pasta[1] + `:/chave:ro` +
		` -v %USERPROFILE%\Downloads\aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age:/entrada/backup.dump.age:ro ` + imagem +
		` age --decrypt --identity /chave/` + chave[1] + ` /entrada/backup.dump.age` +
		` | ssh vps bash ~/aurora-whisper/deploy/scripts/restaurar.sh receber"`
	exigirContem(t, roteiroRestauracao, texto, decifra)
	// Na ordem: a sessão aberta, a API e o backup parados, a decifra, o
	// aplicar, o subir.sh e o DROP.
	ultimo := -1
	for _, passo := range []string{
		"```powershell\nssh vps\n```",
		"stop aurora-whisper-api aurora-whisper-backup\n",
		decifra,
		"bash ~/aurora-whisper/deploy/scripts/restaurar.sh aplicar\n",
		"bash ~/aurora-whisper/deploy/scripts/subir.sh\n",
		"psql -U postgres -d postgres -c 'DROP DATABASE aurora_antes_AAAAMMDD'",
	} {
		i := strings.Index(texto, passo)
		if i < 0 || i <= ultimo {
			t.Fatalf("o %s não tem, ou não na ordem: %q", roteiroRestauracao, passo)
		}
		ultimo = i
	}
	exigirContem(t, roteiroRestauracao, texto, "restaurar.sh descartar", "--sem-copia-antes", "`RESTAURAR`",
		"docker exec aurora-whisper-backup backup.sh",
		// os avisos da ADR antes da aprovação: a revogação em massa, a
		// ressurreição, o selo e os 7 dias
		"Todas as sessões e todos os aparelhos são revogados", "O que foi apagado depois do backup pode voltar",
		"Pode aparecer um selo")
	exigirContem(t, roteiroRestauracao, strings.Join(strings.Fields(texto), " "), "vira `aurora_antes_AAAAMMDD` por **7 dias**")
	// O aplicar mostra os mesmos avisos antes do RESTAURAR, e o DROP que ele
	// imprime é o do roteiro.
	r := ler(t, restaurarSh)
	exigirContem(t, restaurarSh, r, "TODAS as sessoes e TODOS os aparelhos sao revogados", "o que foi apagado depois do backup pode voltar",
		"pode aparecer um selo", `printf '  docker exec -it %s psql -U %s -d postgres -c '"'"'DROP DATABASE %s'"'"'\n'`,
		"docs/restauracao-f11.md")
	if constanteDoScript(t, restaurarSh, "CONTEINER_POSTGRES") != "aurora-whisper-postgres" {
		t.Error("o DROP do roteiro usa o contêiner aurora-whisper-postgres")
	}
	// Os outros documentos apontam para ele.
	for _, d := range []string{roteiroDeploy, "deploy/README.md"} {
		exigirContem(t, d, ler(t, d), roteiroRestauracao)
	}
}

// Uma imagem nova vai sem o JSON do Google (senão uma cópia do client secret
// ia para recebido/ a cada deploy), e a troca do client vai pelo subir.sh,
// que confere, pede o SIM e recria a API: nada de pôr o arquivo à mão em
// segredos/ (o compose não recriaria a API, e o 0444 obriga a um mv que o
// contêiner em execução não vê).
func TestRoteiros_ImagemNovaETrocaDoGoogle(t *testing.T) {
	texto := ler(t, roteiroDeploy)
	imagem := trecho(t, roteiroDeploy, texto, "**Uma imagem nova, depois:**", "**Trocar o client do Google depois**")
	exigirContem(t, "a imagem nova", imagem,
		`scripts\enviar-para-servidor.ps1 -Dominio aurora-whisper.mjj.net.br`+"\n")
	exigirNaoContem(t, "a imagem nova", imagem, "-ArquivoGoogle $")
	troca := trecho(t, roteiroDeploy, texto, "**Trocar o client do Google depois**", "## O que me contar")
	recebido := strings.TrimPrefix(regexpRecebido.FindStringSubmatch(ler(t, subirSh))[1], "$BASE/")
	exigirContem(t, "a troca do client", troca, " vps:aurora-whisper/"+recebido,
		"bash ~/aurora-whisper/deploy/scripts/subir.sh", "mkdir -p aurora-whisper/recebido")
	exigirNaoContem(t, "a troca do client", troca, "chmod 0444", "segredos/google-client.json (com")
	// As frases que o roteiro manda esperar são as do subir.sh.
	exigirContem(t, subirSh, ler(t, subirSh), `ok "um client do Google NOVO em recebido/, conferido`,
		`o client do Google e TROCADO`, `ok "o client do Google trocado`)
}
