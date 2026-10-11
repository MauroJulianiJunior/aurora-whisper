//go:build windows

package audio

// Captura WASAPI em Go puro (ADR-010 D1): sem CGO, sobre syscall puro e
// vtable COM manual. Escopo deliberadamente apertado:
//
//   - IMMDeviceEnumerator -> GetDefaultAudioEndpoint (dispositivo padrão,
//     sem seleção);
//   - IAudioClient::Initialize em modo COMPARTILHADO (nunca exclusivo);
//   - IAudioClient::Start / Stop / Reset para ligar e estacionar o
//     microfone a cada ditado (ADR-015);
//   - IAudioCaptureClient::GetBuffer / ReleaseBuffer / GetNextPacketSize,
//     em polling (sem SetEventHandle) — mais simples, custo aceitável
//     para um único stream de voz, e só enquanto o microfone está ligado.
//
// Toda chamada aqui presume ser feita da mesma thread do SO que chamou
// CoInitializeEx — é responsabilidade do chamador (session.go: run())
// garantir isso com runtime.LockOSThread() ANTES de abrir o dispositivo, e
// nunca soltar a thread até fechar.
//
// Alvo é Windows 10/11 x64 (contexto do projeto): REFERENCE_TIME (int64) e
// ponteiros cabem num único registrador de 64 bits, então não há
// necessidade de partir argumentos de 64 bits entre dois de 32 — o que
// seria obrigatório num alvo x86.
//
// Nota sobre unsafe.Pointer/go vet: todo ponteiro de objeto/interface COM
// é mantido como unsafe.Pointer de ponta a ponta (comObject, e todo
// parâmetro de saída de uma chamada COM) — nunca como uintptr guardado
// entre chamadas. uintptr só aparece no limite exato de
// syscall.SyscallN/LazyProc.Call, onde a API exige. É isso que mantém
// `go vet` limpo: o analisador unsafeptr sinaliza exatamente a conversão
// inversa (uintptr guardado -> unsafe.Pointer), que este arquivo evita por
// construção, não por supressão.

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GUIDs dos objetos e interfaces COM usados (mmdevapi). Um byte errado
// aqui devolve E_NOINTERFACE em runtime, não um erro de compilação —
// conferidos contra a documentação da Microsoft (mmdeviceapi.h /
// audioclient.h).
var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

const (
	eCapture = 1 // EDataFlow
	eConsole = 0 // ERole

	clsctxInprocServer  = 0x1
	clsctxInprocHandler = 0x2
	clsctxLocalServer   = 0x4
	clsctxAll           = clsctxInprocServer | clsctxInprocHandler | clsctxLocalServer

	audclntShareModeShared = 0 // AUDCLNT_SHAREMODE_SHARED — nunca exclusivo (ADR-010)

	audclntBufferflagsSilent         = 0x2 // AUDCLNT_BUFFERFLAGS_SILENT
	audclntBufferflagsTimestampError = 0x4 // AUDCLNT_BUFFERFLAGS_TIMESTAMP_ERROR

	// hnsBufferDuration: 200ms, um buffer compartilhado folgado o
	// bastante para não perder pacote sob jitter de agendamento comum,
	// sem acrescentar latência perceptível para ditado (que já espera o
	// keyup antes de processar). Unidade: 100ns (REFERENCE_TIME).
	hnsBufferDuration = 200 * 10000 // 200ms em unidades de 100ns
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

// hresult é o valor de retorno de toda chamada COM: um int32 assinado —
// negativo é falha.
type hresult int32

func (h hresult) failed() bool { return h < 0 }

func (h hresult) err(context string) error {
	if !h.failed() {
		return nil
	}
	return fmt.Errorf("audio: %s: HRESULT 0x%08X", context, uint32(h))
}

// comObject é um ponteiro para um objeto COM — isto é, um ponteiro para o
// ponteiro de vtable do objeto, o layout padrão de todo objeto COM em
// C++. É embrulhado numa struct de um campo só porque a linguagem não
// permite métodos num named type cujo underlying type é diretamente
// unsafe.Pointer (é tratado como tipo-ponteiro para essa restrição).
// O campo fica em unsafe.Pointer, nunca em uintptr, do início ao fim da
// sua vida: é o que dispensa qualquer conversão uintptr -> Pointer depois
// de guardado (ver nota de go vet no topo do arquivo).
type comObject struct{ p unsafe.Pointer }

// asComObject embrulha um ponteiro de objeto/interface COM recém-obtido
// (tipicamente de um parâmetro de saída "ppv" de uma chamada COM).
func asComObject(p unsafe.Pointer) comObject { return comObject{p: p} }

func (o comObject) valid() bool { return o.p != nil }

// vtable lê o primeiro campo do objeto: o ponteiro para o array de
// métodos.
func (o comObject) vtable() unsafe.Pointer {
	return *(*unsafe.Pointer)(o.p)
}

// method lê o ponteiro de função no índice index da vtable — a ORDEM DA
// INTERFACE, contando os 3 métodos de IUnknown (QueryInterface=0,
// AddRef=1, Release=2) primeiro. unsafe.Add faz a aritmética de ponteiro
// sem nunca materializar um uintptr solto — é o substituto abençoado (Go
// 1.17+) de `unsafe.Pointer(uintptr(p) + offset)`.
func (o comObject) method(index int) uintptr {
	slot := unsafe.Add(o.vtable(), uintptr(index)*unsafe.Sizeof(uintptr(0)))
	return *(*uintptr)(slot)
}

// call invoca o método index da vtable, passando o próprio objeto como
// primeiro argumento ("this"), seguido de args — a convenção COM em x64
// coincide com a convenção padrão do Windows para chamada de função.
//
// runtime.KeepAlive(o) depois da chamada não é estritamente necessário —
// a memória de um objeto COM é do processo, alocada pelo próprio COM, não
// pelo alocador do Go, então o GC nunca a move nem a libera por conta
// própria — mas documenta a intenção e custa nada.
func (o comObject) call(index int, args ...uintptr) uintptr {
	fn := o.method(index)
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(o.p))
	all = append(all, args...)
	r1, _, _ := syscall.SyscallN(fn, all...)
	runtime.KeepAlive(o)
	return r1
}

