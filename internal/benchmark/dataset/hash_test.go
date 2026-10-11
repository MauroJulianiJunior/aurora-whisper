package dataset

import "testing"

func TestHash_DeterministicRegardlessOfOrder(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)
	writeTestWAV(t, dir, "b.wav", 16000, 1600)

	s1 := Sample{ID: "a", AudioPath: dir + "/a.wav", Reference: "oi", Category: CategoryIngles}
	s2 := Sample{ID: "b", AudioPath: dir + "/b.wav", Reference: "tchau", Category: CategoryIngles}

	h1, err := Hash([]Sample{s1, s2})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, err := Hash([]Sample{s2, s1})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("Hash depende da ordem: %s != %s", h1, h2)
	}
	if h1 == "" {
		t.Fatal("Hash vazio")
	}
}

func TestHash_ChangesWhenAudioContentChanges(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)
	s := Sample{ID: "a", AudioPath: dir + "/a.wav", Reference: "oi", Category: CategoryIngles}

	before, err := Hash([]Sample{s})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	// Regrava o mesmo arquivo com conteúdo de áudio diferente (duração
	// diferente muda os bytes do chunk data).
	writeTestWAV(t, dir, "a.wav", 16000, 3200)

	after, err := Hash([]Sample{s})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if before == after {
		t.Fatal("Hash não mudou quando o conteúdo do áudio mudou")
	}
}

func TestHash_ChangesWhenReferenceChanges(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)

	s1 := Sample{ID: "a", AudioPath: dir + "/a.wav", Reference: "oi", Category: CategoryIngles}
	s2 := Sample{ID: "a", AudioPath: dir + "/a.wav", Reference: "oi mudou", Category: CategoryIngles}

	h1, err := Hash([]Sample{s1})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, err := Hash([]Sample{s2})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 == h2 {
		t.Fatal("Hash não mudou quando a referência mudou")
	}
}

func TestHash_MissingAudioFileReturnsError(t *testing.T) {
	s := Sample{ID: "a", AudioPath: "does-not-exist.wav", Reference: "oi", Category: CategoryIngles}
	if _, err := Hash([]Sample{s}); err == nil {
		t.Fatal("want erro para áudio ausente, got nil")
	}
}

func TestHash_EmptySampleList(t *testing.T) {
	h, err := Hash(nil)
	if err != nil {
		t.Fatalf("Hash(nil): %v", err)
	}
	if h == "" {
		t.Fatal("Hash(nil) devolveu string vazia")
	}
}
