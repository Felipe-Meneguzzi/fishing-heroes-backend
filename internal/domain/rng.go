package domain

import "math"

// RNG determinístico por evento.
//
// Cada evento de pesca deriva seu próprio stream de hash(seed, índice), de modo
// que o resultado de qualquer evento é reproduzível e independente da ordem em
// que os lotes são resolvidos — base do anti-cheat e da aditividade dos lotes.
//
// Usa splitmix64: rápido, sem estado externo, boa distribuição.

const golden = 0x9E3779B97F4A7C15

type rng struct{ state uint64 }

// newEventRNG cria o stream determinístico do evento `index` da sessão `seed`.
func newEventRNG(seed uint64, index uint64) *rng {
	// Mistura seed e índice para descorrelacionar streams de eventos vizinhos.
	s := mix64(seed) ^ mix64(index*golden+0x6A09E667F3BCC909)
	return &rng{state: s}
}

func mix64(x uint64) uint64 {
	z := x + golden
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// nextU64 avança o stream.
func (r *rng) nextU64() uint64 {
	r.state += golden
	return mix64Raw(r.state)
}

func mix64Raw(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Float64 retorna um valor em [0, 1).
func (r *rng) Float64() float64 {
	return float64(r.nextU64()>>11) / float64(uint64(1)<<53)
}

// Intn retorna um inteiro em [0, n).
func (r *rng) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.nextU64() % uint64(n))
}

// NormFloat64 retorna uma amostra da Normal padrão N(0,1) via Box-Muller.
// Determinística: consome dois uniformes do stream do evento.
func (r *rng) NormFloat64() float64 {
	u1 := r.Float64()
	if u1 < 1e-12 {
		u1 = 1e-12 // evita log(0)
	}
	u2 := r.Float64()
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}
