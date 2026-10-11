package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Os scripts da VPS (F11f) e a imagem do backup. Primeiro o que o texto
// promete; depois o bash de verdade com o docker, o psql, o pg_dump, o
// pg_restore, o age, o rclone e o flock de mentira.

const (
	gerarSegredos = "deploy/scripts/gerar-segredos.sh"
	backupSh      = "deploy/scripts/backup.sh"
	restaurarSh   = "deploy/scripts/restaurar.sh"
	conferirB2    = "deploy/scripts/conferir-b2.sh"
	agendarBackup = "deploy/scripts/agendar-backup.sh"
	subirSh       = "deploy/scripts/subir.sh"
	backupImagem  = "deploy/backup/Dockerfile"
)

var scriptsDaVPS = []string{gerarSegredos, backupSh, restaurarSh, conferirB2, agendarBackup, subirSh}

// curlDoSubir é o único curl que um script da VPS roda: o subir.sh confere o
// https pelo Traefik (um GET no health, com o certificado conferido).
var curlDoSubir = regexp.MustCompile(`^(if command -v curl > /dev/null 2>&1; then|codigo="\$\(curl --silent --output /dev/null --write-out '%\{http_code\}' --max-time 10 "https://\$dominio/api/v1/health/live" 2> /dev/null \|\| true\)")$`)

// --- o texto ----------------------------------------------------------------

func TestVPS_ScriptsSoASCIIELFComOsAjustesDoBash(t *testing.T) {
	for _, s := range scriptsDaVPS {
		texto := ler(t, s)
		exigirASCII(t, s, texto)
		exigirLF(t, s, texto)
		if !strings.HasPrefix(texto, "#!/usr/bin/env bash\n") {
			t.Errorf("%s deveria começar com #!/usr/bin/env bash", s)
		}
		c := codigo(texto)
		exigirContem(t, s, c, "set -euo pipefail\n", "umask 077\n")
		// Sem eco de comando (set -x imprimiria os valores), sem programa
		// por caminho fixo (os testes trocam os programas pelo PATH), sem
		// sudo, sem rede além do que cada um faz, sem o socket do Docker. O
		// texto que o script só imprime (printf) pode citar o ssh do PC.
		var executado strings.Builder
		for _, l := range linhasDeCodigo(texto) {
			if s == subirSh && curlDoSubir.MatchString(l.texto) {
				continue
			}
			if !strings.HasPrefix(l.texto, "printf ") {
				executado.WriteString(l.texto + "\n")
			}
		}
		exigirNaoContem(t, s, executado.String(), "set -x", "set -o xtrace", "/usr/bin/", "/bin/docker", "sudo ",
			"curl ", "wget ", "ssh ", "scp ", "docker.sock", "--privileged", "--network host", "eval ")
		if regexp.MustCompile(`(?m)^\s*echo `).MatchString(c) {
			t.Errorf("%s usa echo: os scripts imprimem com printf (o echo interpreta o texto)", s)
		}
	}
}

func TestVPS_GerarSegredosNoTexto(t *testing.T) {
	c := codigo(ler(t, gerarSegredos))
	exigirContem(t, gerarSegredos, c,
		"od -An -vtx1 -N32 /dev/urandom", // 32 bytes, em hexadecimal
		`chmod 0444 "$tmp"/*`, `chmod 0700 "$tmp"`, `chmod 0600 "$tmpconfig"`,
		`if [ -e "$SEGREDOS" ] || [ -e "$CONFIG" ]; then`, // recusa sobrescrever
		`mv -T -- "$tmp" "$SEGREDOS"`,
		"read -r -s -p", // a application key do B2 sem eco
		// A keyID, a application key e a chave pública sem eco: colada no
		// lugar errado, a recusa vem antes de aparecer na tela.
		"perguntar_sem_eco keyid ", "perguntar_sem_eco appkey ", "perguntar_sem_eco chave_publica ",
		// Toda recusa descarta o que sobrou na entrada (o resto de um colado
		// de várias linhas iria para o bash da VPS).
		"while IFS= read -r -s -t 0.2 -n 4096 _; do :; done",
		"falhou() {\ndescartar_entrada\n",
		"HOST_DO_POSTGRES='aurora-whisper-postgres'",
		`postgres://$PAPEL:$senha_aurora@$HOST_DO_POSTGRES:5432/$BANCO?sslmode=disable`,
		"PUBLIC_BASE_URL=https://%s", "DOMINIO=%s", "CONTAS_PERMITIDAS=%s", "B2_BUCKET=%s",
		// O JSON do Google: conferido sem imprimir (grep -q) e movido para
		// segredos/ junto dos outros, só no fim.
		`RECEBIDO="$BASE/recebido/google-client.json"`,
		`grep -Fq "\"https://$dominio/auth/google/callback\"" "$RECEBIDO"`,
		`cp -- "$RECEBIDO" "$tmp/google-client.json"`, `rm -f -- "$RECEBIDO"`,
	)
	exigirNaoContem(t, gerarSegredos, c, `cat "$RECEBIDO"`, `cat -- "$RECEBIDO"`)
	exigirNaoContem(t, gerarSegredos, c, "perguntar keyid ", "perguntar appkey ", "perguntar chave_publica ")
	// Nenhum segredo vai para a linha de comando de outro programa: todos são
	// gravados pelo printf do próprio bash.
	for _, l := range linhasDeCodigo(ler(t, gerarSegredos)) {
		for _, v := range []string{"$senha_superusuario", "$senha_aurora", "$senha_backup", "$session_secret", "$pepper", "$appkey"} {
			if !strings.Contains(l.texto, v) {
				continue
			}
			ok := strings.HasPrefix(l.texto, "gravar ") || strings.HasPrefix(l.texto, "printf ") ||
				strings.HasPrefix(l.texto, "if [ ") || strings.HasPrefix(l.texto, "[[ ") ||
				strings.Contains(l.texto, `="$(aleatorio)"`)
			if !ok {
				t.Errorf("linha %d usa %s fora do printf do bash: %s", l.n, v, l.texto)
			}
		}
	}
}

func TestVPS_BackupNoTexto(t *testing.T) {
	c := codigo(ler(t, backupSh))
	exigirContem(t, backupSh, c,
		"pg_dump --format=custom --no-owner --no-privileges",
		`| tee "$fifo"`,
		`age --encrypt --recipients-file "$CHAVE_PUBLICA" --output "$parcial"`,
		`estados=("${PIPESTATUS[@]}")`,
		"pg_restore --list",
		`[ "${estados[0]}" -eq 0 ]`, `[ "${estados[1]}" -eq 0 ]`, `[ "${estados[2]}" -eq 0 ]`,
		`mv -- "$parcial" "$PASTA/$nome"`,
		"rclone copyto --no-check-dest",
		`rclone lsf --format sp --files-only "$pasta_remota"`,
		"flock -n 9",
		"IFS= read -r senha",
		// A senha só no ambiente do pg_dump: nem o tee, o age ou o rclone a
		// herdam.
		`PGPASSWORD="$senha" pg_dump --format=custom`,
		"unset PGPASSWORD",
		// O papel só de leitura, com a senha dele.
		`export PGUSER="${PGUSER:-aurora_backup}"`,
		`SENHA_FILE="${AW_SENHA_FILE:-/run/secrets/postgres-backup-senha}"`,
	)
	exigirNaoContem(t, backupSh, c, "export PGPASSWORD", "read -r PGPASSWORD")
	// O servidor não apaga nada no B2 (a chave nem pode): nada de delete,
	// purge, sync, move nem cleanup.
	for _, ruim := range []string{"rclone delete", "rclone deletefile", "rclone purge", "rclone sync", "rclone move", "rclone cleanup", "hard-delete", "hard_delete"} {
		if strings.Contains(c, ruim) {
			t.Errorf("o backup.sh tem %q", ruim)
		}
	}
	// Nenhum dump em claro no disco: o pg_dump só sai por cano.
	for _, l := range linhasDeCodigo(ler(t, backupSh)) {
		if strings.Contains(l.texto, "pg_dump --format") && !strings.HasSuffix(l.texto, `2> /dev/null \`) {
			t.Errorf("linha %d: o pg_dump tem de seguir por cano: %s", l.n, l.texto)
		}
		if strings.Contains(l.texto, "pg_dump") && regexp.MustCompile(`(^|[^2])>\s*"?\$`).MatchString(l.texto) {
			t.Errorf("linha %d: saída do pg_dump para arquivo: %s", l.n, l.texto)
		}
	}
	// A hora do último sucesso só depois do envio conferido.
	envio := strings.Index(c, `enviar "$PASTA/$nome" ||`)
	sucesso := strings.Index(c, ".ultimo-sucesso.novo")
	if envio < 0 || sucesso < 0 || sucesso < envio {
		t.Error("o .ultimo-sucesso tem de ser gravado depois do envio ao B2")
	}
	// A senha nunca na linha de comando: nada de -W, --password nem URL.
	exigirNaoContem(t, backupSh, c, "--password", "postgres://", "PGPASSWORD=\"$(")
}

func TestVPS_RestaurarNoTexto(t *testing.T) {
	texto := ler(t, restaurarSh)
	c := codigo(texto)
	exigirContem(t, restaurarSh, c,
		"--exit-on-error", "--single-transaction",
		`CREATE DATABASE $novo OWNER $PAPEL`,
		`GRANT CONNECT ON DATABASE $novo TO $PAPEL_BACKUP`, "PAPEL_BACKUP='aurora_backup'",
		`ALTER DATABASE $BANCO RENAME TO $antigo`, `ALTER DATABASE $novo RENAME TO $BANCO`,
		// O migrate e o pos-restauracao no banco novo, pela URL dele.
		`api_descartavel "$url_novo" migrate -status`, `api_descartavel "$url_novo" migrate ||`,
		`api_descartavel "$url_novo" pos-restauracao -confirmo`,
		`[ "$resposta" != RESTAURAR ]`, `[ "$resposta" != SIM ]`,
		// A memória só do usuário (nunca o /dev/shm), com prazo.
		`RAIZ_DA_MEMORIA="${AW_MEMORIA:-${XDG_RUNTIME_DIR:-/run/user/$(id -u)}}"`, `mkdir -m 0700 -- "$MEMORIA"`,
		"IDADE_MAXIMA_MIN=120",
		// A leitura inteira do dump no receber.
		`pg_restore -f /dev/null < "$MEMORIA/dump"`,
		// A API e o backup em qualquer estado vivo, e ninguém conectado.
		`if vivo "$CONTEINER_API"; then`, `if vivo "$CONTEINER_BACKUP"; then`,
		`docker ps -q --filter "name=^/?$1\$")`,
		"pg_stat_activity WHERE datname = '$BANCO' AND backend_type = 'client backend'",
		"DIAS_DO_BANCO_ANTIGO=7",
		"--network none", // a cifra do banco atual não precisa de rede
	)
	exigirNaoContem(t, restaurarSh, c, "--clean", "--if-exists", "dropdb", "pg_restore -c ", "/dev/shm")
	// A troca de nomes é o último passo: depois do pos-restauracao.
	pos := strings.Index(c, `api_descartavel "$url_novo" pos-restauracao -confirmo`)
	troca := strings.Index(c, `ALTER DATABASE $BANCO RENAME TO $antigo`)
	if pos < 0 || troca < 0 || troca < pos {
		t.Error("a troca de nomes tem de vir depois do pos-restauracao")
	}
	// O DROP só aparece no texto que o script IMPRIME para o Mauro.
	for _, l := range linhasDeCodigo(texto) {
		if regexp.MustCompile(`(?i)drop\s+(database|table|schema|role|owned)|dropdb`).MatchString(l.texto) && !strings.HasPrefix(l.texto, "printf ") {
			t.Errorf("linha %d: DROP fora do printf (o script nunca apaga banco): %s", l.n, l.texto)
		}
	}
	// Toda restauração de verdade (pg_restore sem --list) para no primeiro erro.
	for _, l := range linhasDeCodigo(texto) {
		if strings.Contains(l.texto, "pg_restore -U") && !strings.Contains(c, "--exit-on-error --single-transaction") {
			t.Errorf("linha %d: pg_restore sem --exit-on-error --single-transaction", l.n)
		}
	}
}

