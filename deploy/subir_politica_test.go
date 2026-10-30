package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A política de privacidade da instância no subir.sh (docs/deploy-privacidade.md):
// o arquivo que o enviar-para-servidor.ps1 -ArquivoPolitica mandou para
// recebido/ é conferido (as regras do servidor), entra em politica/ (a pasta que
// o compose monta só para leitura) depois do SIM, a API é recriada (ela lê o
// arquivo só na subida) e a borda confere o GET /privacidade. Sem arquivo
// novo, nada muda.

const politicaDeTeste = "# Política de privacidade\n\nO responsável é o fulano de tal, pelo contato dele.\n"

// logDaAPIComPolitica é o log da subida de uma API que publicou a política.
const logDaAPIComPolitica = `{"time":"2026-10-09T12:00:00Z","level":"INFO","msg":"arrumação","logins_pendentes":0,"sessoes":0,"pedidos":0}
{"time":"2026-10-09T12:00:00Z","level":"INFO","msg":"retenção","usuarios":0,"ativos":0,"lixeira":0,"lapides_criadas":0,"lapides_vencidas":0,"eventos":0,"mutacoes":0,"falhas":0,"ms":3}
{"time":"2026-10-09T12:00:01Z","level":"INFO","msg":"servindo","endereco":"[::]:8080","contas_permitidas":1,"https":true,"proxy_confiavel":true,"chaves_da_ia":true,"politica_publicada":true}
`

// receberPolitica põe a política em recebido/, como o scp do script do PC.
func (s *subirDeTeste) receberPolitica(t *testing.T, conteudo []byte) string {
	t.Helper()
	p := filepath.Join(s.base, "recebido", "politica-de-privacidade.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, conteudo, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *subirDeTeste) arquivoDaPolitica() string {
	return filepath.Join(s.base, "politica", "politica-de-privacidade.md")
}

func (s *subirDeTeste) politicaAtual(t *testing.T) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(s.arquivoDaPolitica())
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

// comLogDaPolitica troca o log da API de mentira pelo de quem publicou a política.
func (s *subirDeTeste) comLogDaPolitica(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.a.dir, "log-da-api.txt"), []byte(logDaAPIComPolitica), 0o644); err != nil {
		t.Fatal(err)
	}
}

// comCurlDaPrivacidade faz o curl de mentira responder o código dado só ao
// /privacidade (o health continua 200).
func (s *subirDeTeste) comCurlDaPrivacidade(codigo string) {
	s.a.falso("curl", "case \"$*\" in *privacidade*) printf '"+codigo+"' ;; *) printf '%s' \"${FALSO_HTTPS:-200}\" ;; esac")
}

// A pasta da política existe sempre (o bind de uma pasta que falta o Docker
// cria como root); sem arquivo novo, a API sobe como sempre, sem recriar e sem
// conferir a página.
func TestSubirPolitica_SemArquivoNadaMuda(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	r := s.rodar("SIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	if info, err := os.Stat(filepath.Join(s.base, "politica")); err != nil || !info.IsDir() {
		t.Errorf("a pasta politica/ deveria existir: %v", err)
	}
	if _, ok := s.politicaAtual(t); ok {
		t.Error("criou uma política que ninguém mandou")
	}
	for _, c := range s.a.chamadas() {
		if strings.Contains(c, "--force-recreate") {
			t.Errorf("recriou a API sem política nova: %s", c)
		}
		if strings.Contains(c, "privacidade") {
			t.Errorf("conferiu a página sem política: %s", c)
		}
	}
	if strings.Contains(r.tudo(), "politica de privacidade") {
		t.Errorf("a saída fala da política sem ela: %q", r.saida)
	}
}

