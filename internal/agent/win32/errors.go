package win32

import "errors"

// ErrNotSupported é devolvido por toda função deste pacote quando
// chamada fora do Windows (GOOS != "windows"). Existe para que pacotes
// que importam win32 continuem compilando e com testes executáveis em
// GOOS=linux no CI (ver ADR-010, seção D3); nenhuma chamada Win32 real
// acontece nesse caminho.
var ErrNotSupported = errors.New("win32: não suportado fora do Windows")