// Os nomes que o compose da Parte 2 vai adotar: os mesmos nos três scripts e no
// conferir-postgres.sh.
func TestVPS_NomesDosConteineres(t *testing.T) {
	exigirContem(t, gerarSegredos, ler(t, gerarSegredos), "'aurora-whisper-postgres'")
	exigirContem(t, backupSh, ler(t, backupSh), `PGHOST="${PGHOST:-aurora-whisper-postgres}"`)
	exigirContem(t, restaurarSh, ler(t, restaurarSh),
		"CONTEINER_POSTGRES='aurora-whisper-postgres'", "CONTEINER_API='aurora-whisper-api'",
		"CONTEINER_BACKUP='aurora-whisper-backup'", "REDE_INTERNA='aurora-whisper-interna'")
	exigirContem(t, conferirPostgre, ler(t, conferirPostgre), "CONTEINER_POSTGRES='aurora-whisper-postgres'")
}

func TestVPS_ImagemDoBackup(t *testing.T) {
	texto := ler(t, backupImagem)
	exigirASCII(t, backupImagem, texto)
	exigirLF(t, backupImagem, texto)
	goVer, _, _ := versoesDoRepositorio(t)
	es := estagios(instrucoes(texto))
	if len(es) != 3 {
		t.Fatalf("a imagem do backup deveria ter 3 estágios (age, rclone, final), tem %d", len(es))
	}
	if want := "golang:" + goVer + " AS age"; es[0][0].args != want {
		t.Errorf("o age se compila com o Go do repositório: quero %q, é %q", want, es[0][0].args)
	}
	if !regexp.MustCompile(`^rclone/rclone:\d+\.\d+\.\d+ AS rclone$`).MatchString(es[1][0].args) {
		t.Errorf("o rclone em versão exata: %q", es[1][0].args)
	}
	if !regexp.MustCompile(`^postgres:18\.\d+-alpine$`).MatchString(es[2][0].args) {
		t.Errorf("a final é o Postgres 18 (o mesmo major do servidor) em versão exata: %q", es[2][0].args)
	}
	exigirContem(t, backupImagem, texto,
		"go install -trimpath filippo.io/age/cmd/age@v", "go install -trimpath filippo.io/age/cmd/age-keygen@v",
		"COPY backup.sh /usr/local/bin/backup.sh", "COPY agendar-backup.sh /usr/local/bin/agendar-backup.sh",
		"chmod 0555 /usr/local/bin/backup.sh /usr/local/bin/agendar-backup.sh", "USER postgres", "command -v flock",
		"docker build -f deploy/backup/Dockerfile -t aurora-whisper-backup:producao deploy/scripts")
	if m := regexp.MustCompile(`age(?:-keygen)?@(v\d+\.\d+\.\d+)`).FindAllStringSubmatch(texto, -1); len(m) != 2 || m[0][1] != m[1][1] {
		t.Errorf("o age e o age-keygen na mesma versão exata: %q", m)
	}
	for _, i := range instrucoes(texto) {
		if i.cmd == "ADD" || i.cmd == "ENV" && regexp.MustCompile(`(?i)(KEY|SECRET|PASSWORD|TOKEN)`).MatchString(i.args) {
			t.Errorf("instrução proibida: %s %s", i.cmd, i.args)
		}
		if i.cmd == "RUN" && regexp.MustCompile(`curl|wget|apk add|apt-get`).MatchString(i.args) {
			t.Errorf("a imagem do backup não baixa nada além do go install: %s", i.args)
		}
	}
	if strings.Contains(texto, "# syntax=") || strings.Contains(texto, ":latest") {
		t.Error("versões que andam na imagem do backup")
	}
	ign := ler(t, "deploy/backup/Dockerfile.dockerignore")
	var regras []string
	for _, l := range strings.Split(ign, "\n") {
		if s := strings.TrimSpace(l); s != "" && !strings.HasPrefix(s, "#") {
			regras = append(regras, s)
		}
	}
	if strings.Join(regras, " ") != "* !backup.sh !agendar-backup.sh" {
		t.Errorf("o contexto da imagem do backup é só o backup.sh e o agendar-backup.sh: %q", regras)
	}
}

// --- gerar-segredos.sh com programas de mentira -----------------------------

