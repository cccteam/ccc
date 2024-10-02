package ccc

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

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
		tt := tt
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
		tt := tt
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
	}
	for _, tt := range tests {
		tt := tt
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
		tt := tt
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
		tt := tt
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
		tt := tt
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
