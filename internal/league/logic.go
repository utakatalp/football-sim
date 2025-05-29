// internal/league/logic.go
package league

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

func (m *Match) ScoreLine() string {
	return fmt.Sprintf("%s %d - %d %s",
		m.Home.Name, m.HomeGoals,
		m.AwayGoals, m.Away.Name,
	)
}

// SimulateMatch sets HomeGoals and AwayGoals for a match based on team strengths.
// If scores are already non-zero, it assumes a manual override and does not simulate.
func SimulateMatch(
	m *Match,
	prevResults map[string]int, // results[teamName] = # times that team has beaten this opponent
) (homeGoals, awayGoals int) {
	// 1) manual override?
	if m.HomeGoals != 0 || m.AwayGoals != 0 {
		return m.HomeGoals, m.AwayGoals
	}

	// 2) head-to-head form & points form
	hhHome := float64(prevResults[m.Home.Name]) // each prior H2H win = +1.0
	hhAway := float64(prevResults[m.Away.Name])

	// 3) base "attack strength" using ELO + form factors
	baseHome := m.Home.ELO + hhHome*0.4
	baseAway := m.Away.ELO + hhAway*0.4

	// 4) normalize into Poisson means (scaled so avg goals ≈3)
	total := baseHome + baseAway
	lambdaHome := baseHome / total * 3.0
	lambdaAway := baseAway / total * 3.0

	// 5) sample goals with added randomness
	homeGoals = samplePoisson(lambdaHome) + rand.Intn(2) // Add randomness
	awayGoals = samplePoisson(lambdaAway) + rand.Intn(2) // Add randomness

	// updating the structs
	m.Away.Goals_Against = m.Away.Goals_Against + homeGoals
	m.Away.Goals_For = m.Away.Goals_For + awayGoals
	m.Away.Played++
	m.Home.Goals_Against = m.Home.Goals_Against + awayGoals
	m.Home.Goals_For = m.Home.Goals_For + homeGoals
	m.Home.Played++
	// fmt.Printf("%d. Week \n", m.Week)
	return
}

func GenerateFullSeason(teams []*Team) [][]*Match {
	// First half schedule
	firstHalf := GenerateSchedule(teams)
	// Second half with swapped home/away
	secondHalf := make([][]*Match, len(firstHalf))
	for i, rnd := range firstHalf {
		swapped := make([]*Match, len(rnd))
		for j, m := range rnd {
			swapped[j] = &Match{Home: m.Away, Away: m.Home, Week: i + len(teams)}
		}
		secondHalf[i] = swapped
	}
	// Combine both halves
	return append(firstHalf, secondHalf...)
}

func PrintSchedule(label string, season [][]*Match) {
	fmt.Println(label)
	for _, round := range season {
		fmt.Printf("Week %d:\n", round[0].Week)
		for _, match := range round {
			fmt.Printf("  %s vs %s\n", match.Home.Name, match.Away.Name)
		}
	}
}

// GenerateSchedule returns a round-robin schedule for the provided teams.
// It outputs a slice of rounds, each round being a slice of pointers to Match.
func GenerateSchedule(teams []*Team) [][]*Match {
	n := len(teams)
	// If odd number of teams, add a nil placeholder (bye)
	odd := false
	if n%2 != 0 {
		odd = true
		teams = append(teams, nil)
		n++
	}

	// Create working slice copying teams (excluding fixed first element)
	// subs := make([]*Team, n)
	// copy(subs, teams)

	rounds := make([][]*Match, n-1)
	for i := 0; i < n-1; i++ {
		round := make([]*Match, n/2)
		for j := 0; j < n/2; j++ {
			home := teams[j]
			away := teams[n-1-j]
			if home != nil && away != nil {
				round[j] = &Match{Home: home, Away: away, Week: i + 1}
			}
		}
		rounds[i] = round

		// Rotate teamscribers (except first)
		last := teams[len(teams)-1]
		copy(teams[2:], teams[1:len(teams)-1])
		teams[1] = last
	}

	if odd {
		// Remove nil matches
		for i, rnd := range rounds {
			filtered := rnd[:0]
			for _, m := range rnd {
				if m != nil {
					filtered = append(filtered, m)
				}
			}
			rounds[i] = filtered
		}
	}

	return rounds
}

// samplePoisson generates a random sample from a Poisson distribution with mean lambda
func samplePoisson(lambda float64) int {
	L := math.Exp(-lambda)
	p := 1.0
	k := 0
	for p > L {
		k++
		p *= rand.Float64()
	}
	return k - 1
}

func CalculateTable(matches []*Match) []*TableEntry {
	// Initialize table entries
	entriesMap := make(map[*Team]*TableEntry)
	for _, m := range matches {
		teams := []*Team{m.Home, m.Away}
		for _, t := range teams {
			if _, ok := entriesMap[t]; !ok {
				entriesMap[t] = &TableEntry{Team: t}
			}
		}

		// Update played count
		entriesMap[m.Home].Played++
		entriesMap[m.Away].Played++

		// Goals
		entriesMap[m.Home].GoalsFor += m.HomeGoals
		entriesMap[m.Home].GoalsAgainst += m.AwayGoals
		entriesMap[m.Away].GoalsFor += m.AwayGoals
		entriesMap[m.Away].GoalsAgainst += m.HomeGoals

		// Win/Draw/Loss and Points
		switch {
		case m.HomeGoals > m.AwayGoals:
			entriesMap[m.Home].Wins++
			entriesMap[m.Away].Losses++
			entriesMap[m.Home].Points += 3
		case m.HomeGoals < m.AwayGoals:
			entriesMap[m.Away].Wins++
			entriesMap[m.Home].Losses++
			entriesMap[m.Away].Points += 3
		default:
			entriesMap[m.Home].Draws++
			entriesMap[m.Away].Draws++
			entriesMap[m.Home].Points++
			entriesMap[m.Away].Points++
		}
	}

	// Collect and compute GoalDiff
	entries := make([]*TableEntry, 0, len(entriesMap))
	for _, e := range entriesMap {
		e.GoalDiff = e.GoalsFor - e.GoalsAgainst
		entries = append(entries, e)
	}

	// Sort
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.GoalDiff != b.GoalDiff {
			return a.GoalDiff > b.GoalDiff
		}
		if a.GoalsFor != b.GoalsFor {
			return a.GoalsFor > b.GoalsFor
		}
		return a.Team.Name < b.Team.Name
	})

	return entries
}
func PrintTable(label string, table []*TableEntry) {
	fmt.Println(label)
	fmt.Printf("%-10s %2s %2s %2s %2s %3s %3s %3s %3s",
		"Team", "P", "W", "D", "L", "GF", "GA", "GD", "Pts")
	for _, entry := range table {
		fmt.Printf("\n%-10s %2d %2d %2d %2d %3d %3d %3d %3d",
			entry.Team.Name,
			entry.Played,
			entry.Wins,
			entry.Draws,
			entry.Losses,
			entry.GoalsFor,
			entry.GoalsAgainst,
			entry.GoalDiff,
			entry.Points,
		)
	}
}
