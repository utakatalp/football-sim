package league

type Prediction struct {
	Team        *Team
	Probability float64
}

// Team represents a club in the league.
type Team struct {
	ID            int
	Name          string
	Played        int
	Points        int
	Win           int
	Lose          int
	Goals_For     int
	Goals_Against int
	ELO           float64 `json:"elo"`
}

// Match represents a fixture between two teams.
type Match struct {
	Home, Away *Team
	HomeGoals  int
	Week       int
	AwayGoals  int
	ID         int
}

// TableEntry holds the standings info for one team.
type TableEntry struct {
	Team                        *Team
	Played, Wins, Draws, Losses int
	GoalsFor, GoalsAgainst      int
	GoalDiff, Points            int
}