func (o comObject) callHR(index int, args ...uintptr) hresult {
	return hresult(int32(o.call(index, args...)))
}

// liberar chama IUnknown::Release (método 2 em toda interface COM) e ZERA
// o ponteiro. Zerar é o que torna seguro liberar duas vezes: com a
// política Liberar (ADR-015) o cliente é solto no meio da sessão, e o
// Close final chamaria Stop e Release num objeto já solto se o campo
// continuasse apontando para ele.
func (o *comObject) liberar() {
	if o.valid() {
		o.call(2)
		o.p = nil
	}
}

// wasapiDevice implementa a interface device sobre um endpoint de captura
// WASAPI padrão, em modo compartilhado, por polling.
type wasapiDevice struct {
	enumerator    comObject
	mmDevice      comObject
	audioClient   comObject
	captureClient comObject

	sampleRate    int
	channels      int
	bitsPerSample int
	isFloat       bool
	periodo       time.Duration

	// comOwned reporta se esta abertura foi quem de fato tomou posse do
	// apartment COM desta thread (ver coInitialize) — só nesse caso Close
	// deve chamar CoUninitialize.
	comOwned bool
}

// openDevice implementa a abertura real do dispositivo padrão de captura,
// em modo compartilhado (ADR-010 D1). Deve ser chamado já na thread com
// runtime.LockOSThread() feito por session.go — CoInitializeEx é chamado
// aqui dentro, na mesma thread, e nunca mais migra.
func openDevice(ctx context.Context) (device, error) {
	comOwned, err := coInitialize()
	if err != nil {
		return nil, err
	}
	// A partir daqui, newWASAPIDevice é o único dono do ciclo de vida:
	// falhando ou não, é ele (via wasapiDevice.Close, chamado por si
	// mesmo em erro ou pelo defer do chamador em sucesso) quem decide se
	// chama CoUninitialize — exatamente uma vez, nunca duas. Repeti-lo
	// aqui em cima de um Close() que já rodou desequilibraria a contagem
	// de referência do apartment COM desta thread.
	return newWASAPIDevice(comOwned)
}

// coInitialize chama CoInitializeEx nesta thread e reporta se esta chamada
// de fato tomou posse do apartment (e portanto precisa ser pareada com um
// CoUninitialize nosso). RPC_E_CHANGED_MODE indica que outra parte já
// inicializou COM nesta thread, num modo diferente — nossa chamada não
// incrementou a contagem de referência, e um CoUninitialize nosso
// desequilibraria a contagem alheia.
func coInitialize() (owned bool, err error) {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		const rpcEChangedMode = 0x80010106
		if errno, ok := err.(windows.Errno); ok && uint32(errno) == rpcEChangedMode {
			return false, nil
		}
		return false, fmt.Errorf("audio: CoInitializeEx: %w", err)
	}
	return true, nil
}