// O teste roda uma CÓPIA do script numa árvore de mentira (projeto/deploy/
// scripts/), para que uma falha na recusa da pasta nunca grave nada dentro do
// repositório de verdade.
func copiaDoGerar(t *testing.T, a *ambienteBash) string {
	t.Helper()
	dir := filepath.Join(a.dir, "projeto", "deploy", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "gerar-segredos.sh")
	if err := os.WriteFile(p, []byte(ler(t, gerarSegredos)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// jsonDoGoogleDeTeste é o JSON de um client Web de mentira, com o redirect
// de produção do domínio das respostas de teste e o do PC. O "secret" é
// montado por partes, para o teste poder procurá-lo na saída.
func jsonDoGoogleDeTeste(dominio string) string {
	return `{"web":{"client_id":"123-abc.apps.googleusercontent.com","project_id":"aurora-teste",` +
		`"client_secret":"` + segredoDoClientDeTeste + `","redirect_uris":["https://` + dominio +
		`/auth/google/callback","http://localhost:8080/auth/google/callback"]}}` + "\n"
}

var segredoDoClientDeTeste = "nao-" + "e-segredo-" + strings.Repeat("x", 12)

// baseDoGerar aponta o AW_BASE para a pasta da VPS de mentira e põe nela o
// JSON do Google que o enviar-para-servidor.ps1 manda antes (recebido/).
func baseDoGerar(t *testing.T, a *ambienteBash) string {
	t.Helper()
	base := filepath.Join(a.dir, "vps", "aurora-whisper")
	a.env["AW_BASE"] = posix(base)
	if err := os.MkdirAll(filepath.Join(base, "recebido"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "recebido", "google-client.json"),
		[]byte(jsonDoGoogleDeTeste("aurora.exemplo.com.br")), 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

var (
	chavePublicaDeTeste = "age1" + strings.Repeat("q", 58)
	keyIDDeTeste        = "005" + strings.Repeat("0", 10) + "abcdef123456"
	// Montada por partes: o arquivo de teste não pode parecer uma chave.
	appKeyDeTeste = "K00" + "5" + strings.Repeat("Ab", 13) + "c"
)

func respostasDoGerar(trocas map[int]string) []byte {
	r := []string{"aurora.exemplo.com.br", "mauro@exemplo.com", "bucket-de-teste", keyIDDeTeste, appKeyDeTeste, chavePublicaDeTeste}
	for i, v := range trocas {
		r[i] = v
	}
	return []byte(strings.Join(r, "\n") + "\n")
}

func lerTodos(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entradas {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(data)
	}
	return out
}

func TestVPS_GerarSegredosGravaSemMostrar(t *testing.T) {
	a := novoAmbienteBash(t)
	script := copiaDoGerar(t, a)
	base := baseDoGerar(t, a)
	r := a.rodar(script, respostasDoGerar(nil))
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	seg := lerTodos(t, filepath.Join(base, "segredos"))
	nomes := []string{"backup-chave-publica", "database-url", "device-token-pepper", "google-client.json", "postgres-aurora-senha", "postgres-backup-senha", "postgres-superusuario-senha", "rclone.conf", "session-secret"}
	if len(seg) != len(nomes) {
		t.Fatalf("arquivos em segredos/: %v", seg)
	}
	// O JSON do Google foi movido de recebido/ para segredos/, igual.
	if seg["google-client.json"] != jsonDoGoogleDeTeste("aurora.exemplo.com.br") {
		t.Error("o google-client.json em segredos/ não é o recebido")
	}
	if _, err := os.Stat(filepath.Join(base, "recebido")); err == nil {
		t.Error("a pasta recebido/ ficou (o JSON de entrada tem de sair)")
	}
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}\n$`)
	for _, n := range []string{"postgres-superusuario-senha", "postgres-aurora-senha", "postgres-backup-senha", "session-secret", "device-token-pepper"} {
		if !hex64.MatchString(seg[n]) {
			t.Errorf("%s deveria ser uma linha de 64 hexadecimais", n)
		}
	}
	vistos := map[string]bool{}
	for _, n := range []string{"postgres-superusuario-senha", "postgres-aurora-senha", "postgres-backup-senha", "session-secret", "device-token-pepper"} {
		if vistos[seg[n]] {
			t.Errorf("%s repete outro segredo", n)
		}
		vistos[seg[n]] = true
	}
	senha := strings.TrimSpace(seg["postgres-aurora-senha"])
	if want := "postgres://aurora:" + senha + "@aurora-whisper-postgres:5432/aurora?sslmode=disable\n"; seg["database-url"] != want {
		t.Errorf("database-url não é a do papel aurora com a senha dele")
	}
	if strings.Contains(seg["database-url"], strings.TrimSpace(seg["postgres-superusuario-senha"])) {
		t.Error("a DATABASE_URL da API leva a senha do superusuário")
	}
	if seg["backup-chave-publica"] != chavePublicaDeTeste+"\n" {
		t.Errorf("chave pública: %q", seg["backup-chave-publica"])
	}
	if seg["rclone.conf"] != "[b2]\ntype = b2\naccount = "+keyIDDeTeste+"\nkey = "+appKeyDeTeste+"\n" {
		t.Errorf("rclone.conf inesperado")
	}
	config, err := os.ReadFile(filepath.Join(base, "config.env"))
	if err != nil {
		t.Fatal(err)
	}
	exigirContem(t, "config.env", string(config), "PUBLIC_BASE_URL=https://aurora.exemplo.com.br\n",
		"DOMINIO=aurora.exemplo.com.br\n", "CONTAS_PERMITIDAS=mauro@exemplo.com\n", "B2_BUCKET=bucket-de-teste\n")
	// Nada de valor na tela nem nas chamadas.
	saida := r.tudo() + strings.Join(a.chamadas(), "\n")
	if strings.Contains(saida, segredoDoClientDeTeste) || strings.Contains(saida, "client_id") {
		t.Error("o JSON do Google aparece na saída")
	}
	for n, v := range seg {
		if n == "backup-chave-publica" || n == "google-client.json" {
			continue
		}
		for _, linha := range strings.Split(strings.TrimSpace(v), "\n") {
			if len(linha) > 20 && strings.Contains(saida, strings.TrimPrefix(strings.TrimPrefix(linha, "account = "), "key = ")) {
				t.Errorf("o valor de %s aparece na saída", n)
			}
		}
	}
	if strings.Contains(saida, appKeyDeTeste) || strings.Contains(saida, senha) {
		t.Error("segredo na saída")
	}
	exigirContem(t, "a saída", r.saida, "OK: segredos em", "OK: configuracao em")
	// Nada de pasta temporária esquecida.
	sobras, _ := filepath.Glob(filepath.Join(base, ".*"))
	if len(sobras) > 0 {
		t.Errorf("sobrou: %v", sobras)
	}
	// Os modos só valem no Linux (o NTFS do Windows não tem 0444 de verdade).
	if runtime.GOOS != "windows" {
		modo := func(p string) os.FileMode {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			return info.Mode().Perm()
		}
		if m := modo(filepath.Join(base, "segredos")); m != 0o700 {
			t.Errorf("segredos/ tem modo %o, quero 700", m)
		}
		for _, n := range nomes {
			if m := modo(filepath.Join(base, "segredos", n)); m != 0o444 {
				t.Errorf("%s tem modo %o, quero 444", n, m)
			}
		}
		if m := modo(filepath.Join(base, "config.env")); m != 0o600 {
			t.Errorf("config.env tem modo %o, quero 600", m)
		}
	}

	// De novo: recusa, e nada muda.
	r2 := a.rodar(script, respostasDoGerar(nil))
	if r2.codigo == 0 || !strings.Contains(r2.erro, "FALHOU: ja existe") {
		t.Fatalf("a segunda vez deveria recusar: %d %q", r2.codigo, r2.tudo())
	}
	depois := lerTodos(t, filepath.Join(base, "segredos"))
	for n, v := range seg {
		if depois[n] != v {
			t.Errorf("a segunda vez mudou %s", n)
		}
	}
}

func TestVPS_GerarSegredosRecusaRespostaRuimSemGravarNada(t *testing.T) {
	for _, c := range []struct {
		nome string
		i    int
		v    string
	}{
		{"domínio com https", 0, "https://aurora.exemplo.com.br"},
		{"domínio vazio", 0, ""},
		{"domínio com maiúscula", 0, "Aurora.exemplo.com.br"},
		{"dois e-mails", 1, "a@exemplo.com,b@exemplo.com"},
		{"e-mail sem arroba", 1, "mauro.exemplo.com"},
		{"bucket curto", 2, "b2"},
		{"keyID com espaço", 3, "005 abc def 123"},
		{"keyID da master key (12 hexadecimais)", 3, "0123456789ab"},
		{"application key no lugar da keyID", 3, appKeyDeTeste},
		{"application key curta", 4, "K005abc"},
		{"keyID no lugar da application key", 4, keyIDDeTeste},
		{"chave PRIVADA no lugar da pública", 5, "AGE-SECRET-KEY-1" + strings.Repeat("Q", 58)},
		{"chave pública curta", 5, "age1qqqq"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a := novoAmbienteBash(t)
			script := copiaDoGerar(t, a)
			base := baseDoGerar(t, a)
			r := a.rodar(script, respostasDoGerar(map[int]string{c.i: c.v}))
			if r.codigo == 0 || !strings.Contains(r.erro, "FALHOU") {
				t.Fatalf("deveria recusar: %d %q", r.codigo, r.tudo())
			}
			if c.v != "" && len(c.v) > 8 && strings.Contains(r.tudo(), c.v) && c.i >= 3 {
				t.Errorf("a resposta recusada aparece na saída")
			}
			for _, p := range []string{"segredos", "config.env"} {
				if _, err := os.Stat(filepath.Join(base, p)); err == nil {
					t.Errorf("gravou %s mesmo recusando", p)
				}
			}
			if sobras, _ := filepath.Glob(filepath.Join(base, ".*")); len(sobras) > 0 {
				t.Errorf("sobrou: %v", sobras)
			}
		})
	}
}

// O JSON do Google que o enviar-para-servidor.ps1 mandou: sem ele, ou sem o
// redirect de produção, ou de outro tipo, ou grande demais, nada é gravado, o
// recebido fica onde estava e nada dele aparece na saída.
func TestVPS_GerarSegredosConfereOJSONDoGoogle(t *testing.T) {
	for _, c := range []struct {
		nome, json, msg string
		semArquivo      bool
	}{
		{"sem o JSON", "", "nao achei o JSON do client do Google", true},
		{"o redirect de outro domínio", jsonDoGoogleDeTeste("outro.exemplo.com.br"), "nao tem o redirect https://aurora.exemplo.com.br/auth/google/callback", false},
		{"só o redirect do PC", strings.Replace(jsonDoGoogleDeTeste("aurora.exemplo.com.br"), "https://aurora.exemplo.com.br/auth", "http://localhost:9/auth", 1), "nao tem o redirect", false},
		{"o client de computador", `{"installed":{"client_id":"x","client_secret":"` + segredoDoClientDeTeste + `","redirect_uris":["https://aurora.exemplo.com.br/auth/google/callback"]}}`, "nao e o JSON de um client do tipo Aplicativo da Web", false},
		{"grande demais", jsonDoGoogleDeTeste("aurora.exemplo.com.br") + strings.Repeat(" ", 70000), "grande demais", false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a := novoAmbienteBash(t)
			script := copiaDoGerar(t, a)
			base := baseDoGerar(t, a)
			recebido := filepath.Join(base, "recebido", "google-client.json")
			if c.semArquivo {
				if err := os.Remove(recebido); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(recebido, []byte(c.json), 0o600); err != nil {
				t.Fatal(err)
			}
			r := a.rodar(script, respostasDoGerar(nil))
			if r.codigo == 0 || !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, c.msg) {
				t.Fatalf("deveria recusar com %q: %d %q", c.msg, r.codigo, r.tudo())
			}
			if strings.Contains(r.tudo(), segredoDoClientDeTeste) {
				t.Error("o JSON aparece na saída")
			}
			for _, p := range []string{"segredos", "config.env"} {
				if _, err := os.Stat(filepath.Join(base, p)); err == nil {
					t.Errorf("gravou %s mesmo recusando", p)
				}
			}
			if !c.semArquivo {
				if data, err := os.ReadFile(recebido); err != nil || string(data) != c.json {
					t.Error("o recebido mudou ou sumiu na recusa")
				}
			}
		})
	}
}

// O arquivo .key inteiro colado na pergunta da chave pública (as três linhas
// que o Bloco de Notas mostra: "# created", "# public key" e a PRIVADA). O
// read pega só a primeira; sem o descarte, as outras duas ficariam na
// entrada e o bash da VPS as executaria depois do script (e as gravaria no
// histórico). Aqui o "bash da VPS" é um cat que lê o que sobrou na mesma
// entrada.
func TestVPS_GerarSegredosDescartaOQueSobraNaEntrada(t *testing.T) {
	privada := "AGE-SECRET-KEY-1" + strings.Repeat("Q", 58)
	arquivoKey := "# created: 2026-10-09T12:00:00Z\n# public key: " + chavePublicaDeTeste + "\n" + privada
	for _, c := range []struct {
		nome    string
		trocas  map[int]string
		semFimd bool // a última linha colada sem o fim de linha
	}{
		{"o .key inteiro na chave pública", map[int]string{5: arquivoKey}, false},
		{"o .key inteiro, sem o fim de linha", map[int]string{5: arquivoKey}, true},
		{"duas linhas na keyID", map[int]string{3: appKeyDeTeste + "\n" + appKeyDeTeste}, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a := novoAmbienteBash(t)
			script := copiaDoGerar(t, a)
			base := baseDoGerar(t, a)
			embrulho := filepath.Join(a.dir, "bash-da-vps.sh")
			if err := os.WriteFile(embrulho, []byte("bash \"$1\"; rc=$?\nprintf 'RESTO['; cat; printf ']\\n'\nexit \"$rc\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			entrada := respostasDoGerar(c.trocas)
			if c.semFimd {
				entrada = entrada[:len(entrada)-1]
			}
			r := a.rodar(embrulho, entrada, posix(script))
			if r.codigo == 0 || !strings.Contains(r.erro, "FALHOU") {
				t.Fatalf("deveria recusar: %d %q", r.codigo, r.tudo())
			}
			if !strings.Contains(r.saida, "RESTO[]") {
				t.Errorf("sobrou entrada para o bash da VPS: %q", r.saida)
			}
			if strings.Contains(r.tudo(), privada) || strings.Contains(r.tudo(), appKeyDeTeste) {
				t.Error("o que foi colado aparece na saída")
			}
			for _, p := range []string{"segredos", "config.env"} {
				if _, err := os.Stat(filepath.Join(base, p)); err == nil {
					t.Errorf("gravou %s mesmo recusando", p)
				}
			}
		})
	}
	t.Run("a chave privada tem a mensagem dela", func(t *testing.T) {
		a := novoAmbienteBash(t)
		script := copiaDoGerar(t, a)
		baseDoGerar(t, a)
		r := a.rodar(script, respostasDoGerar(map[int]string{5: arquivoKey}))
		if r.codigo == 0 || !strings.Contains(r.erro, "isso e a chave PRIVADA") {
			t.Fatalf("quero a mensagem da chave privada: %d %q", r.codigo, r.tudo())
		}
	})
}

// Uma falha no meio da gravação (aqui, o chmod dos arquivos) não deixa nada:
// nem a pasta temporária, nem segredos/ pela metade.
func TestVPS_GerarSegredosFalhaNoMeioNaoDeixaSobra(t *testing.T) {
	a := novoAmbienteBash(t)
	script := copiaDoGerar(t, a)
	base := baseDoGerar(t, a)
	// O chmod de mentira falha no 0444 e repassa o resto ao de verdade, que
	// está nas ferramentas (depois dos falsos no PATH).
	a.falso("chmod", `if [ "$1" = 0444 ]; then exit 1; fi
exec "$(dirname "$0")/../ferramentas-reais/chmod" "$@"`)
	if err := os.MkdirAll(filepath.Join(a.dir, "ferramentas-reais"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dir, "ferramentas-reais", "chmod"),
		[]byte("#!/usr/bin/env bash\nPATH=\"${PATH#*:}\" exec chmod \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := a.rodar(script, respostasDoGerar(nil))
	if r.codigo == 0 {
		t.Fatalf("deveria falhar com o chmod quebrado: %q", r.tudo())
	}
	if !a.chamou("chmod [0444]") {
		t.Fatal("o chmod de mentira não foi chamado: o teste não valeu")
	}
	for _, p := range []string{"segredos", "config.env"} {
		if _, err := os.Stat(filepath.Join(base, p)); err == nil {
			t.Errorf("ficou %s depois da falha", p)
		}
	}
	if sobras, _ := filepath.Glob(filepath.Join(base, ".*")); len(sobras) > 0 {
		t.Errorf("sobrou: %v", sobras)
	}
}

func TestVPS_GerarSegredosRecusaPastaNoRepositorio(t *testing.T) {
	t.Run("dentro do deploy", func(t *testing.T) {
		a := novoAmbienteBash(t)
		script := copiaDoGerar(t, a)
		base := filepath.Join(a.dir, "projeto", "deploy", "segredos-aqui")
		a.env["AW_BASE"] = posix(base)
		r := a.rodar(script, respostasDoGerar(nil))
		if r.codigo == 0 || !strings.Contains(r.erro, "nao pode ficar dentro de") {
			t.Fatalf("deveria recusar a pasta dentro do deploy: %d %q", r.codigo, r.tudo())
		}
		if _, err := os.Stat(base); err == nil {
			t.Error("criou a pasta mesmo recusando")
		}
	})
	t.Run("dentro de um repositório git", func(t *testing.T) {
		a := novoAmbienteBash(t)
		script := copiaDoGerar(t, a)
		repo := filepath.Join(a.dir, "outro-repo")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		base := filepath.Join(repo, "config", "aurora")
		a.env["AW_BASE"] = posix(base)
		r := a.rodar(script, respostasDoGerar(nil))
		if r.codigo == 0 || !strings.Contains(r.erro, "repositorio git") {
			t.Fatalf("deveria recusar a pasta dentro de um repositório: %d %q", r.codigo, r.tudo())
		}
		if _, err := os.Stat(base); err == nil {
			t.Error("criou a pasta mesmo recusando")
		}
	})
}

// --- backup.sh com programas de mentira -------------------------------------

const pgDumpDeMentira = `
case "${FALSO_DUMP:-ok}" in
  falha) printf 'pg_dump: error: connection to server failed\n' >&2; exit 1 ;;
  vazio) exit 0 ;;
esac
if [ -n "${PGPASSWORD:-}" ]; then printf 'pg_dump recebeu a senha pelo ambiente\n' >> "$REGISTRO"; fi
printf 'pg_dump como %s\n' "${PGUSER:-}" >> "$REGISTRO"
printf 'PGDMP-FALSO\n'
if [ "${FALSO_DUMP:-ok}" = pequeno ]; then printf 'FIM-DO-DUMP\n'; exit 0; fi
i=0
while [ "$i" -lt 4000 ]; do
  printf 'linha %06d do dump de mentira, com um texto ditado de mentira\n' "$i"
  i=$((i + 1))
done
if [ "${FALSO_DUMP:-ok}" = meio ]; then exit 1; fi
printf 'FIM-DO-DUMP\n'
`

// O pg_restore --list de mentira lê só o cabeçalho e o índice e para, sem
// ler o resto, como o de verdade.
const listaDeMentira = `
IFS= read -r cab || exit 1
if [ "${FALSO_LISTA_DEVAGAR:-}" = 1 ]; then sleep 1; fi
if [ "$cab" != PGDMP-FALSO ]; then
  printf 'pg_restore: error: input file does not appear to be a valid archive\n' >&2
  exit 1
fi
if [ "${FALSO_LISTA_FALHA:-}" = 1 ]; then
  printf 'pg_restore: error: could not read the table of contents\n' >&2
  exit 1
fi
printf ';\n; Archive created by a test\n;\n'
printf '215; 1259 16385 TABLE public schema_migrations aurora\n'
if [ "${FALSO_SEM_USERS:-}" != 1 ]; then printf '216; 1259 16390 TABLE public users aurora\n'; fi
printf '217; 1259 16400 TABLE public transcriptions aurora\n'
exit 0
`

const pgRestoreDeMentira = `
for a in "$@"; do
  if [ "$a" = --list ]; then
` + listaDeMentira + `
  fi
done
cat > /dev/null
`

// herdouASenha anota no registro o programa de mentira que recebeu a senha
// do banco pelo ambiente: só o pg_dump pode.
const herdouASenha = `if [ -n "${PGPASSWORD:-}" ]; then printf 'herdou a senha do banco: %s\n' "${0##*/}" >> "$REGISTRO"; fi
`

const ageDeMentira = `
saida=''; destinatarios=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output|-o) saida="$2"; shift 2 ;;
    --recipients-file|-R) destinatarios="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ "${FALSO_AGE_FALHA:-}" = 1 ]; then exit 1; fi
grep -q '^age1' "$destinatarios" || exit 1
if [ -n "$saida" ]; then
  { printf 'age-encryption.org/v1\n'; cat; } > "$saida"
else
  printf 'age-encryption.org/v1\n'; cat
fi
`

const rcloneDeMentira = `
[ -r "${RCLONE_CONFIG:-}" ] || { printf 'rclone: sem config\n' >&2; exit 1; }
case "$1" in
  copyto)
    if [ "${FALSO_RCLONE_FALHA:-}" = 1 ]; then exit 1; fi
    # Como o B2 de verdade com a chave sem readFiles: a pergunta "ja existe?"
    # (sem --no-check-dest) e um HEAD de download, e da 401.
    case " $* " in *" --no-check-dest "*) ;; *) printf 'failed to HEAD for download: Unknown 401\n' >&2; exit 1 ;; esac
    origem="${@: -2:1}"; destino="${@: -1}"
    case "$destino" in b2:*) ;; *) exit 1 ;; esac
    if [ -n "${FALSO_RCLONE_FALHA_PARA:-}" ]; then
      case "$destino" in *"$FALSO_RCLONE_FALHA_PARA"*) exit 1 ;; esac
    fi
    mkdir -p "$FALSO_REMOTO"
    cp "$origem" "$FALSO_REMOTO/${destino##*/}"
    ;;
  lsf)
    destino="${@: -1}"
    # O lsf de um arquivo so e um HEAD (401 com esta chave); a listagem da
    # pasta funciona, no formato do --format sp ("tamanho;nome").
    case "$destino" in */) ;; *) printf 'failed to HEAD for download: Unknown 401\n' >&2; exit 1 ;; esac
    [ -d "$FALSO_REMOTO" ] || exit 0
    for f in "$FALSO_REMOTO"/*; do
      [ -f "$f" ] || continue
      if [ "${FALSO_RCLONE_TAMANHO_ERRADO:-}" = 1 ]; then printf '1;%s\n' "${f##*/}"; else printf '%s;%s\n' "$(stat -c %s "$f")" "${f##*/}"; fi
    done
    ;;
  *) printf 'rclone de mentira: so copyto e lsf\n' >&2; exit 99 ;;
