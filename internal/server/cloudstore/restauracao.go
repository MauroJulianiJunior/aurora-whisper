package cloudstore

import (
	"context"
	"time"
)

// Depois de restaurar um backup da nuvem (ADR-019, "Decisões da F11", F11d;
// 12-rollout.md, "Rollback"). O backup é a cópia do banco de uma noite; ao
// voltar a ele:
//   - cada conta ganha um sync_epoch NOVO (UUID v7), pelo mesmo comando do
//     "apagar meus dados" (o epoch troca e a marca d'água das lápides sobe ao
//     last_seq). O PC que lê o epoch novo recomeça (Fila.Recomecar): cursor 0,
//     as versões da nuvem que ele guardou deixam de valer, e ele reenvia o
//     que tem com a base vazia. Sem a troca, o PC continuaria do cursor de
//     antes, num seq que o banco restaurado não tem, com versões que não batem;
//   - todas as sessões e todos os aparelhos são revogados: o backup guarda o
//     revoked_at antigo, e um aparelho revogado (ou uma sessão derrubada)
//     depois do backup voltaria a valer. Todo PC reconecta à mão e o celular
//     entra de novo (o "revoke massivo" do 09, que pede aprovação: o roteiro
//     de restauração diz isso antes de o dono aprovar).
//
// O que foi apagado DEPOIS do backup pode voltar (a decisão do Mauro de
// 09-out-2026, "aceitar e deixar escrito"): o ditado excluído de vez depois
// do backup está vivo no banco restaurado, e o pull do 0 o devolve ao PC. O
// que a janela de 6 meses já venceu, o Reter apaga antes de a porta abrir
// (F11c).

// Restauracao conta o que DepoisDeRestaurar fez. Só números: o comando
// pos-restauracao imprime isto e nada mais (nenhum id, e-mail nem nome).
type Restauracao struct {
	// Contas com o sync_epoch trocado.
	Contas int64
	// Sessoes derrubadas (todas as que havia).
	Sessoes int64
	// Aparelhos revogados agora (os que ainda não estavam).
	Aparelhos int64
}

// Restaurador é o passo de depois de restaurar um backup. O Store do Postgres
// (pgstore) e o em memória o implementam.
type Restaurador interface {
	// DepoisDeRestaurar troca o sync_epoch de TODAS as contas (um UUID v7 novo
	// cada, com a marca d'água das lápides subindo ao last_seq), apaga todas
	// as sessões e revoga todos os aparelhos (revoked_at = now, versão mais um,
	// os tokens apagados), numa transação só. Os ditados, as lápides, os
	// eventos e o aprendido ficam como o backup os trouxe.
	DepoisDeRestaurar(ctx context.Context, now time.Time) (Restauracao, error)
}
