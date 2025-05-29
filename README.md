# Football League Simulator

This project is a football league simulation application. It simulates matches between teams, updates the league table, and makes championship predictions.

## Features

- League table display
- Weekly match simulation
- Championship predictions (using Monte Carlo simulation)
- ELO rating based match result prediction
- Web interface
- REST API support

## Technical Details

- Go 1.24.3
- PostgreSQL database
- Gorilla Mux web framework
- ELO rating system

## Simulation Algorithms

### Match Simulation (Poisson Distribution)
The application uses a Poisson distribution to simulate match results, which is a common statistical model for predicting football scores. The process works as follows:

1. Base Attack Strength:
   - Uses team ELO ratings as base strength
   - Adds head-to-head form factor (0.4 weight for each previous win)
   - Combines home/away team strengths

2. Goal Prediction:
   - Converts attack strengths to Poisson distribution parameters
   - Scales to achieve average of ~3 goals per match
   - Samples from Poisson distribution for both teams
   - Formula: `λ = (team_strength / total_strength) * 3.0`

3. Implementation:
```go
// Base attack strength using ELO + form factors
baseHome := m.Home.ELO + hhHome*0.4
baseAway := m.Away.ELO + hhAway*0.4

// Normalize into Poisson means
total := baseHome + baseAway
lambdaHome := baseHome / total * 3.0
lambdaAway := baseAway / total * 3.0

// Sample goals using Poisson distribution
homeGoals = samplePoisson(lambdaHome)
awayGoals = samplePoisson(lambdaAway)
```

### Championship Prediction (Monte Carlo)
The application uses Monte Carlo simulation to predict championship probabilities:

1. Simulation Process:
   - Runs multiple simulations (default: 1000) of remaining matches
   - Each simulation uses the Poisson-based match simulation
   - Tracks the number of times each team wins the league

2. Probability Calculation:
   - For each team: `probability = (number_of_wins / total_simulations) * 100`
   - Results are sorted by probability in descending order

3. Implementation:
```go
// Count wins for each team
wins := make(map[int]int, len(teams))
for i := 0; i < runs; i++ {
    champID := s.SimulateChampion(teams, fixtures, startWeek)
    wins[champID]++
}

// Calculate probabilities
for _, t := range teams {
    count := wins[t.ID]
    probability := (float64(count) / float64(runs)) * 100.0
    predictions = append(predictions, Prediction{
        Team: t,
        Probability: probability
    })
}
```

## API Endpoints

You can find a complete Postman collection for testing all API endpoints here:
[Football League Simulator API Collection](https://www.postman.com/cloudy-spaceship-341537/workspace/insiderinternship/collection/41015291-e7debb3c-f691-488f-8076-9f687ea944c5?action=share&creator=41015291)

### Web Interface
- `GET /` - Shows the league table
- `GET /champion` - Shows the champion
- `GET /simulateNextWeek` - Simulates the next week
- `POST /restart` - Restarts the season

### REST API
- `GET /API/table` - Returns the league table in JSON format
- `GET /API/fixture` - Returns the fixture in JSON format
- `GET /API/getPredictions` - Returns championship predictions
- `POST /API/simulateNextWeek` - Simulates the next week
- `GET /API/simulateAll` - Simulates all remaining matches
- `POST /API/restart` - Restarts the season

## Database

The application uses PostgreSQL as its database. Here are the main database operations:

### Database Migration

The application includes an automatic database migration system that creates the necessary tables if they don't exist. The migration is handled by the `Migrate()` function which creates two main tables:

1. Teams Table:
```sql
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
```

2. Matches Table:
```sql
CREATE TABLE IF NOT EXISTS matches (
    id SERIAL PRIMARY KEY,
    week INT NOT NULL,
    home_team TEXT NOT NULL REFERENCES teams(name),
    away_team TEXT NOT NULL REFERENCES teams(name),
    home_goals INT,
    away_goals INT
);
```

Key features of the migration:
- Uses `IF NOT EXISTS` to prevent errors on reruns
- Sets up proper foreign key relationships
- Initializes default values for team statistics
- Uses SERIAL for auto-incrementing IDs
- Establishes proper constraints (UNIQUE, NOT NULL)

### Data Persistence Functions

1. Team Management:
   ```go
   // InsertTeams persists multiple teams to the database
   func (s *Store) InsertTeams(teams []*league.Team) error {
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
   ```
   Key features:
   - Uses `ON CONFLICT DO NOTHING` for idempotent operations
   - Sets initial ELO rating to 800
   - Handles multiple team insertions in a single function call
   - Returns detailed error messages

2. Match Management:
   ```go
   // SaveMatch persists a match result for a given week
   func (s *Store) SaveMatch(m *league.Match) error {
       query := `
       INSERT INTO matches (week, home_team, away_team, home_goals, away_goals)
       VALUES ($1, $2, $3, $4, $5)
       RETURNING id
       `
       err := s.DB.QueryRow(
           query, 
           m.Week, 
           m.Home.Name, 
           m.Away.Name, 
           m.HomeGoals, 
           m.AwayGoals
       ).Scan(&m.ID)
       
       if err != nil {
           return fmt.Errorf("saving match: %w", err)
       }
       return nil
   }
   ```
   Key features:
   - Uses `RETURNING id` to get the generated match ID
   - Stores team names as references to maintain data integrity
   - Handles both new matches and updates
   - Returns detailed error messages

3. Batch Operations:
   ```go
   // InitFullSeason saves the entire season's schedule
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
   ```
   Key features:
   - Handles bulk insertion of matches
   - Maintains week-by-week organization
   - Provides transaction-like behavior
   - Returns detailed error messages

### Main Operations

1. Team Operations:
   - Insert teams: `INSERT INTO teams (id, name, ELO) VALUES ($1, $2, $3)`
   - Update team stats: `UPDATE teams SET played = $1, win = $2, lose = $3, goals_for = $4, goals_against = $5, points = $6 WHERE id = $7`
   - Update ELO rating: `UPDATE teams SET elo = $1 WHERE id = $2`

2. Match Operations:
   - Insert/Update match: `INSERT INTO matches (week, home_team, away_team, home_goals, away_goals) VALUES ($1, $2, $3, $4, $5)`
   - Get previous matches: `SELECT home_team, away_team, home_goals, away_goals FROM matches WHERE (home_team = $1 AND away_team = $2) OR (home_team = $2 AND away_team = $1)`
   - Get fixture: `SELECT week, home_team, away_team, home_goals, away_goals FROM matches ORDER BY week, home_team`

3. Maintenance Operations:
   - Delete all matches: `DELETE FROM matches`
   - Delete all teams: `DELETE FROM teams`

## Installation

1. Install Go (1.24.3 or higher)
2. Install and run PostgreSQL
3. Clone the project:
   ```bash
   git clone https://github.com/utakatalp/league-simulator.git
   cd league-simulator
   ```
4. Install dependencies:
   ```bash
   go mod download
   ```
5. Configure database connection settings
6. Run the application:
   ```bash
   go run cmd/simulator/main.go
   ```



## Project Structure

```
.
├── cmd/
│   └── simulator/     # Main application
├── internal/
│   ├── league/        # League management
│   ├── store/         # Database operations
│   └── sim/           # Simulation logic
├── pkg/               # Public packages
├── go.mod
└── go.sum
```

## Contributing

1. Fork this repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details. 