// Attribution: robbiet480's sns.go repo (https://github.com/robbiet480/go.sns) was used as the starting point for this file under the MIT License.

package ccc

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"time"

	"github.com/go-playground/errors/v5"
)

type SNSPayload struct {
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

var snsHostPattern = regexp.MustCompile(`^sns\.[a-zA-Z0-9\-]{3,}\.amazonaws\.com(\.cn)?$`)

// VerifyAuthenticity verifies that the payload is authentic (i.e., that it was sent by AWS SNS)
func (s *SNSPayload) VerifyAuthenticity(ctx context.Context) error {
	payloadSignature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return errors.Wrap(err, "base64.StdEncoding.DecodeString()")
	}

	certURL, err := url.Parse(s.SigningCertURL)
	if err != nil {
		return errors.Wrap(err, "url.Parse()")
	}

	if certURL.Scheme != "https" {
		return errors.New("signing certificate URL is not https")
	}

	if !snsHostPattern.MatchString(certURL.Host) {
		return errors.New("signing certificate URL does not match SNS host pattern")
	}

	certReq, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL.String(), http.NoBody)
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}

	httpClient := &http.Client{Timeout: time.Second * 10}
	certResp, err := httpClient.Do(certReq)
	if err != nil {
		return errors.Wrap(err, "http.Get()")
	}
	defer certResp.Body.Close()

	encodedCert, err := io.ReadAll(certResp.Body)
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}

	decodedCert, _ := pem.Decode(encodedCert)
	if decodedCert == nil {
		return errors.New("the decoded signing certificate is empty")
	}

	parsedCert, err := x509.ParseCertificate(decodedCert.Bytes)
	if err != nil {
		return errors.Wrap(err, "x509.ParseCertificate()")
	}

	if err := parsedCert.CheckSignature(s.signatureAlgorithm(), s.signaturePayload(), payloadSignature); err != nil {
		return errors.Wrap(err, "parsedCert.CheckSignature()")
	}

	return nil
}

func (s *SNSPayload) signaturePayload() []byte {
	var signature bytes.Buffer
	reflectedPayload := reflect.Indirect(reflect.ValueOf(s))
	for _, key := range snsSignatureKeys() {
		field := reflectedPayload.FieldByName(key)
		value := field.String()
		if field.IsValid() && value != "" {
			signature.WriteString(key + "\n")
			signature.WriteString(value + "\n")
		}
	}

	return signature.Bytes()
}

func (s *SNSPayload) signatureAlgorithm() x509.SignatureAlgorithm {
	if s.SignatureVersion == "2" {
		return x509.SHA256WithRSA
	}

	return x509.SHA1WithRSA
}

func snsSignatureKeys() []string {
	return []string{"Message", "MessageId", "Subject", "SubscribeURL", "Timestamp", "Token", "TopicArn", "Type"}
}
