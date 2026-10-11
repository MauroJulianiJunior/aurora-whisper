//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os dois portões da F6, na montagem de verdade do Agent (montarNuvem), sob
// go test (CLAUDE.md, "Testes que agem no sistema real"): o navegador
// (painel.AbrirNoNavegador cria janela) e o Gerenciador de Credenciais
// (chaves.NovoGerenciador). Os dois passam pelo sysinput.Guard, que recusa
// SEMPRE sob go test. Este teste prova que o Conectar de verdade para no
// portão do navegador e que o retorno de verdade para no portão do
// Gerenciador: nada abre, nada é gravado, e o aparelho que nasceu na nuvem
// é revogado.
//
// A nuvem é um httptest no loopback. O teste não abre janela, não toca no
// Gerenciador de Credenciais real e não grava fora da pasta temporária.
func TestNuvem_PortoesNaMontagemDoAgent(t *testing.T) {
	const pedido = "0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e6f"
	var (
		mu      sync.Mutex
		trocas  int
		deletes []string
		token   string
	)
	falsa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/device-auth/start":
			io.WriteString(w, `{"pedido":"`+pedido+`","conferencia":"K7QM","expires_in":600}`)
		case r.URL.Path == "/api/v1/device-auth/exchange":
			trocas++
			var b [32]byte
			_, _ = rand.Read(b[:])
			token = base64.RawURLEncoding.EncodeToString(b[:])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_id": "0192f0a1-0000-7000-8000-0000000a9a1e", "token": token,
				"conta": map[string]string{"id": "0192f0a1-0000-7000-8000-00000000c0a1", "email": "voce@teste.local"},
			})
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path+" "+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer falsa.Close()
	porta := falsa.Listener.Addr().(interface{ String() string }).String()
	porta = porta[strings.LastIndex(porta, ":")+1:]

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	inst := storage.NewSQLiteSettingsRepository(db).Instalacao()

	// O bin\ (modo portátil) de um build de desenvolvimento (go test usa o
	// literal de version): a política deixa http://localhost.
	s := montarNuvem(inst, nil, pastas.ModoPortatil, nil)
	s.UsarPorta(51234)

	// 1. O Conectar fala com a nuvem e para no portão do navegador.
	e, err := s.Conectar(context.Background(), "http://localhost:"+porta)
	if !errors.Is(err, nuvem.ErrNaoAbriu) || !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Conectar: err = %v; quero ErrNaoAbriu com sysinput.ErrDisabledInTest", err)
	}
	if e.Estado != nuvem.Conectando || e.Aviso != nuvem.AvisoNaoAbriu {
		t.Fatalf("estado = %+v", e)
	}

	// 2. O retorno troca o code e para no portão do Gerenciador: o token
	// não é guardado, e o aparelho é revogado na nuvem com ele.
	rr := httptest.NewRecorder()
	s.Retorno().ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/nuvem/retorno?code=%s&state=%s", strings.Repeat("A", 43), pedido), nil))
	if rr.Code == http.StatusOK {
		t.Fatalf("o retorno conectou sob go test: %d", rr.Code)
	}
	mu.Lock()
	if trocas != 1 || len(deletes) != 1 || deletes[0] != "/api/v1/devices/0192f0a1-0000-7000-8000-0000000a9a1e "+token {
		t.Errorf("trocas %d, revogações %v", trocas, deletes)
	}
	mu.Unlock()
	for _, nome := range []string{"nuvem_aparelho", "nuvem_conta", "nuvem_url"} {
		if v, ok, err := inst.Ler(context.Background(), nome); err != nil || ok {
			t.Errorf("instalacao[%s] = %q, %v, %v: gravou sem o token", nome, v, ok, err)
		}
	}
	if e, err := s.Estado(context.Background()); err != nil || e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoFalhou {
		t.Errorf("estado depois do retorno = %+v, %v", e, err)
	}

	// 3. O Gerenciador da montagem, direto: os dois alvos, as três ações.
	g := chaves.NovoGerenciador()
	for _, alvo := range []string{nuvem.NomeNoCofreInstalado, nuvem.NomeNoCofrePortatil} {
		if err := g.Gravar(alvo, "nao-grava"); !errors.Is(err, sysinput.ErrDisabledInTest) {
			t.Errorf("Gravar(%s) = %v", alvo, err)
		}
		if _, err := g.Ler(alvo); !errors.Is(err, sysinput.ErrDisabledInTest) {
			t.Errorf("Ler(%s) = %v", alvo, err)
		}
		if err := g.Apagar(alvo); !errors.Is(err, sysinput.ErrDisabledInTest) {
			t.Errorf("Apagar(%s) = %v", alvo, err)
		}
	}
}
