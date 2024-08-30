// Attribution: robbiet480's sns.go repo (https://github.com/robbiet480/go.sns) was used as the starting point for this file under the MIT License.

// Package sns provides AWS SNS related functionality.
package sns

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/go-playground/errors/v5"
)

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

var hostPattern = regexp.MustCompile(`^sns\.[a-zA-Z0-9\-]{3,}\.amazonaws\.com(\.cn)?$`)

// VerifyAuthenticity verifies that the payload is authentic (i.e., that it was sent by AWS SNS)
func (p *Payload) VerifyAuthenticity(ctx context.Context) error {
	payloadSignature, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil {
		return errors.Wrap(err, "base64.StdEncoding.DecodeString()")
	}

	certURL, err := url.Parse(p.SigningCertURL)
	if err != nil {
		return errors.Wrap(err, "url.Parse()")
	}

	if certURL.Scheme != "https" {
		return errors.New("signing certificate URL is not https")
	}

	if !hostPattern.MatchString(certURL.Host) {
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

	if err := parsedCert.CheckSignature(p.signatureAlgorithm(), p.signaturePayload(), payloadSignature); err != nil {
		return errors.Wrap(err, "parsedCert.CheckSignature()")
	}

	return nil
}

func (p *Payload) signaturePayload() []byte {
	var signature bytes.Buffer

	signatureMap := map[string]string{
		"Message":      p.Message,
		"MessageId":    p.MessageID,
		"Subject":      p.Subject,
		"SubscribeURL": p.SubscribeURL,
		"Timestamp":    p.Timestamp,
		"Token":        p.Token,
		"TopicArn":     p.TopicArn,
		"Type":         p.Type,
	}

	for key, value := range signatureMap {
		if value != "" {
			signature.WriteString(fmt.Sprintf("%s\n%s\n", key, value))
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
