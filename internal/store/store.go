package store

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"

	_ "github.com/lib/pq"
	"github.com/utakatalp/league-simulator/internal/league"
)

var counter int

const (
	host     = "localhost"
	port     = 5432
	user     = "postgres"
	password = "1234"
	dbname   = "LeagueSimulator"
)

// Store wraps a Postgres connection and provides methods to persist and retrieve league data.
type Store struct {
	DB *sql.DB
}

func (s *Store) ChampionshipOdds(
	teams []*league.Team,
	fixtures [][]*league.Match,
	startWeek, runs int,
) ([]league.Prediction, error) {
	// 1) Count how many times each team ID wins
	wins := make(map[int]int, len(teams))
	for i := 0; i < runs; i++ {
		champID := s.SimulateChampion(teams, fixtures, startWeek)
		wins[champID]++
	}

	// 2) Turn counts into probabilities, attaching *Team pointers
	preds := make([]league.Prediction, 0, len(teams))
	for _, t := range teams {
		count := wins[t.ID]
		p := (float64(count) / float64(runs)) * 100.0
		preds = append(preds, league.Prediction{Team: t, Probability: math.Round(p*100) / 100})
	}

	// 3) Sort descending by probability
	sort.Slice(preds, func(i, j int) bool {
		return preds[i].Probability > preds[j].Probability
	})

	return preds, nil
}
func (s *Store) SimulateChampion(
	teams []*league.Team,
	fixtures [][]*league.Match,
	startWeek int,
) int {
	// 1) Build a fresh stats record per team, seeded from current standings
	type stats struct {
		points, played, wins, losses, gf, ga int
	}
	statsT := make(map[int]*stats, len(teams))
	for _, t := range teams {
		statsT[t.ID] = &stats{
			points: t.Points,
			played: t.Played,
			wins:   t.Win,
			losses: t.Lose,
			gf:     t.Goals_For,
			ga:     t.Goals_Against,
		}
	}

	// 2) Simulate each remaining week
	for week := startWeek; week < len(fixtures); week++ {
		for _, m := range fixtures[week] {
			// get head-to-head form if you like:
			results := s.LoadPreviousMatchScoresBetweenTwoTeam(*m)
			hg, ag := league.SimulateMatch(m, results)

			homeStats := statsT[m.Home.ID]
			awayStats := statsT[m.Away.ID]

			homeStats.played++
			awayStats.played++
			homeStats.gf += hg
			homeStats.ga += ag
			awayStats.gf += ag
			awayStats.ga += hg

			switch {
			case hg > ag:
				homeStats.points += 3
				homeStats.wins++
				awayStats.losses++
			case ag > hg:
				awayStats.points += 3
				awayStats.wins++
				homeStats.losses++
			default:
				homeStats.points++
				awayStats.points++
			}
		}
	}

	// 3) Pick the champion by highest seeded+simulated points
	bestID, bestPts := 0, math.MinInt64
	for _, t := range teams {
		if statsT[t.ID].points > bestPts {
			bestPts = statsT[t.ID].points
			bestID = t.ID
		}
	}
	fmt.Printf("%d: %d\n", counter, bestID)
	counter++
	return bestID
}

// NewStore opens a Postgres connection using the given connection string.
func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// verify early
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	fmt.Println("Connection succesfull")
	return &Store{DB: db}, nil
}

func InitStore() *Store {
	// Build the Postgres connection string
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname,
	)

	// Connect to the database
	stats, err := NewStore(connStr)
	if err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	return stats
}

// Migrate creates the necessary tables if they do not exist.
func (s *Store) Migrate() error {
	queries := []string{
		`
		CREATE TABLE IF NOT EXISTS teams (
        id            SERIAL PRIMARY KEY,
        name          TEXT    NOT NULL UNIQUE,
        played        INT     NOT NULL DEFAULT 0,
        points        INT     NOT NULL DEFAULT 0,
        win           INT     NOT NULL DEFAULT 0,
        lose          INT     NOT NULL DEFAULT 0,
        goals_for     INT     NOT NULL DEFAULT 0,
        goals_against INT     NOT NULL DEFAULT 0,
        elo           DOUBLE PRECISION NOT NULL DEFAULT 1500
    );
    `,
		`CREATE TABLE IF NOT EXISTS matches (
		    id SERIAL PRIMARY KEY,
		    week INT NOT NULL,
		    home_team TEXT NOT NULL REFERENCES teams(name),
		    away_team TEXT NOT NULL REFERENCES teams(name),
		    home_goals INT,
		    away_goals INT
		);`,
	}
	for _, q := range queries {
		if _, err := s.DB.Exec(q); err != nil {
			return fmt.Errorf("migrating: %w", err)
		}
	}
	return nil
}

