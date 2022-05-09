package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestEvaluateConditions(t *testing.T) {
	tests := []struct {
		name                string
		config              conditionsConfig
		buildkiteExitStatus string
		buildkiteBranch     string
		wantErr             bool
		wantResult          bool
	}{
		{
			name:                "no conditions",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "0",
			config:              conditionsConfig{},
			wantResult:          true,
		},
		{
			name:                "OK specific branch",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				branches: []string{"main"},
			},
			wantResult: true,
		},
		{
			name:                "OK specific branches",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				branches: []string{"foo", "main"},
			},
			wantResult: true,
		},
		{
			name:                "NOK specific branch",
			buildkiteBranch:     "other",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				branches: []string{"main"},
			},
			wantResult: false,
		},
		{
			name:                "NOK specific branches",
			buildkiteBranch:     "other",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				branches: []string{"main", "foo"},
			},
			wantResult: false,
		},
		{
			name:                "OK failed",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "1",
			config: conditionsConfig{
				failed: true,
			},
			wantResult: true,
		},
		{
			name:                "NOK failed",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				failed: true,
			},
			wantResult: false,
		},
		{
			name:                "OK exit codes",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "2",
			config: conditionsConfig{
				exitCodes: []int{222, 2},
			},
			wantResult: true,
		},
		{
			name:                "NOK exit codes",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "3",
			config: conditionsConfig{
				exitCodes: []int{222, 2},
			},
			wantResult: false,
		},
		{
			name:                "OK exit codes and failed",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "2",
			config: conditionsConfig{
				exitCodes: []int{222, 2},
				failed:    true,
			},
			wantResult: true,
		},
		{
			name:                "NOK exit codes and failed",
			buildkiteBranch:     "main",
			buildkiteExitStatus: "0",
			config: conditionsConfig{
				exitCodes: []int{222, 2},
				failed:    true,
			},
			wantResult: false,
		},
		{
			name:                "OK branches and exit codes",
			buildkiteBranch:     "foo",
			buildkiteExitStatus: "6",
			config: conditionsConfig{
				branches:  []string{"main", "foo"},
				exitCodes: []int{222, 6},
				failed:    true,
			},
			wantResult: true,
		},
		{
			name:                "NOK branches and exit codes (wrong exit code)",
			buildkiteBranch:     "foo",
			buildkiteExitStatus: "7",
			config: conditionsConfig{
				branches:  []string{"main", "foo"},
				exitCodes: []int{222, 6},
				failed:    true,
			},
			wantResult: false,
		},
		{
			name:                "NOK branches and exit codes (wrong branch)",
			buildkiteBranch:     "bar",
			buildkiteExitStatus: "6",
			config: conditionsConfig{
				branches:  []string{"main", "foo"},
				exitCodes: []int{222, 6},
				failed:    true,
			},
			wantResult: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := evaluateConditions(test.buildkiteExitStatus, test.buildkiteBranch, &config{conditions: test.config})
			if res != test.wantResult {
				t.Logf("wanted result to be %v but got %v instead", test.wantResult, res)
				t.Fail()
			}
		})
	}
}

func TestParseMentions(t *testing.T) {
	tests := []struct {
		message  string
		mentions []string
	}{
		{
			message:  `hello <@jh>`,
			mentions: []string{`jh`},
		},
		{
			message:  `hello <@jh> and <@romeo>`,
			mentions: []string{`jh`, `romeo`},
		},
		{
			message:  `hello <@jh> \n <@romeo> and <@untitled goose>`,
			mentions: []string{`jh`, `romeo`, `untitled goose`},
		},
		{
			message:  `hello <@jh>, <@some user group>, how is it going?`,
			mentions: []string{`jh`, `some user group`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := parseMentions(tt.message)
			if !reflect.DeepEqual(got, tt.mentions) {
				t.Logf("expected to find %s in %q but got %v instead", strings.Join(tt.mentions, ","), tt.message, got)
				t.Fail()
			}
		})
	}
}
