package nuvem

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/textproto"
)

// campo é uma parte de texto do multipart.
type campo struct{ nome, valor string }

// corpoMultipart é um multipart/form-data de TAMANHO CONHECIDO, montado de
// partes prontas: nada é copiado para um buffer grande.
type corpoMultipart struct {
	// leitor entrega, em ordem, o cabeçalho das partes (todos os campos e o
	// começo da parte do arquivo), os bytes do áudio e o fecho.
	leitor io.Reader
	// tamanho é o total de bytes de leitor: o Content-Length do pedido.
	tamanho int64
	// contentType é o Content-Type do pedido, com o boundary.
	contentType string
}

// montarCorpo monta o multipart com os campos de texto e, por ÚLTIMO, o
// arquivo. O arquivo vai por último para que o corpo seja três pedaços — o
// cabeçalho, o áudio e o fecho —, e o áudio entra como está: bytes.NewReader
// lê o slice recebido, sem copiá-lo. É isso que permite o Content-Length
// calculado sem uma segunda cópia do áudio na memória (ADR-021, decisão D).
//
// O cabeçalho e o fecho vêm do próprio mime/multipart (o boundary, as aspas e
// o escape do nome ficam por conta dele): escreve-se tudo até a abertura da
// parte do arquivo num buffer pequeno, guarda-se uma cópia dele, e o Close
// escreve o fecho no mesmo buffer, esvaziado. O tamanho do buffer é o dos
// campos de texto (algumas centenas de bytes), nunca o do áudio.
func montarCorpo(campos []campo, nomeArquivo, tipoArquivo string, audio []byte) (corpoMultipart, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, c := range campos {
		if err := w.WriteField(c.nome, c.valor); err != nil {
			return corpoMultipart{}, err
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", multipart.FileContentDisposition("file", nomeArquivo))
	h.Set("Content-Type", tipoArquivo)
	if _, err := w.CreatePart(h); err != nil {
		return corpoMultipart{}, err
	}
	cabeca := append([]byte(nil), buf.Bytes()...)

	buf.Reset()
	if err := w.Close(); err != nil {
		return corpoMultipart{}, err
	}
	fecho := append([]byte(nil), buf.Bytes()...)

	return corpoMultipart{
		leitor: io.MultiReader(
			bytes.NewReader(cabeca),
			bytes.NewReader(audio),
			bytes.NewReader(fecho),
		),
		tamanho:     int64(len(cabeca)) + int64(len(audio)) + int64(len(fecho)),
		contentType: w.FormDataContentType(),
	}, nil
}