// newWASAPIDevice abre e PREPARA o microfone padrão, sem ligar (ADR-015):
// Initialize deixa a sessão de áudio "inativa", e só o Start do keydown a
// torna ativa. Abrir continua na subida para que um microfone que não
// abre apareça ali, e não no primeiro ditado.
func newWASAPIDevice(comOwned bool) (*wasapiDevice, error) {
	d := &wasapiDevice{comOwned: comOwned}

	enumerator, err := coCreateInstance(clsidMMDeviceEnumerator, iidIMMDeviceEnumerator)
	if err != nil {
		d.Close()
		return nil, fmt.Errorf("audio: CoCreateInstance(MMDeviceEnumerator): %w", err)
	}
	d.enumerator = enumerator

	if err := d.abrirCliente(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// abrirCliente abre o endpoint de captura padrão e prepara o cliente:
// GetDefaultAudioEndpoint, Activate, GetMixFormat, Initialize,
// GetDevicePeriod e GetService — sem Start. Serve à subida e à reabertura
// depois da política Liberar; nesta, pega o microfone padrão ATUAL.
//
// Em falha, solta o que chegou a abrir e deixa os campos zerados: o
// enumerador e o apartment COM continuam com o chamador.
func (d *wasapiDevice) abrirCliente() (err error) {
	defer func() {
		if err != nil {
			d.liberarCliente()
		}
	}()

	var mmDevice unsafe.Pointer
	hr := d.enumerator.callHR(4 /* GetDefaultAudioEndpoint */, uintptr(eCapture), uintptr(eConsole), uintptr(unsafe.Pointer(&mmDevice)))
	if err := hr.err("GetDefaultAudioEndpoint"); err != nil {
		return err
	}
	d.mmDevice = asComObject(mmDevice)

	var audioClient unsafe.Pointer
	hr = d.mmDevice.callHR(3 /* Activate */, uintptr(unsafe.Pointer(&iidIAudioClient)), uintptr(clsctxAll), 0, uintptr(unsafe.Pointer(&audioClient)))
	if err := hr.err("IMMDevice.Activate(IAudioClient)"); err != nil {
		return err
	}
	d.audioClient = asComObject(audioClient)

	blob, formatPtr, err := d.getMixFormat()
	if err != nil {
		return err
	}
	// Liberado ao sair desta função, a cada abertura: numa reabertura, o
	// mix format é lido de novo e o bloco novo também é do chamador.
	defer windows.CoTaskMemFree(formatPtr)

	// Toda a decodificação mora em mixformat.go, fora da build tag, para
	// poder ser testada sem Windows. Aqui só copiamos os bytes e
	// repassamos: este arquivo não interpreta formato.
	mf, err := parseMixFormat(blob)
	if err != nil {
		return err
	}
	d.sampleRate = mf.SampleRate
	d.channels = mf.Channels
	d.bitsPerSample = mf.BitsPerSample
	d.isFloat = mf.IsFloat

	hr = d.audioClient.callHR(3, /* Initialize */
		uintptr(audclntShareModeShared),
		0, // StreamFlags: nenhuma flag especial — polling simples
		uintptr(hnsBufferDuration),
		0, // hnsPeriodicity deve ser 0 em modo compartilhado
		uintptr(formatPtr),
		0, // AudioSessionGuid: NULL
	)
	if err := hr.err("IAudioClient.Initialize"); err != nil {
		return err
	}

	// O período só alimenta a tolerância do detector de contaminação:
	// falhar aqui não impede o ditado, e a Session cai no padrão de 10 ms.
	var hnsPadrao, hnsMinimo int64
	if hr := d.audioClient.callHR(9 /* GetDevicePeriod */, uintptr(unsafe.Pointer(&hnsPadrao)), uintptr(unsafe.Pointer(&hnsMinimo))); !hr.failed() && hnsPadrao > 0 {
		d.periodo = duracaoDeTicks(hnsPadrao)
	} else {
		d.periodo = 0
	}

	var captureClient unsafe.Pointer
	hr = d.audioClient.callHR(14 /* GetService */, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&captureClient)))
	if err := hr.err("IAudioClient.GetService(IAudioCaptureClient)"); err != nil {
		return err
	}
	d.captureClient = asComObject(captureClient)
	return nil
}

