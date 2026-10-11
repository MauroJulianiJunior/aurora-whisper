// Package audio implementa a captura de áudio do Windows Agent (ADR-010,
// decisão D1): WASAPI em Go puro, dentro do processo, atrás de uma
// interface Source/Session equivalente ao AudioSource/AudioSession
// descrito em ADR-007, seção 4, e em docs/ai-spec/18-tool-contract.md.
//
// Escopo (ADR-010 D1, apertado de propósito):
//
//   - IMMDeviceEnumerator -> GetDefaultAudioEndpoint (dispositivo padrão,
//     sem seleção de dispositivo);
//   - IAudioClient::Initialize em modo COMPARTILHADO (nunca exclusivo);
//   - IAudioClient::Start / Stop / Reset para ligar e estacionar o
//     microfone a cada ditado (ADR-015);
//   - IAudioCaptureClient::GetBuffer / ReleaseBuffer / GetNextPacketSize,
//     com os carimbos de tempo (QPC) de cada pacote;
//   - resampling do mix format (tipicamente float32 estéreo a
//     44,1/48 kHz) para 16 kHz mono int16, em Go puro.
//
// Fora de escopo neste sprint: modo exclusivo, seleção de dispositivo,
// IMMNotificationClient/hot-plug. Um dispositivo invalidado se recupera
// pelos dois lados: quando o Start falha (cliente que ficou inválido com o
// microfone estacionado), o endpoint padrão é reaberto uma vez
// (ligarOuReabrir) — e, se nem isso ligar, falha só aquele ditado, e o
// keydown seguinte tenta de novo (naoLigou); quando uma LEITURA falha com
// o microfone ligado, o cliente é solto e a sessão volta a poder ligar —
// sozinha, com recuo, no sempre pronto; no próximo keydown, no sob demanda
// (Session.leituraFalhou; ADR-015, "Recuperação do microfone").
//
// # Ciclo de vida do microfone (ADR-015)
//
// [Source.Start] abre e PREPARA o microfone na subida, sem ligar. No
// padrão, ele só liga no [Session.MarkStart] do keydown e estaciona
// (Stop + Reset) quando não há mais ditado — sempre depois do corte da
// cauda. Em repouso a goroutine de áudio fica bloqueada, sem consultar o
// dispositivo. Com [Config.SemprePronto] (ou
// [Session.DefinirSemprePronto]), o microfone fica ligado desde a subida
// e o buffer inclui o pre-roll, como antes da ADR-015. Em qualquer modo o
// corte nunca começa antes do início da janela do microfone.
//
// Toda tentativa que chega a [Session.Stop] devolve, junto do áudio, uma
// [Janela] com os números do microfone — só números — para o log.
//
// # Organização
//
// O pacote é dividido em duas camadas, deliberadamente:
//
//   - Matemática pura, sem build tag, testável em qualquer SO:
//     [RingBuffer] (pre-roll), [Resampler] (mix format -> 16 kHz mono
//     int16) e [ClassifyVAD] (detecção de fala por energia). Nenhum dos
//     três toca em Win32.
//   - A porta de captura de baixo nível (device), específica de
//     plataforma: wasapi_windows.go (build tag windows) contém os
//     bindings COM reais sobre mmdevapi; wasapi_other.go (build tag
//     !windows) devolve [ErrNotSupported], só para que o pacote continue
//     compilando em CI (ubuntu-latest) — ver AGENTS/CLAUDE.md sobre
//     compilação multiplataforma.
//
// [Session] liga as duas camadas: um único goroutine, com
// runtime.LockOSThread(), abre o dispositivo e faz todas as chamadas COM
// subsequentes (Start/Stop/Reset/GetBuffer/ReleaseBuffer) na mesma thread do sistema
// operacional — COM tem apartment threading e o runtime do Go move
// goroutines entre threads livremente; sem o lock, a chamada escapa para a
// thread errada e o comportamento fica indefinido (ADR-010, "COM em Go
// puro").
package audio
