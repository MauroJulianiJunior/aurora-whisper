package overlay

import "fmt"

// Este arquivo não tem build tag: é a cópia do quadro para a superfície de
// desenho da janela (o DIB), feita em Go puro sobre bytes. Fica fora de
// window_windows.go para ser testável — superficie_test.go.

// copiaParaSuperficie copia c para o canto superior esquerdo de pix, uma
// superfície BGRA de largura x altura pixels, linha a linha.
//
// A superfície tem o tamanho da MAIOR pílula (TamanhoDaSuperficie) e o
// quadro, o da pílula da vez: cada linha de c ocupa o começo de uma linha
// da superfície, que tem passo largura*4. O resto de cada linha, e as
// linhas abaixo de c.H, ficam com o que houver — o UpdateLayeredWindow só
// apresenta o retângulo c.W x c.H a partir do canto (window_windows.go),
// então nada de uma pílula anterior, maior, aparece em volta da atual.
//
// Um quadro maior que a superfície é erro de quem chama, e é devolvido em
// vez de escrever fora dela.
func copiaParaSuperficie(pix []byte, largura, altura int, c *Canvas) error {
	if c.W > largura || c.H > altura {
		return fmt.Errorf("overlay: quadro %dx%d não cabe na superfície %dx%d", c.W, c.H, largura, altura)
	}
	if len(pix) < largura*altura*4 {
		return fmt.Errorf("overlay: superfície com %d bytes, precisa de %d", len(pix), largura*altura*4)
	}
	linha := c.W * 4
	for y := 0; y < c.H; y++ {
		copy(pix[y*largura*4:y*largura*4+linha], c.Pix[y*linha:(y+1)*linha])
	}
	return nil
}
