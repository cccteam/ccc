package condition

import (
	"slices"
	"testing"
)

// TestClassification pins the lexical classification the load-time and
// deploy-time validations build on: row-free-ness, and the referenced
// binding, subject-set, and subject-value names.
func TestClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		source            string
		wantRowFree       bool
		wantBindings      []string
		wantSubjectSets   []string
		wantSubjectValues []string
		wantUsesNow       bool
		wantUsesPostImage bool
		wantComparesAttrs bool
	}{
		{
			name:        "environment window is row-free",
			source:      "now < '2027-03-01T00:00:00Z' AND now >= '2027-01-01T00:00:00Z'",
			wantRowFree: true,
			wantUsesNow: true,
		},
		{
			name:            "subject-set membership references a row",
			source:          "crew IN subject.crews",
			wantBindings:    []string{"crew"},
			wantSubjectSets: []string{"crews"},
		},
		{
			name:         "plain comparison references a row",
			source:       "state = 'open'",
			wantBindings: []string{"state"},
		},
		{
			name:         "null test references a row",
			source:       "assignee IS NULL",
			wantBindings: []string{"assignee"},
		},
		{
			name:              "post-image collapses to the binding name",
			source:            "assignee IS NULL AND new.assignee = subject",
			wantBindings:      []string{"assignee"},
			wantUsesPostImage: true,
		},
		{
			name:              "threshold pattern references the subject value",
			source:            "new.estimatedCost <= subject.approvalLimit",
			wantBindings:      []string{"estimatedCost"},
			wantSubjectValues: []string{"approvalLimit"},
			wantUsesPostImage: true,
		},
		{
			name:              "old-vs-new references both sides' bindings",
			source:            "new.priority <= priority AND state = 'draft'",
			wantBindings:      []string{"priority", "state"},
			wantUsesPostImage: true,
			wantComparesAttrs: true,
		},
		{
			name:         "bindings dedupe and sort",
			source:       "b = 1 AND a = 2 AND new.b = 3 AND contractEnd > now",
			wantBindings: []string{"a", "b", "contractEnd"},
			wantUsesNow:  true,

			wantUsesPostImage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}

			if got := RowFree(expr); got != tt.wantRowFree {
				t.Errorf("RowFree() = %v, want %v", got, tt.wantRowFree)
			}
			if got := Bindings(expr); !slices.Equal(got, tt.wantBindings) {
				t.Errorf("Bindings() = %v, want %v", got, tt.wantBindings)
			}
			if got := SubjectSets(expr); !slices.Equal(got, tt.wantSubjectSets) {
				t.Errorf("SubjectSets() = %v, want %v", got, tt.wantSubjectSets)
			}
			if got := SubjectValues(expr); !slices.Equal(got, tt.wantSubjectValues) {
				t.Errorf("SubjectValues() = %v, want %v", got, tt.wantSubjectValues)
			}
			if got := ComparesAttributes(expr); got != tt.wantComparesAttrs {
				t.Errorf("ComparesAttributes() = %v, want %v", got, tt.wantComparesAttrs)
			}
			if got := UsesNow(expr); got != tt.wantUsesNow {
				t.Errorf("UsesNow() = %v, want %v", got, tt.wantUsesNow)
			}
			if got := UsesPostImage(expr); got != tt.wantUsesPostImage {
				t.Errorf("UsesPostImage() = %v, want %v", got, tt.wantUsesPostImage)
			}
		})
	}
}