func TestSubirPolitica_UmaPoliticaNovaEntraDepoisDoSIMEARecria(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.comLogDaPolitica(t)
	recebido := s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("SIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	if got, ok := s.politicaAtual(t); !ok || got != politicaDeTeste {
		t.Errorf("politica/ tem %q (existe: %v), quero a que veio", got, ok)
	}
	if info, err := os.Stat(s.arquivoDaPolitica()); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Errorf("a política não ficou só leitura (0444): %v", err)
	}
	if _, err := os.Stat(recebido); !os.IsNotExist(err) {
		t.Error("a cópia de entrada ficou em recebido/")
	}
	if sobras, _ := filepath.Glob(filepath.Join(s.base, "politica", ".politica.nova.*")); len(sobras) > 0 {
		t.Errorf("sobrou: %v", sobras)
	}
	exigirContem(t, "a saída", r.saida, "uma politica de privacidade NOVA em recebido/",
		"vira a de politica/, publica em https://aurora.exemplo.com.br/privacidade",
		"OK: a politica de privacidade em", "OK: a API publica a politica de privacidade da instancia",
		"OK: https://aurora.exemplo.com.br/privacidade responde 200", "OK: a nuvem esta no ar")
	s.ordem(t, "[migrate] [-status]", "[up] [-d] [--force-recreate] [aurora-whisper-api]",
		"docker [logs] [--since] ["+inicioDaAPI+"] [aurora-whisper-api]", "[up] [-d] [aurora-whisper-backup]", "curl [--silent]")
	// O texto da política nunca aparece na saída nem numa chamada.
	if strings.Contains(r.tudo()+strings.Join(s.a.chamadas(), "\n"), "fulano de tal") {
		t.Error("o texto da política apareceu na saída ou numa chamada")
	}
}

func TestSubirPolitica_AMesmaSaiDeRecebidoSemRecriar(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	if err := os.MkdirAll(filepath.Join(s.base, "politica"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.arquivoDaPolitica(), []byte(politicaDeTeste), 0o444); err != nil {
		t.Fatal(err)
	}
	s.comLogDaPolitica(t)
	recebido := s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("SIM\n")
	if r.codigo != 0 || !strings.Contains(r.saida, "e a mesma de politica/ (nada a trocar)") {
		t.Fatalf("deveria seguir sem trocar: %d %q", r.codigo, r.tudo())
	}
	if _, err := os.Stat(recebido); !os.IsNotExist(err) {
		t.Error("a cópia igual ficou em recebido/")
	}
	for _, c := range s.a.chamadas() {
		if strings.Contains(c, "--force-recreate") {
			t.Errorf("recriou a API sem troca: %s", c)
		}
	}
	// Com a política no lugar, a borda a confere mesmo sem troca.
	exigirContem(t, "a saída", r.saida, "OK: https://aurora.exemplo.com.br/privacidade responde 200")
}

func TestSubirPolitica_SemOSIMNaoPublica(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	recebido := s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("nao\n")
	if r.codigo != 1 || !strings.Contains(r.erro, "a politica de privacidade nao foi publicada") {
		t.Fatalf("deveria parar: %d %q", r.codigo, r.tudo())
	}
	if _, ok := s.politicaAtual(t); ok {
		t.Error("publicou a política sem o SIM")
	}
	if _, err := os.Stat(recebido); err != nil {
		t.Error("a política nova deveria continuar em recebido/")
	}
	if s.a.indice("[up] [-d] [--force-recreate]") >= 0 {
		t.Error("recriou a API sem o SIM")
	}
}

// O que o servidor recusaria na subida o subir.sh recusa antes, e o arquivo
// fica onde veio (é do Mauro decidir).
func TestSubirPolitica_Recusas(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		nome     string
		conteudo []byte
		quero    string
	}{
		{"campo do modelo", []byte("# Política\n\n**[A PREENCHER: nome do responsável]**\n"), "ainda tem campos [A PREENCHER...]"},
		{"campo, se for o caso", []byte("# Política\n\n**[A PREENCHER, se for o caso: x]**\n"), "ainda tem campos [A PREENCHER...]"},
		{"nota do topo", []byte("# Política\n\nPreencha os campos **[A PREENCHER]**.\n"), "ainda tem campos [A PREENCHER...]"},
		{"vazia", nil, "esta vazia"},
		{"Latin-1", []byte("# Pol\xedtica\n"), "nao e um texto UTF-8 valido"},
		{"binária", []byte("# Política\x00\x01\n"), "caracteres de controle"},
		{"ESC", []byte("# Política\x1b[31m\n"), "caracteres de controle"},
		{"grande demais", []byte("# Política\n\n" + strings.Repeat("a", 98304)), "passa de 96 KiB"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			t.Parallel()
			s := novoSubir(t)
			recebido := s.receberPolitica(t, c.conteudo)
			if c.nome == "Latin-1" {
				// O iconv da VPS (da libc) recusa o que não é UTF-8; o PC de
				// teste pode não o ter, então o de mentira faz a recusa.
				s.a.falso("iconv", "cat > /dev/null; exit 1")
			}
			r := s.rodar("SIM\nSIM\n")
			if r.codigo != 1 || !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, c.quero) ||
				!strings.Contains(r.erro, "rm "+posix(recebido)) {
				t.Fatalf("quero FALHOU com %q e o rm: %d %q", c.quero, r.codigo, r.tudo())
			}
			if _, ok := s.politicaAtual(t); ok {
				t.Error("publicou uma política recusada")
			}
			if _, err := os.Stat(recebido); err != nil {
				t.Error("a recusa apagou o que veio")
			}
			for _, ch := range s.a.chamadas() {
				if strings.Contains(ch, "[up]") || strings.HasPrefix(ch, "docker [run]") {
					t.Errorf("subiu algo antes de recusar: %s", ch)
				}
			}
		})
	}
}

