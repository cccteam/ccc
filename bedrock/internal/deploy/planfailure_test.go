package deploy

import (
	"testing"
)

// TestFailedPlanLine: a failed plan's comment line carries the plan's errors, the first
// three of them with a count of the rest, else the fixed line pointing at the log.
func TestFailedPlanLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "no error recorded",
			text: "tofu: something went wrong\n",
			want: "The plan of tst's stack failed; the build log says why (build b1).",
		},
		{
			name: "one provider error, its details and the resource left to the log",
			text: "Plan: 4 to add, 0 to change, 0 to destroy.\n\nError: Error when reading or editing Logging Exclusion x: googleapi: Error 403: Permission 'logging.exclusions.get' denied on resource 'projects/p/exclusions/x' (or it may not exist).\nDetails:\n[\n  {\n    \"reason\": \"IAM_PERMISSION_DENIED\"\n  }\n]\n, forbidden\n\n  with google_logging_project_exclusion.request_log[0],\n  on logging.tf line 115, in resource \"google_logging_project_exclusion\" \"request_log\":\n",
			want: "The plan of tst's stack failed: `Error when reading or editing Logging Exclusion x: googleapi: Error 403: Permission 'logging.exclusions.get' denied on resource 'projects/p/exclusions/x' (or it may not exist).` (build b1 has the log).",
		},
		{
			name: "five errors: three shown, two counted",
			text: "Error: one\n\nError: two\n\nError: three\n\nError: four\n\nError: five\n",
			want: "The plan of tst's stack failed: `one`; `two`; `three`; and 2 more (build b1 has the log).",
		},
		{
			name: "an indented Error line and an empty one",
			text: "  Error: indented\nError: \n",
			want: "The plan of tst's stack failed: `indented` (build b1 has the log).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := failedPlanLine("tst", tt.text, "b1"); got != tt.want {
				t.Errorf("failedPlanLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