esac
`

type backupDeTeste struct {
	a      *ambienteBash
	pasta  string
	remoto string
	senha  string
}

func novoBackup(t *testing.T) *backupDeTeste {
	t.Helper()
	a := novoAmbienteBash(t)
	b := &backupDeTeste{a: a, pasta: filepath.Join(a.dir, "backup"), remoto: filepath.Join(a.dir, "remoto"),
		senha: strings.Repeat("5e", 32)}
	if err := os.MkdirAll(b.pasta, 0o700); err != nil {
		t.Fatal(err)
	}
	seg := filepath.Join(a.dir, "segredos")
	if err := os.MkdirAll(seg, 0o700); err != nil {
		t.Fatal(err)
	}
	for n, v := range map[string]string{
		"postgres-backup-senha": b.senha + "\n",
		"backup-chave-publica":  chavePublicaDeTeste + "\n",
		"rclone.conf":           "[b2]\ntype = b2\naccount = x\nkey = y\n",
	} {
		if err := os.WriteFile(filepath.Join(seg, n), []byte(v), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	a.env["AW_PASTA_BACKUP"] = posix(b.pasta)
	a.env["AW_SENHA_FILE"] = posix(filepath.Join(seg, "postgres-backup-senha"))
	a.env["AW_CHAVE_PUBLICA"] = posix(filepath.Join(seg, "backup-chave-publica"))
	a.env["RCLONE_CONFIG"] = posix(filepath.Join(seg, "rclone.conf"))
	a.env["B2_BUCKET"] = "bucket-de-teste"
	a.env["FALSO_REMOTO"] = posix(b.remoto)
	a.falso("pg_dump", pgDumpDeMentira)
	a.falso("pg_restore", herdouASenha+pgRestoreDeMentira)
	a.falso("age", herdouASenha+ageDeMentira)
	a.falso("rclone", herdouASenha+rcloneDeMentira)
	a.falso("flock", herdouASenha+`if [ "${FALSO_FLOCK_OCUPADO:-}" = 1 ]; then exit 1; fi`)
	return b
}

func (b *backupDeTeste) rodar() resultado {
	return b.a.rodar(backupSh, nil)
}

// arquivos devolve os nomes na pasta do backup.
func (b *backupDeTeste) arquivos(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(b.pasta)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// semDumpEmClaro: nenhum arquivo da pasta do backup tem o dump sem a cifra.
func (b *backupDeTeste) semDumpEmClaro(t *testing.T) {
	t.Helper()
	_ = filepath.WalkDir(b.pasta, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "PGDMP-FALSO") && !strings.HasPrefix(string(data), "age-encryption.org/v1\n") {
			t.Errorf("dump em claro no disco: %s", p)
		}
		return nil
	})
}

func TestVPS_BackupCaminhoFeliz(t *testing.T) {
	b := novoBackup(t)
	r := b.rodar()
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	linha := regexp.MustCompile(`^backup: OK \((aurora-whisper-\d{8}T\d{6}Z\.dump\.age), \d+ bytes, \d+ s\)\n$`).FindStringSubmatch(r.saida)
	if linha == nil {
		t.Fatalf("uma linha de log só, com o OK: %q", r.tudo())
	}
	nome := linha[1]
	local, err := os.ReadFile(filepath.Join(b.pasta, nome))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(local), "age-encryption.org/v1\nPGDMP-FALSO\n") || !strings.HasSuffix(string(local), "FIM-DO-DUMP\n") {
		t.Error("o cifrado do volume não é o dump inteiro passado pelo age")
	}
	remoto, err := os.ReadFile(filepath.Join(b.remoto, nome))
	if err != nil || string(remoto) != string(local) {
		t.Fatalf("o que subiu não é o que ficou: %v", err)
	}
	for _, f := range []string{nome + ".enviado", ".ultimo-sucesso"} {
		if _, err := os.Stat(filepath.Join(b.pasta, f)); err != nil {
			t.Errorf("faltou %s", f)
		}
	}
	for _, f := range b.arquivos(t) {
		if strings.HasSuffix(f, ".partial") || strings.HasPrefix(f, ".trabalho") {
			t.Errorf("sobrou %s", f)
		}
	}
	b.semDumpEmClaro(t)
	ch := b.a.chamadas()
	if !b.a.chamou("pg_dump recebeu a senha pelo ambiente") {
		t.Error("o pg_dump não recebeu a senha pelo ambiente")
	}
	if !b.a.chamou("pg_dump como aurora_backup") {
		t.Error("o pg_dump deveria entrar pelo papel só de leitura, aurora_backup")
	}
	if b.a.chamou("herdou a senha do banco") {
		t.Errorf("outro programa além do pg_dump recebeu a senha do banco: %q", ch)
	}
	todas := strings.Join(ch, "\n") + r.tudo()
	if strings.Contains(todas, b.senha) {
		t.Error("a senha aparece numa linha de comando ou na saída")
	}
	if b.a.indice("rclone [copyto] [--no-check-dest]") < 0 || b.a.indice("[b2:bucket-de-teste/aurora-whisper/"+nome+"]") < 0 {
		t.Errorf("o envio deveria ser rclone copyto --no-check-dest para b2:bucket/aurora-whisper/<nome>: %q", ch)
	}
}

// Um dump pequeno cabe inteiro no cano: o cifrado termina antes de o
// pg_restore --list (aqui, devagar) terminar de ler, e o script tem de
// esperar por ele.
func TestVPS_BackupDumpPequeno(t *testing.T) {
	b := novoBackup(t)
	b.a.env["FALSO_DUMP"] = "pequeno"
	b.a.env["FALSO_LISTA_DEVAGAR"] = "1"
	r := b.rodar()
	if r.codigo != 0 || !strings.HasPrefix(r.saida, "backup: OK (") {
		t.Fatalf("o dump pequeno deveria passar: %d %q", r.codigo, r.tudo())
	}
}

// Se o processo morre no meio da cifra (kill -9: o trap não roda), o que
// fica é um .partial, que nunca sobe; um arquivo com o nome final pela
// metade subiria na vez seguinte como "pendente".
func TestVPS_BackupMortoNoMeioNaoSobeOPedaco(t *testing.T) {
	b := novoBackup(t)
	b.a.falso("age", `
saida=''
while [ "$#" -gt 0 ]; do
  case "$1" in --output|-o) saida="$2"; shift 2 ;; *) shift ;; esac
done
if [ "${FALSO_AGE_MATA:-}" = 1 ]; then
  printf 'age-encryption.org/v1\npedaco\n' > "$saida"
  kill -9 "$PPID"
  exit 1
