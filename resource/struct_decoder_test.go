package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type validateMock struct {
	validateFunc        func(s any) error
	validatePartialFunc func(s any, fields ...string) error
}

func (v *validateMock) Struct(s any) error {
	return v.validateFunc(s)
}

func (v *validateMock) StructPartial(s any, fields ...string) error {
	return v.validatePartialFunc(s, fields...)
}

func TestDecoder_Decode(t *testing.T) {
	t.Parallel()

	type args struct {
		body          string
		validatorFunc ValidatorFunc
	}
	tests := []struct {
		name             string
		args             args
		wantDecodeErr    bool
		wantValidatorErr bool
	}{
		{
			name: "successfully decodes the request",
			args: args{
				body: `{"Name":"Zach"}`,
				validatorFunc: &validateMock{
					validateFunc: func(_ any) error {
						return nil
					},
				},
			},
		},
		{
			name: "Fails on decoding the request",
			args: args{
				body: "this is a bad json req body",
			},
			wantDecodeErr: true,
		},
		{
			name:          "null body",
			args:          args{body: `null`},
			wantDecodeErr: true,
		},
		{
			name:          "scalar body",
			args:          args{body: `true`},
			wantDecodeErr: true,
		},
		{
			name:          "empty body",
			args:          args{body: ``},
			wantDecodeErr: true,
		},
		{
			name:          "array body",
			args:          args{body: `["Zach"]`},
			wantDecodeErr: true,
		},
		{
			name:          "trailing data after the object",
			args:          args{body: `{"Name":"Zach"} {"Name":"Zach"}`},
			wantDecodeErr: true,
		},
		{
			name:          "null into a value field",
			args:          args{body: `{"Name":null}`},
			wantDecodeErr: true,
		},
		{
			name: "fails to validate the request",
			args: args{
				body: `{"Name":"Zach"}`,
				validatorFunc: &validateMock{
					validateFunc: func(_ any) error {
						return errors.New("Failed to validate the request")
					},
				},
			},
			wantValidatorErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type request struct {
				Name string
			}

			decoder, err := newStructDecoder[request]()
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			ctx := context.Background()
			r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/test", strings.NewReader(tt.args.body))
			if _, err := decoder.Decode(r); (err != nil) != tt.wantDecodeErr {
				t.Fatalf("Decoder.DecodeRequest() error = %v, wantErr %v", err, tt.wantDecodeErr)
			}

			if tt.wantDecodeErr {
				return
			}

			decoder = decoder.WithValidator(tt.args.validatorFunc)

			r = httptest.NewRequestWithContext(ctx, http.MethodGet, "/test", strings.NewReader(tt.args.body))
			if _, err := decoder.Decode(r); (err != nil) != tt.wantValidatorErr {
				t.Errorf("Decoder.DecodeRequest() error = %v, wantErr %v", err, tt.wantValidatorErr)
			}
		})
	}
}

func Test_newStructDecoder_Error(t *testing.T) {
	t.Parallel()

	type args struct {
		body string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "successfully decodes the request",
			args: args{
				body: `{"Name":"Zach"}`,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type request struct {
				Name string `json:"name"`
				NAME string
			}

			_, err := newStructDecoder[request]()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDecoder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStructDecoder_Decode_bodyLimit pins the decoder's side of a body limit: a body that
// runs past the limit the route carries answers 413 naming the limit, and one within it
// decodes as before.
func TestStructDecoder_Decode_bodyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		limit       int64
		body        string
		wantCode    int
		wantMessage string
	}{
		{name: "a body within the limit decodes", limit: 64, body: `{"Name":"Zach"}`},
		{name: "a body over the limit answers 413 naming the limit", limit: 8, body: `{"Name":"Zach"}`, wantCode: http.StatusRequestEntityTooLarge, wantMessage: "the request body exceeds the maximum of 8 bytes"},
		{name: "a limit in kibibytes is named in kibibytes", limit: 1024, body: `{"Name":"` + strings.Repeat("Z", 1024) + `"}`, wantCode: http.StatusRequestEntityTooLarge, wantMessage: "the request body exceeds the maximum of 1KB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type request struct {
				Name string
			}
			decoder, err := newStructDecoder[request]()
			if err != nil {
				t.Fatalf("newStructDecoder() error = %v", err)
			}

			ctx := context.Background()
			rr := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/test", strings.NewReader(tt.body))
			r.Body = http.MaxBytesReader(rr, r.Body, tt.limit)

			_, err = decoder.Decode(r)
			if (err != nil) != (tt.wantCode != 0) {
				t.Fatalf("structDecoder.Decode() error = %v, wantCode %d", err, tt.wantCode)
			}
			if err == nil {
				return
			}
			_ = httpio.NewEncoder(rr).ClientMessage(ctx, err)
			if rr.Code != tt.wantCode {
				t.Errorf("Encoder.ClientMessage() code = %d, want %d", rr.Code, tt.wantCode)
			}
			if got := httpio.Message(err); got != tt.wantMessage {
				t.Errorf("httpio.Message() = %q, want %q", got, tt.wantMessage)
			}
		})
	}
}
