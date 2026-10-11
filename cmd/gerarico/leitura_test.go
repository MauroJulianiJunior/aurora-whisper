package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"image/color"
	"image/png"
)

// Leitores de teste: o .ico e o .syso são lidos de volta por código INDEPENDENTE
// do que os escreve (o syso, pelo debug/pe da biblioteca padrão e por um
// percurso da árvore de recursos escrito aqui), para um erro de formato não
// passar só por ser simétrico.

var assinaturaPNG = []byte("\x89PNG\r\n\x1a\n")

// camadaLida é uma camada do .ico, decodificada.
type camadaLida struct {
	Tam   int
	Dados []byte // os bytes crus da camada (DIB ou PNG)
	PNG   bool
	Pix   []byte // BGRA, alfa direto
}

// decodificarCamada lê os bytes de uma camada: um PNG, ou o bitmap (DIB) de um
// ícone. Falha se o formato não for o que o gerador promete.
func decodificarCamada(dados []byte) (camadaLida, error) {
	if bytes.HasPrefix(dados, assinaturaPNG) {
		img, err := png.Decode(bytes.NewReader(dados))
		if err != nil {
			return camadaLida{}, fmt.Errorf("png: %w", err)
		}
		b := img.Bounds()
		if b.Dx() != b.Dy() {
			return camadaLida{}, fmt.Errorf("png %dx%d não é quadrado", b.Dx(), b.Dy())
		}
		pix := make([]byte, 0, b.Dx()*b.Dy()*4)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				pix = append(pix, c.B, c.G, c.R, c.A)
			}
		}
		return camadaLida{Tam: b.Dx(), Dados: dados, PNG: true, Pix: pix}, nil
	}

	le := binary.LittleEndian
	if len(dados) < 40 {
		return camadaLida{}, fmt.Errorf("DIB com %d bytes, sem cabeçalho", len(dados))
	}
	if tamCab := le.Uint32(dados[0:]); tamCab != 40 {
		return camadaLida{}, fmt.Errorf("biSize = %d, quero 40", tamCab)
	}
	largura, altura := int(int32(le.Uint32(dados[4:]))), int(int32(le.Uint32(dados[8:])))
	if altura != 2*largura || largura <= 0 {
		return camadaLida{}, fmt.Errorf("DIB %dx%d: a altura deve ser o dobro da largura (XOR + AND)", largura, altura)
	}
	if le.Uint16(dados[12:]) != 1 || le.Uint16(dados[14:]) != 32 || le.Uint32(dados[16:]) != 0 {
		return camadaLida{}, fmt.Errorf("DIB: planos/bits/compressão inesperados (%d, %d, %d)", le.Uint16(dados[12:]), le.Uint16(dados[14:]), le.Uint32(dados[16:]))
	}
	tam := largura
	linhaMascara := ((tam + 31) / 32) * 4
	xor := tam * tam * 4
	if quer := 40 + xor + linhaMascara*tam; len(dados) != quer {
		return camadaLida{}, fmt.Errorf("DIB tem %d bytes, quero %d", len(dados), quer)
	}
	if got, quer := int(le.Uint32(dados[20:])), xor+linhaMascara*tam; got != quer {
		return camadaLida{}, fmt.Errorf("biSizeImage = %d, quero %d", got, quer)
	}
	pix := make([]byte, xor)
	for yArq := 0; yArq < tam; yArq++ { // de baixo para cima
		y := tam - 1 - yArq
		copy(pix[y*tam*4:(y+1)*tam*4], dados[40+yArq*tam*4:40+(yArq+1)*tam*4])
	}
	mascara := dados[40+xor:]
	for yArq := 0; yArq < tam; yArq++ {
		y := tam - 1 - yArq
		for x := 0; x < tam; x++ {
			bit := mascara[yArq*linhaMascara+x/8]>>(7-uint(x%8))&1 == 1
			if transp := pix[(y*tam+x)*4+3] == 0; bit != transp {
				return camadaLida{}, fmt.Errorf("DIB: a máscara AND de (%d, %d) é %v e o alfa diz transparente=%v", x, y, bit, transp)
			}
		}
	}
	return camadaLida{Tam: tam, Dados: dados, Pix: pix}, nil
}

// entradaDoIco é uma ICONDIRENTRY.
type entradaDoIco struct {
	largura, altura  byte
	cores, reservado byte
	planos, bits     uint16
	bytes, desloc    uint32
}

