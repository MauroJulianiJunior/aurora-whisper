package syncengine

import (
	"reflect"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A tabela da varredura (ADR-019, "No PC, a varredura"), linha a linha, pura:
// DecidirVarredura e o filtro do que não muda (soOQueMuda). Os percursos com
// o arquivo e o espelho de verdade estão em varredura_percurso_test.go.

var agoraDaTabela = time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)

func emArquivo(p *dictionary.ParDoPainel) VistaDoArquivo { return VistaDoArquivo{Projecao: p} }

func criar() int { return storage.MarcadorCriar }

func TestDecidirVarredura(t *testing.T) {
	ecf, ecd := projDe("sf", "ECF"), projDe("sf", "ECD")
	esperando := func(e storage.EstadoDoAprendido) storage.EstadoDoAprendido {
		e.Marcador = &storage.Marcador{EntityType: storage.EntidadeAprendido, EntityID: e.ID}
		return e
	}
	mudadoAqui := func(e storage.EstadoDoAprendido) storage.EstadoDoAprendido { e.VersaoLocal++; return e }
	aplicando := func(guardada, esperada *dictionary.ParDoPainel) storage.EstadoDoAprendido {
		return com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.Projecao, e.Esperada = guardada, esperada
			e.Situacao = storage.EspelhoAplicando
			e.Nuvem = guardadoDe(4, parDe("sf", "ECD"))
			e.VeioDaNuvem = true
		})
	}
	desde := agoraDaTabela.Add(-PrazoDaEtiqueta)
	comEtiqueta := func(e storage.EstadoDoAprendido, em time.Time, pendente bool) storage.EstadoDoAprendido {
		e.Situacao, e.SituacaoEm = storage.EspelhoDesfeitoEmOutro, &em
		v := *e.ServerVersion
		if pendente {
			v++
		}
		e.Nuvem = guardadoDe(v, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true})
		return e
	}
	// Uma situação que não é etiqueta, começada uma hora além do prazo dela.
	vencida := agoraDaTabela.Add(-PrazoDaEtiqueta - time.Hour)
	haMuito := func(e storage.EstadoDoAprendido, s storage.SituacaoDoEspelho, motivo string) storage.EstadoDoAprendido {
		e.Situacao, e.Motivo, e.SituacaoEm = s, motivo, &vencida
		return e
	}
	nada := storage.AcaoDoAprendido{}

	casos := []struct {
		nome   string
		e      storage.EstadoDoAprendido
		v      VistaDoArquivo
		aceito bool
		quero  storage.AcaoDoAprendido
		tipo   string
		segura bool
		sai    bool
	}{
		// O envio em voo: a projeção é a do envio; só a versão local, uma vez.
		{"em voo, o arquivo mudou", emVoo(confirmado(), 2), emArquivo(ecd), true,
			storage.AcaoDoAprendido{MudouAqui: true}, TipoVarreduraEmVoo, false, false},
		{"em voo, a mudança já foi vista", emVoo(mudadoAqui(confirmado()), 2), emArquivo(ecd), true, nada, "", false, false},
		{"em voo, igual", emVoo(confirmado(), 2), emArquivo(ecf), true, nada, "", false, false},
		{"em voo, o par saiu à mão", emVoo(confirmado(), 2), VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{MudouAqui: true}, TipoVarreduraEmVoo, false, true},
		{"em voo, adotado", emVoo(confirmado(), 2), VistaDoArquivo{Adotada: true}, true, nada, "", false, false},

		// A linha "aplicando". A confirmação deixa o par da nuvem guardado,
		// com a versão, pendente: a tentativa de novo dele anda a base.
		{"aplicando, igual à esperada", aplicando(ecf, ecd), emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd},
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicado}},
			TipoVarreduraConfirmado, false, false},
		{"aplicando, igual à esperada, o par da nuvem sem a versão (o recomeço)",
			com(aplicando(ecf, ecd), func(e *storage.EstadoDoAprendido) { e.Nuvem.Versao = nil }), emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd},
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicado}, LimparNuvem: true},
			TipoVarreduraConfirmado, false, false},
		{"aplicando, a esperada sem o par (o desfazer)", aplicando(ecf, nil), VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, Situacao: semSituacaoGravada},
			TipoVarreduraConfirmado, false, false},
		{"aplicando, igual à guardada", aplicando(ecf, ecd), emArquivo(ecf), true,
			storage.AcaoDoAprendido{Situacao: semSituacaoGravada}, TipoVarreduraDeVolta, false, false},
		{"aplicando, diferente das duas", aplicando(ecf, ecd), emArquivo(projDe("sf", "LALUR")), true,
			storage.AcaoDoAprendido{Situacao: semSituacaoGravada, Projecao: &storage.NovaProjecao{Par: projDe("sf", "LALUR")},
				MudouAqui: true, Marcador: criar(), VeioDaNuvem: storage.VeioSai},
			TipoVarreduraUpsert, false, false},
		{"aplicando, igual à esperada, e o gancho depois do lote", mudadoAqui(aplicando(ecf, ecd)), emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd},
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicado},
				Marcador: criar(), VeioDaNuvem: storage.VeioSai},
			TipoVarreduraUpsert, false, false},

		// O recusado.
		{"recusado, a projeção recusada", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.Situacao, e.Motivo = storage.EspelhoRecusado, "campo_invalido"
		}), emArquivo(ecf), true, nada, "", false, false},
		{"recusado, o par mudou", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.Situacao, e.Motivo = storage.EspelhoRecusado, "campo_invalido"
		}), emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd}, MudouAqui: true,
				Situacao: semSituacaoGravada, Marcador: criar()},
			TipoVarreduraUpsert, false, false},

		// O par presente.
		{"igual à guardada, a nuvem tem", confirmado(), emArquivo(ecf), true, nada, "", false, false},
		{"o par novo", semLinha(), emArquivo(ecf), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecf}, MudouAqui: true, Marcador: criar()},
			TipoVarreduraUpsert, false, false},
		{"o par novo, antes do aceite", semLinha(), emArquivo(ecf), false,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecf}, MudouAqui: true},
			TipoVarreduraSegurado, true, false},
		{"a troca de termo de um par que a nuvem tem, antes do aceite", confirmado(), emArquivo(ecd), false,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd}, MudouAqui: true},
			TipoVarreduraSegurado, true, false},
		{"o gancho com o arquivo igual", mudadoAqui(confirmado()), emArquivo(ecf), true,
			storage.AcaoDoAprendido{Marcador: criar()}, TipoVarreduraUpsert, false, false},
		{"o gancho com o arquivo igual, antes do aceite", mudadoAqui(confirmado()), emArquivo(ecf), false,
			nada, TipoVarreduraSegurado, true, false},
		{"presente sem a nuvem ter, fora do veio", com(confirmado(), func(e *storage.EstadoDoAprendido) { e.ServerVersion = nil }),
			emArquivo(ecf), true, storage.AcaoDoAprendido{Marcador: criar()}, TipoVarreduraUpsert, false, false},
		{"presente sem a nuvem ter, fora do veio, antes do aceite",
			com(confirmado(), func(e *storage.EstadoDoAprendido) { e.ServerVersion = nil }),
			emArquivo(ecf), false, nada, TipoVarreduraSegurado, true, false},
		{"presente sem a nuvem ter, do veio da nuvem", com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.ServerVersion, e.VeioDaNuvem = nil, true
		}), emArquivo(ecf), true, nada, "", false, false},
		{"presente sem a base, com o state de um conflict", com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.ServerVersion, e.Nuvem = nil, guardadoDe(1, parDe("sf", "ECD"))
			e.Situacao = storage.EspelhoTrocadoEmOutro
		}), emArquivo(ecf), true, nada, "", false, false},
		{"o marcador que espera leva a projeção nova", esperando(mudadoAqui(confirmado())), emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd}, MudouAqui: true},
			TipoVarreduraUpsert, false, false},
		{"aprendido de novo aqui, o veio sai", com(confirmado(), func(e *storage.EstadoDoAprendido) { e.VeioDaNuvem = true }),
			emArquivo(ecd), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecd}, MudouAqui: true, Marcador: criar(),
				VeioDaNuvem: storage.VeioSai},
			TipoVarreduraUpsert, false, false},

		// Fora da projeção.
		{"no arquivo, e não sobe", confirmado(), VistaDoArquivo{ForaDaProjecao: true}, true, nada, "", false, false},
		{"adotado", mudadoAqui(confirmado()), VistaDoArquivo{Adotada: true}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{},
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoJaValeAqui}, Cobre: i64(3)},
			TipoVarreduraAdotado, false, false},
		{"adotado, com o marcador que esperava", esperando(confirmado()), VistaDoArquivo{Adotada: true}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{},
				Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoJaValeAqui}, Marcador: storage.MarcadorSai},
			TipoVarreduraAdotado, false, false},

		// O par que saiu.
		{"saiu à mão, a nuvem tem", confirmado(), VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, MudouAqui: true, Marcador: criar()},
			TipoVarreduraDelete, false, true},
		{"saiu pelo painel, a nuvem tem, antes do aceite", mudadoAqui(confirmado()), VistaDoArquivo{}, false,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, MudouAqui: true, Marcador: criar()},
			TipoVarreduraDelete, false, false},
		{"saiu, a nuvem tem só o pendente do pull", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.ServerVersion, e.Nuvem = nil, guardadoDe(2, parDe("sf", "ECD"))
		}), VistaDoArquivo{}, false,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, MudouAqui: true, Marcador: criar()},
			TipoVarreduraDelete, false, false},
		{"saiu, a nuvem nunca teve", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) { e.ServerVersion = nil }),
			VistaDoArquivo{}, true, storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, Cobre: i64(3)},
			TipoVarreduraAssentado, false, false},
		{"saiu, de volta à mesma conta depois de um esquecer", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.ServerVersion, e.MesmaContaEsquecida = nil, true
		}), VistaDoArquivo{}, true, storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, MudouAqui: true},
			TipoVarreduraEspera, false, false},
		{"saiu, a nuvem nunca teve, com o upsert que esperava",
			esperando(com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) { e.ServerVersion = nil })),
			VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, Cobre: i64(3), Marcador: storage.MarcadorSai},
			TipoVarreduraAssentado, false, false},
		{"saiu, recusado", com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.Situacao, e.Motivo = storage.EspelhoRecusado, "campo_invalido"
		}), VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{}, Situacao: semSituacaoGravada, MudouAqui: true,
				Marcador: criar()},
			TipoVarreduraDelete, false, true},

		// Nem no arquivo nem na projeção guardada.
		{"o desfeito aqui, e o pull trouxe a versão", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.Projecao, e.ServerVersion, e.Nuvem = nil, nil, guardadoDe(5, parDe("sf", "ECF"))
		}), VistaDoArquivo{}, false, storage.AcaoDoAprendido{Marcador: criar()}, TipoVarreduraDelete, false, false},
		{"o desfeito aqui, sem a versão ainda", com(mudadoAqui(confirmado()), func(e *storage.EstadoDoAprendido) {
			e.Projecao, e.ServerVersion = nil, nil
		}), VistaDoArquivo{}, true, nada, "", false, false},
		{"o desfeito confirmado", com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.Projecao, e.BaseDesfeita = nil, true
		}), VistaDoArquivo{}, true, nada, "", false, false},

		// A etiqueta.
		{"a etiqueta dentro do prazo", comEtiqueta(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil }),
			desde.Add(time.Second), false), VistaDoArquivo{}, true, nada, "", false, false},
		{"a etiqueta no prazo", comEtiqueta(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil }),
			desde, false), VistaDoArquivo{}, true,
			storage.AcaoDoAprendido{Situacao: semSituacaoGravada, LimparNuvem: true}, TipoVarreduraEtiqueta, false, false},
		{"a etiqueta no prazo, com o state ainda pendente", comEtiqueta(confirmado(), desde, true), emArquivo(ecf), true,
			storage.AcaoDoAprendido{Situacao: semSituacaoGravada}, TipoVarreduraEtiqueta, false, false},
		{"a etiqueta e a pessoa aprende de novo", comEtiqueta(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Projecao = nil }),
			agoraDaTabela, false), emArquivo(ecf), true,
			storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: ecf}, MudouAqui: true, Situacao: semSituacaoGravada,
				LimparNuvem: true, Marcador: criar()},
			TipoVarreduraUpsert, false, false},

		// O prazo é só das etiquetas: as outras situações não vencem. O
		// "aplicado" e o "já vale aqui" ficam na lista, e o "não aplicado"
		// fica pendente (sem a situação, o que andou a base até a versão do
		// par deixaria de ser pendente, e a varredura não o tentaria mais).
		{"aplicado, além do prazo da etiqueta", haMuito(com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.VeioDaNuvem = true
		}), storage.EspelhoAplicado, ""), emArquivo(ecf), true, nada, "", false, false},
		{"já vale aqui, além do prazo da etiqueta", haMuito(com(confirmado(), func(e *storage.EstadoDoAprendido) {
			e.Projecao = nil
		}), storage.EspelhoJaValeAqui, ""), VistaDoArquivo{}, true, nada, "", false, false},
		{"não aplicado pelo arquivo indisponível, além do prazo da etiqueta",
			haMuito(com(confirmado(), func(e *storage.EstadoDoAprendido) { e.Nuvem = guardadoDe(4, parDe("sf", "ECD")) }),
				storage.EspelhoNaoAplicado, string(dictionary.MotivoArquivoIndisponivel)),
			emArquivo(ecf), true, nada, "", false, false},
		{"não aplicado pelo domínio ausente, além do prazo da etiqueta",
			haMuito(com(confirmado(), func(e *storage.EstadoDoAprendido) {
				e.Projecao = nil
				e.Nuvem = guardadoDe(3, dictionary.ParDaNuvem{Variante: "sf", Termo: "ECD", Restrito: true, Dominio: "fiscal"})
			}), storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)),
			VistaDoArquivo{}, true, nada, "", false, false},
	}
	for _, c := range casos {
		d := DecidirVarredura(c.e, c.v, c.aceito, agoraDaTabela)
		if !reflect.DeepEqual(d.Acao, c.quero) {
			t.Errorf("%s:\n got %s\nwant %s", c.nome, acaoEmTexto(d.Acao), acaoEmTexto(c.quero))
		}
		if d.Tipo != c.tipo || d.Segura != c.segura || d.Sai != c.sai {
			t.Errorf("%s: tipo %q segura %v sai %v; want %q %v %v", c.nome, d.Tipo, d.Segura, d.Sai, c.tipo, c.segura, c.sai)
		}
	}
}

