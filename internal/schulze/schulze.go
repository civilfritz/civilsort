package schulze

import "sort"

// Preference represents one voter's ranking.
// The key is the candidate index, the value is the rank (1 = best).
// Candidates absent from the map are considered unranked.
type Preference map[int]int

// Result holds one candidate's final standing.
type Result struct {
	CandidateIndex int
	Rank           int // 1 = winner
	Wins           int // Number of pairwise wins (for internal sorting)
}

// Compute takes the number of candidates and all voter preferences,
// and returns the Schulze ranking (sorted by Rank ascending).
func Compute(numCandidates int, preferences []Preference) []Result {
	if numCandidates == 0 {
		return []Result{}
	}

	// Step 1: Build pairwise preference matrix d[i][j]
	d := buildPairwise(numCandidates, preferences)

	// Step 2: Initialize strongest path matrix p[i][j]
	p := initStrengthMatrix(numCandidates, d)

	// Step 3: Compute strongest paths (Floyd-Warshall variant)
	computeStrongestPaths(numCandidates, p)

	// Step 4: Determine ranking from p
	return determineRanking(numCandidates, p)
}

// buildPairwise constructs the pairwise preference matrix.
// d[i][j] = number of voters who prefer candidate i over candidate j.
func buildPairwise(n int, prefs []Preference) [][]int {
	d := make([][]int, n)
	for i := range d {
		d[i] = make([]int, n)
	}

	for _, p := range prefs {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				ri, iRanked := p[i]
				rj, jRanked := p[j]

				switch {
				case iRanked && jRanked && ri < rj:
					// i is ranked higher (lower position) than j
					d[i][j]++
				case iRanked && !jRanked:
					// i is ranked, j is not; i is preferred
					d[i][j]++
				// Other cases: j preferred or tied, no increment to d[i][j]
				}
			}
		}
	}

	return d
}

// initStrengthMatrix initializes the strongest path matrix.
// p[i][j] = strength of direct win from i to j (0 if i doesn't beat j).
func initStrengthMatrix(n int, d [][]int) [][]int {
	p := make([][]int, n)
	for i := range p {
		p[i] = make([]int, n)
	}

	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j {
				if d[i][j] > d[j][i] {
					p[i][j] = d[i][j]
				} else {
					p[i][j] = 0
				}
			}
		}
	}

	return p
}

// computeStrongestPaths runs the Floyd-Warshall variant to find strongest paths.
// p[j][k] = max(p[j][k], min(p[j][i], p[i][k]))
func computeStrongestPaths(n int, p [][]int) {
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			for k := 0; k < n; k++ {
				if i == k || j == k {
					continue
				}
				strength := min(p[j][i], p[i][k])
				if strength > p[j][k] {
					p[j][k] = strength
				}
			}
		}
	}
}

// determineRanking computes the final ranking from the strongest path matrix.
// Candidate i beats j if p[i][j] > p[j][i].
func determineRanking(n int, p [][]int) []Result {
	wins := make([]int, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j && p[i][j] > p[j][i] {
				wins[i]++
			}
		}
	}

	results := make([]Result, n)
	for i := range results {
		results[i] = Result{
			CandidateIndex: i,
			Wins:           wins[i],
		}
	}

	// Sort by wins descending
	sort.Slice(results, func(a, b int) bool {
		return results[a].Wins > results[b].Wins
	})

	// Assign ranks (handling ties)
	rank := 1
	for i := range results {
		if i > 0 && results[i].Wins < results[i-1].Wins {
			rank = i + 1
		}
		results[i].Rank = rank
	}

	return results
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
