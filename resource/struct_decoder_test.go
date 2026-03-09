package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

			decoder, err := NewStructDecoder[request]()
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

func TestNewStructDecoder_Error(t *testing.T) {
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

			_, err := NewStructDecoder[request]()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDecoder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
