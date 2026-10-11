package main

import "fmt"

// O .syso é um arquivo-objeto COFF com uma seção .rsrc que o `go build` liga ao
// executável sozinho, sem cgo e sem ferramenta externa, quando o nome do arquivo
// casa com o sistema e a arquitetura (cmd/agent/icone_windows_amd64.syso: só o
// build para Windows x64 o enxerga; GOOS=linux o ignora). É assim que o
// executável do Agent ganha o ícone que o Explorador, o Menu Iniciar e "Aplicativos"
// mostram, e que o UninstallDisplayIcon do instalador aponta.
//
// Ferramentas como rsrc e goversioninfo fazem exatamente isto; escrevê-lo aqui
// são ~100 linhas e evita uma dependência. O formato é o de "PE Format" da
// Microsoft (cabeçalho COFF, tabela de seções, árvore de recursos) e foi
// conferido contra o ligador do Go (cmd/link/internal/loadpe) e, no teste,
// contra debug/pe.
//
// # A árvore de recursos
//
//	raiz
//	├── RT_ICON (3)           uma entrada por camada, id 1..n
//	│   └── id i → idioma 0x0409 → dados: os bytes da camada (DIB ou PNG)
//	└── RT_GROUP_ICON (14)    id 1 (o primeiro grupo: o ícone do arquivo)
//	    └── idioma 0x0409 → dados: o GRPICONDIR, que aponta para os ids de RT_ICON
//
// O campo OffsetToData de cada entrada de dados é um RVA, que só o ligador sabe;
// por isso cada um leva uma relocação IMAGE_REL_AMD64_ADDR32NB contra o símbolo
// da seção .rsrc, e o valor gravado no lugar é o deslocamento do dado DENTRO da
// seção (o ligador soma o RVA da seção).

const (
	rtIcon      = 3
	rtGroupIcon = 14

	idDoGrupo  = 1      // RT_GROUP_ICON id 1: o ícone do executável
	idiomaEUA  = 0x0409 // en-US, o que as ferramentas usam; o Windows o acha de qualquer idioma
	flagSubdir = 0x80000000

	maquinaAMD64      = 0x8664
	relocAddr32NB     = 0x0003     // IMAGE_REL_AMD64_ADDR32NB
	secaoDadosLeitura = 0x40000040 // CNT_INITIALIZED_DATA | MEM_READ
	classeEstatica    = 3          // IMAGE_SYM_CLASS_STATIC

	tamCabecalhoCOFF = 20
	tamSecao         = 40
	tamSimbolo       = 18
	tamRelocacao     = 10
)

