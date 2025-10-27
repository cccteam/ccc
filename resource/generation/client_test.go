package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_formatInterfaceTypes(t *testing.T) {
	t.Parallel()

	type args struct {
		types []string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				types: []string{},
			},
			want: "",
		},
		{
			name: "One type",
			args: args{
				types: []string{"Resource1"},
			},
			want: "\tResource1",
		},
		{
			name: "many type",
			args: args{
				types: []string{
					"Resource1",
					"MyResource1",
					"YourResource1",
					"Resource2",
					"Resource3",
					"Resource4",
					"Resource5",
					"Resource6",
					"Resource7",
					"Resource8",
					"Resource9",
				},
			},
			want: "\tResource1 | MyResource1 | YourResource1 | Resource2 | Resource3 | Resource4 | Resource5 | Resource6 | \n\tResource7 | Resource8 | Resource9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatInterfaceTypes(tt.args.types)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("formatResourceInterfaceTypes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_client_sanitizeEnumIdentifier(t *testing.T) {
	t.Parallel()

	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				name: "",
			},
			want: "",
		},
		{
			name: "simple",
			args: args{
				name: "test",
			},
			want: "Test",
		},
		{
			name: "with leading numbers",
			args: args{
				name: "123test",
			},
			want: "N123Test",
		},
		{
			name: "with punctuation",
			args: args{
				name: "test, test",
			},
			want: "TestTest",
		},
		{
			name: "with space",
			args: args{
				name: "test test",
			},
			want: "TestTest",
		},
		{
			name: "with space and punctuation",
			args: args{
				name: "test test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "with number",
			args: args{
				name: "test1",
			},
			want: "Test1",
		},
		{
			name: "with number and punctuation",
			args: args{
				name: "test1.test",
			},
			want: "Test1Test",
		},
		{
			name: "with number and space",
			args: args{
				name: "test 1",
			},
			want: "Test1",
		},
		{
			name: "with number, space and punctuation",
			args: args{
				name: "test 1.test",
			},
			want: "Test1Test",
		},
		{
			name: "with hyphen",
			args: args{
				name: "test-test",
			},
			want: "TestTest",
		},
		{
			name: "with hyphen and punctuation",
			args: args{
				name: "test-test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "with hyphen and space",
			args: args{
				name: "test- test",
			},
			want: "TestTest",
		},
		{
			name: "with hyphen, space and punctuation",
			args: args{
				name: "test- test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "Bankruptcy (Chapter 12 or 13)",
			args: args{
				name: "Bankruptcy (Chapter 12 or 13)",
			},
			want: "BankruptcyChapter12Or13",
		},
		{
			name: "Defaulted, Then Bankrupt, Active, Chapter 13",
			args: args{
				name: "Defaulted, Then Bankrupt, Active, Chapter 13",
			},
			want: "DefaultedThenBankruptActiveChapter13",
		},
		{
			name: "Borrower's Bankrupt",
			args: args{
				name: "Borrower's Bankrupt",
			},
			want: "BorrowersBankrupt",
		},
		{
			name: "8-10",
			args: args{
				name: "8-10",
			},
			want: "N8N10",
		},
		{
			name: "8_10",
			args: args{
				name: "8_10",
			},
			want: "N8N10",
		},
		{
			name: "8 10",
			args: args{
				name: "8 10",
			},
			want: "N8N10",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizeEnumIdentifier(tt.args.name); got != tt.want {
				t.Errorf("sanitizeEnumIdentifier() = %v, want %v, from %s", got, tt.want, tt.args.name)
			}
		})
	}
}