// O filtro do que não muda: a ação que só regrava o que o espelho já tem
// fica vazia (não é gravada); o que muda fica.
func TestSoOQueMuda(t *testing.T) {
	e := com(confirmado(), func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Motivo = storage.EspelhoNaoAplicado, string(dictionary.MotivoDominioAusente)
		e.Nuvem = guardadoDe(3, parDe("sf", "ECD"))
		e.VeioDaNuvem = true
		e.Marcador = &storage.Marcador{}
	})
	igual := storage.AcaoDoAprendido{
		Base:        &storage.BaseDoAprendido{ServerVersion: 3},
		Cobre:       i64(2),
		Projecao:    &storage.NovaProjecao{Par: projDe("sf", "ECF")},
		Nuvem:       guardadoDe(3, parDe("sf", "ECD")),
		Situacao:    &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoDominioAusente)},
		VeioDaNuvem: storage.VeioEntra,
		Marcador:    storage.MarcadorCriar,
	}
	if got := soOQueMuda(e, igual); got != (storage.AcaoDoAprendido{}) {
		t.Errorf("o que não muda ficou: %s", acaoEmTexto(got))
	}
	semNada := storage.AcaoDoAprendido{ZerarBase: true, LimparNuvem: true}
	if got := soOQueMuda(storage.EstadoDoAprendido{ID: idSF}, semNada); got != (storage.AcaoDoAprendido{}) {
		t.Errorf("zerar e limpar o que não há: %s", acaoEmTexto(got))
	}
	for nome, a := range map[string]storage.AcaoDoAprendido{
		"outra versão":   {Base: &storage.BaseDoAprendido{ServerVersion: 4}},
		"desfeita":       {Base: &storage.BaseDoAprendido{ServerVersion: 3, Desfeita: true}},
		"outra projeção": {Projecao: &storage.NovaProjecao{Par: projDe("sf", "ECD")}},
		"outro motivo": {Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado,
			Motivo: string(dictionary.MotivoNaoAgiria)}},
		"o veio sai":         {VeioDaNuvem: storage.VeioSai},
		"a versão local":     {MudouAqui: true},
		"cobre outra":        {Cobre: i64(1)},
		"limpa a nuvem":      {LimparNuvem: true},
		"o marcador sai":     {Marcador: storage.MarcadorSai},
		"outro par guardado": {Nuvem: guardadoDe(4, parDe("sf", "ECD"))},
	} {
		if got := soOQueMuda(e, a); got != a {
			t.Errorf("%s: %s, want %s", nome, acaoEmTexto(got), acaoEmTexto(a))
		}
	}
	// A intenção igual à que está gravada não é regravada; outra esperada é.
	ap := com(e, func(e *storage.EstadoDoAprendido) {
		e.Situacao, e.Motivo, e.Esperada = storage.EspelhoAplicando, "", projDe("sf", "ECD")
	})
	mesma := storage.AcaoDoAprendido{Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicando},
		Esperada: &storage.NovaProjecao{Par: projDe("sf", "ECD")}}
	if got := soOQueMuda(ap, mesma); got != (storage.AcaoDoAprendido{}) {
		t.Errorf("a mesma intenção: %s", acaoEmTexto(got))
	}
	outra := storage.AcaoDoAprendido{Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoAplicando},
		Esperada: &storage.NovaProjecao{Par: projDe("sf", "LALUR")}}
	if got := soOQueMuda(ap, outra); got != outra {
		t.Errorf("outra intenção: %s", acaoEmTexto(got))
	}
}

