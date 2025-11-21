package securehash

import (
	"reflect"
	"testing"
)

func Test_parseUint32(t *testing.T) {
	t.Parallel()

	type args struct {
		s rune
		b []byte
	}
	tests := []struct {
		name          string
		args          args
		wantU32       uint32
		wantRemaining []byte
		wantErr       bool
	}{
		{
			name: "success sep",
			args: args{
				s: sep,
				b: []byte("12$8$4$bXktc2FsdA==.bXkta2V5"),
			},
			wantU32:       12,
			wantRemaining: []byte("8$4$bXktc2FsdA==.bXkta2V5"),
			wantErr:       false,
		},
		{
			name: "error, empty input. sep",
			args: args{
				s: sep,
				b: []byte(""),
			},
			wantErr: true,
		},
		{
			name: "error, empty input. eol",
			args: args{
				s: eol,
				b: []byte(""),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotU32, gotRemaining, err := parseUint32(tt.args.s, tt.args.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseUint32() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotU32 != tt.wantU32 {
				t.Errorf("parseUint32() gotU32 = %v, want %v", gotU32, tt.wantU32)
			}
			if !reflect.DeepEqual(gotRemaining, tt.wantRemaining) {
				t.Errorf("parseUint32() gotRemaining = %v, want %v", gotRemaining, tt.wantRemaining)
			}
		})
	}
}

func Test_parseUint8(t *testing.T) {
	t.Parallel()

	type args struct {
		s rune
		b []byte
	}
	tests := []struct {
		name          string
		args          args
		wantU8        uint8
		wantRemaining []byte
		wantErr       bool
	}{
		{
			name: "success sep",
			args: args{
				s: sep,
				b: []byte("4$bXktc2FsdA==.bXkta2V5"),
			},
			wantU8:        4,
			wantRemaining: []byte("bXktc2FsdA==.bXkta2V5"),
			wantErr:       false,
		},
		{
			name: "error, empty input. sep",
			args: args{
				s: sep,
				b: []byte(""),
			},
			wantErr: true,
		},
		{
			name: "error, empty input. eol",
			args: args{
				s: eol,
				b: []byte(""),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotU8, gotRemaining, err := parseUint8(tt.args.s, tt.args.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseUint8() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotU8 != tt.wantU8 {
				t.Errorf("parseUint8() gotU8 = %v, want %v", gotU8, tt.wantU8)
			}
			if !reflect.DeepEqual(gotRemaining, tt.wantRemaining) {
				t.Errorf("parseUint8() gotRemaining = %v, want %v", gotRemaining, tt.wantRemaining)
			}
		})
	}
}

func Test_parseBase64(t *testing.T) {
	t.Parallel()

	type args struct {
		s rune
		b []byte
	}
	tests := []struct {
		name          string
		args          args
		wantVal       []byte
		wantRemainder []byte
		wantErr       bool
	}{
		{
			name: "success dot",
			args: args{
				s: dot,
				b: []byte("bXktc2FsdA==.bXkta2V5"),
			},
			wantVal:       []byte("my-salt"),
			wantRemainder: []byte("bXkta2V5"),
			wantErr:       false,
		},
		{
			name: "error, empty input. dot",
			args: args{
				s: dot,
				b: []byte(""),
			},
			wantErr: true,
		},
		{
			name: "success eol",
			args: args{
				s: eol,
				b: []byte("bXkta2V5"),
			},
			wantVal:       []byte("my-key"),
			wantRemainder: []byte(""),
			wantErr:       false,
		},
		{
			name: "error, empty input. eol",
			args: args{
				s: eol,
				b: []byte(""),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotVal, gotRemainder, err := parseBase64(tt.args.s, tt.args.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseBase64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotVal, tt.wantVal) {
				t.Errorf("parseBase64() gotVal = %v, want %v", gotVal, tt.wantVal)
			}
			if !reflect.DeepEqual(gotRemainder, tt.wantRemainder) {
				t.Errorf("parseBase64() gotRemainder = %v, want %v", gotRemainder, tt.wantRemainder)
			}
		})
	}
}
