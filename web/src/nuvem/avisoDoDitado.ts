// A versão do texto do aviso do ditado no celular (ADR-021, decisão H e C3b).
//
// A folha do aceite (C6b) mostra um texto específico: o que vai ao provedor, a
// quem e por quanto tempo. O servidor guarda o aceite com a versão desse texto
// (POST /api/v1/dictation/consent, campo "versao") e só o aceita se for a do
// aviso atual: um app com o texto velho na memória não pode registrar que a
// pessoa aceitou o texto novo (409, AVISO_DESATUALIZADO). Mudar o texto da
// folha é subir este número, e o servidor passa a pedir o aceite de novo a
// todas as contas.
//
// A MESMA versão mora no servidor, em cloudstore.VersaoDoAviso
// (internal/server/cloudstore/ditado_celular.go). Um teste do Go
// (TestVersaoDoAviso_AppEServidorConferem) lê ESTE arquivo e prende os dois
// números: subir um sem o outro quebra a suíte, e não o primeiro aceite de
// alguém. Por isso a linha abaixo tem de continuar com esta forma.
export const VERSAO_DO_AVISO = 1
