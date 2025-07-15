package ccc

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewDuration(t *testing.T) {
	t.Parallel()

	type args struct {
		d time.Duration
	}
	tests := []struct {
		name string
		args args
		want Duration
	}{
		{
			name: "new from time.Duration",
			args: args{d: 5 * time.Second},
			want: Duration{Duration: 5 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NewDuration(tt.args.d); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewDurationFromString(t *testing.T) {
	t.Parallel()

	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    Duration
		wantErr bool
	}{
		{
			name: "success parse",
			args: args{s: "10h3s"},
			want: Duration{Duration: Must(time.ParseDuration("10h3s"))},
		},
		{
			name: "success parse with spaces",
			args: args{s: "10h 3s"},
			want: Duration{Duration: Must(time.ParseDuration("10h3s"))},
		},
		{
			name:    "failure to parse",
			args:    args{s: "10h 3x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewDurationFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDurationFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NewDurationFromString() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_UnmarshalText(t *testing.T) {
	t.Parallel()

	type args struct {
		val []byte
	}
	tests := []struct {
		name    string
		args    args
		want    Duration
		wantErr bool
	}{
		{
			name:    "Successful Unmarshal",
			args:    args{val: []byte("10m4s")},
			want:    Duration{Duration: Must(time.ParseDuration("10m4s"))},
			wantErr: false,
		},
		{
			name:    "Invalid Duration",
			args:    args{val: []byte(`"10m3x"`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &Duration{}
			if err := d.UnmarshalText(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("Duration.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("Duration.UnmarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_MarshalText(t *testing.T) {
	t.Parallel()

	type fields struct {
		Duration time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{Duration: Must(time.ParseDuration("10m3s"))},
			want:    []byte("10m3s"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := Duration{
				Duration: tt.fields.Duration,
			}
			got, err := d.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("Duration.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Duration.MarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	type args struct {
		val []byte
	}
	tests := []struct {
		name    string
		args    args
		want    Duration
		wantErr bool
	}{
		{
			name:    "Successful Unmarshal",
			args:    args{val: []byte(`"10m4s"`)},
			want:    Duration{Duration: Must(time.ParseDuration("10m4s"))},
			wantErr: false,
		},
		{
			name:    "Invalid Duration",
			args:    args{val: []byte(`"10m3x"`)},
			wantErr: true,
		},
		{
			name:    "Invalid JSON",
			args:    args{val: []byte(`"10m3s`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &Duration{}
			if err := d.UnmarshalJSON(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("Duration.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("Duration.UnmarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	t.Parallel()

	type fields struct {
		Duration time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{Duration: Must(time.ParseDuration("10m3s"))},
			want:    []byte(`"10m3s"`),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := Duration{
				Duration: tt.fields.Duration,
			}
			got, err := d.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("Duration.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Duration.MarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_DecodeSpanner(t *testing.T) {
	t.Parallel()

	type args struct {
		val any
	}
	tests := []struct {
		name    string
		args    args
		want    Duration
		wantErr bool
	}{
		{
			name:    "Successful decode string",
			args:    args{val: "10m4s"},
			want:    Duration{Duration: Must(time.ParseDuration("10m4s"))},
			wantErr: false,
		},
		{
			name:    "Successful decode []byte",
			args:    args{val: []byte("10m4s")},
			want:    Duration{Duration: Must(time.ParseDuration("10m4s"))},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			args:    args{val: 23},
			wantErr: true,
		},
		{
			name:    "Invalid UUID",
			args:    args{val: "10m3x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &Duration{}
			if err := d.DecodeSpanner(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("Duration.DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("Duration.DecodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuration_EncodeSpanner(t *testing.T) {
	t.Parallel()

	type fields struct {
		Duration time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		want    any
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{Duration: Must(time.ParseDuration("10m3s"))},
			want:    "10m3s",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := Duration{
				Duration: tt.fields.Duration,
			}
			got, err := d.EncodeSpanner()
			if (err != nil) != tt.wantErr {
				t.Errorf("Duration.EncodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Duration.EncodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullNewDuration(t *testing.T) {
	t.Parallel()

	type args struct {
		d time.Duration
	}
	tests := []struct {
		name string
		args args
		want NullDuration
	}{
		{
			name: "new from time.Duration",
			args: args{d: 5 * time.Second},
			want: NullDuration{Duration: Duration{Duration: 5 * time.Second}, Valid: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewNullDuration(tt.args.d)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NewNullDuration() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullNewDurationFromString(t *testing.T) {
	t.Parallel()

	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    NullDuration
		wantErr bool
	}{
		{
			name: "success parse",
			args: args{s: "10h3s"},
			want: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10h3s"))}, Valid: true},
		},
		{
			name: "success parse with spaces",
			args: args{s: "10h 3s"},
			want: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10h3s"))}, Valid: true},
		},
		{
			name:    "failure to parse",
			args:    args{s: "10h 3x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewNullDurationFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDurationFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NewDurationFromString() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_UnmarshalText(t *testing.T) {
	t.Parallel()

	type args struct {
		val []byte
	}
	tests := []struct {
		name    string
		args    args
		want    NullDuration
		wantErr bool
	}{
		{
			name:    "Successful Unmarshal",
			args:    args{val: []byte("10m4s")},
			want:    NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m4s"))}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid Duration",
			args:    args{val: []byte(`"10m3x"`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &NullDuration{}
			if err := d.UnmarshalText(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("NullDuration.UnmarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_MarshalText(t *testing.T) {
	t.Parallel()

	type fields struct {
		NullDuration NullDuration
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name:   "Successful Encode",
			fields: fields{NullDuration: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m3s"))}, Valid: true}},
			want:   []byte("10m3s"),
		},
		{
			name:   "Successful Encode nil Duration",
			fields: fields{NullDuration: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m3s"))}, Valid: false}},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.fields.NullDuration.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullDuration.MarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	type args struct {
		val []byte
	}
	tests := []struct {
		name    string
		args    args
		want    NullDuration
		wantErr bool
	}{
		{
			name:    "Successful Unmarshal",
			args:    args{val: []byte(`"10m4s"`)},
			want:    NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m4s"))}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful Unmarshal nil value",
			args:    args{val: []byte(`null`)},
			want:    NullDuration{},
			wantErr: false,
		},
		{
			name:    "Invalid Duration",
			args:    args{val: []byte(`"10m3x"`)},
			wantErr: true,
		},
		{
			name:    "Invalid JSON",
			args:    args{val: []byte(`"10m3s`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &NullDuration{}
			if err := d.UnmarshalJSON(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("NullDuration.UnmarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_MarshalJSON(t *testing.T) {
	t.Parallel()

	type fields struct {
		NullDuration NullDuration
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{NullDuration: NullDuration{Duration: Duration{Must(time.ParseDuration("10m3s"))}, Valid: true}},
			want:    []byte(`"10m3s"`),
			wantErr: false,
		},
		{
			name:    "Successful Encode nil value",
			fields:  fields{NullDuration: NullDuration{Duration: Duration{Must(time.ParseDuration("10m3s"))}, Valid: false}},
			want:    []byte("null"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.fields.NullDuration.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullDuration.MarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_DecodeSpanner(t *testing.T) {
	t.Parallel()

	type args struct {
		val any
	}
	tests := []struct {
		name    string
		args    args
		want    NullDuration
		wantErr bool
	}{
		{
			name:    "Successful decode string",
			args:    args{val: "10m4s"},
			want:    NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m4s"))}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful decode *string",
			args:    args{val: Ptr("10m4s")},
			want:    NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m4s"))}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful decode []byte",
			args:    args{val: []byte("10m4s")},
			want:    NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m4s"))}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful decode nil",
			args:    args{val: (*string)(nil)},
			want:    NullDuration{},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			args:    args{val: 23},
			wantErr: true,
		},
		{
			name:    "Invalid UUID",
			args:    args{val: "10m3x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &NullDuration{}
			if err := d.DecodeSpanner(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *d); diff != "" {
				t.Errorf("NullDuration.DecodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullDuration_EncodeSpanner(t *testing.T) {
	t.Parallel()

	type fields struct {
		NullDuration NullDuration
	}
	tests := []struct {
		name    string
		fields  fields
		want    any
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{NullDuration: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m3s"))}, Valid: true}},
			want:    "10m3s",
			wantErr: false,
		},
		{
			name:    "Successful Encode nil value",
			fields:  fields{NullDuration: NullDuration{Duration: Duration{Duration: Must(time.ParseDuration("10m3s"))}, Valid: false}},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.fields.NullDuration.EncodeSpanner()
			if (err != nil) != tt.wantErr {
				t.Errorf("NullDuration.EncodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullDuration.EncodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
