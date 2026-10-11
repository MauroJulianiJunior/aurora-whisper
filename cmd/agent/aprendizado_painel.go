package main

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
)

// A API local do painel (etapa 3, fatia F3) pede ao aprendizado do Agent,
// além do que dictionary.Aprendizado já oferece, o dicionário de agora, para
// o motor de sugestão saber o que já existe: aprendiz.Aplicador.Dicionario.

var _ localapi.Aprendizado = (*aprendizadoDoAgent)(nil)
