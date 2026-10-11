package main

import (
	"errors"
	"strings"
)

// LeitorChave lê uma chave do terminal com o eco desligado. É uma INTERFACE de
// propósito: ligar e desligar o eco (SetConsoleMode) muda o estado do console,
// que é global da máquina, e nenhum teste pode fazer isso (CLAUDE.md, "Testes
// que agem no sistema real"). Só o binário de verdade usa a implementação do
// console (chave_windows.go); todo teste usa uma falsa.
type LeitorChave interface {
	// Ler mostra o prompt, lê uma linha sem ecoar o que se digita e a devolve
	// sem o fim de linha nem os espaços das pontas. Vazio = "Enter pula".
	// Devolve ErrInterrompido se a pessoa apertou Ctrl+C.
	Ler(prompt string) (string, error)
}

// ErrInterrompido é o erro do leitor quando a pessoa cancela.
var ErrInterrompido = errors.New("interrompido")

// menorChaveMascarada é o tamanho mínimo de uma chave para o mascaramento
// trocá-la por texto: uma "chave" de 3 letras trocaria pedaços de palavra.
const menorChaveMascarada = 6

// mascarar troca, em s, cada chave informada por "[chave]". É a última
// defesa: nenhum caminho do programa escreve a chave, mas o relatório leva
// texto que veio de fora (a saída do modelo), e o que sai do programa passa
// por aqui.
func mascarar(s string, chaves []string) string {
	for _, c := range chaves {
		if len(c) >= menorChaveMascarada {
			s = strings.ReplaceAll(s, c, "[chave]")
		}
	}
	return s
}
