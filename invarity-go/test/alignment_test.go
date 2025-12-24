package test

import (
	"testing"

	"invarity/internal/llm"
	"invarity/internal/types"
)

func TestAggregateVotes_AnyDenyIsDeny(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.AlignmentVoter
		expected types.Vote
	}{
		{
			name: "single deny",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteDeny},
				{VoterID: "v3", Vote: types.VoteAllow},
			},
			expected: types.VoteDeny,
		},
		{
			name: "all deny",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteDeny},
				{VoterID: "v2", Vote: types.VoteDeny},
				{VoterID: "v3", Vote: types.VoteDeny},
			},
			expected: types.VoteDeny,
		},
		{
			name: "deny overrides escalate",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteEscalate},
				{VoterID: "v2", Vote: types.VoteDeny},
				{VoterID: "v3", Vote: types.VoteEscalate},
			},
			expected: types.VoteDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateVotes_MajorityAllow(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.AlignmentVoter
		expected types.Vote
	}{
		{
			name: "all allow",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteAllow},
				{VoterID: "v3", Vote: types.VoteAllow},
			},
			expected: types.VoteAllow,
		},
		{
			name: "2 of 3 allow",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteAllow},
				{VoterID: "v3", Vote: types.VoteEscalate},
			},
			expected: types.VoteAllow,
		},
		{
			name: "exactly 2 allow",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteEscalate},
				{VoterID: "v3", Vote: types.VoteAllow},
			},
			expected: types.VoteAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateVotes_Escalate(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.AlignmentVoter
		expected types.Vote
	}{
		{
			name: "all escalate",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteEscalate},
				{VoterID: "v2", Vote: types.VoteEscalate},
				{VoterID: "v3", Vote: types.VoteEscalate},
			},
			expected: types.VoteEscalate,
		},
		{
			name: "1 allow 2 escalate",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteEscalate},
				{VoterID: "v3", Vote: types.VoteEscalate},
			},
			expected: types.VoteEscalate,
		},
		{
			name: "no majority allow",
			voters: []types.AlignmentVoter{
				{VoterID: "v1", Vote: types.VoteAllow},
				{VoterID: "v2", Vote: types.VoteEscalate},
				{VoterID: "v3", Vote: types.VoteEscalate},
			},
			expected: types.VoteEscalate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateVotes_EdgeCases(t *testing.T) {
	// Empty voters should escalate (safe default)
	result := llm.AggregateVotes([]types.AlignmentVoter{})
	if result != types.VoteEscalate {
		t.Errorf("empty voters: got %s, want ESCALATE", result)
	}

	// Single voter scenarios
	singleAllow := llm.AggregateVotes([]types.AlignmentVoter{
		{VoterID: "v1", Vote: types.VoteAllow},
	})
	if singleAllow != types.VoteEscalate {
		t.Errorf("single allow: got %s, want ESCALATE (no majority)", singleAllow)
	}

	singleDeny := llm.AggregateVotes([]types.AlignmentVoter{
		{VoterID: "v1", Vote: types.VoteDeny},
	})
	if singleDeny != types.VoteDeny {
		t.Errorf("single deny: got %s, want DENY", singleDeny)
	}
}
