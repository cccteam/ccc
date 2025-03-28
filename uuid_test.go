package ccc

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/google/go-cmp/cmp"
)

func TestMust_string(t *testing.T) {
	t.Parallel()

	type args struct {
		value string
		err   error
	}
	tests := []struct {
		name      string
		args      args
		want      string
		wantPanic bool
	}{
		{
			name: "No error",
			args: args{value: "test", err: nil},
			want: "test",
		},
		{
			name:      "With error",
			args:      args{value: "test", err: errors.New("test")},
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); (r != nil) != tt.wantPanic {
					t.Errorf("Must() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			if got := Must(tt.args.value, tt.args.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Must() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantVersion byte
		wantErr     bool
	}{
		{
			name:        "New UUID",
			wantVersion: uuid.V4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewUUID()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewUUID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if v := got.Version(); v != tt.wantVersion {
				t.Errorf("UUID.Version() = %v, wantVersion %v", v, tt.wantVersion)
			}
			if got.IsNil() {
				t.Error("UUID.IsNil() = true, want valid UUID", got)
			}
		})
	}
}

func TestUUIDFromString(t *testing.T) {
	t.Parallel()

	type args struct {
		s string
	}
	tests := []struct {
		name        string
		args        args
		want        UUID
		wantVersion byte
		wantErr     bool
	}{
		{
			name:        "Valid UUID",
			args:        args{s: "4192bff0-e1e0-43ce-a4db-912808c32493"},
			want:        UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")},
			wantVersion: uuid.V4,
		},
		{
			name:    "InValid UUID",
			args:    args{s: "4192bff0-e1e0-43ce-a4db-912808c3249x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := UUIDFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("UUIDFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("UUIDFromString() mismatch (-want +got):\n%s", diff)
			}
			if tt.wantErr {
				return
			}
			if v := got.Version(); v != tt.wantVersion {
				t.Errorf("UUID.Version() = %v, wantVersion %v", v, tt.wantVersion)
			}
		})
	}
}