// liberarCliente solta o cliente de captura, o cliente de áudio e o
// endpoint, zerando cada um, e mantém o enumerador e o apartment COM.
func (d *wasapiDevice) liberarCliente() {
	d.captureClient.liberar()
	d.audioClient.liberar()
	d.mmDevice.liberar()
}

func (d *wasapiDevice) Periodo() time.Duration { return d.periodo }

// Ligar dá o Start. Depois de Liberar, reabre o endpoint padrão antes.
// Um Start num stream que já roda (AUDCLNT_E_NOT_STOPPED) não é falha:
// ver ligarFalhou.
func (d *wasapiDevice) Ligar() error {
	if !d.audioClient.valid() {
		if err := d.abrirCliente(); err != nil {
			return err
		}
	}
	if hr := d.audioClient.callHR(10 /* Start */); ligarFalhou(int32(hr)) {
		return hr.err("IAudioClient.Start")
	}
	return nil
}

// Parar dá o Stop, mantendo o cliente. Stop num stream já parado devolve
// S_FALSE, que não é falha.
func (d *wasapiDevice) Parar() error {
	if !d.audioClient.valid() {
		return fmt.Errorf("audio: IAudioClient.Stop sem cliente de áudio")
	}
	return d.audioClient.callHR(11 /* Stop */).err("IAudioClient.Stop")
}

// Zerar dá o Reset: descarta os pacotes pendentes e zera a posição do
// stream. GetBuffer e ReleaseBuffer ficam sempre pareados nesta mesma
// thread, então não há leitura em curso para dar
// AUDCLNT_E_BUFFER_OPERATION_PENDING; se der, a Session libera o cliente.
func (d *wasapiDevice) Zerar() error {
	if !d.audioClient.valid() {
		return fmt.Errorf("audio: IAudioClient.Reset sem cliente de áudio")
	}
	return d.audioClient.callHR(12 /* Reset */).err("IAudioClient.Reset")
}

// Liberar para o stream (se houver cliente) e solta o cliente; o próximo
// Ligar reabre o endpoint padrão.
func (d *wasapiDevice) Liberar() {
	if d.audioClient.valid() {
		d.audioClient.call(11 /* Stop */)
	}
	d.liberarCliente()
}

// getMixFormat devolve tanto os campos já decodificados quanto o
// ponteiro bruto (necessário para: 1. inspecionar o SubFormat quando
// WAVEFORMATEXTENSIBLE, 2. repassar como pFormat a Initialize, 3. liberar
// com CoTaskMemFree). O chamador é dono de liberar formatPtr.
// Devolve uma CÓPIA dos bytes nativos, com o tamanho exato declarado pelo
// próprio bloco (18 + cbSize) — nunca uma struct Go sobreposta à memória
// nativa. Foi essa sobreposição que corrompeu o dataset: a struct Go
// equivalente ocupa 20 bytes onde a nativa ocupa 18, e tudo depois de
// cbSize saía deslocado.
func (d *wasapiDevice) getMixFormat() (blob []byte, formatPtr unsafe.Pointer, err error) {
	var ptr unsafe.Pointer
	hr := d.audioClient.callHR(8 /* GetMixFormat */, uintptr(unsafe.Pointer(&ptr)))
	if err := hr.err("IAudioClient.GetMixFormat"); err != nil {
		return nil, nil, err
	}

	// Lê cbSize primeiro para saber o tamanho real do bloco, em vez de
	// assumir 40: um driver que devolva WAVEFORMATEX simples entrega 18
	// bytes, e ler além disso é leitura fora do buffer.
	head := unsafe.Slice((*byte)(ptr), waveFormatExNativeSize)
	total := waveFormatExNativeSize + int(binary.LittleEndian.Uint16(head[offCbSize:]))

	out := make([]byte, total)
	copy(out, unsafe.Slice((*byte)(ptr), total))
	return out, ptr, nil
}

func (d *wasapiDevice) MixFormat() (sampleRate, channels int) {
	return d.sampleRate, d.channels
}