fi
{ printf 'age-encryption.org/v1\n'; cat; } > "$saida"
`)
	b.a.env["FALSO_AGE_MATA"] = "1"
	r := b.rodar()
	if r.codigo == 0 {
		t.Fatalf("o backup morto deveria sair com erro: %q", r.tudo())
	}
	time.Sleep(1100 * time.Millisecond) // o nome leva o segundo
	delete(b.a.env, "FALSO_AGE_MATA")
	r = b.rodar()
	if r.codigo != 0 {
		t.Fatalf("a vez seguinte deveria passar: %d %q", r.codigo, r.tudo())
	}
	subiram, _ := filepath.Glob(filepath.Join(b.remoto, "*"))
	if len(subiram) != 1 {
		t.Fatalf("deveria ter subido só o backup inteiro: %v", subiram)
	}
	if data, _ := os.ReadFile(subiram[0]); strings.Contains(string(data), "pedaco") {
		t.Error("o pedaço do backup morto subiu ao B2")
	}
}

func TestVPS_BackupQueFalhaNaoSobeNada(t *testing.T) {
	for _, c := range []struct {
		nome, env, valor, quero string
	}{
		{"dump que falha no meio", "FALSO_DUMP", "meio", "o pg_dump falhou"},
		{"dump que falha no começo", "FALSO_DUMP", "falha", "o pg_dump falhou"},
		{"dump vazio", "FALSO_DUMP", "vazio", "o pg_restore --list nao leu"},
		// O dump de mentira (uns 250 KB) é maior que o cano: se o grupo do
		// pg_restore --list saísse sem drenar, o tee morreria de SIGPIPE e a
		// causa no log seria o tee.
		{"pg_restore --list que falha depois do cabeçalho", "FALSO_LISTA_FALHA", "1", "o pg_restore --list nao leu"},
		{"banco errado", "FALSO_SEM_USERS", "1", "nao tem a tabela users"},
		{"age que falha", "FALSO_AGE_FALHA", "1", "o age falhou"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			b := novoBackup(t)
			b.a.env[c.env] = c.valor
			r := b.rodar()
			if r.codigo == 0 || !strings.Contains(r.erro, "backup: FALHOU (") || !strings.Contains(r.erro, c.quero) {
				t.Fatalf("quero FALHOU (%s...): %d %q", c.quero, r.codigo, r.tudo())
			}
			// Uma linha de log só: nada de "No such file" de um .rc que não
			// foi gravado.
			if strings.Count(strings.TrimSpace(r.erro), "\n") > 0 {
				t.Errorf("o log deveria ter uma linha só: %q", r.erro)
			}
			if b.a.chamou("rclone") {
				t.Error("chamou o rclone com o dump que falhou")
			}
			for _, f := range b.arquivos(t) {
				if strings.HasSuffix(f, ".dump.age") || strings.HasSuffix(f, ".partial") || f == ".ultimo-sucesso" || strings.HasPrefix(f, ".trabalho") {
					t.Errorf("sobrou %s", f)
				}
			}
			if _, err := os.Stat(b.remoto); err == nil {
				t.Error("algo subiu ao B2")
			}
		})
	}
}

func TestVPS_BackupRecusaAntesDoDump(t *testing.T) {
	for _, c := range []struct {
		nome  string
		mexer func(b *backupDeTeste)
		quero string
	}{
		{"sem bucket", func(b *backupDeTeste) { delete(b.a.env, "B2_BUCKET") }, "B2_BUCKET nao esta definida"},
		{"chave privada no lugar da pública", func(b *backupDeTeste) {
			p := filepath.Join(b.a.dir, "chave-errada")
			_ = os.WriteFile(p, []byte("AGE-SECRET-KEY-1"+strings.Repeat("Q", 58)+"\n"), 0o444)
			b.a.env["AW_CHAVE_PUBLICA"] = posix(p)
		}, "a chave publica nao e uma chave do age"},
		{"outro backup rodando", func(b *backupDeTeste) { b.a.env["FALSO_FLOCK_OCUPADO"] = "1" }, "outro backup esta rodando"},
		{"senha vazia", func(b *backupDeTeste) {
			p := filepath.Join(b.a.dir, "senha-vazia")
			_ = os.WriteFile(p, nil, 0o444)
			b.a.env["AW_SENHA_FILE"] = posix(p)
		}, "o arquivo da senha esta vazio"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			b := novoBackup(t)
			c.mexer(b)
			r := b.rodar()
			if r.codigo == 0 || !strings.Contains(r.erro, c.quero) {
				t.Fatalf("quero %q: %d %q", c.quero, r.codigo, r.tudo())
			}
			if b.a.chamou("pg_dump") || b.a.chamou("rclone") {
				t.Error("chegou ao dump ou ao envio")
			}
		})
	}
}

func TestVPS_BackupComOB2ForaGuardaEReenvia(t *testing.T) {
	b := novoBackup(t)
	b.a.env["FALSO_RCLONE_FALHA"] = "1"
	r := b.rodar()
	if r.codigo == 0 || !strings.Contains(r.erro, "o envio ao B2 falhou") {
		t.Fatalf("quero FALHOU do envio: %d %q", r.codigo, r.tudo())
	}
	var primeiro string
	for _, f := range b.arquivos(t) {
		if strings.HasSuffix(f, ".dump.age") {
			primeiro = f
		}
		if strings.HasSuffix(f, ".enviado") || f == ".ultimo-sucesso" {
			t.Errorf("com o B2 fora, não pode haver %s", f)
		}
	}
	if primeiro == "" {
		t.Fatal("o cifrado não ficou no volume para reenviar")
	}
	b.semDumpEmClaro(t)

	// Na vez seguinte, com o B2 de volta, sobem os dois.
	time.Sleep(1100 * time.Millisecond) // o nome leva o segundo
	delete(b.a.env, "FALSO_RCLONE_FALHA")
	r = b.rodar()
	if r.codigo != 0 || strings.Contains(r.saida, "sem subir") {
		t.Fatalf("a segunda vez deveria subir tudo: %d %q", r.codigo, r.tudo())
	}
	if _, err := os.Stat(filepath.Join(b.remoto, primeiro)); err != nil {
		t.Error("o backup que tinha ficado não subiu na vez seguinte")
	}
	if _, err := os.Stat(filepath.Join(b.pasta, primeiro+".enviado")); err != nil {
		t.Error("o reenviado não ficou marcado")
	}
}

func TestVPS_BackupGuardaTresDiasDoQueSubiu(t *testing.T) {
	b := novoBackup(t)
	velho := time.Now().Add(-4 * 24 * time.Hour)
	recente := time.Now().Add(-1 * time.Hour)
	criar := func(nome string, quando time.Time) {
		p := filepath.Join(b.pasta, nome)
		if err := os.WriteFile(p, []byte("age-encryption.org/v1\nx"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, quando, quando); err != nil {
			t.Fatal(err)
		}
	}
	criar("aurora-whisper-20000101T000000Z.dump.age", velho) // subiu há 4 dias: sai
	criar("aurora-whisper-20000101T000000Z.dump.age.enviado", velho)
	criar("aurora-whisper-20000102T000000Z.dump.age", recente) // subiu há 1 h: fica
	criar("aurora-whisper-20000102T000000Z.dump.age.enviado", recente)
	criar("aurora-whisper-20000103T000000Z.dump.age.partial", velho) // sobra de uma queda: sai
	trabalhoVelho := filepath.Join(b.pasta, ".trabalho.velho")       // a pasta de trabalho de uma queda: sai
	if err := os.MkdirAll(trabalhoVelho, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(trabalhoVelho, velho, velho); err != nil {
		t.Fatal(err)
	}
	r := b.rodar()
	if r.codigo != 0 {
		t.Fatalf("falhou: %d %q", r.codigo, r.tudo())
	}
	tem := map[string]bool{}
	for _, f := range b.arquivos(t) {
		tem[f] = true
	}
	if tem["aurora-whisper-20000101T000000Z.dump.age"] || tem["aurora-whisper-20000101T000000Z.dump.age.enviado"] {
		t.Error("o que subiu há mais de 3 dias deveria sair do volume")
	}
	if !tem["aurora-whisper-20000102T000000Z.dump.age"] {
		t.Error("o que subiu há 1 h deveria ficar")
	}
	if tem["aurora-whisper-20000103T000000Z.dump.age.partial"] {
		t.Error("o .partial velho deveria sair")
	}
	if tem[".trabalho.velho"] {
		t.Error("a pasta de trabalho velha deveria sair")
	}
}

// O antigo que não sobe de novo (o B2 recusa só ele) continua no volume, por
// mais velho que seja, e o log conta; o de hoje sobe e vale.
func TestVPS_BackupNuncaApagaODoVolumeQueNaoSubiu(t *testing.T) {
	b := novoBackup(t)
	velho := time.Now().Add(-10 * 24 * time.Hour)
	p := filepath.Join(b.pasta, "aurora-whisper-20000101T000000Z.dump.age")
	if err := os.WriteFile(p, []byte("age-encryption.org/v1\nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, velho, velho); err != nil {
		t.Fatal(err)
	}
	b.a.env["FALSO_RCLONE_FALHA_PARA"] = "20000101T000000Z"
	r := b.rodar()
	if r.codigo != 0 || !strings.Contains(r.saida, "1 antigo(s) ainda sem subir") {
		t.Fatalf("quero o OK com o antigo pendente: %d %q", r.codigo, r.tudo())
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("um cifrado que nunca subiu foi apagado do volume")
	}
	if _, err := os.Stat(filepath.Join(b.pasta, ".ultimo-sucesso")); err != nil {
		t.Error("o de hoje subiu: o .ultimo-sucesso tinha de ser gravado")
	}
}

// O rclone diz que subiu, mas o tamanho lá não bate: não vale.
func TestVPS_BackupConfereOTamanhoDoQueSubiu(t *testing.T) {
	b := novoBackup(t)
	b.a.env["FALSO_RCLONE_TAMANHO_ERRADO"] = "1"
	r := b.rodar()
	if r.codigo == 0 || !strings.Contains(r.erro, "o envio ao B2 falhou") {
		t.Fatalf("quero FALHOU com o tamanho errado: %d %q", r.codigo, r.tudo())
	}
	for _, f := range b.arquivos(t) {
		if strings.HasSuffix(f, ".enviado") || f == ".ultimo-sucesso" {
			t.Errorf("com o tamanho errado, não pode haver %s", f)
		}
	}
}

// bash -n em todo script do deploy (e no diagnóstico): só a sintaxe, nada roda.
func TestVPS_BashN(t *testing.T) {
	a := novoAmbienteBash(t)
	scripts, err := filepath.Glob(caminho("deploy", "*", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	mais, _ := filepath.Glob(caminho("deploy", "*", "*", "*.sh"))
	scripts = append(append(scripts, mais...), caminho("scripts", "diagnostico-servidor.sh"))
	if len(scripts) < 6 {
		t.Fatalf("só %d scripts: %v", len(scripts), scripts)
	}
	for _, s := range scripts {
		abs, err := filepath.Abs(s)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := a.sintaxe(abs); err != nil {
			t.Errorf("bash -n %s: %v\n%s", s, err, out)
		}
	}
}

// --- restaurar.sh com programas de mentira ----------------------------------

// O docker de mentira da restauração. O ps imita o de verdade: sem o filtro
// de status, mostra também o que está reiniciando (FALSO_REINICIANDO); com
// status=running, só o que está de pé (FALSO_RODANDO). E exige o filtro de
// nome ^/?nome$ (o nome do contêiner, para o Docker, começa com /).
const dockerDaRestauracao = `
sub="$1"; shift
case "$sub" in
  ps)
    nome=''; so_rodando=0
    for a in "$@"; do
      case "$a" in
        'name=^/?'*'$') nome="${a#'name=^/?'}"; nome="${nome%'$'}" ;;
        name=*) printf 'docker de mentira: filtro de nome sem ^/?...$: %s\n' "$a" >&2; exit 97 ;;
        status=running) so_rodando=1 ;;
      esac
    done
    vivos="${FALSO_RODANDO:-}"
    if [ "$so_rodando" = 0 ]; then vivos="$vivos ${FALSO_REINICIANDO:-}"; fi
    for r in $vivos; do
      if [ "$r" = "$nome" ]; then printf 'c0ffee\n'; fi
    done
    exit 0
    ;;
  exec)
    # O docker exec -i de verdade repassa a entrada ao conteiner.
    interativo=0
    if [ "$1" = -i ]; then interativo=1; shift; fi
    if [ "$1" != aurora-whisper-postgres ]; then printf 'docker de mentira: conteiner %s\n' "$1" >&2; exit 97; fi
    shift
    prog="$1"; shift
    case "$prog" in
      pg_restore)
        for a in "$@"; do
          if [ "$a" = -f ]; then
            # A leitura inteira: o dump de mentira termina em FIM-DO-DUMP.
            ultima=''
            while IFS= read -r l; do ultima="$l"; done
            if [ "$ultima" != FIM-DO-DUMP ]; then
              printf 'pg_restore: error: could not read from input file: end of file\n' >&2
              exit 1
            fi
            exit 0
          fi
        done
        for a in "$@"; do
          if [ "$a" = --list ]; then
` + listaDeMentira + `
          fi
        done
        if [ "${FALSO_RESTORE_FALHA:-}" = 1 ]; then cat > /dev/null; exit 1; fi
        cat > "$REGISTRO.restaurado"
        ;;
      pg_dump)
        if [ "${FALSO_DUMP:-ok}" = falha ]; then exit 1; fi
        printf 'PGDMP-FALSO\nbanco atual\n'
        ;;
      psql)
        if [ "$interativo" = 1 ]; then cat > /dev/null; fi
        sql="${@: -1}"
        case "$sql" in
          "SELECT 1 FROM pg_database WHERE datname = '"*)
            n="${sql#*datname = \'}"; n="${n%\'}"
            for b in ${FALSO_BANCOS:-}; do
              if [ "$b" = "$n" ]; then printf '1\n'; fi
            done
            ;;
          "SELECT count(*) FROM pg_stat_activity WHERE datname = 'aurora' AND backend_type = 'client backend'")
            printf '%s\n' "${FALSO_CONEXOES:-0}" ;;
          "CREATE DATABASE "*|"REVOKE "*|"GRANT CONNECT ON DATABASE "*) ;;
          "ALTER DATABASE aurora_restaurado_"*) if [ "${FALSO_RENAME_NOVO_FALHA:-}" = 1 ]; then exit 1; fi ;;
          "ALTER DATABASE "*) if [ "${FALSO_RENAME_FALHA:-}" = 1 ]; then exit 1; fi ;;
          *) printf 'psql de mentira: SQL inesperado\n' >&2; exit 98 ;;
        esac
        ;;
      *) printf 'docker exec de mentira: %s\n' "$prog" >&2; exit 96 ;;
    esac
    ;;
  run)
    junto=" $* "
    # O banco da DATABASE_URL montada (o arquivo do -v), para o teste ver
    # em que banco o migrate e o pos-restauracao rodaram.
    url=''; prev=''
    for a in "$@"; do
      if [ "$prev" = -v ]; then
        case "$a" in *:/run/secrets/database-url:ro) url="${a%:/run/secrets/database-url:ro}" ;; esac
      fi
      prev="$a"
    done
    if [ -n "$url" ]; then
      u=''
      IFS= read -r u < "$url" || true
      [ -n "$u" ] || { printf 'docker de mentira: nao li a URL montada\n' >&2; exit 94; }
      b="${u%%\?*}"
      printf 'banco da url: %s\n' "${b##*/}" >> "$REGISTRO"
    fi
    case "$junto" in
      *" age --encrypt "*) printf 'age-encryption.org/v1\n'; cat ;;
      *" migrate -status "*)
        if [ "${FALSO_STATUS_FALHA:-}" = 1 ]; then exit 1; fi
        printf '%b' "${FALSO_STATUS:-aplicada  0001_users_table.sql\nmigrate: 16 aplicadas, 0 pendentes\n}" ;;
      *" pos-restauracao -confirmo "*)
        if [ "${FALSO_POS_FALHA:-}" = 1 ]; then exit 1; fi
        printf 'pos-restauracao: 1 conta(s) com o epoch novo\n' ;;
      *" migrate "*) printf 'migrate: 1 aplicadas agora\n' ;;
      *) printf 'docker run de mentira: inesperado\n' >&2; exit 95 ;;
    esac
    ;;
  *) printf 'docker de mentira: %s\n' "$sub" >&2; exit 99 ;;