func TestUUID_DecodeSpanner(t *testing.T) {
	t.Parallel()

	type args struct {
		val any
	}
	tests := []struct {
		name    string
		args    args
		want    UUID
		wantErr bool
	}{
		{
			name:    "Successful decode",
			args:    args{val: "4192bff0-e1e0-43ce-a4db-912808c32493"},
			want:    UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			args:    args{val: []byte("4192bff0-e1e0-43ce-a4db-912808c32493")},
			wantErr: true,
		},
		{
			name:    "Invalid UUID",
			args:    args{val: "4192bff0-e1e0-43ce-a4db-912808c32xyz"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := &UUID{}
			if err := u.DecodeSpanner(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("UUID.DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *u); diff != "" {
				t.Errorf("UUID.DecodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUUID_EncodeSpanner(t *testing.T) {
	t.Parallel()

	type fields struct {
		UUID uuid.UUID
	}
	tests := []struct {
		name    string
		fields  fields
		want    any
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")},
			want:    "4192bff0-e1e0-43ce-a4db-912808c32493",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := UUID{
				UUID: tt.fields.UUID,
			}
			got, err := u.EncodeSpanner()
			if (err != nil) != tt.wantErr {
				t.Errorf("UUID.EncodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("UUID.EncodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUUID_UnmarshalText(t *testing.T) {
	t.Parallel()

	type args struct {
		text []byte
	}
	tests := []struct {
		name    string
		args    args
		want    UUID
		wantErr bool
	}{
		{
			name:    "Successful decode",
			args:    args{text: []byte("4192bff0-e1e0-43ce-a4db-912808c32493")},
			want:    UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")},
			wantErr: false,
		},
		{
			name:    "Invalid UUID",
			args:    args{text: []byte("4192bff0-e1e0-43ce-a4db-912808c32xyz")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := &UUID{}
			if err := u.UnmarshalText(tt.args.text); (err != nil) != tt.wantErr {
				t.Errorf("UUID.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *u); diff != "" {
				t.Errorf("UUID.UnmarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewNullUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantVersion byte
		wantErr     bool
	}{
		{
			name:        "New UUID",
			wantVersion: uuid.V4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewNullUUID()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewNullUUID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if v := got.Version(); v != tt.wantVersion {
				t.Errorf("NullUUID.Version() = %v, wantVersion %v", v, tt.wantVersion)
			}
			if got.IsNil() {
				t.Error("NullUUID.IsNil() = true, want valid UUID", got)
			}
		})
	}
}

func TestNullUUIDFromString(t *testing.T) {
	t.Parallel()

	type args struct {
		s string
	}
	tests := []struct {
		name        string
		args        args
		want        NullUUID
		wantVersion byte
		wantErr     bool
	}{
		{
			name:        "Valid UUID",
			args:        args{s: "4192bff0-e1e0-43ce-a4db-912808c32493"},
			want:        NullUUID{UUID: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}, Valid: true},
			wantVersion: uuid.V4,
		},
		{
			name:    "InValid UUID",
			args:    args{s: "4192bff0-e1e0-43ce-a4db-912808c3249x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NullUUIDFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("NullUUIDFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullUUIDFromString() mismatch (-want +got):\n%s", diff)
			}
			if tt.wantErr {
				return
			}
			if v := got.Version(); v != tt.wantVersion {
				t.Errorf("NullUUID.Version() = %v, wantVersion %v", v, tt.wantVersion)
			}
		})
	}
}

func TestNullUUIDFromUUID(t *testing.T) {
	t.Parallel()

	type args struct {
		u UUID
	}
	tests := []struct {
		name string
		args args
		want NullUUID
	}{
		{
			name: "Valid UUID",
			args: args{u: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}},
			want: NullUUID{UUID: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}, Valid: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NullUUIDFromUUID(tt.args.u)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullUUIDFromUUID() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullUUID_DecodeSpanner(t *testing.T) {
	t.Parallel()

	type args struct {
		val any
	}
	tests := []struct {
		name    string
		args    args
		want    NullUUID
		wantErr bool
	}{
		{
			name:    "Successful decode",
			args:    args{val: "4192bff0-e1e0-43ce-a4db-912808c32493"},
			want:    NullUUID{UUID: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful decode nil *string",
			args:    args{val: Ptr("4192bff0-e1e0-43ce-a4db-912808c32493")},
			want:    NullUUID{UUID: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Successful decode *string",
			args:    args{val: (*string)(nil)},
			want:    NullUUID{Valid: false},
			wantErr: false,
		},
		{
			name:    "Successful decode nil ",
			args:    args{val: nil},
			want:    NullUUID{Valid: false},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			args:    args{val: []byte("4192bff0-e1e0-43ce-a4db-912808c32493")},
			wantErr: true,
		},
		{
			name:    "Invalid UUID",
			args:    args{val: "4192bff0-e1e0-43ce-a4db-912808c32xyz"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := &NullUUID{}
			if err := u.DecodeSpanner(tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("NullUUID.DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *u); diff != "" {
				t.Errorf("NullUUID.DecodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullUUID_EncodeSpanner(t *testing.T) {
	t.Parallel()

	type fields struct {
		UUID  uuid.UUID
		Valid bool
	}
	tests := []struct {
		name    string
		fields  fields
		want    any
		wantErr bool
	}{
		{
			name:    "Successful Encode",
			fields:  fields{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493"), Valid: true},
			want:    "4192bff0-e1e0-43ce-a4db-912808c32493",
			wantErr: false,
		},
		{
			name:    "Successful Null Encode",
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := NullUUID{
				UUID:  UUID{UUID: tt.fields.UUID},
				Valid: tt.fields.Valid,
			}
			got, err := u.EncodeSpanner()
			if (err != nil) != tt.wantErr {
				t.Errorf("NullUUID.EncodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullUUID.EncodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullUUID_UnmarshalText(t *testing.T) {
	t.Parallel()

	type args struct {
		text []byte
	}
	tests := []struct {
		name    string
		args    args
		want    NullUUID
		wantErr bool
	}{
		{
			name:    "Successful decode",
			args:    args{text: []byte("4192bff0-e1e0-43ce-a4db-912808c32493")},
			want:    NullUUID{UUID: UUID{UUID: uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493")}, Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid UUID",
			args:    args{text: []byte("4192bff0-e1e0-43ce-a4db-912808c32xyz")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := &NullUUID{}
			if err := u.UnmarshalText(tt.args.text); (err != nil) != tt.wantErr {
				t.Errorf("NullUUID.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, *u); diff != "" {
				t.Errorf("NullUUID.UnmarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullUUID_IsNil(t *testing.T) {
	t.Parallel()

	type fields struct {
		UUID  uuid.UUID
		Valid bool
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Not Null",
			fields: fields{
				UUID:  uuid.FromStringOrNil("4192bff0-e1e0-43ce-a4db-912808c32493"),
				Valid: true,
			},
			want: false,
		},
		{
			name: "Not Null",
			fields: fields{
				Valid: false,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := NullUUID{
				UUID:  UUID{UUID: tt.fields.UUID},
				Valid: tt.fields.Valid,
			}
			if got := u.IsNil(); got != tt.want {
				t.Errorf("NullUUID.IsNil() = %v, want %v", got, tt.want)
			}
		})
	}
}
