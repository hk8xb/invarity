package test

import (
	"testing"

	"invarity/internal/llm"
	"invarity/internal/types"
)

func TestAggregateIntentVotes_AllDenyIsDeny(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.IntentVoterResult
		expected types.IntentDecision
	}{
		{
			name: "all deny",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteDeny},
				{VoterID: "v2", Vote: types.IntentVoteDeny},
				{VoterID: "v3", Vote: types.IntentVoteDeny},
			},
			expected: types.IntentDecisionDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateIntentVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateIntentVotes_AnyDenyIsEscalate(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.IntentVoterResult
		expected types.IntentDecision
	}{
		{
			name: "single deny among safe",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteSafe},
				{VoterID: "v2", Vote: types.IntentVoteDeny},
				{VoterID: "v3", Vote: types.IntentVoteSafe},
			},
			expected: types.IntentDecisionEscalate,
		},
		{
			name: "deny with abstain",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteAbstain},
				{VoterID: "v2", Vote: types.IntentVoteDeny},
				{VoterID: "v3", Vote: types.IntentVoteSafe},
			},
			expected: types.IntentDecisionEscalate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateIntentVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateIntentVotes_AllSafeIsSafe(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.IntentVoterResult
		expected types.IntentDecision
	}{
		{
			name: "all safe",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteSafe},
				{VoterID: "v2", Vote: types.IntentVoteSafe},
				{VoterID: "v3", Vote: types.IntentVoteSafe},
			},
			expected: types.IntentDecisionSafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateIntentVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateIntentVotes_AnyAbstainIsEscalate(t *testing.T) {
	tests := []struct {
		name     string
		voters   []types.IntentVoterResult
		expected types.IntentDecision
	}{
		{
			name: "single abstain among safe",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteSafe},
				{VoterID: "v2", Vote: types.IntentVoteAbstain},
				{VoterID: "v3", Vote: types.IntentVoteSafe},
			},
			expected: types.IntentDecisionEscalate,
		},
		{
			name: "all abstain",
			voters: []types.IntentVoterResult{
				{VoterID: "v1", Vote: types.IntentVoteAbstain},
				{VoterID: "v2", Vote: types.IntentVoteAbstain},
				{VoterID: "v3", Vote: types.IntentVoteAbstain},
			},
			expected: types.IntentDecisionEscalate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := llm.AggregateIntentVotes(tt.voters)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestAggregateIntentVotes_EdgeCases(t *testing.T) {
	// Empty voters should escalate (safe default)
	result := llm.AggregateIntentVotes([]types.IntentVoterResult{})
	if result != types.IntentDecisionEscalate {
		t.Errorf("empty voters: got %s, want ESCALATE (safe default)", result)
	}

	// Single voter scenarios
	singleSafe := llm.AggregateIntentVotes([]types.IntentVoterResult{
		{VoterID: "v1", Vote: types.IntentVoteSafe},
	})
	if singleSafe != types.IntentDecisionSafe {
		t.Errorf("single safe: got %s, want SAFE", singleSafe)
	}

	singleDeny := llm.AggregateIntentVotes([]types.IntentVoterResult{
		{VoterID: "v1", Vote: types.IntentVoteDeny},
	})
	if singleDeny != types.IntentDecisionDeny {
		t.Errorf("single deny: got %s, want DENY", singleDeny)
	}

	singleAbstain := llm.AggregateIntentVotes([]types.IntentVoterResult{
		{VoterID: "v1", Vote: types.IntentVoteAbstain},
	})
	if singleAbstain != types.IntentDecisionEscalate {
		t.Errorf("single abstain: got %s, want ESCALATE", singleAbstain)
	}
}
