// Package rss amostra o RSS (Resident Set Size — memória física residente)
// de um processo candidato pelo PID, para o gate "RSS alvo ≤~1 GB" de
// docs/ai-spec/19-eval-plan.md.
//
// É específico de plataforma por natureza (não existe API portável no
// stdlib para RSS de um PID arbitrário), por isso o pacote é só a
// interface comum (este arquivo) mais um par rss_windows.go / rss_other.go
// — a convenção de docs/CLAUDE.md ("Pacote novo precisa de par
// _windows.go / _other.go SE tiver código específico de plataforma").
//
// Quando a amostragem não é possível no ambiente (plataforma sem suporte
// implementado, processo já encerrado, permissão negada), Sample devolve
// ok == false — NUNCA um valor inventado apresentado como medição real
// (ADR-012, "O que o benchmark não pode fazer").
package rss

// Sample devolve o RSS atual do processo identificado por pid, em bytes.
// ok é false quando a medição não foi possível neste ambiente — quem
// chama deve tratar isso como "não medido", nunca substituir por zero.
func Sample(pid int) (bytes int64, ok bool) {
	return sample(pid)
}

// SampleTree devolve a soma do RSS de pid e de todos os seus
// descendentes genuínos (filhos, netos, ...), em bytes. É o que um
// candidato "warm com adaptador" precisa: cmd/whisperwarm, por exemplo, é
// um processo Go fininho que sobe whisper-server.exe como filho, e é o
// filho quem segura o modelo — medir só o PID que o harness iniciou (o
// que Sample faz) mediria o adaptador, não o candidato.
//
// Para um processo sem filhos, SampleTree devolve exatamente o mesmo
// valor que Sample devolveria (ver TestSumTree_SingleNode em tree_test.go)
// — chamar SampleTree no lugar de Sample é seguro também para candidatos
// cold, que não têm subprocessos.
//
// ok é false quando pid não existe (ou já encerrou), quando a plataforma
// não implementa a amostragem de árvore (ver rss_other.go), ou quando
// qualquer nó da árvore não pôde ser medido — ver a documentação de
// sumTree em tree.go para a decisão sobre falha parcial e reaproveitamento
// de PID. Como em Sample, ok == false nunca deve ser tratado como "0
// bytes".
func SampleTree(pid int) (bytes int64, ok bool) {
	return sampleTree(pid)
}
