// Package cofre cifra a chave de API do provedor de IA que a pessoa cola no
// app do celular (ADR-021, decisão G e C3a). A chave mora no banco só cifrada
// (ai_provider_keys); a chave de cifra é do servidor, vem de um arquivo
// (AI_KEYS_SECRET_FILE) e nunca entra no banco nem no backup.
//
// # A cifra
//
//   - A chave de cifra são 32 bytes, escritos no arquivo como 64 caracteres
//     hexadecimais (como os outros segredos do servidor). Ela nunca é usada
//     crua: a cifra usa uma subchave dela, derivada por HKDF-SHA256 com o
//     rótulo fixo RotuloDaCifra, e o id dela é um HMAC-SHA256 com outro
//     rótulo (RotuloDoID), truncado em 8 bytes.
//   - AES-256-GCM da biblioteca padrão, com um nonce aleatório de 12 bytes a
//     cada gravação.
//   - O AAD prende o texto cifrado ao usuário, ao provedor e ao id da chave
//     de cifra: o cifrado copiado para outra conta ou para outro provedor não
//     decifra.
//   - Cada linha guarda o id da chave de cifra que a cifrou, e não um número
//     de versão. Trocar o arquivo leva toda linha com o id antigo a "não
//     decifra" (ErrOutraChaveDeCifra), sem tentar: a pessoa cola de novo. Duas
//     chaves de cifra ao mesmo tempo ficam fora (ADR-021, "Fora do escopo").
//
// # O que nunca sai daqui
//
// Nenhum erro carrega a chave de cifra, a chave da pessoa, o usuário nem o
// provedor: são todos frases fixas. O pacote não loga nada. A chave aberta é
// devolvida a quem chama, que a usa só durante a chamada ao provedor; o Go não
// zera memória, então a promessa é "descartada ao fim", e não "apagada".
//
// # O que a cifra protege, e o que não protege
//
// Protege o banco que vaza (um dump, um backup, uma leitura indevida, o log
// do Postgres). Não protege de quem tem a raiz da VPS: essa pessoa lê o
// arquivo da chave de cifra e o banco, e decifra tudo (ADR-021, "Riscos").
package cofre

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

// Os rótulos de finalidade (ADR-021, decisão G). Mudar qualquer um deles é
// trocar a chave de cifra: as linhas gravadas deixam de decifrar.
const (
	// RotuloDaCifra é o info do HKDF que deriva a subchave do AES-GCM.
	RotuloDaCifra = "aurora-whisper/ai-keys/v1"
	// RotuloDoID é a mensagem do HMAC que dá o id da chave de cifra.
	RotuloDoID = "aurora-whisper/ai-keys/id/v1"
)

// Os tamanhos fixos (o CHECK da migration 0017 confere os mesmos).
const (
	// TamanhoDaChaveDeCifra: 32 bytes (64 caracteres hexadecimais no arquivo).
	TamanhoDaChaveDeCifra = 32
	// TamanhoDoID: os 8 primeiros bytes do HMAC.
	TamanhoDoID = 8
	// TamanhoDoNonce: o nonce padrão do GCM.
	TamanhoDoNonce = 12
	// TamanhoDaEtiqueta: a etiqueta do GCM, que vai no fim do texto cifrado.
	TamanhoDaEtiqueta = 16
)

// Erros do cofre. Frases fixas: nenhum traz valor.
var (
	// ErrChaveDeCifraMalformada: o conteúdo do arquivo não são 64 caracteres
	// hexadecimais. A mensagem nunca mostra o conteúdo.
	ErrChaveDeCifraMalformada = errors.New("cofre: a chave de cifra tem de ser 64 caracteres hexadecimais (32 bytes)")
	// ErrOutraChaveDeCifra: a linha foi cifrada com outra chave de cifra (o
	// arquivo foi trocado, ou o backup foi restaurado sem o mesmo arquivo).
	ErrOutraChaveDeCifra = errors.New("cofre: a chave guardada foi cifrada com outra chave de cifra")
	// ErrNaoDecifra: o id confere, mas o texto cifrado não abre com este
	// usuário e este provedor (copiado de outra linha, ou adulterado).
	ErrNaoDecifra = errors.New("cofre: a chave guardada não decifra")
	// ErrVazia: não há o que cifrar, ou falta o usuário ou o provedor.
	ErrVazia = errors.New("cofre: falta a chave, o usuário ou o provedor")
)

// Cofre cifra e decifra com uma chave de cifra. Seguro para uso concorrente
// (o AEAD do GCM é).
type Cofre struct {
	aead cipher.AEAD
	id   [TamanhoDoID]byte
}

