package workflow

import "testing"

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		phase string
		want  string
	}{
		{"async_pending", "In Progress"},
		{"addressing_feedback", "Addressing Feedback"},
		{"docker_pending", "Starting Container"},
		{"pushing", "Pushing"},
		{"retry_pending", "Retrying"},
		{"idle", "Idle"},
		{"", "Idle"},
		// Unknown values get title-cased
		{"some_custom_phase", "Some Custom Phase"},
		{"waiting", "Waiting"},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			got := PhaseLabel(tt.phase)
			if got != tt.want {
				t.Errorf("PhaseLabel(%q) = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

func TestStepLabel(t *testing.T) {
	tests := []struct {
		step string
		want string
	}{
		{"", "—"},
		{"coding", "Coding"},
		{"await_ci", "Await Ci"},
		{"fix_ci", "Fix Ci"},
		{"open_pr", "Open Pr"},
		{"address_review", "Address Review"},
		{"push_conflict_fix", "Push Conflict Fix"},
		// Template-expanded names: _t_<prefix>_<state>
		{"_t_plan_planning", "Planning"},
		{"_t_ci_await_ci", "Await Ci"},
		{"_t_review_address_review", "Address Review"},
		// Single-word prefix
		{"_t_x_coding", "Coding"},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			got := StepLabel(tt.step)
			if got != tt.want {
				t.Errorf("StepLabel(%q) = %q, want %q", tt.step, got, tt.want)
			}
		})
	}
}

func TestToTitleCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"hello world", "Hello World"},
		{"foo bar baz", "Foo Bar Baz"},
		{"already Title", "Already Title"},
		{"single", "Single"},
	}
	for _, tt := range tests {
		got := toTitleCase(tt.input)
		if got != tt.want {
			t.Errorf("toTitleCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