esac
`

// O stat de mentira responde ao tipo do sistema de arquivos (FALSO_FS_TIPO) e
// ao dono e modo (FALSO_DONO_MODO; "meu 700" é o usuário que roda) da pasta em
// memória, e repassa o resto ao stat de verdade, que está nas ferramentas.
const statDeMentira = `
if [ "$1" = -f ] && [ "$2" = -c ] && [ "$3" = %T ]; then printf '%s\n' "${FALSO_FS_TIPO:-ntfs}"; exit 0; fi
if [ "$1" = -c ] && [ "$2" = '%u %a' ]; then
  case "${FALSO_DONO_MODO:-}" in
    'meu '*) printf '%s %s\n' "$(id -u)" "${FALSO_DONO_MODO#meu }" ;;
    *) printf '%s\n' "${FALSO_DONO_MODO:-}" ;;
  esac
  exit 0
fi
PATH="${PATH#*:}" exec stat "$@"
`

type restauracaoDeTeste struct {
	a       *ambienteBash
	base    string
	memoria string
	dump    []byte
}

func novaRestauracao(t *testing.T) *restauracaoDeTeste {
	t.Helper()
	a := novoAmbienteBash(t)
	r := &restauracaoDeTeste{a: a, base: filepath.Join(a.dir, "aurora-whisper"), memoria: filepath.Join(a.dir, "memoria"),
		dump: []byte("PGDMP-FALSO\no backup decifrado\nFIM-DO-DUMP\n")}
	seg := filepath.Join(r.base, "segredos")
	for _, d := range []string{seg, r.memoria} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for n, v := range map[string]string{
		"database-url":         "postgres://aurora:senha@aurora-whisper-postgres:5432/aurora?sslmode=disable\n",
		"backup-chave-publica": chavePublicaDeTeste + "\n",
	} {
		if err := os.WriteFile(filepath.Join(seg, n), []byte(v), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	swaps := filepath.Join(a.dir, "swaps")
	if err := os.WriteFile(swaps, []byte("Filename\tType\tSize\tUsed\tPriority\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.env["AW_BASE"] = posix(r.base)
	a.env["AW_MEMORIA"] = posix(r.memoria)
	a.env["AW_PROC_SWAPS"] = posix(swaps)
	a.env["FALSO_RODANDO"] = "aurora-whisper-postgres"
	a.env["FALSO_BANCOS"] = "aurora"
	a.falso("docker", dockerDaRestauracao)
	return r
}

func (r *restauracaoDeTeste) recebido() string {
	return filepath.Join(r.memoria, "aurora-whisper-restauracao")
}

func (r *restauracaoDeTeste) receber(t *testing.T) {
	t.Helper()
	res := r.a.rodar(restaurarSh, r.dump, "receber")
	if res.codigo != 0 {
		t.Fatalf("receber falhou: %d %q", res.codigo, res.tudo())
	}
}

// envelhecer muda a hora do dump recebido para horas atrás.
func (r *restauracaoDeTeste) envelhecer(t *testing.T, horas int) {
	t.Helper()
	velho := time.Now().Add(-time.Duration(horas) * time.Hour)
	if err := os.Chtimes(filepath.Join(r.recebido(), "dump"), velho, velho); err != nil {
		t.Fatal(err)
	}
}

func TestVPS_RestaurarReceber(t *testing.T) {
	r := novaRestauracao(t)
	res := r.a.rodar(restaurarSh, r.dump, "receber")
	if res.codigo != 0 || !strings.Contains(res.saida, "OK: dump recebido e conferido ate o fim") {
		t.Fatalf("receber: %d %q", res.codigo, res.tudo())
	}
	data, err := os.ReadFile(filepath.Join(r.recebido(), "dump"))
	if err != nil || string(data) != string(r.dump) {
		t.Fatalf("o dump não ficou inteiro na memória: %v", err)
	}
	if !r.a.chamou("docker [exec] [-i] [aurora-whisper-postgres] [pg_restore] [--list]") {
		t.Error("o receber não conferiu o índice com o pg_restore --list")
	}
	if !r.a.chamou("docker [exec] [-i] [aurora-whisper-postgres] [pg_restore] [-f] [/dev/null]") {
		t.Error("o receber não leu o dump inteiro (pg_restore -f /dev/null)")
	}
	for _, c := range r.a.chamadas() {
		if strings.Contains(c, "CREATE") || strings.Contains(c, "ALTER") || strings.Contains(c, "[run]") {
			t.Errorf("o receber mexeu em algo: %s", c)
		}
	}
	exigirContem(t, "a saída do receber", res.saida, "ate o aplicar, o descartar, o fim da sua ultima sessao", "120 min")
	if strings.Contains(res.saida, "AVISO: a VPS tem swap") {
		t.Error("avisou do swap sem swap")
	}
	// De novo: recusa, sem perder o que já chegou.
	res = r.a.rodar(restaurarSh, r.dump, "receber")
	if res.codigo == 0 || !strings.Contains(res.erro, "ja ha um dump recebido") {
		t.Fatalf("o segundo receber deveria recusar: %d %q", res.codigo, res.tudo())
	}
	if _, err := os.Stat(filepath.Join(r.recebido(), "dump")); err != nil {
		t.Error("o segundo receber apagou o primeiro")
	}
	// descartar apaga.
	res = r.a.rodar(restaurarSh, nil, "descartar")
	if res.codigo != 0 {
		t.Fatalf("descartar: %d %q", res.codigo, res.tudo())
	}
	if _, err := os.Stat(r.recebido()); err == nil {
		t.Error("descartar não apagou o dump da memória")
	}
}

func TestVPS_RestaurarReceberRecusa(t *testing.T) {
	for _, c := range []struct {
		nome    string
		entrada []byte
		mexer   func(r *restauracaoDeTeste)
		quero   string
	}{
		{"nada chegou", nil, nil, "nada chegou pela entrada"},
		{"ainda cifrado", []byte("age-encryption.org/v1\nxxx"), nil, "o pg_restore --list nao leu"},
		{"de outro banco", []byte("PGDMP-FALSO\nFIM-DO-DUMP\n"), func(r *restauracaoDeTeste) { r.a.env["FALSO_SEM_USERS"] = "1" }, "nao e do banco da aplicacao"},
		// O índice está no começo e passa no --list; o resto foi cortado no
		// meio da transferência.
		{"cortado no meio", []byte("PGDMP-FALSO\no backup decif"), nil, "o dump veio cortado ou estragado"},
		{"Postgres parado", []byte("PGDMP-FALSO\nFIM-DO-DUMP\n"), func(r *restauracaoDeTeste) { r.a.env["FALSO_RODANDO"] = "" }, "nao esta de pe"},
		{"Postgres reiniciando", []byte("PGDMP-FALSO\nFIM-DO-DUMP\n"), func(r *restauracaoDeTeste) {
			r.a.env["FALSO_RODANDO"] = ""
			r.a.env["FALSO_REINICIANDO"] = "aurora-whisper-postgres"
		}, "nao esta de pe"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := novaRestauracao(t)
			if c.mexer != nil {
				c.mexer(r)
			}
			res := r.a.rodar(restaurarSh, c.entrada, "receber")
			if res.codigo == 0 || !strings.Contains(res.erro, c.quero) {
				t.Fatalf("quero %q: %d %q", c.quero, res.codigo, res.tudo())
			}
			if _, err := os.Stat(r.recebido()); err == nil {
				t.Error("sobrou o dump na memória depois da recusa")
			}
		})
	}
}

// A pasta em memória: só a do usuário (o XDG_RUNTIME_DIR, /run/user/<uid>),
// tmpfs, dele e 0700. O /dev/shm, que é de todos e que um contêiner com
// --ipc=host enxerga, nunca é o padrão.
func TestVPS_RestaurarReceberSoNaMemoriaDoUsuario(t *testing.T) {
	for _, c := range []struct {
		nome, tipo, donoModo, quero string
		semPasta                    bool
	}{
		{"tmpfs só do usuário", "tmpfs", "meu 700", "", false},
		{"disco", "ext2/ext3", "meu 700", "nao e memoria (tmpfs)", false},
		{"pasta de todos", "tmpfs", "0 1777", "nao e so sua", false},
		{"aberta aos outros", "tmpfs", "meu 755", "nao e so sua", false},
		{"sem a pasta (nenhuma sessão aberta)", "tmpfs", "meu 700", "nao achei a pasta em memoria so sua", true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := novaRestauracao(t)
			delete(r.a.env, "AW_MEMORIA")
			raiz := r.memoria
			if c.semPasta {
				raiz = filepath.Join(r.a.dir, "nao-existe")
			}
			r.a.env["XDG_RUNTIME_DIR"] = posix(raiz)
			r.a.env["FALSO_FS_TIPO"] = c.tipo
			r.a.env["FALSO_DONO_MODO"] = c.donoModo
			r.a.falso("stat", statDeMentira)
			res := r.a.rodar(restaurarSh, r.dump, "receber")
			if c.quero == "" {
				if res.codigo != 0 {
					t.Fatalf("receber falhou: %d %q", res.codigo, res.tudo())
				}
				if _, err := os.Stat(filepath.Join(r.recebido(), "dump")); err != nil {
					t.Error("o dump não foi para a pasta do XDG_RUNTIME_DIR")
				}
				return
			}
			if res.codigo == 0 || !strings.Contains(res.erro, c.quero) {
				t.Fatalf("quero %q: %d %q", c.quero, res.codigo, res.tudo())
			}
			if _, err := os.Stat(r.recebido()); err == nil {
				t.Error("gravou o dump numa pasta recusada")
			}
		})
	}
	t.Run("sem o XDG_RUNTIME_DIR, o /run/user do usuário", func(t *testing.T) {
		r := novaRestauracao(t)
		delete(r.a.env, "AW_MEMORIA")
		r.a.falso("stat", statDeMentira)
		res := r.a.rodar(restaurarSh, r.dump, "receber")
		// A recusa cita o /run/user/<uid>: "nao achei" onde ele não existe, e
		// "nao e memoria" onde existe (um runner de CI com sessão aberta), porque
		// o stat de mentira, sem FALSO_FS_TIPO, não diz tmpfs.
		if res.codigo == 0 || !regexp.MustCompile(`(nao achei a pasta em memoria so sua \(/run/user/\d+\)|/run/user/\d+ nao e memoria \(tmpfs\))`).MatchString(res.erro) {
			t.Fatalf("quero a recusa com /run/user/<uid>: %d %q", res.codigo, res.tudo())
		}
	})
	exigirNaoContem(t, restaurarSh, codigo(ler(t, restaurarSh)), "/dev/shm")
}

func TestVPS_RestaurarReceberAvisaDoSwapEApagaOVencido(t *testing.T) {
	r := novaRestauracao(t)
	if err := os.WriteFile(filepath.Join(r.a.dir, "swaps"),
		[]byte("Filename\tType\tSize\tUsed\tPriority\n/swapfile\tfile\t2097148\t0\t-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.receber(t)
	r.envelhecer(t, 3)
	// O que sobrou vencido sai, e o novo entra.
	res := r.a.rodar(restaurarSh, r.dump, "receber")
	if res.codigo != 0 {
		t.Fatalf("o receber deveria apagar o vencido e seguir: %d %q", res.codigo, res.tudo())
	}
	exigirContem(t, "a saída do receber", res.saida, "tinha mais de 120 min e saiu da memoria", "AVISO: a VPS tem swap ligado")
	info, err := os.Stat(filepath.Join(r.recebido(), "dump"))
	if err != nil || time.Since(info.ModTime()) > time.Hour {
		t.Errorf("o dump novo não substituiu o vencido: %v", err)
	}
}

// A ordem do aplicar: tudo no banco novo (o restore, o migrate e o
// pos-restauracao) e, por último, a troca de nomes.
func TestVPS_RestaurarAplicarCaminhoFeliz(t *testing.T) {
	r := novaRestauracao(t)
	r.receber(t)
	res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar")
	if res.codigo != 0 {
		t.Fatalf("aplicar falhou: %d %q", res.codigo, res.tudo())
	}
	ordem := []string{
		"pg_stat_activity",
		"[pg_dump] [-U] [aurora] [-d] [aurora]",
		"[CREATE DATABASE aurora_restaurado_",
		"[REVOKE ALL ON DATABASE aurora_restaurado_",
		" TO aurora_backup]", // o GRANT CONNECT do papel do backup no banco novo
		"[pg_restore] [-U] [aurora] [-d] [aurora_restaurado_",
		"[migrate] [-status]",
		"[pos-restauracao] [-confirmo]",
		"[ALTER DATABASE aurora RENAME TO aurora_antes_",
		"RENAME TO aurora]",
	}
	ultimo := -1
	for _, passo := range ordem {
		i := r.a.indice(passo)
		if i < 0 {
			t.Fatalf("faltou o passo %q:\n%s", passo, strings.Join(r.a.chamadas(), "\n"))
		}
		if i <= ultimo {
			t.Errorf("o passo %q veio fora de ordem:\n%s", passo, strings.Join(r.a.chamadas(), "\n"))
		}
		ultimo = i
	}
	// O migrate e o pos-restauracao rodaram no banco novo, e não no aurora.
	urls := 0
	for _, c := range r.a.chamadas() {
		if strings.HasPrefix(c, "banco da url: ") {
			urls++
			if !strings.HasPrefix(c, "banco da url: aurora_restaurado_") {
				t.Errorf("a API descartável rodou em outro banco: %s", c)
			}
		}
	}
	if urls != 2 {
		t.Errorf("o migrate -status e o pos-restauracao deveriam rodar com a URL do banco novo: %d", urls)
	}
	restaurado, err := os.ReadFile(r.a.registro + ".restaurado")
	if err != nil || string(restaurado) != string(r.dump) {
		t.Errorf("o pg_restore não recebeu o dump recebido: %v", err)
	}
	for _, c := range r.a.chamadas() {
		if strings.Contains(c, "DROP DATABASE") || strings.Contains(c, "dropdb") || strings.Contains(c, "--clean") {
			t.Errorf("o aplicar apagou banco: %s", c)
		}
		if strings.Contains(c, "[run]") && strings.Contains(c, "[migrate]") &&
			!(strings.Contains(c, "[--network] [aurora-whisper-interna]") && strings.Contains(c, "[--read-only]") &&
				strings.Contains(c, "[-e] [DATABASE_URL_FILE=/run/secrets/database-url]")) {
			t.Errorf("a API descartável sem a rede interna, o read-only ou o _FILE: %s", c)
		}
		if strings.Contains(c, "[migrate]") && !strings.Contains(c, "[-status]") {
			t.Errorf("rodou o migrate sem migration pendente: %s", c)
		}
	}
	// A cópia cifrada do banco atual, sem sobra.
	antes, _ := filepath.Glob(filepath.Join(r.base, "antes-de-restaurar", "*"))
	if len(antes) != 1 || !strings.HasSuffix(antes[0], ".dump.age") {
		t.Fatalf("a cópia antes de restaurar: %v", antes)
	}
	if data, _ := os.ReadFile(antes[0]); !strings.HasPrefix(string(data), "age-encryption.org/v1\nPGDMP-FALSO\n") {
		t.Error("a cópia antes de restaurar não é o dump cifrado")
	}
	// Nem o dump nem a cópia da URL (com a senha) ficam na memória.
	if _, err := os.Stat(r.recebido()); err == nil {
		t.Error("o dump decifrado ou a URL do banco novo ficaram na memória")
	}
	hoje := time.Now().UTC().Format("20060102")
	exigirContem(t, "a saída", res.saida,
		"OK: restaurado",
		"DROP DATABASE aurora_antes_"+hoje,
		"rm -f "+posix(r.base)+"/antes-de-restaurar/aurora-whisper-antes-de-restaurar-",
		"TODOS os aparelhos sao revogados")
	if strings.Contains(res.tudo(), "senha@") {
		t.Error("a URL do banco, com a senha, aparece na saída")
	}
}

func TestVPS_RestaurarAplicarComMigrationPendente(t *testing.T) {
	status := `aplicada  0001_users_table.sql\npendente  0017_nova.sql\nmigrate: 16 aplicadas, 1 pendentes\n`
	t.Run("com SIM", func(t *testing.T) {
		r := novaRestauracao(t)
		r.a.env["FALSO_STATUS"] = status
		r.receber(t)
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\nSIM\n"), "aplicar")
		if res.codigo != 0 {
			t.Fatalf("aplicar falhou: %d %q", res.codigo, res.tudo())
		}
		m := -1
		for i, c := range r.a.chamadas() {
			if strings.HasSuffix(c, "[migrate]") {
				m = i
			}
		}
		p := r.a.indice("[pos-restauracao]")
		troca := r.a.indice("[ALTER DATABASE aurora RENAME TO aurora_antes_")
		if m < 0 || p < 0 || m > p || troca < p {
			t.Errorf("a ordem tem de ser migrate, pos-restauracao, troca de nomes: %d %d %d", m, p, troca)
		}
	})
	t.Run("sem SIM", func(t *testing.T) {
		r := novaRestauracao(t)
		r.a.env["FALSO_STATUS"] = status
		r.receber(t)
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\nnao\n"), "aplicar")
		if res.codigo == 0 || !strings.Contains(res.erro, "PAROU: sem o SIM") || !strings.Contains(res.erro, "O banco aurora nao foi tocado") {
			t.Fatalf("deveria parar sem tocar no aurora: %d %q", res.codigo, res.tudo())
		}
		if !regexp.MustCompile(`DROP DATABASE aurora_restaurado_\d{14}`).MatchString(res.erro) {
			t.Error("deveria imprimir o DROP do banco novo, para recomeçar")
		}
		for _, c := range r.a.chamadas() {
			if strings.HasSuffix(c, "[migrate]") || strings.Contains(c, "[pos-restauracao]") || strings.Contains(c, "RENAME") {
				t.Errorf("seguiu sem o SIM: %s", c)
			}
		}
		if _, err := os.Stat(r.recebido()); err == nil {
			t.Error("depois da confirmação, o dump decifrado ficou na memória")
		}
	})
}

// Qualquer parada depois do banco novo e antes da troca de nomes deixa o
// aurora como estava: nunca uma restauração sem o pos-restauracao com o nome
// que a API usa.
func TestVPS_RestaurarAplicarParadaAntesDaTrocaNaoMexeNoAurora(t *testing.T) {
	for _, c := range []struct{ nome, env, quero string }{
		{"o pg_restore falha", "FALSO_RESTORE_FALHA", "FALHOU: a restauracao no banco aurora_restaurado_"},
		{"o migrate -status falha", "FALSO_STATUS_FALHA", "FALHOU: o migrate -status falhou no banco aurora_restaurado_"},
		{"o pos-restauracao falha", "FALSO_POS_FALHA", "FALHOU: o pos-restauracao falhou no banco aurora_restaurado_"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := novaRestauracao(t)
			r.receber(t)
			r.a.env[c.env] = "1"
			res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar")
			if res.codigo == 0 || !strings.Contains(res.erro, c.quero) || !strings.Contains(res.erro, "O banco aurora nao foi tocado") {
				t.Fatalf("quero %q: %d %q", c.quero, res.codigo, res.tudo())
			}
			if !regexp.MustCompile(`DROP DATABASE aurora_restaurado_\d{14}`).MatchString(res.erro) {
				t.Error("deveria imprimir o DROP do banco novo, para o Mauro")
			}
			for _, ch := range r.a.chamadas() {
				if strings.Contains(ch, "RENAME") {
					t.Errorf("trocou os nomes depois da falha: %s", ch)
				}
			}
			if _, err := os.Stat(r.recebido()); err == nil {
				t.Error("o dump decifrado ficou na memória")
			}
		})
	}
}

func TestVPS_RestaurarAplicarRecusaSemTocarNoBanco(t *testing.T) {
	hoje := time.Now().UTC().Format("20060102")
	for _, c := range []struct {
		nome     string
		mexer    func(t *testing.T, r *restauracaoDeTeste)
		entrada  string
		quero    string
		guardado bool // o dump continua na memória (recusa antes da confirmação)
	}{
		{"API de pé", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_RODANDO"] = "aurora-whisper-postgres aurora-whisper-api"
		}, "RESTAURAR\n", "a API (aurora-whisper-api) esta de pe", true},
		// Em laço de reinício, o status é "restarting", e não "running".
		{"API reiniciando", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_REINICIANDO"] = "aurora-whisper-api"
		}, "RESTAURAR\n", "a API (aurora-whisper-api) esta de pe (ou reiniciando", true},
		{"backup de pé", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_RODANDO"] = "aurora-whisper-postgres aurora-whisper-backup"
		}, "RESTAURAR\n", "o backup (aurora-whisper-backup) esta de pe", true},
		{"backup reiniciando", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_REINICIANDO"] = "aurora-whisper-backup"
		}, "RESTAURAR\n", "o backup (aurora-whisper-backup) esta de pe", true},
		// Uma API com outro nome, ou outro serviço: o nome do contêiner não
		// pega, a conexão ao banco pega.
		{"conexão aberta ao banco", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_CONEXOES"] = "2"
		}, "RESTAURAR\n", "ha 2 conexao(oes) ao banco aurora", true},
		{"já restaurou hoje", func(t *testing.T, r *restauracaoDeTeste) {
			r.a.env["FALSO_BANCOS"] = "aurora aurora_antes_" + hoje
		}, "RESTAURAR\n", "ja existe o banco aurora_antes_" + hoje, true},
		{"dump vencido", func(t *testing.T, r *restauracaoDeTeste) { r.envelhecer(t, 3) },
			"RESTAURAR\n", "tinha mais de 120 min e saiu da memoria", false},
		{"sem confirmar", nil, "sim\n", "PAROU: nada foi feito", false},
		{"cópia do atual que falha", func(t *testing.T, r *restauracaoDeTeste) { r.a.env["FALSO_DUMP"] = "falha" },
			"RESTAURAR\n", "nao consegui a copia cifrada do banco atual", false},
		{"lista da cópia do atual que falha", func(t *testing.T, r *restauracaoDeTeste) { r.a.env["FALSO_LISTA_FALHA"] = "1" },
			"RESTAURAR\n", "nao consegui a copia cifrada do banco atual", false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := novaRestauracao(t)
			r.receber(t)
			if c.mexer != nil {
				c.mexer(t, r)
			}
			res := r.a.rodar(restaurarSh, []byte(c.entrada), "aplicar")
			if res.codigo == 0 || !strings.Contains(res.tudo(), c.quero) {
				t.Fatalf("quero %q: %d %q", c.quero, res.codigo, res.tudo())
			}
			// Nenhum arquivo que o script esperava e não gravou (o .rc da
			// lista, quando o grupo saía no set -e).
			if strings.Contains(res.erro, "No such file") {
				t.Errorf("sobrou erro de arquivo que não foi gravado: %q", res.erro)
			}
			for _, ch := range r.a.chamadas() {
				if strings.Contains(ch, "CREATE DATABASE") || strings.Contains(ch, "ALTER DATABASE") ||
					strings.Contains(ch, "-d] [aurora_restaurado") || strings.Contains(ch, "[run]") && !strings.Contains(ch, "age") {
					t.Errorf("mexeu no banco mesmo recusando: %s", ch)
				}
			}
			_, err := os.Stat(filepath.Join(r.recebido(), "dump"))
			if c.guardado && err != nil {
				t.Error("a recusa antes da confirmação apagou o dump recebido")
			}
			if !c.guardado && err == nil {
				t.Error("o dump decifrado ficou na memória")
			}
			if c.guardado {
				if sobras, _ := filepath.Glob(filepath.Join(r.recebido(), "database-url*")); len(sobras) > 0 {
					t.Errorf("a recusa deixou a URL do banco (com a senha) na memória: %v", sobras)
				}
			}
		})
	}
}

// A troca de nomes é o último passo. Se a primeira falha (uma conexão aberta
// ao aurora), nada muda e o script imprime as duas trocas, prontas; se a
// segunda falha, a primeira é desfeita.
func TestVPS_RestaurarAplicarTrocaDeNomes(t *testing.T) {
	t.Run("a segunda troca falha e a primeira é desfeita", func(t *testing.T) {
		r := novaRestauracao(t)
		r.receber(t)
		r.a.env["FALSO_RENAME_NOVO_FALHA"] = "1"
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar")
		if res.codigo == 0 || !strings.Contains(res.erro, "o atual voltou ao nome dele") {
			t.Fatalf("quero a falha da troca de nomes: %d %q", res.codigo, res.tudo())
		}
		if r.a.indice("[ALTER DATABASE aurora_antes_") < 0 {
			t.Error("não devolveu o nome ao banco atual")
		}
		if p, troca := r.a.indice("[pos-restauracao]"), r.a.indice("RENAME"); p < 0 || troca < p {
			t.Errorf("o pos-restauracao vem antes da troca de nomes: %d %d", p, troca)
		}
	})
	t.Run("a primeira troca falha e nada muda", func(t *testing.T) {
		r := novaRestauracao(t)
		r.receber(t)
		r.a.env["FALSO_RENAME_FALHA"] = "1"
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar")
		if res.codigo == 0 || !strings.Contains(res.erro, "troque os") {
			t.Fatalf("quero a falha da primeira troca: %d %q", res.codigo, res.tudo())
		}
		hoje := time.Now().UTC().Format("20060102")
		exigirContem(t, "a saída", res.erro, "ALTER DATABASE aurora RENAME TO aurora_antes_"+hoje)
		if !regexp.MustCompile(`ALTER DATABASE aurora_restaurado_\d{14} RENAME TO aurora'`).MatchString(res.erro) {
			t.Error("deveria imprimir a segunda troca, pronta")
		}
		if r.a.indice("[ALTER DATABASE aurora_restaurado_") >= 0 {
			t.Error("tentou a segunda troca depois de a primeira falhar")
		}
	})
}