// Selada é a chave cifrada, como a linha da ai_provider_keys a guarda.
type Selada struct {
	// IDDaCifra é o id da chave de cifra que a cifrou (TamanhoDoID bytes).
	IDDaCifra []byte
	// Nonce tem TamanhoDoNonce bytes, aleatórios a cada gravação.
	Nonce []byte
	// Cifrado é o texto cifrado com a etiqueta do GCM no fim.
	Cifrado []byte
}

// Novo monta o cofre a partir do conteúdo do arquivo da chave de cifra:
// exatamente 64 caracteres hexadecimais (maiúsculas ou minúsculas), sem nada
// em volta (quem lê o arquivo já tirou o fim de linha). Qualquer outra coisa é
// ErrChaveDeCifraMalformada, sem mostrar o conteúdo.
func Novo(hexadecimal string) (*Cofre, error) {
	if len(hexadecimal) != 2*TamanhoDaChaveDeCifra {
		return nil, ErrChaveDeCifraMalformada
	}
	segredo, err := hex.DecodeString(hexadecimal)
	if err != nil || len(segredo) != TamanhoDaChaveDeCifra {
		return nil, ErrChaveDeCifraMalformada
	}
	subchave, err := hkdf.Key(sha256.New, segredo, nil, RotuloDaCifra, TamanhoDaChaveDeCifra)
	if err != nil {
		return nil, ErrChaveDeCifraMalformada
	}
	bloco, err := aes.NewCipher(subchave)
	if err != nil {
		return nil, ErrChaveDeCifraMalformada
	}
	aead, err := cipher.NewGCM(bloco)
	if err != nil {
		return nil, ErrChaveDeCifraMalformada
	}
	c := &Cofre{aead: aead}
	copy(c.id[:], idDe(segredo))
	return c, nil
}

// idDe é o id da chave de cifra: o HMAC-SHA256 dela sobre RotuloDoID,
// truncado.
func idDe(segredo []byte) []byte {
	m := hmac.New(sha256.New, segredo)
	m.Write([]byte(RotuloDoID))
	return m.Sum(nil)[:TamanhoDoID]
}

// ID devolve uma cópia do id da chave de cifra deste cofre.
func (c *Cofre) ID() []byte { return append([]byte(nil), c.id[:]...) }

// MesmoID diz se id é o desta chave de cifra (o "a pessoa precisa colar de
// novo" que a tela mostra sem decifrar nada).
func (c *Cofre) MesmoID(id []byte) bool {
	return len(id) == TamanhoDoID && subtle.ConstantTimeCompare(id, c.id[:]) == 1
}

// Selar cifra a chave de userID para provedor, com um nonce novo.
func (c *Cofre) Selar(userID, provedor, chave string) (Selada, error) {
	if userID == "" || provedor == "" || chave == "" {
		return Selada{}, ErrVazia
	}
	nonce := make([]byte, TamanhoDoNonce)
	if _, err := rand.Read(nonce); err != nil {
		return Selada{}, errors.New("cofre: não consegui gerar o nonce")
	}
	id := c.ID()
	return Selada{
		IDDaCifra: id,
		Nonce:     nonce,
		Cifrado:   c.aead.Seal(nil, nonce, []byte(chave), aad(userID, provedor, id)),
	}, nil
}

// Abrir decifra a chave de userID para provedor. Outro id de chave de cifra é
// ErrOutraChaveDeCifra (sem tentar); o resto do que não abre (outro usuário,
// outro provedor, um byte trocado, um nonce do tamanho errado) é
// ErrNaoDecifra.
func (c *Cofre) Abrir(userID, provedor string, s Selada) (string, error) {
	if !c.MesmoID(s.IDDaCifra) {
		return "", ErrOutraChaveDeCifra
	}
	if userID == "" || provedor == "" || len(s.Nonce) != TamanhoDoNonce || len(s.Cifrado) <= TamanhoDaEtiqueta {
		return "", ErrNaoDecifra
	}
	claro, err := c.aead.Open(nil, s.Nonce, s.Cifrado, aad(userID, provedor, s.IDDaCifra))
	if err != nil {
		return "", ErrNaoDecifra
	}
	return string(claro), nil
}

// aad são os dados associados do GCM: o rótulo, o usuário, o provedor e o id
// da chave de cifra, cada um com o tamanho na frente (sem separador que um
// valor pudesse imitar).
func aad(userID, provedor string, id []byte) []byte {
	var b []byte
	for _, parte := range [][]byte{[]byte(RotuloDaCifra), []byte(userID), []byte(provedor), id} {
		b = binary.AppendUvarint(b, uint64(len(parte)))
		b = append(b, parte...)
	}
	return b
}