// montarSyso monta o arquivo-objeto COFF (amd64) com os recursos do ícone.
func montarSyso(imgs []imagem) ([]byte, error) {
	n := len(imgs)
	if n == 0 || n > 0xFFF {
		return nil, fmt.Errorf("número de camadas inválido: %d", n)
	}

	// --- Layout da seção, em deslocamentos a partir do início dela. ---
	const (
		tamDiretorio = 16 // IMAGE_RESOURCE_DIRECTORY
		tamEntrada   = 8  // IMAGE_RESOURCE_DIRECTORY_ENTRY
		tamDado      = 16 // IMAGE_RESOURCE_DATA_ENTRY
	)
	offRaiz := 0
	offTipoIcone := offRaiz + tamDiretorio + 2*tamEntrada
	offTipoGrupo := offTipoIcone + tamDiretorio + n*tamEntrada
	offIdiomaIcone := func(i int) int { return offTipoGrupo + tamDiretorio + tamEntrada + i*(tamDiretorio+tamEntrada) }
	offIdiomaGrupo := offIdiomaIcone(n)
	offDado := func(i int) int { return offIdiomaGrupo + tamDiretorio + tamEntrada + i*tamDado } // i = n é o do grupo
	offBlobs := offDado(n + 1)

	// Os blobs: cada camada, e por fim o GRPICONDIR. Alinhados a 4 bytes.
	alinha := func(v int) int { return (v + 3) &^ 3 }
	grupo := montarGrupo(imgs)
	blobs := make([][]byte, 0, n+1)
	for _, im := range imgs {
		blobs = append(blobs, im.Dados)
	}
	blobs = append(blobs, grupo)
	offBlob := make([]int, len(blobs))
	cursor := offBlobs
	for i, b := range blobs {
		offBlob[i] = cursor
		cursor = alinha(cursor + len(b))
	}
	tamSecaoDados := cursor

	// --- Os bytes da seção. ---
	var s escritor
	diretorio := func(nIds int) {
		s.u32(0)            // Characteristics
		s.u32(0)            // TimeDateStamp: zero, o arquivo é reproduzível
		s.u16(0)            // MajorVersion
		s.u16(0)            // MinorVersion
		s.u16(0)            // NumberOfNamedEntries
		s.u16(uint16(nIds)) // NumberOfIdEntries
	}
	entrada := func(id uint32, destino uint32) { s.u32(id); s.u32(destino) }

	diretorio(2) // raiz: RT_ICON, RT_GROUP_ICON (em ordem crescente de id)
	entrada(rtIcon, flagSubdir|uint32(offTipoIcone))
	entrada(rtGroupIcon, flagSubdir|uint32(offTipoGrupo))

	diretorio(n) // RT_ICON: um id por camada
	for i := 0; i < n; i++ {
		entrada(uint32(i+1), flagSubdir|uint32(offIdiomaIcone(i)))
	}
	diretorio(1) // RT_GROUP_ICON: o grupo
	entrada(idDoGrupo, flagSubdir|uint32(offIdiomaGrupo))
	for i := 0; i < n; i++ { // idioma de cada ícone → entrada de dados
		diretorio(1)
		entrada(idiomaEUA, uint32(offDado(i)))
	}
	diretorio(1) // idioma do grupo (depois dos n dos ícones: é onde offIdiomaGrupo aponta)
	entrada(idiomaEUA, uint32(offDado(n)))

	var relocs []uint32 // onde fica o OffsetToData de cada entrada de dados
	for i := 0; i <= n; i++ {
		relocs = append(relocs, uint32(offDado(i)))
		s.u32(uint32(offBlob[i])) // OffsetToData: o deslocamento na seção; o ligador soma o RVA
		s.u32(uint32(len(blobs[i])))
		s.u32(0) // CodePage
		s.u32(0) // Reserved
	}
	if s.Len() != offBlobs {
		return nil, fmt.Errorf("layout dos recursos errado: diretórios e entradas ocupam %d bytes, esperava %d", s.Len(), offBlobs)
	}
	for i, b := range blobs {
		for s.Len() < offBlob[i] {
			s.u8(0)
		}
		s.Write(b)
	}
	for s.Len() < tamSecaoDados {
		s.u8(0)
	}

	// --- O arquivo COFF: cabeçalho, uma seção, dados, relocações, símbolos. ---
	offDados := tamCabecalhoCOFF + tamSecao
	offRelocs := offDados + s.Len()
	offSimbolos := offRelocs + tamRelocacao*len(relocs)

	var f escritor
	f.u16(maquinaAMD64)
	f.u16(1) // NumberOfSections
	f.u32(0) // TimeDateStamp
	f.u32(uint32(offSimbolos))
	f.u32(1) // NumberOfSymbols: o da seção
	f.u16(0) // SizeOfOptionalHeader: objeto, não executável
	f.u16(0) // Characteristics

	nome := [8]byte{'.', 'r', 's', 'r', 'c'}
	f.Write(nome[:])
	f.u32(0) // VirtualSize
	f.u32(0) // VirtualAddress
	f.u32(uint32(s.Len()))
	f.u32(uint32(offDados))
	f.u32(uint32(offRelocs))
	f.u32(0) // PointerToLinenumbers
	f.u16(uint16(len(relocs)))
	f.u16(0) // NumberOfLinenumbers
	f.u32(secaoDadosLeitura)

	f.Write(s.Bytes())
	for _, off := range relocs {
		f.u32(off)
		f.u32(0) // SymbolTableIndex: o símbolo da seção
		f.u16(relocAddr32NB)
	}

	// O símbolo da seção: ".rsrc", valor 0, seção 1, estático, sem auxiliar.
	f.Write(nome[:])
	f.u32(0) // Value
	f.u16(1) // SectionNumber
	f.u16(0) // Type
	f.u8(classeEstatica)
	f.u8(0) // NumberOfAuxSymbols

	f.u32(4) // a tabela de nomes longos: só o campo de tamanho (4), sem nome nenhum
	return f.Bytes(), nil
}

// montarGrupo monta o GRPICONDIR de RT_GROUP_ICON: o mesmo que o ICONDIR do
// .ico, mas cada entrada traz o id do RT_ICON (2 bytes) no lugar do
// deslocamento do dado (4 bytes).
func montarGrupo(imgs []imagem) []byte {
	var g escritor
	g.u16(0) // idReserved
	g.u16(1) // idType: ícone
	g.u16(uint16(len(imgs)))
	for i, im := range imgs {
		g.u8(ladoNoIco(im.Tam))
		g.u8(ladoNoIco(im.Tam))
		g.u8(0)  // bColorCount
		g.u8(0)  // bReserved
		g.u16(1) // wPlanes
		g.u16(32)
		g.u32(uint32(len(im.Dados)))
		g.u16(uint16(i + 1)) // nID: o id do RT_ICON desta camada
	}
	return g.Bytes()
}
