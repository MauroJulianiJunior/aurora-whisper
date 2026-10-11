package audio

// Estacionar o microfone (ADR-015): o que fazer com o cliente WASAPI quando
// nenhum ditado precisa dele.
//
// A decisão mora aqui, fora da build tag, para ser testada contra um dublê
// no Linux e no Windows (CLAUDE.md, "A lógica de decisão é pura"). O
// wasapi_windows.go só executa os passos; quem escolhe é esta função.

// Politica é o jeito de estacionar o microfone.
type Politica int

const (
	// PoliticaParar é IAudioClient::Stop seguido de IAudioClient::Reset,
	// mantendo o cliente preparado para o próximo ditado. É o padrão.
	//
	// O Reset é obrigatório: Stop só congela o relógio do stream, e o
	// Start seguinte retoma de onde parou, entregando os pacotes que
	// sobraram do ditado anterior (documentação de IAudioClient::Stop e
	// ::Reset). Seria o vazamento de 28-set-2026 de volta por outra porta.
	PoliticaParar Politica = iota
	// PoliticaLiberar solta o cliente (Release de IAudioCaptureClient,
	// IAudioClient e IMMDevice, mantendo o enumerador e o apartment COM) e
	// reabre o endpoint padrão no próximo ditado. Custa mais até o primeiro
	// áudio: de +50 a ~+250 ms segundo relatos de terceiros (o TypeWhisper
	// mediu 326 ms até o primeiro buffer numa sessão nova), não medido
	// aqui. É o plano B se o roteiro manual mostrar que o ícone de
	// microfone não apaga com o cliente parado (M0/M1, ADR-015).
	PoliticaLiberar
)

func (p Politica) String() string {
	switch p {
	case PoliticaParar:
		return "parar"
	case PoliticaLiberar:
		return "liberar"
	default:
		return "desconhecida"
	}
}

// politicaDeEstacionamento decide, pelo que aconteceu ao parar, se o
// cliente pode continuar preparado ou precisa ser liberado.
//
// Parar só vale quando o Stop E o Reset deram certo. Um Reset que falhou
// deixa pacotes pendentes do ditado que acabou; Start retomaria dali.
// Liberar é a única saída que garante uma janela limpa.
func politicaDeEstacionamento(padrao Politica, errParar, errZerar error) Politica {
	if padrao != PoliticaParar || errParar != nil || errZerar != nil {
		return PoliticaLiberar
	}
	return PoliticaParar
}

// estacionarDispositivo executa a política no dispositivo e devolve a que
// de fato valeu.
func estacionarDispositivo(dev device, padrao Politica) Politica {
	errParar := dev.Parar()
	var errZerar error
	if padrao == PoliticaParar && errParar == nil {
		errZerar = dev.Zerar()
	}
	usada := politicaDeEstacionamento(padrao, errParar, errZerar)
	if usada == PoliticaLiberar {
		dev.Liberar()
	}
	return usada
}

// HRESULTs de IAudioClient::Start que interessam aqui.
//
// Os códigos AUDCLNT_E_* são AUDCLNT_ERR(n) = 0x88890000 | n, e n não
// segue a ordem que se imaginaria: 0x1 NOT_INITIALIZED, 0x2
// ALREADY_INITIALIZED, 0x4 DEVICE_INVALIDATED, 0x5 NOT_STOPPED
// (audioclient.h). A primeira versão desta constante usou 0x2 — o
// ALREADY_INITIALIZED — e o teste repetia o mesmo número, então passava.
const (
	// audclntENotStopped (AUDCLNT_E_NOT_STOPPED) é Start num stream que já
	// está rodando.
	audclntENotStopped = int32(-2004287483) // 0x88890005
)

// ligarFalhou classifica o HRESULT de IAudioClient::Start.
//
// S_OK e S_FALSE são sucesso (todo HRESULT não negativo é). O
// AUDCLNT_E_NOT_STOPPED também não é falha: o stream já está rodando, que
// é exatamente o estado pedido. A Session só liga a partir de
// "estacionado", então não deveria acontecer — mas, se acontecer, tratar
// como falha mataria o áudio até reiniciar o Agent por nada.
func ligarFalhou(hr int32) bool {
	if hr >= 0 {
		return false
	}
	return hr != audclntENotStopped
}
