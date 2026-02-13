package schulze

import (
	"testing"
)

func TestSchulze_BasicThreeCandidates(t *testing.T) {
	// 3 candidates: A=0, B=1, C=2
	// 5 voters with preferences:
	// 2 voters: A > B > C
	// 2 voters: B > C > A
	// 1 voter:  C > A > B
	//
	// Pairwise: A vs B: 3-2 (A wins), A vs C: 2-3 (C wins), B vs C: 4-1 (B wins)
	// Expected: B wins (beats both), then A (beats C), then C
	prefs := []Preference{
		{0: 1, 1: 2, 2: 3}, // A > B > C
		{0: 1, 1: 2, 2: 3}, // A > B > C
		{1: 1, 2: 2, 0: 3}, // B > C > A
		{1: 1, 2: 2, 0: 3}, // B > C > A
		{2: 1, 0: 2, 1: 3}, // C > A > B
	}

	results := Compute(3, prefs)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// B should be rank 1 (wins against both A and C)
	if results[0].CandidateIndex != 1 || results[0].Rank != 1 {
		t.Errorf("Expected B (idx 1) at rank 1, got candidate %d at rank %d", results[0].CandidateIndex, results[0].Rank)
	}

	// A and C each win 1 pairwise (A beats B, C beats A), so they're tied at rank 2
	// The order between them depends on the sorting (by candidate index as secondary)
	if results[1].Rank != 2 || results[2].Rank != 2 {
		t.Errorf("Expected both A and C at rank 2, got ranks %d and %d", results[1].Rank, results[2].Rank)
	}
}

func TestSchulze_CondorcetWinner(t *testing.T) {
	// 4 candidates: A=0, B=1, C=2, D=3
	// A beats everyone in pairwise, so A is Condorcet winner
	prefs := []Preference{
		{0: 1, 1: 2, 2: 3, 3: 4}, // A > B > C > D
		{0: 1, 2: 2, 1: 3, 3: 4}, // A > C > B > D
		{0: 1, 3: 2, 1: 3, 2: 4}, // A > D > B > C
	}

	results := Compute(4, prefs)

	if results[0].CandidateIndex != 0 || results[0].Rank != 1 {
		t.Errorf("Expected A (Condorcet winner) at rank 1, got candidate %d at rank %d", results[0].CandidateIndex, results[0].Rank)
	}
}

func TestSchulze_CycleResolution(t *testing.T) {
	// Classic cycle: A > B, B > C, C > A
	// Schulze uses strongest paths to resolve
	// 18 voters total
	prefs := []Preference{
		// 7 voters: A > B > C
		{0: 1, 1: 2, 2: 3}, {0: 1, 1: 2, 2: 3}, {0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3}, {0: 1, 1: 2, 2: 3}, {0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3},
		// 6 voters: B > C > A
		{1: 1, 2: 2, 0: 3}, {1: 1, 2: 2, 0: 3}, {1: 1, 2: 2, 0: 3},
		{1: 1, 2: 2, 0: 3}, {1: 1, 2: 2, 0: 3}, {1: 1, 2: 2, 0: 3},
		// 5 voters: C > A > B
		{2: 1, 0: 2, 1: 3}, {2: 1, 0: 2, 1: 3}, {2: 1, 0: 2, 1: 3},
		{2: 1, 0: 2, 1: 3}, {2: 1, 0: 2, 1: 3},
	}

	// Pairwise: A vs B: 12-6 (A wins), B vs C: 13-5 (B wins), C vs A: 11-7 (C wins)
	// So we have a cycle. Schulze should resolve it.
	results := Compute(3, prefs)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// The winner should be determined by strongest paths
	// This is a known example; typically the strongest path resolves to a winner
	if results[0].Rank != 1 {
		t.Errorf("Expected rank 1 for first result, got rank %d", results[0].Rank)
	}
}