// A vista do arquivo: o par da projeção, o que está lá e não sobe, e o
// adotado; o id que saiu de vez não tem vista.
func TestVistasDoArquivo(t *testing.T) {
	s := dictionary.Semente{Usuario: "mauro", AliasOrigem: map[string]string{"sf": dictionary.OrigemPainel, "ecede": dictionary.OrigemPainel},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf", "lalu"}},
			{Canonica: "ECD", Prioridade: 90, Restrito: true, Alias: []string{"ecede"}},
		}}
	es := []storage.EstadoDoAprendido{
		{ID: dictionary.IDDoPar("ecede"), Projecao: projDe("ecede", "ECD")},
		{ID: dictionary.IDDoPar("lalu"), Projecao: projDe("lalu", "ECF")},
		{ID: dictionary.IDDoPar("xis"), Projecao: projDe("xis", "ECF")},
	}
	v := vistasDoArquivo(s, es)
	if !reflect.DeepEqual(v[idSF], emArquivo(projDe("sf", "ECF"))) {
		t.Errorf("o par da projeção: %+v", v[idSF])
	}
	if got := v[dictionary.IDDoPar("ecede")]; !got.ForaDaProjecao || got.Adotada || got.Projecao != nil {
		t.Errorf("o que não sobe: %+v", got)
	}
	if got := v[dictionary.IDDoPar("lalu")]; got.ForaDaProjecao || !got.Adotada {
		t.Errorf("o adotado: %+v", got)
	}
	if got, ok := v[dictionary.IDDoPar("xis")]; !ok || got != (VistaDoArquivo{}) {
		t.Errorf("o que saiu: %+v", got)
	}
}
