package cli

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

// resolveSearchSeed returns the RNG seed. seed 0 picks a new seed and logs it.
func resolveSearchSeed(seed int64) (int64, *rand.Rand) {
	if seed == 0 {
		seed = time.Now().UnixNano()
		fmt.Fprintf(os.Stderr, "[+] search shuffle seed %d (pass -seed %d to replay this order)\n", seed, seed)
	} else {
		fmt.Fprintf(os.Stderr, "[+] search shuffle seed %d\n", seed)
	}
	return seed, rand.New(rand.NewSource(seed))
}

func shuffleSources(specs []srcSpec, rng *rand.Rand) {
	rng.Shuffle(len(specs), func(i, j int) {
		specs[i], specs[j] = specs[j], specs[i]
	})
}

type pageJob struct {
	src        srcSpec
	page       int
	prefetched []Repo
}

func shufflePageJobs(jobs []pageJob, rng *rand.Rand) {
	rng.Shuffle(len(jobs), func(i, j int) {
		jobs[i], jobs[j] = jobs[j], jobs[i]
	})
}