func TestVPS_RestaurarAplicarSemBancoAtualOuSemCopia(t *testing.T) {
	t.Run("sem banco atual", func(t *testing.T) {
		r := novaRestauracao(t)
		r.a.env["FALSO_BANCOS"] = ""
		r.receber(t)
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar")
		if res.codigo != 0 {
			t.Fatalf("aplicar falhou: %d %q", res.codigo, res.tudo())
		}
		if r.a.chamou("docker [exec] [aurora-whisper-postgres] [pg_dump]") || r.a.indice("RENAME TO aurora_antes_") >= 0 {
			t.Error("sem banco atual, não há cópia nem renomeação do atual")
		}
		if strings.Contains(res.saida, "DROP DATABASE") {
			t.Error("sem banco atual, não há DROP a sugerir")
		}
	})
	t.Run("--sem-copia-antes", func(t *testing.T) {
		r := novaRestauracao(t)
		r.receber(t)
		res := r.a.rodar(restaurarSh, []byte("RESTAURAR\n"), "aplicar", "--sem-copia-antes")
		if res.codigo != 0 {
			t.Fatalf("aplicar falhou: %d %q", res.codigo, res.tudo())
		}
		if r.a.chamou("docker [exec] [aurora-whisper-postgres] [pg_dump]") {
			t.Error("--sem-copia-antes fez a cópia")
		}
		if r.a.indice("RENAME TO aurora_antes_") < 0 {
			t.Error("o banco atual tem de ser guardado pelo nome mesmo sem a cópia")
		}
	})
}

