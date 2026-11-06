package storage

import (
	"context"
	"errors"
)

// OpcaoDaCopiaDoDicionario é a opção "Levar o dicionário para o celular"
// (local_settings.dictionary_copy_enabled, ADR-023, F) como o serviço da nuvem e
// o sync a usam: ler e gravar só ela, sobre o SettingsRepository, sem tocar nas
// outras preferências. Quem muda as outras é o PUT /api/settings, que preserva
// este campo.
type OpcaoDaCopiaDoDicionario struct {
	repo   SettingsRepository
	padrao Settings
}

// maxTentativasDaOpcao: quantas vezes a gravação relê a versão depois de um
// conflito (o painel salvou outra preferência no mesmo instante).
const maxTentativasDaOpcao = 5

// NovaOpcaoDaCopiaDoDicionario monta a opção sobre repo. padrao é o que o Agent
// usa antes do primeiro PUT /api/settings: só serve para criar a linha quando a
// opção é ligada antes de qualquer preferência ter sido salva.
func NovaOpcaoDaCopiaDoDicionario(repo SettingsRepository, padrao Settings) *OpcaoDaCopiaDoDicionario {
	if repo == nil {
		panic("storage: repo nil")
	}
	return &OpcaoDaCopiaDoDicionario{repo: repo, padrao: padrao}
}

// Ligada lê a opção. Sem preferências salvas, é desligada (o padrão).
func (o *OpcaoDaCopiaDoDicionario) Ligada(ctx context.Context) (bool, error) {
	s, err := o.repo.Get(ctx)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.DictionaryCopyEnabled, nil
}

// Gravar grava a opção. Já estar no valor pedido não grava nada (não sobe a
// versão das preferências à toa), e desligar sem preferências salvas também não.
func (o *OpcaoDaCopiaDoDicionario) Gravar(ctx context.Context, ligada bool) error {
	for range maxTentativasDaOpcao {
		cur, err := o.repo.Get(ctx)
		versao := cur.Version
		switch {
		case errors.Is(err, ErrNotFound):
			if !ligada {
				return nil
			}
			cur, versao = o.padrao, 0
		case err != nil:
			return err
		case cur.DictionaryCopyEnabled == ligada:
			return nil
		}
		cur.DictionaryCopyEnabled = ligada
		if _, err := o.repo.Put(ctx, cur, versao); err != nil {
			if errors.Is(err, ErrVersionConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return ErrVersionConflict
}