// A política nova com tudo certo, mas a API subiu sem publicá-la (a linha
// servindo não diz politica_publicada=true): o script não finge que deu certo.
func TestSubirPolitica_APISemPublicarFalha(t *testing.T) {
	t.Parallel()
	s := novoSubir(t) // o log de mentira padrão não tem politica_publicada
	s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("SIM\n")
	if r.codigo != 1 || !strings.Contains(r.erro, "a API subiu sem publicar a politica de privacidade") {
		t.Fatalf("deveria falhar: %d %q", r.codigo, r.tudo())
	}
	if s.a.indice("[up] [-d] [aurora-whisper-backup]") >= 0 {
		t.Error("seguiu para o backup depois da falha")
	}
}

// A borda tem de servir a página: um 404 do /privacidade depois de tudo
// certo é falha, com a dica de olhar o log.
func TestSubirPolitica_BordaSemAPaginaFalha(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.comLogDaPolitica(t)
	s.comCurlDaPrivacidade("404")
	s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("SIM\n")
	if r.codigo != 1 || !strings.Contains(r.erro, "https://aurora.exemplo.com.br/privacidade respondeu 404") {
		t.Fatalf("deveria falhar: %d %q", r.codigo, r.tudo())
	}
}

// Junto com a troca do client do Google: uma recriação só, com os dois.
func TestSubirPolitica_JuntoComOClientDoGoogle(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.comLogDaPolitica(t)
	novo := strings.Replace(jsonDoGoogleDeTeste("aurora.exemplo.com.br"), "123-abc", "456-def", 1)
	s.receber(t, novo)
	s.receberPolitica(t, []byte(politicaDeTeste))
	r := s.rodar("SIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	if s.googleAtual(t) != novo {
		t.Error("o client do Google não foi trocado")
	}
	if got, ok := s.politicaAtual(t); !ok || got != politicaDeTeste {
		t.Error("a política não entrou")
	}
	n := 0
	for _, c := range s.a.chamadas() {
		if strings.Contains(c, "[up] [-d] [--force-recreate] [aurora-whisper-api]") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a API foi recriada %d vezes, quero 1", n)
	}
	if _, err := os.Stat(filepath.Join(s.base, "recebido")); !os.IsNotExist(err) {
		t.Error("a pasta recebido/ ficou (as duas cópias de entrada saíram)")
	}
}
