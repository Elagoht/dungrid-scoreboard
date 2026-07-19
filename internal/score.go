package internal

import (
	"strconv"
)

// Weights holds the score calculation multipliers loaded from env.
type Weights struct {
	Floor       int
	DamageDealt int
	DamageTaken int
	Revive      int
	Quest       int
}

// LoadWeights reads score weights from environment variables with sensible defaults.
func LoadWeights() Weights {
	return Weights{
		Floor:       atoiDefault(Getenv("SCORE_WEIGHT_FLOOR", "100"), 100),
		DamageDealt: atoiDefault(Getenv("SCORE_WEIGHT_DAMAGE_DEALT", "2"), 2),
		DamageTaken: atoiDefault(Getenv("SCORE_WEIGHT_DAMAGE_TAKEN", "1"), 1),
		Revive:      atoiDefault(Getenv("SCORE_WEIGHT_REVIVE", "50"), 50),
		Quest:       atoiDefault(Getenv("SCORE_WEIGHT_QUEST", "200"), 200),
	}
}

// Metrics holds the raw game statistics submitted by the client.
type Metrics struct {
	Floor       int `json:"floor"`
	DamageDealt int `json:"damage_dealt"`
	DamageTaken int `json:"damage_taken"`
	Revives     int `json:"revives"`
	Quests      int `json:"quests"`
}

// CalculateScore computes the final score from metrics using the configured weights.
// Result is clamped to a minimum of 0.
func CalculateScore(m Metrics, w Weights) int {
	score := m.Floor*w.Floor +
		m.DamageDealt*w.DamageDealt -
		m.DamageTaken*w.DamageTaken +
		m.Revives*w.Revive +
		m.Quests*w.Quest

	if score < 0 {
		return 0
	}
	return score
}

func atoiDefault(s string, defaultVal int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return n
}