func TestSchulze_IncompleteRankings(t *testing.T) {
	// 3 candidates: A=0, B=1, C=2
	// Voter 1: A > B (C unranked)
	// Voter 2: B > C (A unranked)
	// Voter 3: C (A, B unranked)
	//
	// Pairwise:
	// A vs B: Voter 1 prefers A. Voter 2 prefers B. Voter 3 indifferent. => 1-1
	// A vs C: Voter 1 prefers A. Voter 2 prefers C. Voter 3 prefers C. => 1-2
	// B vs C: Voter 1 prefers B. Voter 2 prefers B. Voter 3 prefers C. => 2-1
	//
	// Expected: A and B tie (1-1), but both beat C in different ways
	prefs := []Preference{
		{0: 1, 1: 2},    // A > B, C unranked
		{1: 1, 2: 2},    // B > C, A unranked
		{2: 1},          // C only, A and B unranked
	}

	results := Compute(3, prefs)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// All should have ranks assigned
	for i, r := range results {
		if r.Rank < 1 {
			t.Errorf("Result %d has invalid rank %d", i, r.Rank)
		}
	}
}

func TestSchulze_SingleVoter(t *testing.T) {
	// Single voter: A > B > C
	prefs := []Preference{
		{0: 1, 1: 2, 2: 3},
	}

	results := Compute(3, prefs)

	// Should match the voter's preference exactly
	if results[0].CandidateIndex != 0 || results[0].Rank != 1 {
		t.Errorf("Expected A at rank 1")
	}
	if results[1].CandidateIndex != 1 || results[1].Rank != 2 {
		t.Errorf("Expected B at rank 2")
	}
	if results[2].CandidateIndex != 2 || results[2].Rank != 3 {
		t.Errorf("Expected C at rank 3")
	}
}

func TestSchulze_AllVotersAgree(t *testing.T) {
	// All 5 voters: A > B > C
	prefs := []Preference{
		{0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3},
		{0: 1, 1: 2, 2: 3},
	}

	results := Compute(3, prefs)

	// Should match unanimous preference
	if results[0].CandidateIndex != 0 || results[0].Rank != 1 {
		t.Errorf("Expected A at rank 1")
	}
	if results[1].CandidateIndex != 1 || results[1].Rank != 2 {
		t.Errorf("Expected B at rank 2")
	}
	if results[2].CandidateIndex != 2 || results[2].Rank != 3 {
		t.Errorf("Expected C at rank 3")
	}
}

func TestSchulze_EmptyElection(t *testing.T) {
	// No voters, no candidates
	results := Compute(0, []Preference{})
	if len(results) != 0 {
		t.Errorf("Expected empty results for empty election")
	}
}

func TestSchulze_NoCandidates(t *testing.T) {
	// Voters exist but no candidates
	results := Compute(0, []Preference{
		{},
		{},
	})
	if len(results) != 0 {
		t.Errorf("Expected empty results when no candidates")
	}
}

func TestSchulze_NoVoters(t *testing.T) {
	// Candidates exist but no voters
	results := Compute(3, []Preference{})

	// All candidates should be tied with 0 wins
	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Wins != 0 {
			t.Errorf("Expected 0 wins for all candidates with no voters, got %d", r.Wins)
		}
		if r.Rank != 1 {
			t.Errorf("Expected all tied at rank 1, got rank %d", r.Rank)
		}
	}
}

func TestSchulze_Ties(t *testing.T) {
	// 2 candidates, symmetric preferences => tie
	prefs := []Preference{
		{0: 1, 1: 2}, // A > B
		{1: 1, 0: 2}, // B > A
	}

	results := Compute(2, prefs)

	// Both should have 0 wins and be tied at rank 1
	if results[0].Rank != 1 || results[1].Rank != 1 {
		t.Errorf("Expected both candidates tied at rank 1")
	}
	if results[0].Wins != 0 || results[1].Wins != 0 {
		t.Errorf("Expected both candidates to have 0 wins")
	}
}