// lerICO lê o arquivo .ico: confere o cabeçalho, cada entrada e o que ela
// aponta, e decodifica as camadas.
func lerICO(b []byte) ([]camadaLida, []entradaDoIco, error) {
	le := binary.LittleEndian
	if len(b) < 6 || le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 {
		return nil, nil, fmt.Errorf("não é um .ico (ICONDIR com reservado/tipo inválidos)")
	}
	n := int(le.Uint16(b[4:]))
	if len(b) < 6+16*n {
		return nil, nil, fmt.Errorf("arquivo curto: %d bytes para %d entradas", len(b), n)
	}
	var camadas []camadaLida
	var entradas []entradaDoIco
	fim := 6 + 16*n // o primeiro dado começa logo depois das entradas
	for i := 0; i < n; i++ {
		e := b[6+16*i:]
		ent := entradaDoIco{e[0], e[1], e[2], e[3], le.Uint16(e[4:]), le.Uint16(e[6:]), le.Uint32(e[8:]), le.Uint32(e[12:])}
		entradas = append(entradas, ent)
		if int(ent.desloc) != fim || int(ent.desloc)+int(ent.bytes) > len(b) {
			return nil, nil, fmt.Errorf("entrada %d: dados em %d (+%d) fora do esperado %d / arquivo de %d bytes", i, ent.desloc, ent.bytes, fim, len(b))
		}
		c, err := decodificarCamada(b[ent.desloc : ent.desloc+ent.bytes])
		if err != nil {
			return nil, nil, fmt.Errorf("entrada %d: %w", i, err)
		}
		camadas = append(camadas, c)
		fim += int(ent.bytes)
	}
	if fim != len(b) {
		return nil, nil, fmt.Errorf("sobram %d bytes depois da última camada", len(b)-fim)
	}
	return camadas, entradas, nil
}

// iconeDoSyso é o que se acha, de volta, nos recursos de um .syso.
type iconeDoSyso struct {
	Icones map[uint32][]byte // RT_ICON, por id
	Grupo  []byte            // o RT_GROUP_ICON de id 1
}

// lerSyso lê o objeto COFF com o debug/pe, confere cabeçalho, seção, relocações
// e símbolo, e percorre a árvore de recursos.
func lerSyso(b []byte) (*iconeDoSyso, error) {
	f, err := pe.NewFile(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("debug/pe não leu o objeto: %w", err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return nil, fmt.Errorf("Machine = %#x, quero amd64 (%#x)", f.Machine, pe.IMAGE_FILE_MACHINE_AMD64)
	}
	if len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
		return nil, fmt.Errorf("quero uma única seção .rsrc, achei %d", len(f.Sections))
	}
	sec := f.Sections[0]
	// O que o ligador do Go aceita para a seção de recursos (loadpe).
	if sec.Characteristics != pe.IMAGE_SCN_CNT_INITIALIZED_DATA|pe.IMAGE_SCN_MEM_READ {
		return nil, fmt.Errorf("Characteristics = %#x", sec.Characteristics)
	}
	if len(f.COFFSymbols) != 1 || f.COFFSymbols[0].StorageClass != 3 || f.COFFSymbols[0].SectionNumber != 1 ||
		f.COFFSymbols[0].Type != 0 || f.COFFSymbols[0].NumberOfAuxSymbols != 0 || f.COFFSymbols[0].Value != 0 || f.COFFSymbols[0].Name[0] != '.' {
		return nil, fmt.Errorf("a tabela de símbolos devia ter só o símbolo da seção, estático: %+v", f.COFFSymbols)
	}
	dados, err := sec.Data()
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	relocadas := map[uint32]bool{} // campos OffsetToData que o ligador vai acertar
	for _, r := range sec.Relocs {
		if r.Type != 3 || r.SymbolTableIndex != 0 { // IMAGE_REL_AMD64_ADDR32NB, o símbolo da seção
			return nil, fmt.Errorf("relocação inesperada: %+v", r)
		}
		relocadas[r.VirtualAddress] = true
	}

	diretorio := func(off int) (map[uint32]uint32, error) {
		if off < 0 || off+16 > len(dados) {
			return nil, fmt.Errorf("diretório em %d fora da seção (%d bytes)", off, len(dados))
		}
		if nomeadas := le.Uint16(dados[off+12:]); nomeadas != 0 {
			return nil, fmt.Errorf("diretório em %d tem %d entradas nomeadas; só há ids", off, nomeadas)
		}
		n := int(le.Uint16(dados[off+14:]))
		if off+16+8*n > len(dados) {
			return nil, fmt.Errorf("diretório em %d com %d entradas passa do fim da seção", off, n)
		}
		out := map[uint32]uint32{}
		anterior := int64(-1)
		for i := 0; i < n; i++ {
			id, dest := le.Uint32(dados[off+16+8*i:]), le.Uint32(dados[off+20+8*i:])
			if int64(id) <= anterior {
				return nil, fmt.Errorf("diretório em %d: ids fora de ordem crescente", off)
			}
			anterior = int64(id)
			out[id] = dest
		}
		return out, nil
	}
	// dado percorre tipo → id → idioma → entrada de dados, e devolve os bytes
	// de cada id do tipo.
	dado := func(destino uint32) ([]byte, error) {
		off := int(destino)
		if off+16 > len(dados) {
			return nil, fmt.Errorf("entrada de dados em %d fora da seção", off)
		}
		if !relocadas[uint32(off)] {
			return nil, fmt.Errorf("a entrada de dados em %d não tem relocação: o OffsetToData ficaria um deslocamento, e não um RVA", off)
		}
		inicio, tam := int(le.Uint32(dados[off:])), int(le.Uint32(dados[off+4:]))
		if inicio < 0 || inicio+tam > len(dados) || inicio%4 != 0 {
			return nil, fmt.Errorf("dado em %d (+%d) fora da seção ou desalinhado", inicio, tam)
		}
		if le.Uint32(dados[off+8:]) != 0 || le.Uint32(dados[off+12:]) != 0 {
			return nil, fmt.Errorf("CodePage/Reserved não são zero")
		}
		return dados[inicio : inicio+tam], nil
	}
	porTipo := func(tipo uint32, raiz map[uint32]uint32) (map[uint32][]byte, error) {
		d, ok := raiz[tipo]
		if !ok || d&flagSubdir == 0 {
			return nil, fmt.Errorf("a raiz não tem o tipo %d como subdiretório", tipo)
		}
		ids, err := diretorio(int(d &^ flagSubdir))
		if err != nil {
			return nil, err
		}
		out := map[uint32][]byte{}
		for id, dest := range ids {
			if dest&flagSubdir == 0 {
				return nil, fmt.Errorf("tipo %d id %d: esperava um diretório de idioma", tipo, id)
			}
			idiomas, err := diretorio(int(dest &^ flagSubdir))
			if err != nil {
				return nil, err
			}
			if len(idiomas) != 1 {
				return nil, fmt.Errorf("tipo %d id %d: %d idiomas, quero 1", tipo, id, len(idiomas))
			}
			for lang, d := range idiomas {
				if lang != idiomaEUA || d&flagSubdir != 0 {
					return nil, fmt.Errorf("tipo %d id %d: idioma %#x ou destino %#x inesperados", tipo, id, lang, d)
				}
				bs, err := dado(d)
				if err != nil {
					return nil, fmt.Errorf("tipo %d id %d: %w", tipo, id, err)
				}
				out[id] = bs
			}
		}
		return out, nil
	}

	raiz, err := diretorio(0)
	if err != nil {
		return nil, err
	}
	if len(raiz) != 2 {
		return nil, fmt.Errorf("a raiz tem %d tipos, quero 2 (RT_ICON e RT_GROUP_ICON)", len(raiz))
	}
	icones, err := porTipo(rtIcon, raiz)
	if err != nil {
		return nil, err
	}
	grupos, err := porTipo(rtGroupIcon, raiz)
	if err != nil {
		return nil, err
	}
	if len(grupos) != 1 || grupos[idDoGrupo] == nil {
		return nil, fmt.Errorf("RT_GROUP_ICON devia ter só o id %d", idDoGrupo)
	}
	// Toda entrada de dados tem a sua relocação, e nenhuma sobra.
	if quer := len(icones) + len(grupos); len(relocadas) != quer {
		return nil, fmt.Errorf("%d relocações para %d entradas de dados", len(relocadas), quer)
	}
	return &iconeDoSyso{Icones: icones, Grupo: grupos[idDoGrupo]}, nil
}

