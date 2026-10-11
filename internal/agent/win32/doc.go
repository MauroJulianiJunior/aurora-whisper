// Package win32 contém os bindings finos de Win32 (user32.dll e
// gdi32.dll) usados pelo hook de teclado e pelo overlay do Windows Agent
// (ver docs/adr/ADR-010-camada-nativa-windows.md, seções D2 e D3).
//
// Este pacote é deliberadamente burro: expõe tipos, constantes e funções
// que espelham a API Win32 quase um-para-um, sem nenhuma lógica de
// produto (decisão de "quem injetou o evento", política de retry,
// composição de efeitos etc. pertence às camadas que consomem este
// pacote, não a ele).
//
// As chamadas reais usam golang.org/x/sys/windows.NewLazySystemDLL e
// LazyProc.Call para resolver a DLL e a convenção de chamada stdcall, e
// windows.NewCallback para os trampolins de callback (hook proc e
// window proc) — ver ADR-010, seção D3. Nenhuma struct nem função deste
// pacote reimplementa essa fiação sobre syscall cru.
//
// Todo arquivo que chama de fato para dentro do Win32 vem em um par:
// um arquivo "_windows.go" (build tag windows) com a implementação real,
// e um arquivo "_other.go" (build tag !windows) com stubs que devolvem
// ErrNotSupported, para que o pacote — e tudo que o importa — continue
// compilando em GOOS=linux no CI. A exceção é a aritmética de conversão
// de ticks para time.Time (ticks.go), que é matemática pura sem nenhuma
// chamada de sistema e roda, e é testada, nas duas plataformas.
package win32
