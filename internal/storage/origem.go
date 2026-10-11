package storage

// A origem de um ditado no histórico do PC (ADR-021, C1).
//
// O PC sempre soube de onde veio um ditado pelo device_id: nulo é "nasceu
// aqui", e um aparelho é "veio de outro PC pela nuvem". O ditado que o celular
// cria na nuvem (cloudstore.Store.Create) também chega sem aparelho, e o PC o
// recebe pelo pull como qualquer outro. Para ele não passar por um ditado
// deste PC (o envio inicial o levaria a uma conta nova, e o "Esquecer esta
// nuvem" o contaria como seu), a origem entra na conta: quem nasceu na nuvem
// diz isso em source_platform, numa lista fechada que é a do servidor
// (cloudstore.SourcePlatformWeb e SourcePlatformAndroid). O PC grava "windows".
const (
	// PlataformaWeb é o ditado do app do celular (o PWA): no histórico do PC,
	// "pelo app".
	PlataformaWeb = "web"
	// PlataformaAndroid é o do app nativo do passo 2 da ADR-021.
	PlataformaAndroid = "android"
)

// plataformasDaNuvemSQL é a lista das origens que nasceram na nuvem, para o SQL.
const plataformasDaNuvemSQL = `'` + PlataformaWeb + `', '` + PlataformaAndroid + `'`

// nasceuAquiSQL é o filtro de "nasceu neste PC": sem aparelho de origem e sem
// ser um ditado que nasceu na nuvem. É um só, para o envio inicial, o
// "Esquecer esta nuvem" e a fila do sync (emitir) concordarem. O nasceuAquiT é
// o mesmo para a tabela com o apelido t.
const (
	nasceuAquiSQL  = `device_id IS NULL AND source_platform NOT IN (` + plataformasDaNuvemSQL + `)`
	nasceuAquiTSQL = `t.device_id IS NULL AND t.source_platform NOT IN (` + plataformasDaNuvemSQL + `)`
)