func (s *Store) InsertTeams(teams []*league.Team) error {
	// If you want to avoid errors on reruns, you can use ON CONFLICT DO NOTHING
	query := `
    INSERT INTO teams (id, name, ELO)
    VALUES ($1, $2, 800)
    ON CONFLICT (id) DO NOTHING
    `
	for _, t := range teams {
		if _, err := s.DB.Exec(query, t.ID, t.Name); err != nil {
			return fmt.Errorf("inserting team %d (%s): %w", t.ID, t.Name, err)
		}
	}
	return nil
}

func (s *Store) UpdateElo(m *league.Match) error {
	const K = 8.0
	// fetch current Elos
	homeElo := m.Home.ELO
	awayElo := m.Away.ELO

	// expected scores
	expHome := 1.0 / (1.0 + math.Pow(10, (awayElo-homeElo)/400))
	expAway := 1.0 / (1.0 + math.Pow(10, (homeElo-awayElo)/400))

	// actual scores
	var scoreHome, scoreAway float64
	if m.HomeGoals > m.AwayGoals {
		scoreHome, scoreAway = 1, 0
	} else if m.HomeGoals < m.AwayGoals {
		scoreHome, scoreAway = 0, 1
	} else {
		scoreHome, scoreAway = 0.5, 0.5
	}

	// new ratings
	newHome := homeElo + K*(scoreHome-expHome)
	newAway := awayElo + K*(scoreAway-expAway)

	// update in DB
	_, err := s.DB.Exec(
		`UPDATE teams SET elo = $1 WHERE id = $2`,
		newHome, m.Home.ID,
	)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE teams SET elo = $1 WHERE id = $2`,
		newAway, m.Away.ID,
	)
	if err != nil {
		return err
	}

	// update in-memory as well
	m.Home.ELO = newHome
	m.Away.ELO = newAway
	return nil
}
func (s *Store) UpdateTeams(match *league.Match) error {
	// 1) Compute increments
	homePts, homeW, homeL := 0, 0, 0
	awayPts, awayW, awayL := 0, 0, 0

	switch {
	case match.HomeGoals > match.AwayGoals:
		homePts, homeW, homeL = 3, 1, 0
		awayPts, awayW, awayL = 0, 0, 1

	case match.AwayGoals > match.HomeGoals:
		awayPts, awayW, awayL = 3, 1, 0
		homePts, homeW, homeL = 0, 0, 1

	default: // draw
		homePts, awayPts = 1, 1
	}

	// 2) Begin a transaction
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin UpdateTeams tx: %w", err)
	}
	defer tx.Rollback()

	// 3) Prepare the UPDATE statement (by ID)
	const q = `
      UPDATE teams
      SET
        played       = played       + 1,
        points       = points       + $1,
        win          = win          + $2,
        lose         = lose         + $3,
        goals_for    = goals_for    + $4,
        goals_against= goals_against+ $5
      WHERE id = $6
    `
	match.Home.Points += homePts
	match.Home.Win += homeW
	match.Home.Lose += homeL
	match.Away.Points += awayPts
	match.Away.Win += awayW
	match.Away.Lose += awayL

	// 4) Update home team
	if _, err := tx.Exec(
		q,
		homePts, homeW, homeL,
		match.HomeGoals, match.AwayGoals,
		match.Home.ID,
	); err != nil {
		return fmt.Errorf("updating home team %d: %w", match.Home.ID, err)
	}

	// 5) Update away team
	if _, err := tx.Exec(
		q,
		awayPts, awayW, awayL,
		match.AwayGoals, match.HomeGoals,
		match.Away.ID,
	); err != nil {
		return fmt.Errorf("updating away team %d: %w", match.Away.ID, err)
	}

	// 6) Commit
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit UpdateTeams tx: %w", err)
	}
	return nil
}

func (s *Store) GetTeams() ([]*league.Team, error) {
	const q = `
        SELECT 
            id,
            name,
            played,
            points,
            win,
            lose,
            goals_for,
            goals_against,
            ELO
        FROM teams
        ORDER BY id
    `

	rows, err := s.DB.Query(q)
	if err != nil {
		return nil, fmt.Errorf("querying teams: %w", err)
	}
	defer rows.Close()

	var teams []*league.Team
	for rows.Next() {
		t := &league.Team{}
		if err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Played,
			&t.Points,
			&t.Win,
			&t.Lose,
			&t.Goals_For,
			&t.Goals_Against,
			&t.ELO,
		); err != nil {
			return nil, fmt.Errorf("scanning team row: %w", err)
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating teams rows: %w", err)
	}
	return teams, nil
}
func (s *Store) GetTable() ([]*league.Team, error) {
	const q = `
    SELECT
      id,
      name,
      played,
      points,
      win,
      lose,
      goals_for,
      goals_against,
      ELO
    FROM teams
    ORDER BY
      points          DESC,
      (goals_for - goals_against) DESC,
      goals_for       DESC,
      name            ASC
    `
	rows, err := s.DB.Query(q)
	if err != nil {
		return nil, fmt.Errorf("querying table: %w", err)
	}
	defer rows.Close()

	var table []*league.Team
	for rows.Next() {
		t := &league.Team{}
		if err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Played,
			&t.Points,
			&t.Win,
			&t.Lose,
			&t.Goals_For,
			&t.Goals_Against,
			&t.ELO,
		); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		table = append(table, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return table, nil
}

func (s *Store) InitFullSeason(rounds [][]*league.Match) error {
	for _, round := range rounds {
		for _, match := range round {
			err := s.SaveMatch(match)
			if err != nil {
				return fmt.Errorf("saving full season: %w", err)
			}
		}
	}
	return nil
}
func (s *Store) UpdateMatch(m *league.Match) error {
	// Upsert: insert or update existing match record
	query := `UPDATE matches
	SET home_goals = $1, away_goals = $2 WHERE id = $3
		`
	_, err := s.DB.Exec(query, m.HomeGoals, m.AwayGoals, m.ID)
	if err != nil {
		return fmt.Errorf("saving match: %w", err)
	}
	return nil
}

// SaveMatch persists a match result for a given week.
func (s *Store) SaveMatch(m *league.Match) error {
	// Upsert: insert or update existing match record
	query := `
INSERT INTO matches (week, home_team, away_team, home_goals, away_goals)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
`
	err := s.DB.QueryRow(query, m.Week, m.Home.Name, m.Away.Name, m.HomeGoals, m.AwayGoals).Scan(&m.ID)
	if err != nil {
		return fmt.Errorf("saving match: %w", err)
	}
	return nil
}

func (s *Store) DeleteAllMatches() error {
	_, err := s.DB.Exec(`DELETE FROM matches;`)
	if err != nil {
		return fmt.Errorf("deleting all matches: %w", err)
	}
	return nil
}
func (s *Store) DeleteAllTeams() error {
	_, err := s.DB.Exec(`DELETE FROM teams;`)
	if err != nil {
		return fmt.Errorf("deleting all teams: %w", err)
	}
	return nil
}

// func (s *Store) LoadPreviousMatchScoresBetweenTwoTeam(match league.Match) {
func (s *Store) LoadPreviousMatchScoresBetweenTwoTeam(match league.Match) map[string]int {
	var homeTeam, awayTeam string
	query := `
	SELECT home_team, away_team, home_goals, away_goals
	FROM matches
	WHERE (home_team = $1 AND away_team = $2) OR (home_team = $2 AND away_team = $1)
	`
	rows, err := s.DB.Query(query, match.Home.Name, match.Away.Name)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var scores = make(map[string]int)
	for rows.Next() {
		var homeGoals, awayGoals int
		if err := rows.Scan(&homeTeam, &awayTeam, &homeGoals, &awayGoals); err != nil {
			return nil
		}
		if homeGoals > awayGoals {
			scores[match.Home.Name]++
		} else if homeGoals < awayGoals {
			scores[match.Away.Name]++
		}
	}
	// fmt.Printf("\nRecent Wins %s: %d %s: %d", homeTeam, scores[match.Home.Name], awayTeam, scores[match.Away.Name])
	return scores
}

// LoadMatches fetches all played matches up to the specified week.
func (s *Store) LoadMatches(uptoWeek int) ([]*league.Match, error) {
	query := `
SELECT week, home_team, away_team, home_goals, away_goals
FROM matches
WHERE week <= $1
ORDER BY week, id;
`
	rows, err := s.DB.Query(query, uptoWeek)
	if err != nil {
		return nil, fmt.Errorf("querying matches: %w", err)
	}
	defer rows.Close()

	var matches []*league.Match
	for rows.Next() {
		var week, homeID, awayID, homeGoals, awayGoals int
		if err := rows.Scan(&week, &homeID, &awayID, &homeGoals, &awayGoals); err != nil {
			return nil, fmt.Errorf("scanning match: %w", err)
		}
		// lookup teams by ID (could cache)
		home := &league.Team{ID: homeID}
		away := &league.Team{ID: awayID}
		m := &league.Match{Home: home, Away: away, HomeGoals: homeGoals, AwayGoals: awayGoals}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}