// Read consulta GetNextPacketSize uma vez. Sem pacote, espera um intervalo
// de consulta (10 ms, ou até ctx terminar) e devolve um pacote VAZIO — não
// fica em laço aqui dentro: é voltando que a Session consegue estacionar
// um microfone que ainda não entregou nada (ADR-015, ajuste C). Com
// pacote, lê com GetBuffer, copia para um []float32 novo (o ponteiro
// devolvido por GetBuffer só é válido até ReleaseBuffer), guarda os
// carimbos e libera o pacote.
func (d *wasapiDevice) Read(ctx context.Context) (pacote, error) {
	select {
	case <-ctx.Done():
		return pacote{}, ctx.Err()
	default:
	}
	if !d.captureClient.valid() {
		return pacote{}, fmt.Errorf("%w: leitura sem cliente de captura", ErrDeviceLost)
	}

	var packetFrames uint32
	hr := d.captureClient.callHR(5 /* GetNextPacketSize */, uintptr(unsafe.Pointer(&packetFrames)))
	if err := hr.err("IAudioCaptureClient.GetNextPacketSize"); err != nil {
		return pacote{}, fmt.Errorf("%w: %v", ErrDeviceLost, err)
	}
	if packetFrames == 0 {
		select {
		case <-ctx.Done():
			return pacote{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		return pacote{}, nil
	}

	var data unsafe.Pointer
	var framesAvailable uint32
	var flags uint32
	var posicao, qpc uint64
	hr = d.captureClient.callHR(3, /* GetBuffer */
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&framesAvailable)),
		uintptr(unsafe.Pointer(&flags)),
		// pu64DevicePosition: posição do primeiro frame no stream.
		uintptr(unsafe.Pointer(&posicao)),
		// pu64QPCPosition: instante, em 100 ns do QPC, em que o
		// dispositivo gravou o primeiro frame. É a base das medições da
		// ADR-015 (relogio.go).
		uintptr(unsafe.Pointer(&qpc)),
	)
	if err := hr.err("IAudioCaptureClient.GetBuffer"); err != nil {
		return pacote{}, fmt.Errorf("%w: %v", ErrDeviceLost, err)
	}

	p := pacote{
		amostras:      d.copyOut(data, framesAvailable, flags),
		qpc:           int64(qpc),
		posicao:       posicao,
		erroTimestamp: flags&audclntBufferflagsTimestampError != 0,
	}

	if hr := d.captureClient.callHR(4 /* ReleaseBuffer */, uintptr(framesAvailable)); hr.failed() {
		return pacote{}, fmt.Errorf("%w: %v", ErrDeviceLost, hr.err("IAudioCaptureClient.ReleaseBuffer"))
	}
	return p, nil
}

// copyOut converte o pacote bruto (frames de d.channels canais, no mix
// format negociado) para um []float32 intercalado, escalado para [-1, 1].
// AUDCLNT_BUFFERFLAGS_SILENT indica que o ponteiro de dados não deve ser
// lido — o pacote é silêncio.
//
// A conversão em si vive em pcm.go, fora da build tag windows, para ser
// testada nas duas plataformas: escala e sinal são onde os erros
// silenciosos acontecem, e aqui dentro nenhum teste os alcançaria.
func (d *wasapiDevice) copyOut(data unsafe.Pointer, frames uint32, flags uint32) []float32 {
	n := int(frames) * d.channels
	if flags&audclntBufferflagsSilent != 0 || data == nil {
		return make([]float32, n) // já zerado
	}

	raw := unsafe.Slice((*byte)(data), n*(d.bitsPerSample/8))
	return convertPCM(raw, d.isFloat, d.bitsPerSample, n)
}

// Close libera os objetos COM obtidos e, só se esta abertura tinha de fato
// tomado posse do apartment (d.comOwned), pareia com um CoUninitialize —
// nunca mais de uma vez por CoInitializeEx bem-sucedido (ver openDevice).
// É seguro chamar em qualquer ponto de falha parcial de newWASAPIDevice e
// depois de Liberar: campos zero (comObject{}) são no-op em
// liberar()/valid(), e liberar() zera o que solta.
func (d *wasapiDevice) Close() error {
	if d.audioClient.valid() {
		d.audioClient.call(11 /* Stop */) // inofensivo com o stream já parado
	}
	d.liberarCliente()
	d.enumerator.liberar()
	if d.comOwned {
		windows.CoUninitialize()
		d.comOwned = false
	}
	return nil
}

// coCreateInstance chama ole32!CoCreateInstance para o CLSID e a IID
// dados, com CLSCTX_ALL (servidor in-proc é o caso real para mmdevapi).
func coCreateInstance(clsid, iid windows.GUID) (comObject, error) {
	var out unsafe.Pointer
	r1, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0, // pUnkOuter: NULL (sem agregação)
		uintptr(clsctxAll),
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(clsid)
	runtime.KeepAlive(iid)
	if hr := hresult(int32(r1)); hr.failed() {
		return comObject{}, hr.err("CoCreateInstance")
	}
	return asComObject(out), nil
}
