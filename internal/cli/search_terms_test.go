package cli

import (
	"math/rand"
	"testing"
)

func TestTermToQueryPlain(t *testing.T) {
	q, code := termToQuery("sk-proj-")
	if q != `"sk-proj-" in:file` || !code {
		t.Fatalf("got %q code=%v", q, code)
	}
}

func TestTermToQueryPassThrough(t *testing.T) {
	q, code := termToQuery(`openai in:file language:python`)
	if q != `openai in:file language:python` || !code {
		t.Fatalf("got %q code=%v", q, code)
	}
}

func TestShufflePageJobsDeterministic(t *testing.T) {
	jobs := []pageJob{
		{page: 1}, {page: 2}, {page: 3}, {page: 4},
	}
	rng1 := rand.New(rand.NewSource(42))
	rng2 := rand.New(rand.NewSource(42))
	a := append([]pageJob{}, jobs...)
	b := append([]pageJob{}, jobs...)
	shufflePageJobs(a, rng1)
	shufflePageJobs(b, rng2)
	for i := range a {
		if a[i].page != b[i].page {
			t.Fatalf("seed 42 mismatch at %d: %d vs %d", i, a[i].page, b[i].page)
		}
	}
}
