// Attribution: robbiet480's sns.go repo (https://github.com/robbiet480/go.sns) was used as the starting point for this file under the MIT License.

// Package sns provides AWS SNS related functionality.
package sns

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/go-playground/errors/v5"
)

var hostPattern = regexp.MustCompile(`^sns\.[a-zA-Z0-9\-]{3,}\.amazonaws\.com(\.cn)?$`)

type Payload struct {
	Message          string `json:"Message"`
	MessageID        string `json:"MessageId"`
	Signature        string `json:"Signature"`
	SignatureVersion string `json:"SignatureVersion"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL"`
	Subject          string `json:"Subject"`
	Timestamp        string `json:"Timestamp"`
	Token            string `json:"Token"`
	TopicArn         string `json:"TopicArn"`
	Type             string `json:"Type"`
	UnsubscribeURL   string `json:"UnsubscribeURL"`
}

type Client struct{}

func New() *Client {
	return &Client{}
}

// VerifyAuthenticity verifies that the body of the request is authentic (i.e., that it was sent by AWS SNS) and returns the payload.
func (c *Client) VerifyAuthenticity(ctx context.Context, reqBody io.Reader) (*Payload, error) {
	var payload Payload

	if err := json.NewDecoder(reqBody).Decode(&payload); err != nil {
		return &payload, errors.Wrap(err, "json.NewDecoder().Decode()")
	}

	payloadSignature, err := base64.StdEncoding.DecodeString(payload.Signature)
	if err != nil {
		return &payload, errors.Wrap(err, "base64.StdEncoding.DecodeString()")
	}

	certURL, err := url.Parse(payload.SigningCertURL)
	if err != nil {
		return &payload, errors.Wrap(err, "url.Parse()")
	}

	if certURL.Scheme != "https" {
		return &payload, errors.New("signing certificate URL is not https")
	}

	if !hostPattern.MatchString(certURL.Host) {
		return &payload, errors.New("signing certificate URL does not match SNS host pattern")
	}

	certReq, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL.String(), http.NoBody)
	if err != nil {
		return &payload, errors.Wrap(err, "http.NewRequestWithContext()")
	}

	httpClient := &http.Client{Timeout: time.Second * 10}
	certResp, err := httpClient.Do(certReq)
	if err != nil {
		return &payload, errors.Wrap(err, "http.Get()")
	}
	defer certResp.Body.Close()

	encodedCert, err := io.ReadAll(certResp.Body)
	if err != nil {
		return &payload, errors.Wrap(err, "io.ReadAll()")
	}

	decodedCert, _ := pem.Decode(encodedCert)
	if decodedCert == nil {
		return &payload, errors.New("the decoded signing certificate is empty")
	}

	parsedCert, err := x509.ParseCertificate(decodedCert.Bytes)
	if err != nil {
		return &payload, errors.Wrap(err, "x509.ParseCertificate()")
	}

	if err := parsedCert.CheckSignature(payload.signatureAlgorithm(), payload.signaturePayload(), payloadSignature); err != nil {
		return &payload, errors.Wrap(err, "parsedCert.CheckSignature()")
	}

	return &payload, nil
}

func (p *Payload) signaturePayload() []byte {
	var signature bytes.Buffer

	signatureFields := []struct {
		key   string
		value string
	}{
		{"Message", p.Message},
		{"MessageId", p.MessageID},
		{"Subject", p.Subject},
		{"SubscribeURL", p.SubscribeURL},
		{"Timestamp", p.Timestamp},
		{"Token", p.Token},
		{"TopicArn", p.TopicArn},
		{"Type", p.Type},
	}

	for _, s := range signatureFields {
		if s.value != "" {
			signature.WriteString(fmt.Sprintf("%s\n%s\n", s.key, s.value))
		}
	}

	return signature.Bytes()
}

func (p *Payload) signatureAlgorithm() x509.SignatureAlgorithm {
	if p.SignatureVersion == "2" {
		return x509.SHA256WithRSA
	}

	return x509.SHA1WithRSA
}
