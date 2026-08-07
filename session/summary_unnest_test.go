package session

import "testing"

func TestNormalizeSummaryGoal(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "string-escaped nested goal",
			in:   `{"goal":"{\"goal\":\"fix pin sheet\",\"r\":1}","root_causes":null}`,
			want: `{"goal":"fix pin sheet","root_causes":null}`,
		},
		{
			name: "markdown-wrapped nested goal",
			in:   "{\"goal\":\"```json\\n{\\n  \\\"goal\\\": \\\"understand project\\\",\\n  \\\"root_causes\\\": []\\n}\\n```\",\"root_causes\":null}",
			want: "{\"goal\":\"understand project\",\"root_causes\":null}",
		},
		{
			name: "clean goal unchanged",
			in:   `{"goal":"ship v2","root_causes":null}`,
			want: `{"goal":"ship v2","root_causes":null}`,
		},
		{
			name: "non-json unchanged",
			in:   `not json at all`,
			want: `not json at all`,
		},
		{
			name: "empty unchanged",
			in:   ``,
			want: ``,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeSummaryGoal(c.in); got != c.want {
				t.Errorf("NormalizeSummaryGoal mismatch:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestLooseExtractGoalContextGuard(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "prose mention not extracted",
			in:   `the "goal": "was to fix"`,
			want: `the "goal": "was to fix"`,
		},
		{
			name: "skip prose match, take real goal",
			in:   `{"note":"the \"goal\": \"x\"","goal":"real goal"}`,
			want: "real goal",
		},
		{
			name: "nested escaped goal still works",
			in:   `{"goal":"inner","r":1}`,
			want: "inner",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UnnestGoal(c.in); got != c.want {
				t.Errorf("UnnestGoal(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
