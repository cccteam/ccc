package generation

import "testing"

func Test_typescriptGenerator_typescriptTypeForGoType(t *testing.T) {
	t.Parallel()

	overrides := defaultTypescriptOverrides()
	overrides["contentmanagement.RichTextDocument"] = "CustomTypes.ContentfulDocument"

	g := &typescriptGenerator{
		typescriptOverrides: overrides,
	}

	tests := []struct {
		name   string
		goType string
		want   string
	}{
		{
			name:   "map pointer string values",
			goType: "map[string]*string",
			want:   "Record<string, string>",
		},
		{
			name:   "map custom type values",
			goType: "map[string]contentmanagement.RichTextDocument",
			want:   "Record<string, CustomTypes.ContentfulDocument>",
		},
		{
			name:   "nested maps",
			goType: "map[string]map[string]*int",
			want:   "Record<string, Record<string, number>>",
		},
		{
			name:   "array of maps",
			goType: "[]map[string]bool",
			want:   "Record<string, boolean>[]",
		},
		{
			name:   "fallback",
			goType: "some.unknown.Type",
			want:   "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := g.typescriptTypeForGoType(tt.goType)
			if got != tt.want {
				t.Errorf("typescriptTypeForGoType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseMapType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		typeName  string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "simple",
			typeName:  "map[string]*string",
			wantKey:   "string",
			wantValue: "*string",
			wantOK:    true,
		},
		{
			name:      "nested map value",
			typeName:  "map[string]map[string]int",
			wantKey:   "string",
			wantValue: "map[string]int",
			wantOK:    true,
		},
		{
			name:     "non map",
			typeName: "[]string",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotValue, gotOK := parseMapType(tt.typeName)
			if gotOK != tt.wantOK {
				t.Fatalf("parseMapType() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotKey != tt.wantKey {
				t.Errorf("parseMapType() key = %v, want %v", gotKey, tt.wantKey)
			}
			if gotValue != tt.wantValue {
				t.Errorf("parseMapType() value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}