func TestVPS_RestaurarUsoErrado(t *testing.T) {
	a := novoAmbienteBash(t)
	a.falso("docker", dockerDaRestauracao)
	for _, args := range [][]string{nil, {"apagar"}, {"aplicar", "--clean"}} {
		res := a.rodar(restaurarSh, nil, args...)
		if res.codigo == 0 {
			t.Errorf("%q deveria ser recusado", args)
		}
	}
}

// --- conferir-b2.sh com o docker de mentira ---------------------------------

// O docker de mentira do conferir-b2.sh: só "run" da imagem do backup, com o
// rclone.conf montado só para leitura, e um B2 de mentira numa pasta
// (FALSO_REMOTO). Por padrão a chave sobe e lista e NÃO apaga (401), como a
// chave restrita; FALSO_B2_APAGA é a chave com deleteFiles e sem Object Lock.
const dockerDoB2 = `
[ "$1" = run ] || { printf 'docker de mentira: so run\n' >&2; exit 99; }
shift
conf=''; prev=''
while [ "$#" -gt 0 ]; do
  if [ "$1" = aurora-whisper-backup:producao ]; then shift; break; fi
  if [ "$prev" = -v ]; then
    case "$1" in *:/run/secrets/rclone.conf:ro) conf="${1%:/run/secrets/rclone.conf:ro}" ;; esac
  fi
  prev="$1"; shift
done
if [ -z "$conf" ] || [ ! -r "$conf" ]; then printf 'docker de mentira: sem o rclone.conf montado so para leitura\n' >&2; exit 98; fi
[ "$1" = rclone ] || { printf 'docker de mentira: so o rclone\n' >&2; exit 97; }
shift
cmd="$1"; shift
destino="${@: -1}"
case "$destino" in
  b2:bucket-de-teste/aurora-whisper/conferencia-da-chave-*.txt) ;;
  # A pasta, so para a listagem (a conferencia do tamanho pela listagem).
  b2:bucket-de-teste/aurora-whisper/) [ "$cmd" = lsf ] || { printf 'docker de mentira: a pasta so na listagem\n' >&2; exit 96; } ;;
  *) printf 'docker de mentira: destino inesperado\n' >&2; exit 96 ;;
esac
arq="$FALSO_REMOTO/${destino##*/}"
case "$cmd" in
  rcat)
    if [ "${FALSO_B2_SEM_ESCRITA:-}" = 1 ]; then cat > /dev/null; exit 1; fi
    mkdir -p "$FALSO_REMOTO"
    cat > "$arq"
    ;;
  lsf)
    if [ "${FALSO_B2_SEM_LISTA:-}" = 1 ]; then exit 1; fi
    # Como o B2 com a chave sem readFiles: o lsf de um arquivo so da 401; a
    # listagem da pasta funciona ("tamanho;nome").
    case "$destino" in */) ;; *) printf 'failed to HEAD for download: Unknown 401\n' >&2; exit 1 ;; esac
    [ -d "$FALSO_REMOTO" ] || exit 0
    for f in "$FALSO_REMOTO"/*; do
      [ -f "$f" ] || continue
      if [ "${FALSO_B2_TAMANHO_ERRADO:-}" = 1 ]; then printf '1;%s\n' "${f##*/}"; else printf '%s;%s\n' "$(stat -c %s "$f")" "${f##*/}"; fi
    done
    ;;
  deletefile)
    case " $* " in *" --b2-hard-delete "*) ;; *) printf 'docker de mentira: deletefile sem --b2-hard-delete\n' >&2; exit 95 ;; esac
    if [ "${FALSO_B2_APAGA:-}" = 1 ]; then rm -f "$arq"; exit 0; fi
    if [ "${FALSO_B2_SOME:-}" = 1 ]; then rm -f "$arq"; fi
    printf 'ERROR : unauthorized (401)\n' >&2
    exit 1
    ;;
  *) printf 'docker de mentira: rclone %s\n' "$cmd" >&2; exit 94 ;;
esac
`

type conferenciaDoB2 struct {
	a      *ambienteBash
	base   string
	remoto string
}

func novaConferenciaDoB2(t *testing.T) *conferenciaDoB2 {
	t.Helper()
	a := novoAmbienteBash(t)
	c := &conferenciaDoB2{a: a, base: filepath.Join(a.dir, "aurora-whisper"), remoto: filepath.Join(a.dir, "b2")}
	seg := filepath.Join(c.base, "segredos")
	if err := os.MkdirAll(seg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seg, "rclone.conf"),
		[]byte("[b2]\ntype = b2\naccount = "+keyIDDeTeste+"\nkey = "+appKeyDeTeste+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.base, "config.env"),
		[]byte("PUBLIC_BASE_URL=https://aurora.exemplo.com.br\nCONTAS_PERMITIDAS=mauro@exemplo.com\nB2_BUCKET=bucket-de-teste\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.env["AW_BASE"] = posix(c.base)
	a.env["FALSO_REMOTO"] = posix(c.remoto)
	a.falso("docker", dockerDoB2)
	return c
}

func TestVPS_ConferirB2NoTexto(t *testing.T) {
	c := codigo(ler(t, conferirB2))
	exigirContem(t, conferirB2, c,
		`nome="conferencia-da-chave-$(date -u +%Y%m%dT%H%M%SZ).txt"`,
		`pasta_remota="b2:$bucket/$PREFIXO/"`,
		`remoto="$pasta_remota$nome"`,
		`rclone_b2 lsf --format sp --files-only "$pasta_remota"`,
		`-v "$SEGREDOS/rclone.conf:/run/secrets/rclone.conf:ro"`,
		"--cap-drop ALL", "--security-opt no-new-privileges",
	)
	// O único "apagar" é o do arquivo de conferência que o próprio script
	// subiu, e só para provar que não consegue.
	if n := strings.Count(c, "deletefile"); n != 1 || !strings.Contains(c, `rclone_b2 deletefile --b2-hard-delete "$remoto"`) {
		t.Errorf("o conferir-b2.sh só tenta apagar o arquivo de conferência (%d deletefile)", n)
	}
	for _, ruim := range []string{"rclone delete ", "rclone_b2 delete ", "purge", " sync ", " move ", "cleanup", "backend "} {
		if strings.Contains(c, ruim) {
			t.Errorf("o conferir-b2.sh tem %q", ruim)
		}
	}
}

func TestVPS_ConferirB2ChaveQueSobeListaENaoApaga(t *testing.T) {
	c := novaConferenciaDoB2(t)
	r := c.a.rodar(conferirB2, nil)
	if r.codigo != 0 {
		t.Fatalf("a chave restrita deveria passar: %d %q", r.codigo, r.tudo())
	}
	exigirContem(t, "a saída", r.saida,
		"OK: a chave sobe arquivos ao bucket bucket-de-teste", "OK: a chave lista o que subiu",
		"OK: a chave nao apaga de vez o que subiu",
		"Object Lock ligado e retencao padrao de 30 dias", "so com listBuckets, listFiles e writeFiles")
	subiram, _ := filepath.Glob(filepath.Join(c.remoto, "conferencia-da-chave-*.txt"))
	if len(subiram) != 1 {
		t.Fatalf("o arquivo de conferência tinha de continuar no B2: %v", subiram)
	}
	if !c.a.chamou("docker [run] [--rm] [-i] [--cap-drop] [ALL]") {
		t.Errorf("o rclone roda num contêiner descartável, sem capacidades: %q", c.a.chamadas())
	}
	if strings.Contains(r.tudo()+strings.Join(c.a.chamadas(), "\n"), appKeyDeTeste) {
		t.Error("a application key aparece na saída ou numa linha de comando")
	}
}

func TestVPS_ConferirB2Recusa(t *testing.T) {
	for _, c := range []struct {
		nome, env, quero string
	}{
		// A chave com deleteFiles, num bucket sem Object Lock.
		{"chave que apaga", "FALSO_B2_APAGA", "a chave APAGOU DE VEZ o arquivo de conferencia"},
		{"chave que não sobe", "FALSO_B2_SEM_ESCRITA", "a chave nao subiu um arquivo"},
		{"chave que não lista", "FALSO_B2_SEM_LISTA", "a chave nao lista o bucket (falta listFiles)"},
		{"arquivo que some sem o rclone apagar", "FALSO_B2_SOME", "o arquivo de conferencia sumiu"},
		{"o tamanho no B2 não bate", "FALSO_B2_TAMANHO_ERRADO", "o tamanho do arquivo de conferencia no B2 nao bate"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			cf := novaConferenciaDoB2(t)
			cf.a.env[c.env] = "1"
			r := cf.a.rodar(conferirB2, nil)
			if r.codigo == 0 || !strings.Contains(r.erro, c.quero) {
				t.Fatalf("quero %q: %d %q", c.quero, r.codigo, r.tudo())
			}
			if strings.Contains(r.saida, "OK: a chave nao apaga") {
				t.Error("disse que a chave não apaga mesmo falhando")
			}
		})
	}
	t.Run("sem o rclone.conf", func(t *testing.T) {
		cf := novaConferenciaDoB2(t)
		cf.a.env["AW_BASE"] = posix(filepath.Join(cf.a.dir, "outra"))
		r := cf.a.rodar(conferirB2, nil)
		if r.codigo == 0 || !strings.Contains(r.erro, "rode antes o gerar-segredos.sh") || cf.a.chamou("docker") {
			t.Fatalf("deveria recusar sem chamar o docker: %d %q", r.codigo, r.tudo())
		}
	})
	t.Run("bucket ruim no config.env", func(t *testing.T) {
		cf := novaConferenciaDoB2(t)
		if err := os.WriteFile(filepath.Join(cf.base, "config.env"), []byte("B2_BUCKET=b2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := cf.a.rodar(conferirB2, nil)
		if r.codigo == 0 || !strings.Contains(r.erro, "nao e um nome de bucket") || cf.a.chamou("docker") {
			t.Fatalf("deveria recusar sem chamar o docker: %d %q", r.codigo, r.tudo())
		}
	})
}
