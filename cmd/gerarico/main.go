// Command gerarico gera o ícone do Aurora Whisper para o executável do Agent e
// para o instalador, a partir do símbolo "Pico estourado" que a bandeja já
// desenha em Go (internal/agent/tray, PixelsDoIcone; o desenho de referência é
// docs/marca/pico-estourado/mark-small.svg), sem imagem de origem, sem cgo e
// sem dependência nova.
//
// Escreve estes arquivos, que ficam no repositório (são pequenos e o build não
// depende de rodar este comando):
//
//   - installer/aurora-whisper.ico: o ícone do Setup (SetupIconFile do
//     installer/aurora-whisper.iss), com as camadas de 16, 24, 32, 48, 64 e
//     256 px;
//   - cmd/agent/icone_windows_amd64.syso: os mesmos bytes como recurso de
//     ícone num objeto COFF que o `go build` do Agent liga sozinho (só para
//     Windows x64: o sufixo do nome o limita). É o ícone do aurora-whisper-agent.exe
//     no Explorador, no Menu Iniciar e em "Aplicativos" do Windows, e o que o
//     UninstallDisplayIcon do instalador aponta;
//   - web/pwa/icone-192.png, icone-512.png e icone-maskable-512.png: os ícones
//     do app instalável da nuvem (ADR-019, F5), que o manifest aponta (pwa.go).
//
// # Como regenerar
//
// Da raiz do repositório, depois de mudar o desenho do ícone da bandeja ou o
// ladrilho (desenho.go):
//
//	go run ./cmd/gerarico
//
// e confira o diff dos arquivos. Um teste (gerado_test.go) falha se os
// arquivos do repositório não forem os que este comando gera hoje, camada por
// camada (compara os pixels, e não os bytes: um PNG pode mudar de bytes com a
// versão do Go sem mudar de desenho).
//
// -ico, -syso e -pwa trocam os destinos; os testes usam pastas temporárias.
// Este comando só escreve estes arquivos: não abre janela, não toca o
// registro, não executa nada.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	ico := flag.String("ico", "installer/aurora-whisper.ico", "onde escrever o .ico do instalador")
	syso := flag.String("syso", "cmd/agent/icone_windows_amd64.syso", "onde escrever o recurso de ícone do Agent (.syso)")
	pwa := flag.String("pwa", "web/pwa", "a pasta dos ícones do app instalável da nuvem")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "gerarico: argumento sobrando %q\n", flag.Arg(0))
		os.Exit(2)
	}
	if err := gerar(*ico, *syso, *pwa); err != nil {
		fmt.Fprintln(os.Stderr, "gerarico:", err)
		os.Exit(1)
	}
	fmt.Printf("gerarico: %s, %s e os ícones do app em %s escritos\n", *ico, *syso, *pwa)
}

// gerar desenha as camadas e escreve o .ico, o .syso e os ícones do app. A
// pasta de cada destino tem de existir (rode da raiz do repositório): não cria
// pasta nenhuma.
func gerar(caminhoIco, caminhoSyso, pastaPWA string) error {
	imgs, err := imagensDoIcone()
	if err != nil {
		return err
	}
	syso, err := montarSyso(imgs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(caminhoIco, montarICO(imgs), 0o644); err != nil {
		return fmt.Errorf("%w (rode da raiz do repositório)", err)
	}
	if err := os.WriteFile(caminhoSyso, syso, 0o644); err != nil {
		return fmt.Errorf("%w (rode da raiz do repositório)", err)
	}
	return gerarIconesDoApp(pastaPWA)
}
