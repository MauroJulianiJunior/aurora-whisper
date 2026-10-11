package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// O agendador do backup (deploy/scripts/agendar-backup.sh), com o sleep e o
// backup.sh de mentira: a espera até a hora, o backup que falha sem parar o
// laço, o SIGTERM que para na hora, e a saúde pelo .ultimo-sucesso.

func novoAgendador(t *testing.T) (*ambienteBash, string) {
	t.Helper()
	a := novoAmbienteBash(t)
	pasta := filepath.Join(a.dir, "backup")
	if err := os.MkdirAll(pasta, 0o700); err != nil {
		t.Fatal(err)
	}
	a.env["AW_PASTA_BACKUP"] = posix(pasta)
	return a, pasta
}

func TestAgendador_Saude(t *testing.T) {
	agora := time.Now().Unix()
	for _, c := range []struct {
		nome     string
		conteudo *string
		ok       bool
		msg      string
	}{
		{"sem nenhum backup ainda", nil, false, "nenhum backup subiu ao B2 ainda"},
		{"de 1 h atrás", ptr(strconv.FormatInt(agora-3600, 10) + "\n"), true, "e de 1 h atras"},
		{"de 25 h e 59 min atrás", ptr(strconv.FormatInt(agora-26*3600+60, 10) + "\n"), true, ""},
		{"de 26 h e 1 min atrás", ptr(strconv.FormatInt(agora-26*3600-60, 10) + "\n"), false, "(mais de 26 h)"},
		{"que não é uma hora", ptr("ontem\n"), false, "nao e uma hora"},
		{"vazio", ptr(""), false, "nao e uma hora"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a, pasta := novoAgendador(t)
			if c.conteudo != nil {
				if err := os.WriteFile(filepath.Join(pasta, ".ultimo-sucesso"), []byte(*c.conteudo), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			r := a.rodar(agendarBackup, nil, "saude")
			if (r.codigo == 0) != c.ok || (r.codigo != 0 && r.codigo != 1) {
				t.Fatalf("código %d, quero saudável=%v: %q", r.codigo, c.ok, r.tudo())
			}
			if c.msg != "" && !strings.Contains(r.tudo(), c.msg) {
				t.Errorf("quero %q: %q", c.msg, r.tudo())
			}
		})
	}
}

func ptr(s string) *string { return &s }

// O laço: o sleep de mentira anota a espera e volta na hora; o backup.sh de
// mentira falha na primeira vez (o laço segue) e, na segunda, manda SIGTERM ao
// agendador (o docker stop), que para com 0.
func TestAgendador_EsperaAteAHoraERodaOBackup(t *testing.T) {
	for _, hora := range []string{"06", "00", "23", "09"} {
		t.Run(hora, func(t *testing.T) {
			a, _ := novoAgendador(t)
			a.env["AW_HORA_DO_BACKUP"] = hora
			a.falso("sleep", "exit 0")
			a.falso("backup.sh", `n="$(grep -c '^backup.sh' "$REGISTRO")"
if [ "$n" -ge 2 ]; then kill -TERM "$PPID"; exit 0; fi
printf 'backup: FALHOU (de mentira)\n' >&2
exit 1`)
			antes := time.Now().Unix()
			r := a.rodar(agendarBackup, nil)
			depois := time.Now().Unix()
			if r.codigo != 0 || !strings.Contains(r.saida, "agendador: parado") {
				t.Fatalf("deveria parar com 0 no SIGTERM: %d %q", r.codigo, r.tudo())
			}
			exigirContem(t, "a saída", r.saida, "agendador: um backup por dia, as "+hora+":00 UTC")
			var esperas []int64
			backups := 0
			for _, c := range a.chamadas() {
				if m := regexp.MustCompile(`^sleep \[(\d+)\]$`).FindStringSubmatch(c); m != nil {
					n, _ := strconv.ParseInt(m[1], 10, 64)
					esperas = append(esperas, n)
				}
				if strings.HasPrefix(c, "backup.sh") {
					backups++
				}
			}
			if backups != 2 || len(esperas) != 2 {
				t.Fatalf("quero duas esperas e dois backups (o primeiro falhou e o laço seguiu): %q", a.chamadas())
			}
			h, _ := strconv.ParseInt(hora, 10, 64)
			for _, e := range esperas {
				if e < 1 || e > 86400 {
					t.Errorf("espera fora de um dia: %d", e)
				}
				// O fim da espera cai na hora cheia, em UTC.
				ok := false
				for agora := antes; agora <= depois; agora++ {
					ok = ok || (agora+e)%86400 == h*3600
				}
				if !ok {
					t.Errorf("a espera de %d s não termina às %s:00 UTC", e, hora)
				}
			}
		})
	}
}

func TestAgendador_RecusaHoraOuUsoErrado(t *testing.T) {
	for _, c := range []struct {
		hora string
		args []string
	}{
		{"24", nil}, {"6", nil}, {"ab", nil}, {"06; rm", nil}, {"06", []string{"agora"}},
	} {
		t.Run(c.hora+strings.Join(c.args, ""), func(t *testing.T) {
			a, _ := novoAgendador(t)
			a.env["AW_HORA_DO_BACKUP"] = c.hora
			a.falso("sleep", "exit 0")
			a.falso("backup.sh", "exit 0")
			r := a.rodar(agendarBackup, nil, c.args...)
			if r.codigo != 1 {
				t.Fatalf("deveria recusar: %d %q", r.codigo, r.tudo())
			}
			if a.chamou("backup.sh") || a.chamou("sleep") {
				t.Error("recusou depois de começar")
			}
		})
	}
}

func TestAgendador_NoTexto(t *testing.T) {
	c := codigo(ler(t, agendarBackup))
	exigirContem(t, agendarBackup, c,
		`HORA="${AW_HORA_DO_BACKUP:-06}"`, "MAX_HORAS=26",
		`sleep "$(segundos_ate_a_hora)" &`, `wait "$!"`, "backup.sh || true",
		`"$PASTA/.ultimo-sucesso"`)
	// Sem date -d (o date do busybox da imagem alpine não o tem como o GNU).
	exigirNaoContem(t, agendarBackup, c, "date -d", "date -u -d", "crontab", "cron")
	// O backup.sh grava o .ultimo-sucesso que a saúde lê.
	exigirContem(t, backupSh, ler(t, backupSh), `mv -f -- "$PASTA/.ultimo-sucesso.novo" "$PASTA/.ultimo-sucesso"`)
}
