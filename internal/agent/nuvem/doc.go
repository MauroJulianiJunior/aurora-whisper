// Package nuvem é o lado do PC de "conectar à nuvem" (ADR-019, "Conectar o
// PC"; fatia F6): o pareamento por retorno a 127.0.0.1 com PKCE (RFC 8252,
// §7.3), o token do aparelho no Gerenciador de Credenciais e o estado
// "conectado / desconectado" que o painel local mostra.
//
// # O fluxo
//
//  1. O painel local pede Conectar com a URL da nuvem. A URL é conferida
//     ANTES de qualquer rede (NormalizarURL e a Politica: o build de release
//     só aceita https; o de desenvolvimento só conecta no modo portátil, e
//     aceita https ou http://localhost).
//  2. O Agent gera o verificador do PKCE e pede o pareamento à nuvem
//     (POST /api/v1/device-auth/start) com o desafio, o nome do PC, a versão,
//     o install_id e a porta do painel local. O pedido em curso (o id, o
//     verificador e o prazo) fica SÓ NA MEMÓRIA; um Conectar novo substitui o
//     anterior, e um Agent reiniciado no meio o perde.
//  3. O Agent monta ele mesmo o endereço da página de aprovação (a resposta
//     da nuvem traz um auth_url, que é ignorado: um servidor ruim não escolhe
//     o que o PC abre) e o abre no navegador, pela função que recebe (Abrir),
//     que no Agent é painel.AbrirNoNavegador, atrás do portão sysinput.
//  4. A pessoa aprova na nuvem; o navegador volta a
//     http://127.0.0.1:<porta>/nuvem/retorno?code=…&state=… (Retorno). O state
//     tem de ser o do pedido em curso (comparado em tempo constante) e o
//     prazo não pode ter vencido; um state errado não consome nada, e o certo
//     consome o pedido ANTES da rede (uso único).
//  5. O Agent troca o code com o verificador (POST
//     /api/v1/device-auth/exchange) e recebe o token. O token vai para o
//     Cofre (no Agent, chaves.NovoGerenciador, atrás do mesmo portão), no
//     alvo do modo (NomeNoCofre), e a conta, a URL e o aparelho vão para a
//     tabela instalacao.
//
// A D3: se a instalação já está ligada a outra conta (ou outra nuvem), o
// Agent revoga o aparelho recém-criado com o token novo, não o guarda, e
// mostra a recusa.
//
// # O que nunca sai daqui
//
// O token não volta em resposta nenhuma (Estado não o tem), não vai para log
// nem para a tabela instalacao. O log diz só o que aconteceu, com códigos de
// uma lista fechada: nunca o code, o state, o pedido, o verificador, a
// conferência, o token, a URL ou o e-mail.
//
// Este pacote não tem build tag: a lógica inteira roda e é testada no Linux,
// com dublês do Cofre, do Abrir e da instalação. A chamada ao sistema real
// mora em internal/agent/chaves e internal/agent/painel, atrás do portão.
package nuvem