// camadasDoSyso lê o grupo e os ícones de um .syso e devolve as camadas na
// ordem do grupo, conferindo que cada entrada do grupo descreve o ícone a que
// aponta (tamanho, id e quantidade de bytes).
func camadasDoSyso(b []byte) ([]camadaLida, error) {
	ic, err := lerSyso(b)
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	g := ic.Grupo
	if len(g) < 6 || le.Uint16(g[0:]) != 0 || le.Uint16(g[2:]) != 1 {
		return nil, fmt.Errorf("GRPICONDIR inválido")
	}
	n := int(le.Uint16(g[4:]))
	if len(g) != 6+14*n || n != len(ic.Icones) {
		return nil, fmt.Errorf("o grupo tem %d entradas em %d bytes, e há %d ícones", n, len(g), len(ic.Icones))
	}
	var out []camadaLida
	for i := 0; i < n; i++ {
		e := g[6+14*i:]
		lado, planos, bits, tamanho, id := e[0], le.Uint16(e[4:]), le.Uint16(e[6:]), le.Uint32(e[8:]), le.Uint16(e[12:])
		dados, ok := ic.Icones[uint32(id)]
		if !ok {
			return nil, fmt.Errorf("a entrada %d do grupo aponta para o RT_ICON %d, que não existe", i, id)
		}
		if int(tamanho) != len(dados) || planos != 1 || bits != 32 || e[1] != lado || e[2] != 0 || e[3] != 0 {
			return nil, fmt.Errorf("a entrada %d do grupo não descreve o RT_ICON %d (bytes %d × %d)", i, id, tamanho, len(dados))
		}
		c, err := decodificarCamada(dados)
		if err != nil {
			return nil, fmt.Errorf("RT_ICON %d: %w", id, err)
		}
		if lado != ladoNoIco(c.Tam) {
			return nil, fmt.Errorf("a entrada %d do grupo diz lado %d e o ícone tem %d px", i, lado, c.Tam)
		}
		out = append(out, c)
	}
	return out, nil
}
